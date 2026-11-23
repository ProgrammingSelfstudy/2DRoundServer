package store

import (
	"errors"
	"testing"
)

func TestPayOfferCanOnlyHaveOneWaitingOrPaidOrder(t *testing.T) {
	st := open(t)
	place := func(no string, product int64) error {
		_, err := st.CreatePayOrder(PayOrder{OrderNo: no, RID: 1, ChargeID: product, AmountCents: 10, OfferID: -1})
		return err
	}
	if err := place("first", 301); err != nil {
		t.Fatal(err)
	}
	if err := place("second", 1705); !errors.Is(err, ErrConflict) {
		t.Fatalf("same offer with different product already waiting: %v", err)
	}
	if err := st.CancelPayOrder("first", "tester", ""); err != nil {
		t.Fatal(err)
	}
	if err := place("second", 1705); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfirmPayOrder("second", "tester", "", 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if status, err := st.PayOfferStatus(1, -1); err != nil || status != PayDone {
		t.Fatalf("paid offer: %d, %v", status, err)
	}
	if err := place("third", 301); !errors.Is(err, ErrConflict) {
		t.Fatalf("same offer already paid: %v", err)
	}
}
