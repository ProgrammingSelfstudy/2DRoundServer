package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/store"
)

// The 快乐币 panel's products cost `gold` yuan, give no diamonds and deliver the table's items.
func TestKlbProducts(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.FindOrCreateAccount("symlf", "klb1")
	role, err := st.CreateRole(acc.ID, "symlf_1", "快乐人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}

	p, ok := KlbProductByPrice(38)
	if !ok || p.ID != 30001 || p.Cents() != 3800 {
		t.Fatalf("the 38 yuan product: %v %+v cents %d", ok, p, p.Cents())
	}
	seen := map[int64]bool{}
	for id := int64(klbFirstID); id <= klbLastID; id++ {
		q, ok := ChargeProductByID(id)
		if !ok || !isKlbProduct(q) || !ChargeDeliverable(q) || q.Cents() != q.Gold*100 || seen[q.Gold] {
			t.Fatalf("product %d: %+v (a price must be used once and every product payable)", id, q)
		}
		seen[q.Gold] = true
		if got, ok := KlbProductByPrice(q.Gold); !ok || got.ID != id {
			t.Fatalf("price %d finds %+v", q.Gold, got)
		}
	}
	if _, ok := KlbProductByPrice(39); ok {
		t.Fatal("a price no product has must not find one")
	}

	now := time.Unix(1_800_000_000, 0)
	o, err := CreateChargeOrder(st, role, 30001, now)
	if err != nil || o.AmountCents != 3800 {
		t.Fatalf("order: %v %+v", err, o)
	}
	if _, err := ConfirmChargeOrder(st, o.OrderNo, "tester", ""); err != nil {
		t.Fatal(err)
	}
	a, _ := st.RoleAssets(role.RID)
	if a[41] != 2_000_000_000 || a[3] != 0 {
		t.Fatalf("assets after 38 yuan: %v (快乐币 20E, no diamonds)", a)
	}
	if rc, _ := st.RoleCharge(role.RID); rc.TotalCents != 3800 || rc.VIPExp != 0 {
		t.Fatalf("paid in: %+v", rc)
	}
	if _, err := ConfirmChargeOrder(st, o.OrderNo, "tester", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a second confirm: %v", err)
	}

	// a gift box is an item, not a currency
	o, err = CreateChargeOrder(st, role, 30002, now)
	if err != nil || o.AmountCents != 9600 {
		t.Fatalf("order 30002: %v %+v", err, o)
	}
	if _, err := ConfirmChargeOrder(st, o.OrderNo, "tester", ""); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.RoleItems(role.RID); it[99911] != 1 {
		t.Fatalf("items after the gift: %v", it)
	}
	if a, _ = st.RoleAssets(role.RID); a[3] != 0 {
		t.Fatalf("a gift gives no diamonds: %v", a)
	}
}

// 10303's klb is a uint32: a bigger ledger value must not fail to encode.
func TestKlbWire(t *testing.T) {
	for _, c := range []struct{ in, want int64 }{{0, 0}, {-5, 0}, {2_000_000_000, 2_000_000_000}, {4_000_000_000, 4_000_000_000}, {6_000_000_000, 4_294_967_295}} {
		if got := klbWire(map[int64]int64{klbLabel: c.in}); got != c.want {
			t.Fatalf("klbWire(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if klbWire(nil) != 0 {
		t.Fatal("no ledger is 0")
	}
}
