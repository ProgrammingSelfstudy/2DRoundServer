package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestLadderShopExchange(t *testing.T) {
	e := newEnv(t, time.Minute)
	sunday := time.Date(2026, 11, 29, 23, 59, 59, 0, dayZone)
	e.setNow(sunday)
	c, role := enterH5Guild(e, "laddershop", "天梯兑换")
	if err := e.st.SetRoleLevel(role.RID, 55); err != nil {
		t.Fatal(err)
	}
	buy := func(eid, num int64) proto.Message {
		t.Helper()
		c.send(13402, proto.Message{"eid": eid, "num": num})
		r := c.waitFor(13402)
		if r.Int("type") != 30 || r.Int("eid") != eid {
			t.Fatal("exchange receipt", r)
		}
		return r
	}
	skinCount := func() int64 {
		t.Helper()
		c.send(13401, proto.Message{"type": 30})
		r := c.waitFor(13401)
		if r.Int("code") != 1 || r.Int("type") != 30 {
			t.Fatal("shelf", r)
		}
		for _, g := range r.Arr("item_list") {
			if g.Int("item_id") == 30011 {
				ext := g.Arr("ext")
				if len(ext) != 1 || ext[0].Int("key") != 1 {
					t.Fatal("quota", g)
				}
				return ext[0].Int("val")
			}
		}
		return 0
	}
	if buy(30011, 1).Int("code") != 0 || skinCount() != 0 {
		t.Fatal("unfunded exchange consumed quota")
	}
	var points, skin int64 = 350000, 0
	if err := e.st.Exchange(role.RID, nil, nil, map[int64]int64{21: points}, nil); err != nil {
		t.Fatal(err)
	}
	checkWallet := func() {
		t.Helper()
		m := c.waitFor(10302)
		if m.Int("sky_coin") != points || m.Int("skin_debris") != skin {
			t.Fatal("wallet push", m.Int("sky_coin"), m.Int("skin_debris"), points, skin)
		}
		c.waitFor(10501)
	}
	buySkin := func(num, bought int64) {
		t.Helper()
		r := buy(30011, num)
		if r.Int("code") != 1 || r.Arr("ext")[0].Int("val") != bought {
			t.Fatal("skin receipt", r)
		}
		points -= 250 * num
		skin += num
		checkWallet()
		if got := skinCount(); got != bought {
			t.Fatalf("purchased %d, want %d", got, bought)
		}
	}
	buySkin(1, 1)
	if buy(30011, 10).Int("code") != 0 || skinCount() != 1 {
		t.Fatal("oversized order was partially filled")
	}
	buySkin(9, 10)
	if buy(30011, 1).Int("code") != 0 || skinCount() != 10 {
		t.Fatal("eleventh skin fragment was sold")
	}

	// Five hero-fragment bundles, four equipment pieces and one material bundle.
	other := []struct{ item, quantity, price int64 }{
		{27906, 50, 50000}, {28906, 50, 50000}, {26907, 50, 37500}, {25907, 50, 37500}, {24901, 50, 37500},
		{40113, 1, 29000}, {40213, 1, 29000}, {40313, 1, 29000}, {40413, 1, 29000}, {10450, 500, 1250},
	}
	initial, err := e.st.RoleItems(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	for i, g := range other {
		eid := int64(30001 + i)
		if r := buy(eid, 1); r.Int("code") != 1 || len(r.Arr("ext")) != 0 {
			t.Fatalf("offer %d: %v", eid, r)
		}
		points -= g.price
		checkWallet()
		bag, err := e.st.RoleItems(role.RID)
		if err != nil || bag[g.item]-initial[g.item] != g.quantity || bag[29] != initial[29] {
			t.Fatalf("offer %d: bag %v, error %v", eid, bag, err)
		}
	}
	e.setNow(sunday.Add(time.Second))
	if skinCount() != 0 {
		t.Fatal("quota did not reset on Monday at 00:00")
	}
	buySkin(10, 10)
	if buy(30011, 1).Int("code") != 0 {
		t.Fatal("limit did not apply in the new week")
	}
	assets, err := e.st.RoleAssets(role.RID)
	if err != nil || assets[21] != points || assets[29] != skin {
		t.Fatal("persistent wallet", assets, err)
	}
}
