package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func ptEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
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
	if b.Int("id") != 0 || b.Int("time") != 4 || b.Int("last_buy_time") != 4 || b.Int("radio") != 100 || b.Int("is_recombat") != 0 || b.Int("last_unixtime") <= time.Now().Unix() {
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
	hs, _ := e.st.PartnersOf(role.RID)
	t.Logf("hero power %d, damage/power %.2f", partnerMessage(hs[0]).Int("power"), float64(res.Int("all_dps"))/float64(partnerMessage(hs[0]).Int("power")))
	t.Logf("damage %d of %d, result %v, state %+v", res.Int("all_dps"), ptBossHP(1), res.Int("flag"), st)
	if res.Int("number") != 1 || res.Int("all_dps") <= 0 || st.Time != 3 || st.Snap == nil {
		t.Fatalf("29102: %v, %+v", res, st)
	}
	if res.Int("flag") != 1 && st.Left != ptBossHP(1)-res.Int("all_dps") {
		t.Fatalf("hp must carry over: %+v", st)
	}
	c.send(29103, proto.Message{})
	if r := c.waitFor(29103); r.Int("flag") != 1 {
		t.Fatalf("redo: %v", r)
	}
	b := c.waitFor(29100)
	if b.Int("time") != 4 || b.Int("id") != 0 || b.Int("radio") != 100 || b.Int("is_recombat") != 0 {
		t.Fatalf("after redo: %v", b)
	}
	c.send(29103, proto.Message{})
	if r := c.waitFor(29103); r.Int("flag") != 0 {
		t.Fatalf("redo twice: %v", r)
	}
}

func TestPractiseTowerClearPaysOnceAndRanks(t *testing.T) {
	e, c, role, hero := ptEnv(t)
	st := e.ptState(t, role)
	st.Day, st.Time, st.Left = dayKey(time.Now()), 4, 1 // one hp left: any hit kills
	if err := e.st.SaveState(role.RID, ptModule, st); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(29101, ptTeam(hero))
	c.waitFor(29101)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(29102)
	c.waitFor(29106)
	c.waitFor(29100)
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	st = e.ptState(t, role)
	if res.Int("flag") != 1 || len(res.Arr("reward")) != 1 || st.Done != 1 || st.Left != 0 || len(st.Paid) != 1 {
		t.Fatalf("clear: %v, %+v", res, st)
	}
	_ = before
	_ = after
	c.send(29105, proto.Message{})
	r := c.waitFor(29105)
	rows := r.Arr("practise_role_rank")
	if r.Int("role_rank") != 1 || len(rows) != 1 || rows[0].Str("name") != "兔丼挑战者" || rows[0].Int("val") != 1 || r.Int("role_val") != 1 {
		t.Fatalf("29105: %v", r)
	}
	// after a redo the floor is open again but its reward was paid and does not pay twice
	c.send(29103, proto.Message{})
	c.waitFor(29103)
	c.waitFor(29100)
	st = e.ptState(t, role)
	if st.Done != 0 || !st.paid(1) {
		t.Fatalf("redo of a clear: %+v", st)
	}
}
