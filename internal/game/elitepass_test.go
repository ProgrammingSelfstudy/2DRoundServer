package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestElitePass(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ep1", "令牌人")
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, dayZone)
	e.setNow(now)
	c.send(24952, proto.Message{})
	r := c.waitFor(24952)
	if r.Int("period") != 2 || r.Int("cur_day") != 5 || r.Int("lev") != 0 || r.Int("win_count") != 0 {
		t.Fatalf("open: %v", r)
	}
	c.send(24953, proto.Message{"id": 1})
	if r := c.waitFor(24953); r.Int("flag") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	for i := 0; i < 5; i++ {
		if err := elitePassWin(e.st, role.RID, now); err != nil {
			t.Fatal(err)
		}
	}
	c.send(24954, proto.Message{})
	if r := c.waitFor(24954); r.Int("flag") != 1 {
		t.Fatalf("red dot: %v", r)
	}
	c.send(24953, proto.Message{"id": 0})
	if r := c.waitFor(24953); r.Int("flag") != 1 {
		t.Fatalf("claim all: %v", r)
	}
	c.send(24952, proto.Message{})
	r = c.waitFor(24952)
	if r.Int("lev") < 2 || len(r.Arr("list")) != int(r.Int("lev")) || r.Int("win_count") != 5 {
		t.Fatalf("after the claim: %v", r)
	}
	c.send(24953, proto.Message{"id": 0})
	if r := c.waitFor(24953); r.Int("flag") != 0 {
		t.Fatalf("nothing left: %v", r)
	}
	e.setNow(time.Date(2026, time.November, 2, 12, 0, 0, 0, dayZone))
	c.send(24952, proto.Message{})
	if r := c.waitFor(24952); r.Int("period") != 1 || r.Int("win_count") != 0 || len(r.Arr("list")) != 0 {
		t.Fatalf("the next month: %v", r)
	}
}
