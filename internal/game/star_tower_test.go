package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func towerEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ua", "挑战者")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	return e, c, role
}

func TestStarTowerLevelGate(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("ua", "新手")
	c.send(11320, proto.Message{})
	if r := c.waitFor(11320); r.Int("result") != 0 {
		t.Fatalf("level 1: %v", r)
	}
	c.send(11322, proto.Message{"tower": 1})
	if r := c.waitFor(11322); r.Int("result") != 0 {
		t.Fatalf("level 1 climbs: %v", r)
	}
}

func TestStarTowerClimb(t *testing.T) {
	e, c, role := towerEnv(t)
	c.send(11320, proto.Message{})
	info := c.waitFor(11320)
	if info.Int("result") != 1 || info.Int("max_tower") != 0 || info.Int("count") != 2 || len(info.Arr("award_list")) != 350 {
		t.Fatalf("11320: %v", info)
	}
	c.send(11322, proto.Message{"tower": 2}) // the floors go one by one
	if r := c.waitFor(11322); r.Int("result") != 0 {
		t.Fatalf("skipping a floor: %v", r)
	}
	c.send(11322, proto.Message{"tower": 1})
	if r := c.waitFor(11322); r.Int("result") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	intro := c.waitFor(20027)
	if intro.Int("combat_type") != combatStarTower {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(11323)
	if res.Int("tower") != 1 || (res.Int("result") != 1 && res.Int("result") != 2) {
		t.Fatalf("result: %v", res)
	}
	tw, _ := e.st.StarTowerOf(role.RID)
	if res.Int("result") == 1 {
		if tw.MaxTower != 1 || len(res.Arr("first_award")) != 1 || len(res.Arr("award")) != 2 || res.Int("count") != 2 { // the first win costs nothing
			t.Fatalf("a win: %v %+v", res, tw)
		}
	} else if tw.MaxTower != 0 {
		t.Fatalf("a loss moved the tower: %+v", tw)
	}
}

func TestStarTowerSweepBuyMilestoneAndRank(t *testing.T) {
	e, c, role := towerEnv(t)
	if err := e.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: 12, ClearedAt: 5}); err != nil {
		t.Fatal(err)
	}
	c.send(11324, proto.Message{"tower": 13})
	if r := c.waitFor(11324); r.Int("result") != 0 {
		t.Fatalf("sweep of an uncleared floor: %v", r)
	}
	c.send(11324, proto.Message{"tower": 3})
	if r := c.waitFor(11324); r.Int("result") != 1 || r.Int("count") != 1 {
		t.Fatalf("sweep: %v", r)
	}
	res := c.waitFor(11323)
	if res.Int("is_skip") != 1 || res.Int("result") != 1 || len(res.Arr("award")) != 2 || len(res.Arr("first_award")) != 0 {
		t.Fatalf("sweep result: %v", res)
	}
	c.send(11324, proto.Message{"tower": 3})
	if r := c.waitFor(11324); r.Int("result") != 1 || r.Int("count") != 0 {
		t.Fatalf("second sweep: %v", r)
	}
	c.waitFor(11323)
	c.send(11324, proto.Message{"tower": 3})
	if r := c.waitFor(11324); r.Int("result") != 0 {
		t.Fatalf("out of challenges: %v", r)
	}
	c.send(11321, proto.Message{})
	if r := c.waitFor(11321); r.Int("result") != 0 {
		t.Fatalf("no gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(11321, proto.Message{})
	if r := c.waitFor(11321); r.Int("result") != 1 || r.Int("count") != 1 || r.Int("buy_count") != 1 {
		t.Fatalf("buy: %v", r)
	}
	// the milestone of floor 10
	c.send(11328, proto.Message{"id": 2}) // floor 20
	if r := c.waitFor(11328); r.Int("result") != 0 {
		t.Fatalf("floor 20 not reached: %v", r)
	}
	c.send(11328, proto.Message{"id": 1})
	// the original sends the changed state first, then the answer (capture original-2026-10-27)
	if u := c.waitFor(11329); len(u.Arr("award_list")) != 1 || u.Arr("award_list")[0].Int("status") != 2 {
		t.Fatalf("milestone push: %v", u)
	}
	if r := c.waitFor(11328); r.Int("result") != 1 || r.Str("msg") != "" {
		t.Fatalf("milestone: %v", r)
	}
	c.send(11328, proto.Message{"id": 1})
	if r := c.waitFor(11328); r.Int("result") != 0 {
		t.Fatalf("milestone twice: %v", r)
	}
	c.send(11320, proto.Message{})
	if i := c.waitFor(11320); i.Arr("award_list")[0].Int("status") != 2 || i.Arr("award_list")[1].Int("status") != 0 {
		t.Fatalf("statuses: %v", i.Arr("award_list")[:2])
	}
	// the top three and the rank list
	c.send(11327, proto.Message{})
	if r := c.waitFor(11327); len(r.Arr("rank_lists")) != 1 || r.Arr("rank_lists")[0].Int("tower") != 12 {
		t.Fatalf("top: %v", r)
	}
	c.send(12900, proto.Message{"type": 3, "start": 1, "num": 10})
	if r := c.waitFor(12900); r.Int("my_idx") != 1 || r.Int("my_val1") != 12 {
		t.Fatalf("rank list: %v", r)
	}
	// the day turns at 5:00: two free challenges again
	c.settle()
	e.advance(24 * time.Hour)
	c.send(11320, proto.Message{})
	if i := c.waitFor(11320); i.Int("count") != 2 || i.Int("buy_count") != 0 {
		t.Fatalf("next day: %v", i)
	}
}
