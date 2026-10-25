package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func heavenEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("uh", "天界客")
	if err := e.st.SetRoleLevel(role.RID, 110); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	return e, c, role, heroes[0].PartnerID
}

func (e *env) heavenState(t *testing.T, role *store.Role) *heavenState {
	st := &heavenState{}
	if _, err := e.st.LoadState(role.RID, heavenModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func heavenForm1(hero int64) proto.Message {
	return proto.Message{"type": 1, "formations": []proto.Message{{"order": 1, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero}}, "hallows_id": 0}}}
}

func TestHeavenChaptersAndCustoms(t *testing.T) {
	_, c, _, _ := heavenEnv(t)
	c.send(25200, proto.Message{})
	r := c.waitFor(25200)
	if r.Int("count") != 2 || r.Int("buy_count") != 0 || len(r.Arr("chapter_info")) != 5 {
		t.Fatalf("25200: count=%d chapters=%d", r.Int("count"), len(r.Arr("chapter_info")))
	}
	c.send(25201, proto.Message{"id": 1})
	ch := c.waitFor(25201)
	if ch.Int("id") != 1 || len(ch.Arr("dun_info")) != 10 || ch.Arr("dun_info")[0].Int("state") != 1 || ch.Arr("dun_info")[1].Int("state") != 0 || len(ch.Arr("dun_info")[0].Arr("star_info")) != 3 {
		t.Fatalf("25201: %v", ch)
	}
	c.send(25201, proto.Message{"id": 6}) // wants 20 stars in chapter 4
	c.send(25200, proto.Message{})
	c.waitFor(25200)
}

func TestHeavenFightStarsSweepAndAwards(t *testing.T) {
	e, c, role, hero := heavenEnv(t)
	c.send(25205, proto.Message{"id": 1, "order_id": 1}) // no formation yet
	if r := c.waitFor(25205); r.Int("code") != 0 {
		t.Fatalf("fought without a formation: %v", r)
	}
	c.send(25211, heavenForm1(999)) // not a hero of the role
	if r := c.waitFor(25211); r.Int("code") != 0 {
		t.Fatalf("a hero that is not ours: %v", r)
	}
	c.send(25211, heavenForm1(hero))
	if r := c.waitFor(25211); r.Int("code") != 1 {
		t.Fatalf("save: %v", r)
	}
	c.send(25210, proto.Message{"type": 1})
	if f := c.waitFor(25210); len(f.Arr("formations")) != 1 || f.Arr("formations")[0].Arr("pos_info")[0].Int("id") != hero {
		t.Fatalf("25210: %v", f)
	}
	c.send(25205, proto.Message{"id": 1, "order_id": 2}) // the first custom is not cleared
	if r := c.waitFor(25205); r.Int("code") != 0 {
		t.Fatalf("skipped a custom: %v", r)
	}
	c.send(25205, proto.Message{"id": 1, "order_id": 1})
	if r := c.waitFor(25205); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatHeaven {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(25206)
	if res.Int("id") != 1 || res.Int("order_id") != 1 || len(res.Arr("star_info")) != 3 {
		t.Fatalf("25206: %v", res)
	}
	c.settle()
	st := e.heavenState(t, role)
	if res.Int("result") != 1 {
		if st.Used != 0 {
			t.Fatalf("a loss must not cost a challenge: %+v", st)
		}
		t.Skip("the fight was lost")
	}
	if !st.cleared(1, 1) || st.Used != 1 || st.MaxDun != 10001 || res.Arr("star_info")[0].Int("state") != 1 || len(res.Arr("award")) < 3 {
		t.Fatalf("after a win: %+v %v", st, res)
	}
	c.send(25201, proto.Message{"id": 1})
	if ch := c.waitFor(25201); ch.Arr("dun_info")[0].Int("state") != 2 || ch.Arr("dun_info")[1].Int("state") != 1 {
		t.Fatalf("customs after: %v", ch)
	}
	// sweep needs all three stars
	c.send(25208, proto.Message{"id": 1, "order_id": 1})
	if r := c.waitFor(25208); st.Stars["1:1"] != 7 && r.Int("code") != 0 {
		t.Fatalf("swept without three stars: %v", r)
	}
	c.settle()
	st = e.heavenState(t, role)
	st.Stars["1:1"] = 7
	st.Stars["1:2"] = 7
	st.Used = 0
	e.st.SaveState(role.RID, heavenModule, st)
	before, _ := e.st.RoleAssets(role.RID)
	c.send(25208, proto.Message{"id": 1, "order_id": 1})
	if r := c.waitFor(25208); r.Int("code") != 1 || r.Int("count") != 1 {
		t.Fatalf("sweep: %v", r)
	}
	after, _ := e.st.RoleAssets(role.RID)
	if after[28] <= before[28] {
		t.Fatalf("a sweep pays: %v -> %v", before[28], after[28])
	}
	// the day's two challenges, then gems
	c.send(25208, proto.Message{"id": 1, "order_id": 1})
	c.waitFor(25208)
	c.send(25208, proto.Message{"id": 1, "order_id": 1})
	if r := c.waitFor(25208); r.Int("code") != 0 {
		t.Fatalf("a third challenge: %v", r)
	}
	c.send(25207, proto.Message{})
	if r := c.waitFor(25207); r.Int("code") != 0 {
		t.Fatalf("bought without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 1000})
	c.send(25207, proto.Message{})
	if r := c.waitFor(25207); r.Int("code") != 1 || r.Int("count") != 1 || r.Int("buy_count") != 1 {
		t.Fatalf("buy: %v", r)
	}
	// star rewards of a chapter: 10 stars
	c.settle()
	st = e.heavenState(t, role)
	st.Stars["1:3"] = 7
	st.Stars["1:4"] = 7
	e.st.SaveState(role.RID, heavenModule, st)
	c.send(25215, proto.Message{"id": 1, "award_id": 1})
	if r := c.waitFor(25215); r.Int("code") != 1 || r.Arr("award_info")[0].Int("flag") != 2 {
		t.Fatalf("star award: %v", r)
	}
	c.send(25215, proto.Message{"id": 1, "award_id": 1})
	if r := c.waitFor(25215); r.Int("code") != 0 {
		t.Fatalf("star award twice: %v", r)
	}
	c.send(25215, proto.Message{"id": 1, "award_id": 2}) // 20 stars
	if r := c.waitFor(25215); r.Int("code") != 0 {
		t.Fatalf("star award early: %v", r)
	}
}

func TestHeavenGoalsAreEvaluated(t *testing.T) {
	f := heavenFacts{won: true, camps: map[int64]int64{1: 2}, careers: map[int64]int64{3: 1}, formType: 3, rounds: 9, dead: 0, hpPerMill: 750}
	for id, want := range map[int64]bool{1001: true, 2001: true, 2011: true, 2021: false, 2101: true, 2123: true, 2403: true, 2406: false, 3003: true, 3001: false, 3101: true, 3203: true, 3204: false, 2222: false, 2391: true} {
		if got := heavenGoalMet(id, f); got != want {
			t.Errorf("goal %d: %v, want %v", id, got, want)
		}
	}
	f.won = false
	if heavenGoalMet(1001, f) {
		t.Error("a lost fight met the win goal")
	}
}

func TestHeavenDial(t *testing.T) {
	e, c, role, _ := heavenEnv(t)
	c.send(25219, proto.Message{})
	l := c.waitFor(25219).Arr("recruit_list")
	if len(l) != 2 || l[0].Int("group_id") != 1 || l[0].Int("free_times") != 1 || l[0].Int("is_open") != 1 || l[1].Int("is_open") != 0 {
		t.Fatalf("25219: %v", l)
	}
	c.send(25217, proto.Message{"group_id": 2, "times": 1, "recruit_type": 1}) // the second ship is closed
	if r := c.waitFor(25217); len(r.Arr("rewards")) != 0 {
		t.Fatalf("closed ship: %v", r)
	}
	c.send(25217, proto.Message{"group_id": 1, "times": 1, "recruit_type": 1})
	r := c.waitFor(25217)
	if len(r.Arr("rewards")) != 1 || r.Int("group_id") != 1 {
		t.Fatalf("free pull: %v", r)
	}
	c.send(25217, proto.Message{"group_id": 1, "times": 1, "recruit_type": 1}) // used
	if r := c.waitFor(25217); len(r.Arr("rewards")) != 0 {
		t.Fatalf("a second free pull: %v", r)
	}
	c.send(25217, proto.Message{"group_id": 1, "times": 10, "recruit_type": 2}) // no tickets
	if r := c.waitFor(25217); len(r.Arr("rewards")) != 0 {
		t.Fatalf("pulled without tickets: %v", r)
	}
	e.addItems(t, role, map[int64]int64{17210: 10})
	e.grantAssets(c, role, map[int64]int64{3: 5000})
	c.send(25217, proto.Message{"group_id": 1, "times": 10, "recruit_type": 2})
	if r := c.waitFor(25217); len(r.Arr("rewards")) != 10 {
		t.Fatalf("ten pulls: %v", r)
	}
	c.send(25217, proto.Message{"group_id": 1, "times": 10, "recruit_type": 3})
	if r := c.waitFor(25217); len(r.Arr("rewards")) != 10 {
		t.Fatalf("ten pulls with gems: %v", r)
	}
	c.settle()
	if d := e.heavenState(t, role).Dial["1"]; d.Pulls != 21 {
		t.Fatalf("pulls: %+v", d)
	}
	c.send(25231, proto.Message{"group_id": 1, "id": 1}) // 40 pulls
	if r := c.waitFor(25231); r.Int("flag") != 0 {
		t.Fatalf("progress award early: %v", r)
	}
	st := e.heavenState(t, role)
	st.Dial["1"].Pulls = 45
	e.st.SaveState(role.RID, heavenModule, st)
	c.send(25231, proto.Message{"group_id": 1, "id": 1})
	if r := c.waitFor(25231); r.Int("flag") != 1 {
		t.Fatalf("progress award: %v", r)
	}
	c.send(25231, proto.Message{"group_id": 1, "id": 1})
	if r := c.waitFor(25231); r.Int("flag") != 0 {
		t.Fatalf("progress award twice: %v", r)
	}
	c.send(25230, proto.Message{"group_id": 1, "lucky_holy_eqm": []proto.Message{{"pos": 1, "lucky_holy_eqm": 101}}})
	if r := c.waitFor(25230); r.Int("flag") != 1 {
		t.Fatalf("wish: %v", r)
	}
	c.send(25230, proto.Message{"group_id": 1, "lucky_holy_eqm": []proto.Message{{"pos": 1, "lucky_holy_eqm": 99999}}})
	if r := c.waitFor(25230); r.Int("flag") != 0 {
		t.Fatalf("bad wish: %v", r)
	}
	c.send(25218, proto.Message{"type": 2, "group_id": 1})
	c.waitFor(25218)
	c.send(25232, proto.Message{})
	if r := c.waitFor(25232); r.Int("score") != 0 {
		t.Fatalf("25232: %v", r)
	}
}
