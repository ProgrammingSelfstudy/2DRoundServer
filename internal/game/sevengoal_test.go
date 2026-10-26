package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// sevenBase is noon of the day the role was created.
func sevenBase(role *store.Role) time.Time {
	return dayStart(time.Unix(role.CreatedAt, 0)).Add(12 * time.Hour)
}

func TestSevenGoalRules(t *testing.T) {
	for _, c := range []struct {
		desc, label string
		need        int64
	}{
		{"荣誉登场：登录冒险大陆一次", evtLoaded, 1},
		{"速战勇者：快速作战四次", questFastCombat, 4},
		{"友谊之光：赠送好友友情点5次以上", evtFriendPresent, 5},
		{"伙伴招募：高级召唤3次", evtRecruit, 3},
		{"勇士战场：挑战5次竞技场", "", 5},
	} {
		if l, n := sevenGoalRule(c.desc); l != c.label || n != c.need {
			t.Fatalf("%s: %s %d", c.desc, l, n)
		}
	}
	for id, p := range sevenGoalCfg {
		if len(p.goals) == 0 || len(p.levs) == 0 {
			t.Fatalf("period %d: %d goals", id, len(p.goals))
		}
		days := map[int64]bool{}
		for _, g := range p.goals {
			days[g.Day] = true
		}
		for d := int64(1); d <= 6; d++ {
			if !days[d] {
				t.Fatalf("period %d has no goals on day %d", id, d)
			}
		}
	}
}

func TestSevenGoalFlow(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("sg1", "七日人")
	e.setNow(sevenBase(role))
	c.send(10300, proto.Message{})
	c.send(13604, proto.Message{})
	r := c.waitFor(13604)
	if r.Int("period") != 2 || r.Int("cur_day") != 1 || r.Int("lev") != 1 || r.Int("end_time") <= sevenBase(role).Unix() {
		t.Fatalf("open: %v", r)
	}
	var login proto.Message
	for _, g := range r.Arr("list") {
		if g.Int("id") == 211 {
			login = g
		}
		if g.Int("id") >= 221 && g.Int("id") < 300 {
			t.Fatalf("a goal of a later day is listed: %v", g)
		}
	}
	if login == nil || login.Int("finish") != 1 {
		t.Fatalf("the login goal after the login: %v", login)
	}
	c.send(13606, proto.Message{"id": 212})
	if r := c.waitFor(13606); r.Int("code") != 0 {
		t.Fatalf("an unmet goal is claimed: %v", r)
	}
	c.send(13606, proto.Message{"id": 211})
	if r := c.waitFor(13606); r.Int("code") != 1 {
		t.Fatalf("claim: %v", r)
	}
	if r := c.waitFor(13605); r.Arr("list")[0].Int("finish") != 2 {
		t.Fatalf("the goal after the claim: %v", r)
	}
	if r := c.waitFor(13609); r.Int("exp") != 10 || r.Int("lev") != 1 {
		t.Fatalf("exp: %v", r)
	}
	c.send(13606, proto.Message{"id": 211})
	if r := c.waitFor(13606); r.Int("code") != 0 {
		t.Fatalf("a second claim: %v", r)
	}
	// the level reward of level 2 needs 70 exp
	c.send(13608, proto.Message{"id": 2})
	if r := c.waitFor(13608); r.Int("flag") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	st := &sevenState{}
	if _, err := e.st.LoadState(role.RID, sevenGoalModule, st); err != nil {
		t.Fatal(err)
	}
	st.Exp = 70
	if err := e.st.SaveState(role.RID, sevenGoalModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(13608, proto.Message{"id": 2})
	if r := c.waitFor(13608); r.Int("flag") != 1 {
		t.Fatalf("level reward: %v", r)
	}
	if r := c.waitFor(13607); len(r.Arr("reward_list")) != 1 {
		t.Fatalf("taken: %v", r)
	}
	c.send(13608, proto.Message{"id": 2})
	if r := c.waitFor(13608); r.Int("flag") != 0 {
		t.Fatalf("a level taken twice: %v", r)
	}
}

func TestSevenGoalDaysAndPeriods(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("sg2", "七日人二")
	base := sevenBase(role)
	e.setNow(base.AddDate(0, 0, 2))
	c.send(13604, proto.Message{})
	r := c.waitFor(13604)
	if r.Int("period") != 2 || r.Int("cur_day") != 3 {
		t.Fatalf("day 3: %v", r)
	}
	seen := map[int64]bool{}
	for _, g := range r.Arr("list") {
		seen[g.Int("id")] = true
	}
	if !seen[231] || seen[241] {
		t.Fatalf("goals of day 3 only up to day 3: %v", seen)
	}
	e.setNow(base.AddDate(0, 0, 8))
	c.send(13604, proto.Message{})
	if r := c.waitFor(13604); r.Int("period") != 3 || r.Int("cur_day") != 2 {
		t.Fatalf("the second week: %v", r)
	}
	e.setNow(base.AddDate(0, 0, 70))
	c.send(13604, proto.Message{})
	if r := c.waitFor(13604); r.Int("period") != 0 || len(r.Arr("list")) != 0 {
		t.Fatalf("after the last week: %v", r)
	}
}

func TestSevenGoalCountsEventsAndPushes(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("sg3", "七日人三")
	e.setNow(sevenBase(role))
	c.send(23201, proto.Message{"group_id": 300, "recruit_type": 1, "times": 1})
	c.waitFor(23201)
	c.send(13604, proto.Message{})
	var goal proto.Message
	for _, g := range c.waitFor(13604).Arr("list") {
		if g.Int("id") == 217 {
			goal = g
		}
	}
	if goal == nil || goal.Int("finish") != 1 {
		t.Fatalf("the summon before the list was opened is counted: %v", goal)
	}
	// a role that has the list open is told when a goal is ready
	c2, role2 := e.enter("sg4", "七日人四")
	e.setNow(sevenBase(role2))
	c2.send(13604, proto.Message{})
	c2.waitFor(13604)
	c2.send(23201, proto.Message{"group_id": 300, "recruit_type": 1, "times": 1})
	if r := c2.waitFor(13605); r.Arr("list")[0].Int("id") != 217 || r.Arr("list")[0].Int("finish") != 1 {
		t.Fatalf("push: %v", r)
	}
}
