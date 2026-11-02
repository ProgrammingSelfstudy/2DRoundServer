package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestElementTemple(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("el1", "元素人")
	now := actNow
	b := elementCfg.Bases[0]
	for !b.openOn(now) { // a day when the first base is open
		now = now.Add(24 * time.Hour)
	}
	e.setNow(now)
	c.send(25000, proto.Message{})
	r := c.waitFor(25000)
	if r.Int("num") != 10 || r.Int("day") != weekday(now) || len(r.Arr("list")) == 0 {
		t.Fatalf("25000: %v", r)
	}
	var row proto.Message
	for _, l := range r.Arr("list") {
		if l.Int("type") == b.Type {
			row = l
		}
	}
	if row.Int("boss_id") != 1 || row.Int("group") != b.groupOf(now) {
		t.Fatalf("the base: %v", r)
	}
	req := func(boss int64) proto.Message { return proto.Message{"type": b.Type, "boss_id": boss} }
	c.send(25001, req(1))
	if r := c.waitFor(25001); r.Int("code") != 0 { // the role is below level 90
		t.Fatalf("level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, 90); err != nil {
		t.Fatal(err)
	}
	c.send(25002, req(1))
	if r := c.waitFor(25002); r.Int("code") != 0 {
		t.Fatalf("sweep before clearing: %v", r)
	}
	c.send(25001, req(2))
	if r := c.waitFor(25001); r.Int("code") != 0 {
		t.Fatalf("skipping a customs: %v", r)
	}
	// heroes of the camp the customs asks for
	cu := elementCfg.byKey[[2]int64{b.groupOf(now), 1}]
	var seeds []store.PartnerSeed
	for bid, p := range partnerTable {
		if int64(p.Camp) == cu.Limit[0][0] && len(seeds) < 5 && bid > 10000 {
			seeds = append(seeds, store.PartnerSeed{BID: bid, Star: 5})
		}
	}
	if ok, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !ok {
		t.Fatalf("heroes: %v %v", ok, err)
	}
	c.send(25001, req(1))
	if r := c.waitFor(25001); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	c.waitFor(20027)
	c.send(20062, proto.Message{}) // skip the fight
	c.waitFor(20062)
	res := c.waitFor(20006)
	if res.Int("combat_type") != combatElement {
		t.Fatalf("20006: %v", res)
	}
	d := &elementState{}
	if _, err := e.st.LoadState(role.RID, elementModule, d); err != nil {
		t.Fatal(err)
	}
	if d.Count != 9 {
		t.Fatalf("a challenge is spent: %+v", d)
	}
	c.waitFor(25000) // the push after the fight
	// the challenges come back, one in three hours
	e.setNow(now.Add(3*time.Hour + time.Minute))
	c.send(25000, proto.Message{})
	if r := c.waitFor(25000); r.Int("num") != 10 {
		t.Fatalf("regained: %v", r)
	}
	c.send(25003, proto.Message{}) // full: nothing to buy
	if r := c.waitFor(25003); r.Int("code") != 0 {
		t.Fatalf("buy when full: %v", r)
	}
}

// The territory challenge privilege: two free challenges once a day and six more purchases after the two normal ones.
func TestElementPrivilege(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("el2", "元素人二")
	c2, plain := e.enter("el3", "元素人三")
	p := &privilegeState{}
	p.set(elementPrivilege, privilegeForever)
	if err := e.st.SaveState(role.RID, privilegeModule, p); err != nil {
		t.Fatal(err)
	}
	give := func(rid, gems int64) {
		if err := e.st.Exchange(rid, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: gems}, map[int64]int64{}); err != nil {
			t.Fatal(err)
		}
	}
	diamonds := func(rid int64) int64 { a, _ := e.st.RoleAssets(rid); return a[3] }
	empty := func(rid int64) {
		if err := e.st.SaveState(rid, elementModule, &elementState{Count: 0, CountAt: actNow.Unix(), BuyDay: dayNo(actNow)}); err != nil {
			t.Fatal(err)
		}
	}

	// the free challenges come once a day, on top of the ten
	c.send(25000, proto.Message{})
	if r := c.waitFor(25000); r.Int("num") != 12 {
		t.Fatalf("with the privilege: %v", r)
	}
	c.send(25000, proto.Message{})
	if r := c.waitFor(25000); r.Int("num") != 12 || r.Int("pr_buy_num") != 0 {
		t.Fatalf("a second look the same day: %v", r)
	}
	c2.send(25000, proto.Message{})
	if r := c2.waitFor(25000); r.Int("num") != 10 {
		t.Fatalf("without it: %v", r)
	}
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(25000, proto.Message{})
	if r := c.waitFor(25000); r.Int("num") != 14 { // the next day's two, the old ones kept
		t.Fatalf("the next day: %v", r)
	}
	e.setNow(actNow)

	// purchases: 50 and 100 as before, then the privilege's 100 100 100 100 150 150, and then no more
	if err := e.st.SaveState(role.RID, elementModule, &elementState{Count: 0, CountAt: actNow.Unix(), BuyDay: dayNo(actNow), GiftDay: dayNo(actNow)}); err != nil {
		t.Fatal(err)
	}
	give(role.RID, 2000)
	start := diamonds(role.RID)
	buy := func(cl *client) proto.Message {
		cl.send(25003, proto.Message{})
		r := cl.waitFor(25003)
		if r.Int("code") == 1 {
			cl.waitFor(25004)
		}
		return r
	}
	for i := 0; i < 8; i++ {
		if r := buy(c); r.Int("code") != 1 {
			t.Fatalf("purchase %d: %v", i+1, r)
		}
	}
	if spent := start - diamonds(role.RID); spent != 50+100+100*4+150*2 {
		t.Fatalf("spent %d", spent)
	}
	if r := buy(c); r.Int("code") != 0 {
		t.Fatalf("the ninth: %v", r)
	}
	c.send(25000, proto.Message{})
	if r := c.waitFor(25000); r.Int("buy_num") != 2 || r.Int("pr_buy_num") != 6 || r.Int("num") != 8 {
		t.Fatalf("counts: %v", r)
	}

	// without the privilege the two normal buys are all there are
	empty(plain.RID)
	give(plain.RID, 1000)
	for i := 0; i < 2; i++ {
		if r := buy(c2); r.Int("code") != 1 {
			t.Fatalf("plain purchase %d: %v", i+1, r)
		}
	}
	if r := buy(c2); r.Int("code") != 0 {
		t.Fatalf("a third without the privilege: %v", r)
	}
}
