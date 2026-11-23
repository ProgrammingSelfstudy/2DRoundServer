package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func ptEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newDungeonEnv(t)
	e.setNow(actNow)
	c, role := e.enter("up", "兔丼挑战者")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	return e, c, role, heroes[0].PartnerID
}

func (e *env) ptState(t *testing.T, role *store.Role) *ptState {
	st := &ptState{}
	if _, err := e.st.LoadState(role.RID, ptModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func ptTeam(hero int64) proto.Message {
	return proto.Message{"id": 1, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero}}, "hallows_id": 0}
}

func TestPractiseTowerInfoAndBuy(t *testing.T) {
	e, c, role, _ := ptEnv(t)
	c.send(29100, proto.Message{})
	b := c.waitFor(29100)
	if b.Int("id") != 0 || b.Int("time") != 4 || b.Int("last_buy_time") != 4 || b.Int("radio") != 100 || b.Int("is_recombat") != 0 || b.Int("last_unixtime") <= actNow.Unix() {
		t.Fatalf("29100: %v", b)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 1000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(29104, proto.Message{})
	if r := c.waitFor(29104); r.Int("flag") != 1 {
		t.Fatalf("buy: %v", r)
	}
	b = c.waitFor(29100)
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	if b.Int("time") != 5 || b.Int("last_buy_time") != 3 || after[3] != before[3]-100 {
		t.Fatalf("buying: %v, diamonds %v -> %v", b, before[3], after[3])
	}
	st := e.ptState(t, role)
	st.Bought = 4
	if err := e.st.SaveState(role.RID, ptModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(29104, proto.Message{})
	if r := c.waitFor(29104); r.Int("flag") != 0 {
		t.Fatalf("buy over the limit: %v", r)
	}
}

func TestPractiseTowerFightCarriesHPAndRedoUndoes(t *testing.T) {
	e, c, role, hero := ptEnv(t)
	c.send(29101, ptTeam(hero))
	if r := c.waitFor(29101); r.Int("flag") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatPT {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(29102)
	c.waitFor(29100)
	c.settle()
	st := e.ptState(t, role)
	if res.Int("number") != 1 || res.Int("flag") != 2 || res.Int("all_dps") <= 0 || st.Time != 3 || st.Snap == nil {
		t.Fatalf("29102: %v, %+v", res, st)
	}
	if st.Left != ptBossHP(1)-res.Int("all_dps") {
		t.Fatalf("hp must carry over: %+v", st)
	}
	if err := e.st.Exchange(role.RID, nil, nil, map[int64]int64{3: 1000}, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(29104, proto.Message{})
	if r := c.waitFor(29104); r.Int("flag") != 1 {
		t.Fatalf("buy after combat: %v", r)
	}
	if b := c.waitFor(29100); b.Int("time") != 4 {
		t.Fatalf("bought attempt missing: %v", b)
	}
	c.send(29103, proto.Message{})
	if r := c.waitFor(29103); r.Int("flag") != 1 {
		t.Fatalf("redo: %v", r)
	}
	b := c.waitFor(29100)
	if b.Int("time") != 5 || b.Int("id") != 0 || b.Int("radio") != 100 || b.Int("is_recombat") != 0 || b.Int("last_buy_time") != 3 {
		t.Fatalf("after redo: %v", b)
	}
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	st = e.ptState(t, role)
	if st.Time != 5 || st.Bought != 1 || st.Left != 0 || after[3] != before[3]-100 {
		t.Fatalf("redo must not undo a purchase: %+v, diamonds %d -> %d", st, before[3], after[3])
	}
	c.send(29103, proto.Message{})
	if r := c.waitFor(29103); r.Int("flag") != 0 {
		t.Fatalf("redo twice: %v", r)
	}
}

func TestPractiseTowerClearPaysOnceAndRanks(t *testing.T) {
	e, c, role, hero := ptEnv(t)
	st := e.ptState(t, role)
	st.Day, st.Time, st.Left = dayKey(actNow), 4, 1
	if err := e.st.SaveState(role.RID, ptModule, st); err != nil {
		t.Fatal(err)
	}
	clear := func(floor int64) proto.Message {
		t.Helper()
		team := ptTeam(hero)
		team["id"] = floor
		c.send(29101, team)
		if r := c.waitFor(29101); r.Int("flag") != 1 {
			t.Fatalf("start floor %d: %v", floor, r)
		}
		c.waitFor(20027)
		c.send(20062, proto.Message{})
		c.waitFor(20062)
		res := c.waitFor(29102)
		if res.Int("flag") != 1 || res.Int("number") != floor {
			t.Fatalf("clear floor %d: %v", floor, res)
		}
		c.waitFor(29106)
		c.waitFor(29100)
		c.settle()
		return res
	}
	res := clear(1)
	st = e.ptState(t, role)
	if len(res.Arr("reward")) != 1 || st.Done != 1 || st.Left != 0 || len(st.Paid) != 1 || st.Power <= 0 || st.At != actNow.Unix() {
		t.Fatalf("clear: %v, %+v", res, st)
	}
	checkRank := func(st *ptState) {
		t.Helper()
		c.send(29105, proto.Message{})
		r := c.waitFor(29105)
		rows := r.Arr("practise_role_rank")
		if r.Int("role_rank") != 1 || len(rows) != 1 || rows[0].Str("name") != role.Name || rows[0].Int("val") != st.Done || r.Int("role_val") != st.Done || rows[0].Int("unixtime") != st.At || rows[0].Int("power") != st.Power || r.Int("role_unixtime") != st.At {
			t.Fatalf("rank before/after redo: %v, want %+v", r, st)
		}
	}
	checkRank(st)
	prior := *st
	st.Left = 1
	if err := e.st.SaveState(role.RID, ptModule, st); err != nil {
		t.Fatal(err)
	}
	partner, err := e.st.PartnerByID(role.RID, hero)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpgradePartner(role.RID, hero, partner.Lev, partner.BreakLev, partner.Lev+5, partner.BreakLev, nil, nil); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Minute)
	if res := clear(2); len(res.Arr("reward")) == 0 {
		t.Fatalf("first floor 2 clear: %v", res)
	}
	higher := e.ptState(t, role)
	if higher.At <= prior.At || higher.Power <= prior.Power {
		t.Fatalf("second clear must have new rank metadata: %+v vs %+v", higher, prior)
	}
	checkRank(higher)
	c.send(29103, proto.Message{})
	if r := c.waitFor(29103); r.Int("flag") != 1 {
		t.Fatalf("redo: %v", r)
	}
	c.waitFor(29100)
	c.settle()
	st = e.ptState(t, role)
	if st.Done != prior.Done || st.Left != 1 || st.Time != prior.Time || st.At != prior.At || st.Power != prior.Power || !st.paid(1) || !st.paid(2) {
		t.Fatalf("redo of a clear: %+v", st)
	}
	checkRank(&prior)
	if res := clear(2); len(res.Arr("reward")) != 0 {
		t.Fatalf("duplicate first clear reward: %v", res)
	}
}
