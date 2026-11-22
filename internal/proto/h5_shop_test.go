package proto

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func TestH5ShopWireSchema(t *testing.T) {
	schema, err := LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Send map[string]json.RawMessage
		Recv map[string]json.RawMessage
	}
	if err := json.Unmarshal(protocolsJSON, &original); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"60720", "60721"} {
		if original.Send[id] != nil || original.Recv[id] != nil {
			t.Fatalf("H5 command %s overlaps a legacy command", id)
		}
	}
	if fields, ok := schema.Send[60720]; !ok || len(fields) != 0 {
		t.Fatal("60720 must accept an empty catalog request")
	}

	const requestID = "0123456789abcdef0123456789abcdef"
	const requestIDBytes = "00203031323334353637383961626364656630313233343536373839616263646566"
	for _, tc := range []struct {
		name    string
		fields  []Field
		message Message
		wireHex string
	}{
		{
			name:   "catalog reply",
			fields: schema.Recv[60720],
			message: Message{"code": int64(1), "msg": "", "sections": []Message{{
				"name": "Market", "currency_id": int64(1), "currency_name": "Coin", "balance": int64(0x0102030405060708),
				"offers": []Message{{"offer_id": int64(-10152), "item_id": int64(10152), "name": "Key",
					"bundle": int64(1), "price": int64(20000), "period": int64(2), "remaining": int64(2)}},
			}}},
			wireHex: "0100000001" + "00064d61726b6574000000010004436f696e01020304050607080001" +
				"ffffd858000027a800034b65790000000100004e200200000002",
		},
		{
			name:   "purchase request",
			fields: schema.Send[60721],
			message: Message{"request_id": requestID, "offer_id": int64(-10152), "item_id": int64(10152), "currency_id": int64(1),
				"bundle": int64(1), "price": int64(20000), "count": int64(2)},
			wireHex: requestIDBytes + "ffffd858000027a8000000010000000100004e200002",
		},
		{
			name:   "purchase reply",
			fields: schema.Recv[60721],
			message: Message{"request_id": requestID, "code": int64(1), "msg": "", "offer_id": int64(-10152), "item_id": int64(10152),
				"amount": int64(2), "currency_id": int64(1), "cost": int64(40000)},
			wireHex: requestIDBytes + "010000ffffd858000027a800000002000000010000000000009c40",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(tc.wireHex)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Encode(DefaultOptions(), tc.fields, tc.message)
			if err != nil || hex.EncodeToString(got) != tc.wireHex {
				t.Fatalf("encoded %x, want %x, err %v", got, want, err)
			}
			decoded, err := Decode(DefaultOptions(), tc.fields, want)
			if err != nil || !reflect.DeepEqual(decoded, tc.message) {
				t.Fatalf("decoded %v, want %v, err %v", decoded, tc.message, err)
			}
		})
	}
}
