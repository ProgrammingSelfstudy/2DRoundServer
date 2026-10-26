package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func leEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("up", "试炼者")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	c.send(11212, proto.Message{"type": formLE, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": heroes[0].PartnerID}}, "hallows_id": 0})
	if r := c.waitFor(11212); r.Int("code") != 1 {
		t.Fatalf("11212: %v", r)
	}
	return e, c, role, heroes[0].PartnerID
}

func (e *env) leState(t *testing.T, role *store.Role) *leState {
	st := &leState{}
	if _, err := e.st.LoadState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLimitExerciseInfoAndBuy(t *testing.T) {
	e, c, role, _ := leEnv(t)
	c.send(25410, proto.Message{})
	b := c.waitFor(25410)
	if b.Int("difficulty") != 1 || b.Int("order") != 1 || b.Int("order_type") != 1 || b.Int("count") != 2 || b.Int("buy_count") != 0 || b.Int("hp_per") != 100 || b.Int("status") != 0 || b.Int("endtime") <= time.Now().Unix() {
		t.Fatalf("25410: %v", b)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 1000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		c.send(25411, proto.Message{})
		if r := c.waitFor(25411); r.Int("code") != 1 || r.Int("count") != 2+int64(i) || r.Int("buy_count") != int64(i) {
			t.Fatalf("buy %d: %v", i, r)
		}
	}
	c.send(25411, proto.Message{})
	if r := c.waitFor(25411); r.Int("code") != 0 {
		t.Fatalf("buy over the limit: %v", r)
	}
}

func TestLimitExerciseFightCarriesHPAndClearCountsHeroes(t *testing.T) {
	e, c, role, hero := leEnv(t)
	c.send(25413, proto.Message{})
	if r := c.waitFor(25413); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatLE || len(m.Arr("extra_args")) != 4 {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	info := c.waitFor(25410)
	c.settle()
	st := e.leState(t, role)
	if st.Count != 1 || st.Fights != 1 {
		t.Fatalf("state: %+v", st)
	}
	if res.Int("result") != 1 && (st.Left <= 0 || info.Int("hp_per") >= 100) {
		t.Fatalf("hp must carry over: %+v %v", st, info)
	}
	// one hp left: any hit clears the level, and the hero is counted
	st.Left = 1
	if err := e.st.SaveState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(25413, proto.Message{})
	c.waitFor(25413)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res = c.waitFor(20006)
	info = c.waitFor(25410)
	c.settle()
	st = e.leState(t, role)
	if res.Int("result") != 1 || st.Order != 2 || st.Bosses != 1 || st.Used[hero] != 1 || info.Int("order") != 2 || info.Int("round_boss") != 1 {
		t.Fatalf("clear: %v %+v %v", res, st, info)
	}
	c.send(25414, proto.Message{})
	if r := c.waitFor(25414); len(r.Arr("p_list")) != 1 || r.Arr("p_list")[0].Int("id") != hero || r.Arr("p_list")[0].Int("count") != 1 {
		t.Fatalf("25414: %v", r)
	}
	// no attempt left
	c.send(25413, proto.Message{})
	if r := c.waitFor(25413); r.Int("code") != 0 {
		t.Fatalf("fight without attempts: %v", r)
	}
}

func TestLimitExerciseHeroLimitAndBox(t *testing.T) {
	e, c, role, hero := leEnv(t)
	st := e.leState(t, role)
	c.send(25410, proto.Message{})
	c.waitFor(25410)
	st = e.leState(t, role)
	st.Used = map[int64]int64{hero: leCfg.num("partner_num")}
	if err := e.st.SaveState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(25413, proto.Message{})
	if r := c.waitFor(25413); r.Int("code") != 0 {
		t.Fatalf("a used-up hero must not fight: %v", r)
	}
	c.send(25412, proto.Message{})
	if r := c.waitFor(25412); r.Int("code") != 0 {
		t.Fatalf("box before the clear: %v", r)
	}
	st.Order, st.Used = leLevels+1, nil
	if err := e.st.SaveState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(25410, proto.Message{})
	if b := c.waitFor(25410); b.Int("status") != 1 || b.Int("order") != leLevels {
		t.Fatalf("25410 after the clear: %v", b)
	}
	c.send(25412, proto.Message{})
	if r := c.waitFor(25412); r.Int("code") != 1 {
		t.Fatalf("box: %v", r)
	}
	c.send(25412, proto.Message{})
	if r := c.waitFor(25412); r.Int("code") != 0 {
		t.Fatalf("box twice: %v", r)
	}
}

func TestLimitExerciseWeekTurnMailsBoxAndMovesDifficulty(t *testing.T) {
	e, c, role, _ := leEnv(t)
	c.send(25410, proto.Message{})
	c.waitFor(25410)
	st := e.leState(t, role)
	st.Week, st.Order, st.Diff = homeWeek(time.Now())-7, 11, 2 // ten levels cleared last week, box not taken
	if err := e.st.SaveState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(25410, proto.Message{})
	b := c.waitFor(25410)
	if b.Int("difficulty") != 2 || b.Int("order") != 1 || b.Int("status") != 0 {
		t.Fatalf("new week: %v", b)
	}
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mails {
		if m.Subject == "试炼之境结算奖励" {
			found = len(m.Attachments) > 0
		}
	}
	if !found {
		t.Fatalf("the settle mail is missing: %v", mails)
	}
	// a week with the down area not cleared goes a difficulty down
	st = e.leState(t, role)
	st.Week, st.Order, st.Diff = homeWeek(time.Now())-7, 3, 2
	if err := e.st.SaveState(role.RID, leModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(25410, proto.Message{})
	if b = c.waitFor(25410); b.Int("difficulty") != 1 {
		t.Fatalf("down: %v", b)
	}
}
