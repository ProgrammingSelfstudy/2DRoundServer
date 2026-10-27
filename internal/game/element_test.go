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
