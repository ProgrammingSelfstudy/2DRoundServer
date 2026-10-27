package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestWhiteDayTrial(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("wd", "女神人")
	c.send(28800, proto.Message{})
	if r := c.waitFor(28800); r.Int("id") != 0 || r.Int("time") != 0 {
		t.Fatalf("closed: %v", r)
	}
	c.send(28801, proto.Message{"id": 2, "formation_type": 1})
	if r := c.waitFor(28801); r.Int("flag") != 0 {
		t.Fatalf("fight while closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 49995, BID: 91051, Status: 1, StartAt: start, EndAt: start + 7*86400, Spec: actSpecJSON(t, false)})
	c.send(28800, proto.Message{})
	if r := c.waitFor(28800); r.Int("id") != 2 || r.Int("time") != 2 || r.Int("buy_time") != 5 {
		t.Fatalf("open: %v", r)
	}
	c.send(28801, proto.Message{"id": 1, "formation_type": 1})
	if r := c.waitFor(28801); r.Int("flag") != 0 {
		t.Fatalf("another camp's boss: %v", r)
	}
	c.send(28801, proto.Message{"id": 2, "formation_type": 1})
	if r := c.waitFor(28801); r.Int("flag") != 1 {
		t.Fatalf("fight: %v", r)
	}
	c.waitFor(20000)
	if intro := c.waitFor(20027); intro.Int("combat_type") != combatWD {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(28802)
	if res.Int("all_dps") <= 0 {
		t.Fatalf("28802: %v", res)
	}
	if want := wdCount(2, res.Int("all_dps")); want > 0 {
		got := res.Arr("reward")
		if len(got) != 1 || got[0].Int("base_id") != 80266 || got[0].Int("num") != want {
			t.Fatalf("reward %v for %d damage, want %d", got, res.Int("all_dps"), want)
		}
	}
	if r := c.waitFor(28800); r.Int("time") != 1 {
		t.Fatalf("a try was not used: %v", r)
	}
	// buying: 200 diamonds, five a day
	e.grantAssets(c, role, map[int64]int64{3: 1500})
	for i, want := range []int64{1, 1, 1, 1, 1, 0} {
		c.send(28803, proto.Message{})
		if b := c.waitFor(28803); b.Int("flag") != want {
			t.Fatalf("buy %d: %v", i+1, b)
		}
		if want == 1 {
			c.waitFor(28800)
		}
	}
	if got, _ := e.st.RoleAssets(role.RID); got[3] != 500 {
		t.Fatalf("diamonds after five buys: %v", got)
	}
}

func TestWhiteDayDamageSteps(t *testing.T) {
	for _, tc := range []struct{ id, dmg, want int64 }{{1, 0, 0}, {1, 1, 1}, {1, 5000, 1}, {1, 5001, 2}, {1, 80000000, 24}, {1, 200000000, 24}} {
		if got := wdCount(tc.id, tc.dmg); got != tc.want {
			t.Errorf("wdCount(%d, %d) = %d, want %d", tc.id, tc.dmg, got, tc.want)
		}
	}
}
