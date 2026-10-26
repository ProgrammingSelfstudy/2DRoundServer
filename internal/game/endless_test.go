package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func endEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ue", "试炼者")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
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

func (e *env) endState(t *testing.T, role *store.Role) *endState {
	st := &endState{}
	if _, err := e.st.LoadState(role.RID, endlessModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func endTeam(hero int64, mode int64) proto.Message {
	return proto.Message{"type": mode, "formation_type": 1, "hallows_id": 0, "pos_info": []proto.Message{{"pos": 1, "owner_id": 0, "owner_srv_id": "", "id": hero}}}
}

func TestEndlessBaseAndGates(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ue", "新人")
	c.send(23901, endTeam(1, endOld))
	if r := c.waitFor(23901); r.Int("code") != 0 {
		t.Fatalf("below 25: %v", r)
	}
	e.st.SetRoleLevel(role.RID, 40)
	c.send(23900, proto.Message{})
	b := c.waitFor(23900)
	if b.Int("type") != 0 || b.Int("select_type") < 1 || b.Int("select_type") > 4 || b.Int("current_round") != 1 || b.Int("max_round") != 0 || b.Int("next_time") <= 0 || b.Int("next_time") > 3*86400 {
		t.Fatalf("23900: %v", b)
	}
	c.send(23901, endTeam(1, 1)) // the new modes need round 200 and level 90
	if r := c.waitFor(23901); r.Int("code") != 0 {
		t.Fatalf("a new mode too early: %v", r)
	}
	c.send(23901, endTeam(999, endOld)) // a hero that does not exist
	if r := c.waitFor(23901); r.Int("code") != 0 {
		t.Fatalf("no hero: %v", r)
	}
}

func TestEndlessRunFightsBuffsAndRewards(t *testing.T) {
	e, c, role, hero := endEnv(t)
	c.send(23901, endTeam(hero, endOld))
	if r := c.waitFor(23901); r.Int("code") != 1 {
		t.Fatalf("start: %v", r)
	}
	opts := c.waitFor(23910)
	if opts.Int("is_select") != 1 || len(opts.Arr("list")) != 3 || opts.Int("round") != 1 || len(opts.Arr("partner")) != 1 {
		t.Fatalf("23910: %v", opts)
	}
	c.send(23911, proto.Message{"buff_id": 99999}) // not offered
	if r := c.waitFor(23911); r.Int("code") != 0 {
		t.Fatalf("a buff that was not offered: %v", r)
	}
	pick := opts.Arr("list")[1].Int("buff_id")
	c.send(23911, proto.Message{"buff_id": pick})
	if r := c.waitFor(23911); r.Int("code") != 1 {
		t.Fatalf("choose: %v", r)
	}
	bd := c.waitFor(23902)
	if bd.Int("round") != 1 || len(bd.Arr("buff_list")) != 1 || bd.Arr("buff_list")[0].Int("id") != pick || bd.Int("rest_round") != 5 || bd.Int("type") != endOld {
		t.Fatalf("23902: %v", bd)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatEndless {
		t.Fatalf("intro: %v", m)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	c.settle()
	st := e.endState(t, role)
	if res.Int("result") != 1 {
		if st.Run.Active {
			t.Fatalf("a lost fight must end the run: %+v", st.Run)
		}
		t.Skip("the first round was lost")
	}
	m := st.mode(endOld)
	if m.Max != 1 || m.DayPass != 1 || !st.Run.Active || st.Run.Round != 2 || st.Run.Rest != 4 || st.Chosen != endOld {
		t.Fatalf("after round 1: %+v run=%+v chosen=%d", m, st.Run, st.Chosen)
	}
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] <= before[1] || len(res.Arr("item_rewards")) != 2 {
		t.Fatalf("a round pays: %v -> %v, %v", before[1], after[1], res)
	}
	// the rank
	c.send(23913, proto.Message{"type": endOld})
	rk := c.waitFor(23913)
	if rk.Int("my_idx") != 1 || len(rk.Arr("rank_list")) != 1 || rk.Arr("rank_list")[0].Int("val") != 1 {
		t.Fatalf("23913: %v", rk)
	}
	// leaving ends the run
	c.send(20008, proto.Message{})
	c.waitFor(20008)
	c.settle()
	if e.endState(t, role).Run.Active {
		t.Fatal("the run must end when the role leaves")
	}
	// another mode today is not allowed
	c.send(23901, endTeam(hero, 1))
	if r := c.waitFor(23901); r.Int("code") != 0 {
		t.Fatalf("a second mode: %v", r)
	}
}

func TestEndlessFirstRewardsAndSupportHeroes(t *testing.T) {
	e, c, role, hero := endEnv(t)
	c.send(23903, proto.Message{"type": endOld})
	if r := c.waitFor(23903); r.Int("id") != 1 || r.Int("status") != 0 || r.Int("max_id") != 300 {
		t.Fatalf("23903: %v", r)
	}
	c.send(23904, proto.Message{"id": 1, "type": endOld})
	if r := c.waitFor(23904); r.Int("code") != 0 {
		t.Fatalf("early first reward: %v", r)
	}
	c.settle()
	st := e.endState(t, role)
	st.mode(endOld).Max = 12
	e.st.SaveState(role.RID, endlessModule, st)
	c.send(23903, proto.Message{"type": endOld})
	if r := c.waitFor(23903); r.Int("status") != 1 {
		t.Fatalf("23903 after: %v", r)
	}
	c.send(23904, proto.Message{"id": 1, "type": endOld})
	if r := c.waitFor(23904); r.Int("code") != 1 {
		t.Fatalf("first reward: %v", r)
	}
	c.send(23904, proto.Message{"id": 1, "type": endOld})
	if r := c.waitFor(23904); r.Int("code") != 0 {
		t.Fatalf("first reward twice: %v", r)
	}
	c.send(23903, proto.Message{"type": endOld})
	if r := c.waitFor(23903); r.Int("id") != 2 || len(r.Arr("rewarded")) != 1 {
		t.Fatalf("next first reward: %v", r)
	}
	// support heroes: a friend lends one, the role hires it
	cb, other := e.enter("uf", "好友")
	if err := e.st.RequestFriend(other.RID, role.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(other.RID, role.RID, 50); err != nil {
		t.Fatal(err)
	}
	cb.send(11000, proto.Message{})
	cb.waitFor(11000)
	theirs, _ := e.st.PartnersOf(other.RID)
	cb.send(23908, proto.Message{"id": theirs[0].PartnerID})
	if r := cb.waitFor(23908); r.Int("code") != 1 {
		t.Fatalf("dispatch: %v", r)
	}
	cb.send(23905, proto.Message{})
	if l := cb.waitFor(23905).Arr("list"); len(l) != 1 {
		t.Fatalf("23905: %v", l)
	}
	c.send(23907, proto.Message{})
	fl := c.waitFor(23907).Arr("list")
	if len(fl) != 1 || fl[0].Int("rid") != other.RID || fl[0].Int("is_return") != 0 {
		t.Fatalf("23907: %v", fl)
	}
	c.send(23909, proto.Message{"rid": other.RID, "srv_id": other.SrvID, "id": theirs[0].PartnerID, "flag": 0})
	if r := c.waitFor(23909); r.Int("code") != 1 {
		t.Fatalf("hire: %v", r)
	}
	c.send(23906, proto.Message{})
	if l := c.waitFor(23906).Arr("list"); len(l) != 1 || l[0].Int("rid") != other.RID {
		t.Fatalf("23906: %v", l)
	}
	assets, _ := e.st.RoleAssets(other.RID)
	if assets[14] != 10 {
		t.Fatalf("the owner gets friend points: %v", assets[14])
	}
	c.send(23909, proto.Message{"rid": other.RID, "srv_id": other.SrvID, "id": theirs[0].PartnerID, "flag": 0}) // one a day
	if r := c.waitFor(23909); r.Int("code") != 0 {
		t.Fatalf("a second hire: %v", r)
	}
	// the hired hero can go along
	c.send(23901, proto.Message{"type": endOld, "formation_type": 1, "pos_info": []proto.Message{
		{"pos": 1, "owner_id": 0, "id": hero}, {"pos": 2, "owner_id": other.RID, "owner_srv_id": other.SrvID, "id": theirs[0].PartnerID}}})
	if r := c.waitFor(23901); r.Int("code") != 1 {
		t.Fatalf("start with a friend: %v", r)
	}
	if o := c.waitFor(23910); len(o.Arr("partner")) != 2 {
		t.Fatalf("team: %v", o)
	}
}
