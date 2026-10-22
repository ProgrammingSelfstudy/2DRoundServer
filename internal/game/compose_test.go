package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestComposeTimes(t *testing.T) {
	need := []sellGain{{10428, 50}}
	for _, c := range []struct{ have, num, want int64 }{{120, 0, 2}, {120, 1, 1}, {120, 2, 2}, {120, 3, 0}, {49, 1, 0}, {49, 0, 0}} {
		if got := composeTimes(need, map[int64]int64{10428: c.have}, c.num); got != c.want {
			t.Errorf("have %d num %d: got %d want %d", c.have, c.num, got, c.want)
		}
	}
}

func TestCompose(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	e.give(c, role, map[int64]int64{10428: 120, 10020: 1})
	do := func(id, num int64) proto.Message {
		c.send(10523, proto.Message{"id": id, "num": num})
		return c.recv(10523)
	}
	if r := do(10020, 1); r.Int("flag") != 0 {
		t.Fatalf("no recipe: %v", r)
	}
	if r := do(10441, 1); r.Int("flag") != 0 { // needs 10429, which the role lacks
		t.Fatalf("no materials: %v", r)
	}
	if r := do(10421, 3); r.Int("flag") != 0 {
		t.Fatalf("too many: %v", r)
	}
	if got, _ := e.st.RoleItems(role.RID); got[10428] != 120 || got[10421] != 0 {
		t.Fatalf("a refusal must change nothing: %v", got)
	}
	if r := do(10421, 1); r.Int("flag") != 1 {
		t.Fatalf("compose: %v", r)
	}
	c.recv(10500)
	if r := do(10421, 0); r.Int("flag") != 1 { // fast: the 70 left make one more
		t.Fatalf("fast compose: %v", r)
	}
	c.recv(10500)
	got, _ := e.st.RoleItems(role.RID)
	if got[10428] != 20 || got[10421] != 2 {
		t.Fatalf("ledger: %v", got)
	}
}
