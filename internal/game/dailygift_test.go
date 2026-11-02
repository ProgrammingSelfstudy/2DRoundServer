package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestDailyGiftTable(t *testing.T) {
	if len(dailyGifts) != 4 {
		t.Fatalf("%d gifts", len(dailyGifts))
	}
	for _, g := range dailyGifts {
		if g.Limit != 3 || len(dailyGiftAward[g.ID]) != 5 {
			t.Fatalf("gift %+v has %d reward rows", g, len(dailyGiftAward[g.ID]))
		}
		// the rows cover every level of the table without a gap
		for lev := int64(1); lev <= 300; lev++ {
			if dailyGiftReward(g.ID, lev) == nil {
				t.Fatalf("gift %d: no reward for level %d", g.ID, lev)
			}
		}
		if dailyGiftReward(g.ID, 301) != nil || dailyGiftReward(g.ID, 0) != nil {
			t.Fatalf("gift %d: a reward outside the levels", g.ID)
		}
	}
	// the 1 yuan gift is the 一元礼包's product and stays there
	if g := dailyGifts[0]; g.ChargeID != oneCentCharge {
		t.Fatalf("first gift %+v", g)
	}
}

func TestDailyGiftPurchases(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.FindOrCreateAccount("symlf", "dg")
	role, err := st.CreateRole(acc.ID, "symlf_1", "礼包人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	chargeClock = func() time.Time { return now }
	defer func() { chargeClock = time.Now }()

	// the 6 yuan gift needs VIP 1: the order is not even opened without it
	if _, err := CreateChargeOrder(st, role, 302, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("VIP 0 opens the 6 yuan gift: %v", err)
	}
	if err := st.SetRoleVIP(role.RID, 1); err != nil {
		t.Fatal(err)
	}
	role, _ = st.RoleByID(role.RID)
	buy := func(id int64) error {
		o, err := CreateChargeOrder(st, role, id, now)
		if err != nil {
			return err
		}
		_, err = ConfirmChargeOrder(st, o.OrderNo, "tester", "")
		return err
	}
	if err := buy(302); err != nil {
		t.Fatal(err)
	}
	// what the panel shows for level 1: 120 diamonds, a hero item, an item and hero exp
	a, _ := st.RoleAssets(role.RID)
	it, _ := st.RoleItems(role.RID)
	if a[3] != 120 || a[22] != 500000 || it[10403] != 1 || it[10002] != 6 {
		t.Fatalf("after the 6 yuan gift: assets %v items %v", a, it)
	}
	if rc, _ := st.RoleCharge(role.RID); rc.TotalCents != 600 || rc.VIPExp != 60 {
		t.Fatalf("paid in: %+v", rc)
	}
	// the 12 yuan gift needs VIP 2
	if _, err := CreateChargeOrder(st, role, 303, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("VIP 1 opens the 12 yuan gift: %v", err)
	}
	// three a day, the fourth is refused, and the next day starts again
	for i := 0; i < 2; i++ {
		if err := buy(302); err != nil {
			t.Fatalf("purchase %d: %v", i+2, err)
		}
	}
	if _, err := CreateChargeOrder(st, role, 302, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("a fourth purchase today: %v", err)
	}
	list, err := dailyGiftList(st, role.RID, now)
	if err != nil || len(list) != 4 || list[1].Int("id") != 202 || list[1].Int("count") != 3 || list[0].Int("count") != 0 {
		t.Fatalf("21006 today: %v %v", list, err)
	}
	now = now.Add(24 * time.Hour)
	if list, _ = dailyGiftList(st, role.RID, now); list[1].Int("count") != 0 {
		t.Fatalf("21006 the next day: %v", list)
	}
	if err := buy(302); err != nil {
		t.Fatalf("the next day: %v", err)
	}
	// the one-key gift needs VIP 3
	if _, err := CreateChargeOrder(st, role, 325, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("VIP 1 opens the one-key gift: %v", err)
	}
	// every product the gifts name is payable once the role may buy it
	for _, id := range []int64{302, 303, 325} {
		if p, ok := ChargeProductByID(id); !ok || !ChargeDeliverable(p) {
			t.Fatalf("product %d is not payable", id)
		}
	}
}

func TestDailyGiftOnTheWire(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("dg2", "礼包人二")
	c.send(21006, proto.Message{})
	r := c.waitFor(21006)
	if len(r.Arr("first_gift")) != 4 {
		t.Fatalf("21006: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 1); err != nil {
		t.Fatal(err)
	}
	fresh, _ := e.st.RoleByID(role.RID)
	o, err := CreateChargeOrder(e.st, fresh, 302, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "tester", ""); err != nil {
		t.Fatal(err)
	}
	// a confirmed order pushes the new count along with the rest of what it changed
	if err := SendChargeState(e.gw.Hub.Online(role.RID), e.st, role.RID); err != nil {
		t.Fatal(err)
	}
	r = c.waitFor(21006)
	counts := map[int64]int64{}
	for _, g := range r.Arr("first_gift") {
		counts[g.Int("id")] = g.Int("count")
	}
	if counts[202] != 1 || counts[203] != 0 {
		t.Fatalf("21006 after a purchase: %v", counts)
	}
}
