package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestTimeSummon(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ts1", "限定人")
	old := summonRand
	summonRand = func() float64 { return 0.999 } // the last outcome: a 3-star hero
	t.Cleanup(func() { summonRand = old })
	c.send(23216, proto.Message{})
	if r := c.waitFor(23216); r.Int("camp_id") != 0 {
		t.Fatalf("no activity open: %v", r)
	}
	c.send(23217, proto.Message{"times": 1, "recruit_type": 1})
	if r := c.waitFor(23217); r.Int("flag") != 0 {
		t.Fatalf("a draw without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 22010, BID: 91193, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(23216, proto.Message{})
	if r := c.waitFor(23216); r.Int("camp_id") != 22010 || r.Int("free_time") != 0 || r.Int("times") != 0 || r.Int("item_id") != 10407 {
		t.Fatalf("open: %v", r)
	}
	draw := func(times, kind int64) {
		c.send(23217, proto.Message{"times": times, "recruit_type": kind})
	}
	// the free draw: the hero, the result, the new state
	draw(1, 1)
	c.waitFor(11001)
	if r := c.waitFor(23201); r.Int("group_id") != 102 || r.Int("times") != 1 || len(r.Arr("partner_bids")) != 1 {
		t.Fatalf("result: %v", r)
	}
	if r := c.waitFor(23216); r.Int("times") != 1 || r.Int("free_time") != actNow.Unix()+86400 {
		t.Fatalf("state: %v", r)
	}
	draw(1, 1)
	if r := c.waitFor(23217); r.Int("flag") != 0 {
		t.Fatalf("a second free draw: %v", r)
	}
	draw(10, 4)
	if r := c.waitFor(23217); r.Int("flag") != 0 {
		t.Fatalf("ten draws without the item: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{10407: 30})
	draw(10, 4)
	c.waitFor(11001)
	r := c.waitFor(23201)
	stars := 0
	for _, b := range r.Arr("partner_bids") {
		if b.Int("init_star") >= 4 {
			stars++
		}
	}
	if len(r.Arr("partner_bids")) != 10 || stars == 0 {
		t.Fatalf("ten draws: %v", r)
	}
	if r := c.waitFor(23216); r.Int("times") != 11 {
		t.Fatalf("state: %v", r)
	}
	c.send(23218, proto.Message{})
	if r := c.waitFor(23218); r.Int("flag") != 0 {
		t.Fatalf("an award not reached: %v", r)
	}
	draw(10, 4)
	c.waitFor(23201)
	draw(10, 4) // 31 draws: the first award (30)
	c.waitFor(23201)
	c.send(23218, proto.Message{})
	if r := c.waitFor(23218); r.Int("flag") != 1 {
		t.Fatalf("award: %v", r)
	}
}

func TestTimeSummonDirectGroup(t *testing.T) {
	g := timeSummonCfg.Groups["100"]
	old := summonRand
	t.Cleanup(func() { summonRand = old })
	summonRand = func() float64 { return 0 }
	if hero, item := timeSummonDraw(g); hero != 25408 || item != 0 {
		t.Fatalf("first row: %d %d", hero, item)
	}
	summonRand = func() float64 { return 0.9999 }
	if hero, item := timeSummonDraw(g); hero != 0 || item != 29904 {
		t.Fatalf("last chip row: %d %d", hero, item)
	}
}
