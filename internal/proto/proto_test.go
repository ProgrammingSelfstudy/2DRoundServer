package proto

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	b, err := Frame{Command: 1164, Payload: []byte{1, 2, 3}}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 5, 0x04, 0x8C, 1, 2, 3}
	if !bytes.Equal(b, want) {
		t.Fatalf("got %x want %x", b, want)
	}
	f, err := ReadFrame(bytes.NewReader(b))
	if err != nil || f.Command != 1164 || !bytes.Equal(f.Payload, []byte{1, 2, 3}) {
		t.Fatalf("decode: %+v %v", f, err)
	}
}

func TestFrameLimits(t *testing.T) {
	if _, err := (Frame{Payload: make([]byte, MaxPayload+1)}).Encode(); err == nil {
		t.Fatal("expected oversize error")
	}
	if _, _, _, err := DecodeHeader([]byte{0, 0, 0, 1, 0, 1}); err == nil {
		t.Fatal("body_length<2 must fail")
	}
	if _, _, _, err := DecodeHeader([]byte{0, 0, 0}); err == nil {
		t.Fatal("short header must fail")
	}
}

func TestHandshake(t *testing.T) {
	if err := ReadHandshake(bytes.NewReader([]byte(Handshake))); err != nil {
		t.Fatal(err)
	}
	if err := ReadHandshake(bytes.NewReader([]byte("game_client-----------x"))); err == nil {
		t.Fatal("bad handshake accepted")
	}
	if len(Handshake) != 23 {
		t.Fatal("handshake must be 23 bytes")
	}
}

func TestSchemaHasLoginCommands(t *testing.T) {
	s, err := LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Send) < 1000 || len(s.Recv) < 1000 {
		t.Fatalf("schema too small: %d/%d", len(s.Send), len(s.Recv))
	}
	for _, c := range []uint16{1164, 10191, 10192, 10193} {
		if s.Send[c] == nil && c != 10193 {
			t.Errorf("send %d missing", c)
		}
		if _, ok := s.Recv[c]; !ok {
			t.Errorf("recv %d missing", c)
		}
	}
}

// The codec is round-trip tested for every width/endianness combination so
// that switching Options after the capture is confirmed needs no code change.
func TestCodecRoundTripAllOptions(t *testing.T) {
	s, _ := LoadSchema()
	fields := s.Recv[1164]
	msg := Message{
		"code": 1, "msg": "ok", "least_career": 3,
		"roles": []Message{
			{"rid": 7, "srv_id": "symlf_1", "name": "路飞", "lev": 60, "sex": 1, "career": 2, "face_id": 1001, "is_online": 0},
			{"rid": 8, "srv_id": "symlf_1", "name": "", "lev": 1, "sex": 0, "career": 1, "face_id": 1, "is_online": 1},
		},
	}
	for _, bo := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		for _, sl := range []int{1, 2, 4} {
			for _, al := range []int{1, 2, 4} {
				for _, nt := range []bool{false, true} {
					o := Options{bo, sl, al, nt}
					b, err := Encode(o, fields, msg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := Decode(o, fields, b)
					if err != nil {
						t.Fatalf("%+v: %v", o, err)
					}
					roles := got.Arr("roles")
					if got.Int("code") != 1 || got.Str("msg") != "ok" || len(roles) != 2 ||
						roles[0].Str("name") != "路飞" || roles[0].Int("rid") != 7 || roles[1].Int("is_online") != 1 {
						t.Fatalf("%+v: bad roundtrip %v", o, got)
					}
				}
			}
		}
	}
}

func TestDecodeRejectsTruncatedAndTrailing(t *testing.T) {
	s, _ := LoadSchema()
	o := DefaultOptions()
	b, _ := Encode(o, s.Send[10192], Message{"rid": 5, "srv_id": "a_1"})
	if _, err := Decode(o, s.Send[10192], b[:len(b)-1]); err == nil {
		t.Fatal("truncated accepted")
	}
	if _, err := Decode(o, s.Send[10192], append(b, 0)); err == nil {
		t.Fatal("trailing accepted")
	}
}

func TestSignedIntegers(t *testing.T) {
	f := []Field{{Name: "a", Type: "int16"}, {Name: "b", Type: "int8"}}
	o := DefaultOptions()
	b, _ := Encode(o, f, Message{"a": -2, "b": -1})
	m, err := Decode(o, f, b)
	if err != nil || m.Int("a") != -2 || m.Int("b") != -1 {
		t.Fatalf("%v %v", m, err)
	}
}
