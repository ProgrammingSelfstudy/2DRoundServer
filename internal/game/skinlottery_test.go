package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSkinLottery(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("sl", "征兵人")
	camp := slCamps[1001]
	c.send(26601, proto.Message{})
	if r := c.waitFor(26601); r.Int("lottery_id") != 0 || r.Int("last_time") != 0 {
		t.Fatalf("nothing open: %v", r)
	}
	c.send(26600, proto.Message{})
	if r := c.waitFor(26600); r.Int("flag") != 0 {
		t.Fatalf("draw while closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 1001, BID: 91001, Status: 1, StartAt: start, EndAt: start + 3*86400, Spec: actSpecJSON(t, false)})
	c.send(26601, proto.Message{})
	if r := c.waitFor(26601); r.Int("lottery_id") != 1001 || r.Int("time") != 0 || r.Int("last_time") != 3*86400-(actNow.Unix()-start) {
		t.Fatalf("26601: %v", r)
	}
	// the first draw is free, the second needs 16 tokens
	draw := func() proto.Message {
		c.send(26600, proto.Message{})
		return c.waitFor(26600)
	}
	r := draw()
	if r.Int("flag") != 1 || r.Int("time") != 1 || r.Int("sort") < 1 || r.Int("sort") > 10 {
		t.Fatalf("first draw: %v", r)
	}
	if rw := c.waitFor(26602).Arr("reward_list"); len(rw) != 1 {
		t.Fatalf("26602: %v", rw)
	}
	if r2 := draw(); r2.Int("flag") != 0 {
		t.Fatalf("second draw without tokens: %v", r2)
	}
	var need int64
	for _, n := range camp.Costs {
		need += n
	}
	e.give(c, role, map[int64]int64{camp.Token: need})
	seen := map[int64]bool{r.Int("sort"): true}
	for i := 2; i <= len(camp.Prizes); i++ {
		r = draw()
		if r.Int("flag") != 1 || r.Int("time") != int64(i) || seen[r.Int("sort")] {
			t.Fatalf("draw %d: %v (seen %v)", i, r, seen)
		}
		seen[r.Int("sort")] = true
		c.waitFor(26602)
		if i < slFirst && r.Int("sort") == slSpecial {
			t.Fatalf("the special prize came at draw %d", i)
		}
	}
	if len(seen) != len(camp.Prizes) || !seen[slSpecial] {
		t.Fatalf("not every prize was drawn: %v", seen)
	}
	if r := draw(); r.Int("flag") != 0 {
		t.Fatalf("an eleventh draw: %v", r)
	}
	c.send(26601, proto.Message{})
	if r := c.waitFor(26601); len(r.Arr("sort_list")) != 10 || r.Int("time") != 10 {
		t.Fatalf("after all: %v", r)
	}
	if left, _ := e.st.RoleItems(role.RID); left[camp.Token] != 0 {
		t.Fatalf("tokens left: %v", left)
	}
}
