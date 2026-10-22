package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSeerpalaceTable(t *testing.T) {
	for _, id := range []int64{1000, 2000, 3000, 4000} {
		g := seerTable[id]
		if g == nil || len(g.Cost) != 1 || g.Cost[0] != [2]int64{14001, 1} || len(g.Gain) != 3 {
			t.Fatalf("group %d: %+v", id, g)
		}
	}
	// a draw at the low end gives the first 5-star entry, at the high end the last 4-star one
	g := seerTable[1000]
	if e := pickSeer(g, 0); e != g.Pool[0] || e.Item != 24900 {
		t.Fatalf("low end: %+v", e)
	}
	if e := pickSeer(g, 0.9999999); e != g.Pool[len(g.Pool)-1] {
		t.Fatalf("high end: %+v", e)
	}
	// the 5-star entries hold 15 % of the weight in the first three groups
	five := 0.0
	for _, e := range g.Pool[:12] {
		five += e.Chance
	}
	if five < 14.9 || five > 15.1 {
		t.Fatalf("5-star share: %v", five)
	}
}

func TestSeerpalaceSummon(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	summon := func(group int64) proto.Message {
		c.send(23213, proto.Message{"group_id": group})
		return c.recvOrNone(23213)
	}
	if r := summon(1000); r != nil { // no crystal
		t.Fatalf("no crystal: %v", r)
	}
	e.give(c, role, map[int64]int64{14001: 2})
	if r := summon(7); r != nil { // unknown group
		t.Fatalf("unknown group: %v", r)
	}
	r := summon(1000)
	rw := r.Arr("rewards")
	if r == nil || r.Int("group_id") != 1000 || len(rw) != 4 || rw[0].Int("base_id") != 24900 || rw[0].Int("num") != 50 ||
		rw[1].Int("base_id") != 24 || rw[2].Int("base_id") != 14002 || rw[2].Int("num") != 30 || rw[3].Int("base_id") != 39 || rw[3].Int("num") != 50 {
		t.Fatalf("reply: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	items, _ := e.st.RoleItems(role.RID)
	assets, _ := e.st.RoleAssets(role.RID)
	if items[14001] != 1 || items[24900] != 50 || items[14002] != 30 || assets[24] != 1 || assets[39] != 50 {
		t.Fatalf("ledgers: items %v assets %v", items, assets)
	}
	if r := summon(1000); r == nil {
		t.Fatal("second crystal")
	}
	c.recv(10302)
	c.recv(10500)
	if r := summon(1000); r != nil { // used up
		t.Fatalf("used up: %v", r)
	}
}

func TestSeerpalaceOpen(t *testing.T) {
	e := newEnv(t, time.Minute)
	day := time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone)
	e.setNow(day)
	c, role := e.enter("u1", "甲")
	c.send(23234, proto.Message{})
	if r := c.recv(23235); r.Int("flag") != 1 {
		t.Fatalf("23235: %v", r)
	}
	if r := c.recv(23234); r.Int("flag") != 1 {
		t.Fatalf("23234: %v", r)
	}
	if n, _ := e.st.Counter(role.RID, seerOpenKey(day)); n != 1 {
		t.Fatalf("opened today: %d", n)
	}
}

func TestSeerpalaceScoreSummon(t *testing.T) {
	// the divination groups cost star-card points and only hold 5-star entries of their camp
	for gid, want := range map[int64]struct{ cost, camp int64 }{10000: {1000, 1}, 20000: {1000, 2}, 30000: {1000, 3}, 40000: {2000, 4}, 50000: {2000, 5}} {
		g := seerTable[gid]
		if g == nil || len(g.Cost) != 1 || g.Cost[0] != [2]int64{39, want.cost} || len(g.Gain) != 0 || len(g.Pool) == 0 {
			t.Fatalf("group %d: %+v", gid, g)
		}
		for _, e := range g.Pool {
			if e.Item/1000%10 != want.camp+3 && e.Item/100%10 != want.camp { // 24xxx-28xxx fragments, 294xx / 295xx camp fragments
				t.Fatalf("group %d holds %d", gid, e.Item)
			}
		}
	}

	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	summon := func(group int64) proto.Message {
		c.send(23213, proto.Message{"group_id": group})
		return c.recvOrNone(23213)
	}
	if r := summon(10000); r != nil { // no points
		t.Fatalf("no points: %v", r)
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 39, Num: 3000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	if r := summon(10000); r != nil { // the divination needs VIP 5
		t.Fatalf("no VIP: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 4); err != nil {
		t.Fatal(err)
	}
	if r := summon(10000); r != nil {
		t.Fatalf("VIP 4: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 5); err != nil {
		t.Fatal(err)
	}
	r := summon(10000)
	rw := r.Arr("rewards")
	if r == nil || len(rw) != 1 || rw[0].Int("base_id") != 24900 || rw[0].Int("num") != 50 {
		t.Fatalf("reply: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	assets, _ := e.st.RoleAssets(role.RID)
	items, _ := e.st.RoleItems(role.RID)
	if assets[39] != 2000 || items[24900] != 50 {
		t.Fatalf("ledgers: %v %v", assets, items)
	}
	if r := summon(40000); r == nil { // 2000 points are just enough
		t.Fatal("light card")
	}
	c.recv(10302)
	c.recv(10500)
	if a, _ := e.st.RoleAssets(role.RID); a[39] != 0 {
		t.Fatalf("points left: %v", a)
	}
}

func TestRoleBaseCarriesVIP(t *testing.T) {
	m := roleBase(&store.Role{VIPLev: 7})
	if m["vip_lev"] != 7 || m["vip_exp"] != int64(15000) { // VIP 7 starts at 15000 in the client's table
		t.Fatalf("vip: %v %v", m["vip_lev"], m["vip_exp"])
	}
	if roleBase(&store.Role{})["vip_exp"] != int64(0) || vipExpFor(MaxVIPLevel+1) != 0 || vipExpFor(-1) != 0 {
		t.Fatal("no VIP, no experience")
	}
	if MaxVIPLevel != 30 {
		t.Fatal("the client's VIP table has levels 0 to 30")
	}
}
