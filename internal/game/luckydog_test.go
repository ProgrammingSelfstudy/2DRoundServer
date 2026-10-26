package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func luckyEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("lk", "锦鲤人")
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: luckyCamp, BID: 993046, Status: 1, StartAt: start, EndAt: start + 7*86400, Spec: actSpecJSON(t, false)})
	return e, c, role
}

func TestLuckyType(t *testing.T) {
	// period 1: 766 / 52 / 8
	for num, want := range map[int64]int64{12345766: 1, 12345652: 2, 12345008: 3, 12345679: 4, 12345052: 2, 12345766 + 1: 4} {
		if got := luckyType(1, num); got != want {
			t.Errorf("luckyType(1, %d) = %d, want %d", num, got, want)
		}
	}
}

func TestLuckyDogFlow(t *testing.T) {
	old := summonRand
	t.Cleanup(func() { summonRand = old })
	e, c, role := luckyEnv(t)
	e.setNow(actNow.AddDate(0, 0, 1)) // the second period: the quest is to summon twice
	e.give(c, role, map[int64]int64{10401: 5})

	c.send(28400, proto.Message{})
	r := c.waitFor(28400)
	if r.Int("period") != 2 || len(r.Arr("info")) != 2 {
		t.Fatalf("28400: %v", r)
	}
	if q := r.Arr("info")[1].Arr("quests"); len(q) != 3 || q[0].Int("quest_id") != 102 || q[0].Int("num") != 0 {
		t.Fatalf("quests: %v", q)
	}
	// the login red dot goes when the panel is opened
	c.send(28402, proto.Message{})
	if r := c.waitFor(28402); r.Int("code") != 1 {
		t.Fatalf("red: %v", r)
	}
	c.send(28403, proto.Message{})
	c.send(28402, proto.Message{})
	if r := c.waitFor(28402); r.Int("code") != 0 {
		t.Fatalf("red after open: %v", r)
	}

	// two summons finish quest 102: the state is pushed with its number (the draws of the summon are not the number's)
	c.send(23201, proto.Message{"group_id": 300, "recruit_type": 1, "times": 1})
	c.waitFor(23201)
	if r := c.waitFor(28400); r.Arr("info")[1].Arr("quests")[0].Int("val") != 1 {
		t.Fatalf("one summon: %v", r)
	}
	summonRand = func() float64 { return 0 } // the number 10000000
	c.send(23201, proto.Message{"group_id": 100, "recruit_type": 4, "times": 1})
	c.waitFor(23201)
	r = c.waitFor(28400)
	if q := r.Arr("info")[1].Arr("quests")[0]; q.Int("num") != 10000000 || q.Int("finish") != 1 || q.Int("lucky_type") != 0 {
		t.Fatalf("finished before the draw: %v", q)
	}
	// before the draw nothing can be taken
	c.send(28401, proto.Message{"period": 2})
	if r := c.waitFor(28401); r.Int("code") != 0 {
		t.Fatalf("taken before the draw: %v", r)
	}
	// after 23:50 the draw is out: the number 10000000 wins the last prize; the quests that were not finished stay open
	e.setNow(dayStart(actNow).AddDate(0, 0, 1).Add(23*time.Hour + 51*time.Minute))
	c.send(28400, proto.Message{})
	if q := c.waitFor(28400).Arr("info")[1]; q.Int("state") != 1 || q.Arr("quests")[0].Int("lucky_type") != 4 || q.Arr("quests")[1].Int("lucky_type") != 0 {
		t.Fatalf("after the draw: %v", q)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(28401, proto.Message{"period": 2})
	if r := c.waitFor(28401); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if q := c.waitFor(28400).Arr("info")[1].Arr("quests")[0]; q.Int("finish") != 2 {
		t.Fatalf("taken: %v", q)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3]-before[3] != 50 {
		t.Fatalf("the last prize is 50 diamonds: %d -> %d", before[3], after[3])
	}
	c.send(28401, proto.Message{"period": 2})
	if r := c.waitFor(28401); r.Int("code") != 0 {
		t.Fatalf("taken twice: %v", r)
	}
}

func TestLuckyDogPeriodsAndClosed(t *testing.T) {
	e, c, role := luckyEnv(t)
	e.setNow(actNow.AddDate(0, 0, 2))
	c.send(28400, proto.Message{})
	if r := c.waitFor(28400); r.Int("period") != 3 || len(r.Arr("info")) != 3 {
		t.Fatalf("day 3: %v", r)
	}
	e.setNow(actNow.AddDate(0, 0, 20))
	c.send(28400, proto.Message{})
	if r := c.waitFor(28400); r.Int("period") != 0 || len(r.Arr("info")) != 0 {
		t.Fatalf("closed: %v", r)
	}
	c.send(28401, proto.Message{"period": 1})
	if r := c.waitFor(28401); r.Int("code") != 0 {
		t.Fatalf("take when closed: %v", r)
	}
	_ = role
}
