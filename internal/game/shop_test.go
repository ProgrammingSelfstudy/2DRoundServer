package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestShopGoodsAreUsable(t *testing.T) {
	for id, g := range shopGoods {
		if _, ok := g.payID(); !ok {
			t.Errorf("good %d: payment %v is unknown", id, g.Pay)
		}
		if !ItemKnown(g.Item) {
			t.Errorf("good %d: item %d is unknown", id, g.Item)
		}
		if g.Price <= 0 || g.Num <= 0 {
			t.Errorf("good %d: price %d num %d", id, g.Price, g.Num)
		}
	}
}

func TestShopLimitWindow(t *testing.T) {
	wed := time.Date(2026, 10, 21, 10, 0, 0, 0, dayZone) // a Wednesday
	if f, to, l := (shopGood{Day: 1}).limitWindow(wed); f != 20261021 || to != 20261021 || l != 1 {
		t.Fatalf("day: %d %d %d", f, to, l)
	}
	if f, to, l := (shopGood{Day: 9999, Week: 2}).limitWindow(wed); f != 20261019 || to != 20261025 || l != 2 {
		t.Fatalf("week (Monday 19th to Sunday 25th): %d %d %d", f, to, l)
	}
	if f, to, l := (shopGood{Month: 3}).limitWindow(wed); f != 20261001 || to != 20261031 || l != 3 {
		t.Fatalf("month: %d %d %d", f, to, l)
	}
	if _, _, l := (shopGood{Day: 9999}).limitWindow(wed); l != 0 {
		t.Fatalf("9999 is unlimited: %d", l)
	}
}

func TestShopBuy(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 21, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	buy := func(eid, num int64) proto.Message {
		c.send(13402, proto.Message{"eid": eid, "num": num})
		return c.recv(13402)
	}
	if r := buy(1031, 1); r.Int("code") != 0 { // no vouchers
		t.Fatalf("poor: %v", r)
	}
	if r := buy(999999, 1); r.Int("code") != 0 {
		t.Fatalf("unknown good: %v", r)
	}
	// 43 is the voucher currency (charge_bi)
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 43, Num: 20000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)

	// good 1031: 5000 vouchers for one 37004, at most one a day
	r := buy(1031, 1)
	if r.Int("code") != 1 || r.Int("eid") != 1031 || r.Int("type") != 1 || r.Arr("ext")[0].Int("val") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	if r := buy(1031, 1); r.Int("code") != 0 {
		t.Fatalf("second one today: %v", r)
	}
	got, _ := e.st.RoleItems(role.RID)
	as, _ := e.st.RoleAssets(role.RID)
	if got[37004] != 1 || as[43] != 15000 {
		t.Fatalf("ledgers: %v %v", got, as)
	}
	// what was bought shows in the shop list
	c.send(13401, proto.Message{"type": 1})
	l := c.recv(13401)
	items := l.Arr("item_list")
	if l.Int("code") != 1 || len(items) != 1 || items[0].Int("item_id") != 1031 || items[0].Arr("ext")[0].Int("val") != 1 {
		t.Fatalf("list: %v", l)
	}
	// next day the daily limit is open again; an unlimited good can be bought several times
	e.setNow(time.Date(2026, 10, 22, 10, 0, 0, 0, dayZone))
	if r := buy(1031, 1); r.Int("code") != 1 {
		t.Fatalf("next day: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	if r := buy(1008, 1); r.Int("code") != 0 { // 20000 vouchers, the role has 10000
		t.Fatalf("too expensive: %v", r)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[43] != 10000 {
		t.Fatalf("a refusal must not take anything: %v", got)
	}
}

func TestVarietyShopListsTheCapturedGoods(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	c.send(13403, proto.Message{"type": 4})
	r := c.recv(13403)
	list := r.Arr("item_list")
	if r.Int("type") != 4 || r.Int("code") != 1 || r.Int("free_count") != 5 || r.Int("num") != 6 || len(list) != 6 {
		t.Fatalf("13403: %v", r)
	}
	if first := list[0]; first.Int("item_id") != 10001 || first.Int("price") != 1000 || first.Int("discount_type") != 5 || first.Int("order") != 1 {
		t.Fatalf("first good: %v", first)
	}
	if last := list[5]; last.Int("item_id") != 10451 || last.Int("pay_type") != 3 || last.Int("price") != 200 {
		t.Fatalf("last good: %v", last)
	}
}

func TestRandomShopsAreEmpty(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	c.send(13403, proto.Message{"type": 2})
	r := c.recv(13403)
	if r.Int("type") != 2 || r.Int("code") != 1 || len(r.Arr("item_list")) != 0 || r.Int("refresh_time") <= e.clk.Load()/1e9 {
		t.Fatalf("13403: %v", r)
	}
	c.send(13405, proto.Message{"type": 9})
	if r := c.recv(13405); r.Int("type") != 9 || r.Int("code") != 0 || r.Str("msg") == "" {
		t.Fatalf("13405: %v", r)
	}
}

// The seer palace shop (type 31): a good without a limit is answered without a bought count
// and with an empty msg, as the original did for eid 31055 (capture 2026-10-21b).
func TestSeerShopBuyAnswer(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 24, Num: 5}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	c.send(13402, proto.Message{"eid": 31055, "num": 1})
	r := c.recv(13402)
	if r.Int("code") != 1 || r.Int("type") != 31 || len(r.Arr("ext")) != 0 || r.Str("msg") != "" {
		t.Fatalf("13402: %v", r)
	}
}

// The tavern shop: a good is bought once for its discounted price; a refresh deals six
// different goods of the pool and spends a free refresh; the refreshes and the first shelf
// come back with the next free-draw day (05:00 China time).
func TestVarietyShopRefreshAndBuy(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 21, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 400}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	total := func(id int64) int64 {
		a, _ := e.st.RoleAssets(role.RID)
		i, _ := e.st.RoleItems(role.RID)
		return a[id] + i[id]
	}
	open := func(cmd uint16) proto.Message {
		c.send(cmd, proto.Message{"type": 4})
		return c.recv(cmd)
	}
	buy := func(order int64) proto.Message {
		c.send(13407, proto.Message{"type": 4, "order": order, "num": 1, "buy_type": 1})
		return c.recv(13407)
	}
	if r := open(13403); r.Int("free_count") != 5 || len(r.Arr("item_list")) != 6 {
		t.Fatalf("open: %v", r)
	}
	// order 2: 300 of item 10001 at the discounted 210 diamonds
	gems, got := total(3), total(10001)
	c.send(13407, proto.Message{"type": 4, "order": 2, "num": 1, "buy_type": 1})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(13407); r.Int("code") != 1 || r.Str("msg") != "" || r.Int("order") != 2 {
		t.Fatalf("buy: %v", r)
	}
	if total(3) != gems-210 || total(10001) != got+300 {
		t.Fatalf("diamonds %d -> %d, item %d -> %d", gems, total(3), got, total(10001))
	}
	if r := open(13403); r.Arr("item_list")[1].Int("has_buy") != 1 || r.Arr("item_list")[0].Int("has_buy") != 0 {
		t.Fatalf("bought flag: %v", r)
	}
	if r := buy(2); r.Int("code") != 0 { // once
		t.Fatalf("twice: %v", r)
	}
	if r := buy(6); r.Int("code") != 0 { // 200 diamonds, the role has 190
		t.Fatalf("poor: %v", r)
	}
	if r := buy(9); r.Int("code") != 0 { // no such order
		t.Fatalf("no order: %v", r)
	}
	// five free refreshes, each a different shelf of six; the sixth is refused
	old := summonRand
	summonRand = func() float64 { return 0.99 }
	t.Cleanup(func() { summonRand = old })
	for left := int64(4); left >= 0; left-- {
		r := open(13405)
		seen := map[int64]bool{}
		for _, g := range r.Arr("item_list") {
			key := g.Int("item_id")*100000000 + g.Int("price")
			if seen[key] || g.Int("has_buy") != 0 {
				t.Fatalf("refresh: %v", r)
			}
			seen[key] = true
		}
		if r.Int("code") != 1 || r.Int("free_count") != left || len(seen) != 6 {
			t.Fatalf("refresh %d: %v", left, r)
		}
	}
	if r := open(13405); r.Int("code") != 0 {
		t.Fatalf("sixth refresh: %v", r)
	}
	e.setNow(time.Date(2026, 10, 22, 10, 0, 0, 0, dayZone))
	if r := open(13403); r.Int("free_count") != 5 || r.Arr("item_list")[0].Int("item_id") != 10001 {
		t.Fatalf("next day: %v", r)
	}
}
