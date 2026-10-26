package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestAnimateLantern(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("an1", "花灯人")
	c.send(24801, proto.Message{})
	if r := c.waitFor(24801); len(r.Arr("award_list")) != 0 {
		t.Fatalf("no activity open: %v", r)
	}
	c.send(24803, proto.Message{"id": 1})
	if r := c.waitFor(24803); r.Int("flag") != 0 {
		t.Fatalf("a light without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: lanternCamp, BID: 93018, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(24801, proto.Message{})
	c.waitFor(10350)
	r := c.waitFor(24801)
	if r.Int("all_num") != 0 || len(r.Arr("award_list")) != 4 || r.Arr("award_list")[0].Int("flag") != 1 || r.Arr("award_list")[1].Int("flag") != 0 {
		t.Fatalf("pools: %v", r)
	}
	c.send(24803, proto.Message{"id": 2})
	if r := c.waitFor(24803); r.Int("flag") != 0 {
		t.Fatalf("a pool not open: %v", r)
	}
	// the first light of a pool is free
	c.send(24803, proto.Message{"id": 1})
	if r := c.waitFor(24803); r.Int("flag") != 1 {
		t.Fatalf("free light: %v", r)
	}
	c.waitFor(10500)
	c.waitFor(10350)
	r = c.waitFor(24802)
	drawn := 0
	for _, p := range r.Arr("rand_list") {
		drawn += int(p.Int("status"))
	}
	if r.Int("count") != 1 || drawn != 1 || len(r.Arr("rand_list")) != 12 {
		t.Fatalf("pool: %v", r)
	}
	c.waitFor(24801)
	// the second light costs 5 wine
	c.send(24803, proto.Message{"id": 1})
	if r := c.waitFor(24803); r.Int("flag") != 0 {
		t.Fatalf("no wine: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{80201: 1000})
	for i := 2; i <= 12; i++ {
		c.send(24803, proto.Message{"id": 1})
		if r := c.waitFor(24803); r.Int("flag") != 1 {
			t.Fatalf("light %d: %v", i, r)
		}
		c.waitFor(10500)
		c.waitFor(10350)
		c.waitFor(24802)
		c.waitFor(24801)
	}
	c.send(24803, proto.Message{"id": 1})
	if r := c.waitFor(24803); r.Int("flag") != 0 {
		t.Fatalf("a pool done: %v", r)
	}
	c.send(24801, proto.Message{})
	c.waitFor(10350)
	if r := c.waitFor(24801); r.Int("all_num") != 215 || r.Arr("award_list")[1].Int("flag") != 1 || r.Arr("award_list")[2].Int("flag") != 1 || r.Arr("award_list")[3].Int("flag") != 0 {
		t.Fatalf("after pool 1: %v", r)
	}
}

func TestAnimateKitchen(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("an2", "厨师")
	c.send(24804, proto.Message{})
	if r := c.waitFor(24804); r.Int("camp_id") != 0 {
		t.Fatalf("no activity open: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 51641, BID: 91641, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(24804, proto.Message{})
	if r := c.waitFor(24804); r.Int("camp_id") != 51641 || r.Int("group_id") != 10001 || r.Int("combat_num") != 3 || r.Int("lev") != 1 || len(r.Arr("make_list")) != 5 {
		t.Fatalf("kitchen: %v", r)
	}
	cook := func(id int64) proto.Message {
		c.send(24806, proto.Message{"id": id})
		return c.waitFor(24806)
	}
	if r := cook(1001); r.Int("flag") != 0 {
		t.Fatalf("no materials: %v", r)
	}
	if r := cook(1002); r.Int("flag") != 0 { // level 2
		t.Fatalf("level: %v", r)
	}
	// the collect fight pays the materials: 3 of the day, the 4th is refused, a bought one adds one
	for i := 0; i < 3; i++ {
		c.send(24808, proto.Message{})
		if r := c.waitFor(24808); r.Int("flag") != 1 {
			t.Fatalf("fight %d: %v", i, r)
		}
		c.waitFor(10500)
		if r := c.waitFor(24804); r.Int("combat_num") != int64(2-i) {
			t.Fatalf("fights left %d: %v", i, r)
		}
		c.waitFor(24805)
	}
	c.send(24808, proto.Message{})
	if r := c.waitFor(24808); r.Int("flag") != 0 {
		t.Fatalf("the 4th fight: %v", r)
	}
	if r := cook(1001); r.Int("flag") != 1 { // 3 fights: 210 80269, 420 802700, 90 80271 and 90 80280
		t.Fatalf("cook: %v", r)
	}
	c.waitFor(10302)
	c.waitFor(10500)
	r := c.waitFor(24804)
	if r.Int("exp") != 100 || r.Arr("show_list")[0].Int("num") != 1 || r.Arr("make_list")[0].Int("num") != 1 {
		t.Fatalf("after cooking: %v", r)
	}
	c.waitFor(24805)
	c.send(24807, proto.Message{"id": 2})
	if r := c.waitFor(24807); r.Int("flag") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	c.send(24807, proto.Message{"id": 1})
	if r := c.waitFor(24807); r.Int("flag") != 1 {
		t.Fatalf("level 1 reward: %v", r)
	}
	c.waitFor(10302)
	c.waitFor(10500)
	c.waitFor(24804)
	if r := c.waitFor(24805); len(r.Arr("reward_list")) != 1 {
		t.Fatalf("claimed: %v", r)
	}
	c.send(24807, proto.Message{"id": 1})
	if r := c.waitFor(24807); r.Int("flag") != 0 {
		t.Fatalf("claimed twice: %v", r)
	}
	// buy a fight: 1000 diamonds each, two a day
	c.send(24809, proto.Message{})
	if r := c.waitFor(24809); r.Int("flag") != 0 {
		t.Fatalf("no diamonds: %v", r)
	}
}
