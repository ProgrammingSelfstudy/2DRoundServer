package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestNewWarOrder(t *testing.T) {
	for id, p := range newWarOrderCfg {
		if len(p.Levs) == 0 || p.expItem == 0 || len(p.Tasks) == 0 {
			t.Fatalf("period %d: %d levels, exp item %d, %d tasks", id, len(p.Levs), p.expItem, len(p.Tasks))
		}
	}
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("nw1", "夺宝人")
	base := sevenBase(role)
	e.setNow(base.AddDate(0, 0, 3))
	c.send(28700, proto.Message{})
	if r := c.waitFor(28700); r.Int("period") != 0 || len(r.Arr("list")) != 0 {
		t.Fatalf("before the opening: %v", r)
	}
	e.setNow(base.AddDate(0, 0, 8))
	c.send(10300, proto.Message{})
	c.send(28700, proto.Message{})
	r := c.waitFor(28700)
	if r.Int("period") != 1 || r.Int("cur_day") != 2 || r.Int("lev") != 1 {
		t.Fatalf("open: %v", r)
	}
	for _, g := range r.Arr("list") {
		if g.Int("type") == 1 && g.Int("id") == 50001 {
			t.Fatalf("a task of day 1 on day 2: %v", g)
		}
	}
	c.send(28704, proto.Message{"id": 0})
	if r := c.waitFor(28704); r.Int("flag") != 1 {
		t.Fatalf("claim the first level: %v", r)
	}
	if r := c.waitFor(28703); len(r.Arr("reward_list")) != 1 {
		t.Fatalf("taken: %v", r)
	}
	c.send(28704, proto.Message{"id": 2})
	if r := c.waitFor(28704); r.Int("flag") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	c.send(28708, proto.Message{})
	c.waitFor(28708)
	e.setNow(base.AddDate(0, 0, 7+30))
	c.send(28700, proto.Message{})
	if r := c.waitFor(28700); r.Int("period") != 2 || r.Int("cur_day") != 1 || r.Int("exp") != 0 {
		t.Fatalf("the second cycle: %v", r)
	}
}
