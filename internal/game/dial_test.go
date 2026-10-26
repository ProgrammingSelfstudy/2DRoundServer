package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestDialWeights(t *testing.T) {
	total := int64(0)
	for _, w := range dialWeights {
		total += w
	}
	if total != 10085 || dialWeights[0] != 0 {
		t.Fatalf("weights add up to %d", total)
	}
	for i := 1; i <= 12; i++ {
		if dialWeights[i] <= 0 {
			t.Fatalf("slot %d has no chance", i)
		}
	}
	types := map[int64]bool{}
	for _, s := range dialCfg.Slots {
		types[s.Type] = true
	}
	if !types[1] || !types[2] || !types[3] || !types[9] {
		t.Fatalf("slot types: %v", types)
	}
}

func TestDialFlow(t *testing.T) {
	old := summonRand
	summonRand = func() float64 { return 0.06 } // slot 4: the first prize
	t.Cleanup(func() { summonRand = old })
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("dl1", "转盘人")
	c.send(16671, proto.Message{"count": 1, "flag": 0})
	if r := c.waitFor(16671); r.Int("result") != 0 {
		t.Fatalf("without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: dialCamp, BID: 93022, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(16670, proto.Message{})
	r := c.waitFor(16670)
	if r.Int("num") != 0 || r.Int("gold") != 10000 || r.Int("is_free") != 0 || len(r.Arr("award_list")) != 10 {
		t.Fatalf("open: %v", r)
	}
	c.send(16671, proto.Message{"count": 1, "flag": 0}) // the free turn
	r = c.waitFor(16671)
	if r.Int("result") != 1 || r.Int("id") != 4 {
		t.Fatalf("free turn: %v", r)
	}
	if g := c.waitFor(16676); g.Int("gold") != 10000 { // the pool paid half of its 10010 and falls back to the minimum
		t.Fatalf("pool: %v", g)
	}
	r = c.waitFor(16670)
	if r.Int("num") != 1 || r.Int("is_free") != 1 {
		t.Fatalf("after the free turn: %v", r)
	}
	c.send(16672, proto.Message{})
	if a := c.waitFor(16672); a.Int("count") != 1 || a.Arr("awards2")[0].Int("bid") != 3 || a.Arr("awards2")[0].Int("num") != 5005 {
		t.Fatalf("result: %v", a)
	}
	c.send(16672, proto.Message{})
	if a := c.waitFor(16672); a.Int("count") != 0 {
		t.Fatalf("asked twice: %v", a)
	}
	as, _ := e.st.RoleAssets(role.RID)
	if as[3] != 5005 {
		t.Fatalf("the prize: %v", as)
	}
	c.send(16671, proto.Message{"count": 1, "flag": 0})
	if r := c.waitFor(16671); r.Int("result") != 0 {
		t.Fatalf("no key: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{37003: 10})
	c.send(16671, proto.Message{"count": 10, "flag": 1})
	if r := c.waitFor(16671); r.Int("result") != 1 {
		t.Fatalf("ten turns: %v", r)
	}
	if a := c.waitFor(16672); a.Int("count") != 10 {
		t.Fatalf("pushed result: %v", a)
	}
	r = c.waitFor(16670)
	if r.Int("num") != 11 {
		t.Fatalf("score: %v", r)
	}
	c.send(16673, proto.Message{"id": 2})
	if r := c.waitFor(16673); r.Int("result") != 0 {
		t.Fatalf("an award not reached: %v", r)
	}
	c.send(16673, proto.Message{"id": 1})
	if r := c.waitFor(16673); r.Int("result") != 1 {
		t.Fatalf("award: %v", r)
	}
	c.send(16673, proto.Message{"id": 1})
	if r := c.waitFor(16673); r.Int("result") != 0 {
		t.Fatalf("award twice: %v", r)
	}
	c.send(16674, proto.Message{"type": 1})
	if l := c.waitFor(16674); l.Int("type") != 1 || len(l.Arr("log_list")) != 11 {
		t.Fatalf("my log: %v", l)
	}
	c.send(16674, proto.Message{"type": 2})
	if l := c.waitFor(16674); len(l.Arr("log_list")) != 11 { // every turn here was a pool prize
		t.Fatalf("all log: %v", l)
	}
}
