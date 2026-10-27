package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestWelfareUnpaidState(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("a", "甲")
	c.send(16705, proto.Message{})
	if r := c.waitFor(16705); r.Int("card1_is_reward") != 0 || r.Int("card2_end_time") != 0 {
		t.Fatalf("16705: %v", r)
	}
	c.send(16706, proto.Message{"card_type": 2})
	if r := c.waitFor(16706); r.Int("code") != 0 || r.Int("card_type") != 2 {
		t.Fatalf("16706: %v", r)
	}
	c.send(21007, proto.Message{"type": 1})
	r := c.waitFor(21007)
	if r.Int("reg_day") != 1 || r.Int("ref_time") == 0 {
		t.Fatalf("21007: %v", r)
	}
	c.send(24703, proto.Message{"id": 5})
	if r := c.waitFor(24703); len(r.Arr("ids")) != 1 || r.Arr("ids")[0].Int("id") != 5 {
		t.Fatalf("24703: %v", r)
	}
	c.send(16635, proto.Message{})
	if r := c.waitFor(16635); r.Int("code") != 1 {
		t.Fatalf("16635: %v", r)
	}
	c.send(16636, proto.Message{"number": "13800000000", "code": "1234"})
	if r := c.waitFor(16636); r.Int("code") != 0 {
		t.Fatalf("16636: %v", r)
	}
	c.send(24604, proto.Message{})
	if r := c.waitFor(24604); r.Int("code") != 0 {
		t.Fatalf("24604: %v", r)
	}
}
