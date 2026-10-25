package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func monoEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("um", "火祭者")
	if err := e.st.SetRoleLevel(role.RID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "火祭会", LeaderRID: role.RID}, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{})
	heroes := c.waitFor(11000).Arr("partners")
	return e, c, role, heroes[0].Int("partner_id")
}

func (e *env) monoState(t *testing.T, role *store.Role) *monoState {
	st := &monoState{}
	if _, err := e.st.LoadState(role.RID, monoModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (e *env) monoSave(t *testing.T, role *store.Role, st *monoState) {
	if err := e.st.SaveState(role.RID, monoModule, st); err != nil {
		t.Fatal(err)
	}
}

func TestMonopolyInfoAndEnter(t *testing.T) {
	e, c, role, _ := monoEnv(t)
	c.send(27400, proto.Message{})
	b := c.waitFor(27400)
	st := b.Arr("stage")
	if b.Int("flag") != 1 || len(st) != 4 || st[0].Int("lock") != 0 || st[1].Int("lock") != 1 {
		t.Fatalf("27400: %v", b)
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[monoDice] != 10 || items[monoSecret] != 3 || items[monoTicket] != 3 {
		t.Fatalf("allowance: %v", items)
	}
	c.send(27401, proto.Message{"id": 2})
	if r := c.waitFor(27401); r.Int("flag") != 0 {
		t.Fatalf("locked map: %v", r)
	}
	c.send(27401, proto.Message{"id": 1})
	r := c.waitFor(27401)
	if r.Int("flag") != 1 || r.Int("pos") != 1 || len(r.Arr("events")) < 20 || r.Int("map_id") < 1 || r.Int("map_id") > 3 {
		t.Fatalf("27401: %v", r)
	}
	if d := c.waitFor(27406); d.Int("type") != 1 {
		t.Fatalf("27406: %v", d)
	}
	c.send(27407, proto.Message{"type": 1, "choice": 1})
	if r := c.waitFor(27407); r.Int("flag") != 1 {
		t.Fatalf("27407: %v", r)
	}
	c.send(27407, proto.Message{"type": 1, "choice": 1})
	if r := c.waitFor(27407); r.Int("flag") != 0 {
		t.Fatalf("story reward twice: %v", r)
	}
}

// land puts the role on a cell holding the event, as if a step had gone there.
func (e *env) monoPlace(t *testing.T, c *client, role *store.Role, evt int64) {
	c.settle() // the server saves after it answers
	st := e.monoState(t, role)
	ms := st.mapOf(1)
	for _, ev := range ms.Events {
		if ev.Type == evt {
			ms.Pos = ev.Pos
		}
	}
	ms.Now = evt
	ms.Pct = 600
	e.monoSave(t, role, st)
}

func TestMonopolyMoveCostsAndExplores(t *testing.T) {
	e, c, role, _ := monoEnv(t)
	c.send(27400, proto.Message{})
	c.waitFor(27400)
	c.send(27401, proto.Message{"id": 1})
	c.waitFor(27401)
	c.waitFor(27406)
	c.send(27403, proto.Message{"type": 2, "num": 9})
	if r := c.waitFor(27403); r.Int("flag") != 0 {
		t.Fatalf("a step out of range: %v", r)
	}
	c.send(27403, proto.Message{"type": 1, "num": 0})
	r := c.waitFor(27403)
	if r.Int("flag") != 1 || r.Int("num") < 1 || r.Int("num") > 4 || r.Int("pos") != 1+r.Int("num") || r.Int("develop") != 1 || r.Int("guild_develop") != 1 || r.Int("now_type") == 0 {
		t.Fatalf("27403: %v", r)
	}
	c.settle()
	items, _ := e.st.RoleItems(role.RID)
	if items[monoDice] != 9 {
		t.Fatalf("the machine is spent: %v", items)
	}
	c.send(27403, proto.Message{"type": 1, "num": 0}) // the cell is not settled yet
	if r := c.waitFor(27403); r.Int("flag") != 0 {
		t.Fatalf("a step on an open event: %v", r)
	}
	c.send(27404, proto.Message{"args": []proto.Message{}})
	if r := c.waitFor(27404); r.Int("flag") == 1 {
		return
	}
}

func TestMonopolyEvents(t *testing.T) {
	e, c, role, _ := monoEnv(t)
	c.send(27400, proto.Message{})
	c.waitFor(27400)
	c.send(27401, proto.Message{"id": 1})
	c.waitFor(27401)
	c.waitFor(27406)

	e.monoPlace(t, c, role, evAward)
	c.send(27404, proto.Message{"args": []proto.Message{}})
	r := c.waitFor(27404)
	push := c.waitFor(27409)
	if r.Int("flag") != 1 || r.Int("type") != evNormal || len(push.Arr("reward")) != 2 {
		t.Fatalf("award: %v %v", r, push)
	}
	if items, _ := e.st.RoleItems(role.RID); items[monoItem] != 10 {
		t.Fatalf("coins: %v", items)
	}

	e.monoPlace(t, c, role, evAdvance)
	c.send(27404, proto.Message{"args": []proto.Message{}})
	c.waitFor(27404)
	if p := c.waitFor(27409); len(p.Arr("buffs")) != 1 || p.Arr("buffs")[0].Int("type") != bfAdvance {
		t.Fatalf("advance: %v", p)
	}
	c.send(27408, proto.Message{"id": 1})
	if b := c.waitFor(27408); len(b.Arr("buffs")) != 1 || b.Arr("buffs")[0].Int("num") != 1 {
		t.Fatalf("27408: %v", b)
	}

	e.monoPlace(t, c, role, evMorra)
	c.send(27404, proto.Message{"args": []proto.Message{}}) // no hand
	if r := c.waitFor(27404); r.Int("flag") != 0 {
		t.Fatalf("morra without a hand: %v", r)
	}
	c.send(27404, proto.Message{"args": []proto.Message{{"type": 4, "arg1": 1, "arg2": 0}}})
	m := c.waitFor(27405)
	if m.Int("choice") != 1 || m.Int("system_choice") < 1 || m.Int("ret") != monoMorra(1, m.Int("system_choice")) {
		t.Fatalf("27405: %v", m)
	}
	c.waitFor(27404)

	e.monoPlace(t, c, role, evFlag)
	c.send(27404, proto.Message{"args": []proto.Message{{"type": 6, "arg1": 3, "arg2": 0}}})
	c.waitFor(27404)
	c.settle()
	if st := e.monoState(t, role); st.mapOf(1).Flag != 3 || st.mapOf(1).Now != 0 {
		t.Fatalf("flag: %+v", st.mapOf(1))
	}
}

func TestMonopolyGuardFight(t *testing.T) {
	e, c, role, hero := monoEnv(t)
	c.send(27400, proto.Message{})
	c.waitFor(27400)
	c.send(27401, proto.Message{"id": 1})
	c.waitFor(27401)
	c.waitFor(27406)
	e.monoPlace(t, c, role, evTrap)
	c.send(27410, proto.Message{"id": 1})
	g := c.waitFor(27410)
	if len(g.Arr("guards")) == 0 || g.Int("power") <= 0 {
		t.Fatalf("27410: %v", g)
	}
	c.send(27404, proto.Message{"args": []proto.Message{}}) // no team
	if r := c.waitFor(27404); r.Int("flag") != 0 {
		t.Fatalf("fight without a team: %v", r)
	}
	c.send(27404, proto.Message{"args": []proto.Message{{"type": 2, "arg1": 1, "arg2": hero}, {"type": 1, "arg1": 1, "arg2": 0}, {"type": 3, "arg1": 0, "arg2": 0}}})
	if m := c.waitFor(20027); m.Int("combat_type") != 101 {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	r := c.waitFor(27404)
	if r.Int("flag") != 1 {
		t.Fatalf("settle: %v", r)
	}
	c.settle()
	if st := e.monoState(t, role); st.mapOf(1).Now != 0 {
		t.Fatalf("event left open: %+v", st.mapOf(1))
	}
	if items, _ := e.st.RoleItems(role.RID); items[monoItem] == 0 {
		t.Fatalf("no coins after a guard: %v", items)
	}
}

func TestMonopolyEndStartsNewLapAndBuffsGuild(t *testing.T) {
	e, c, role, _ := monoEnv(t)
	c.send(27400, proto.Message{})
	c.waitFor(27400)
	c.send(27401, proto.Message{"id": 1})
	c.waitFor(27401)
	c.waitFor(27406)
	e.monoPlace(t, c, role, evEnd)
	c.send(27404, proto.Message{"args": []proto.Message{}})
	c.waitFor(27504)
	if r := c.waitFor(27404); r.Int("type") != evEnd {
		t.Fatalf("end cell: %v", r)
	}
	c.waitFor(27409)
	c.settle()
	if st := e.monoState(t, role); st.mapOf(1).Lap != 1 {
		t.Fatalf("lap: %+v", st.mapOf(1))
	}
	c.send(27504, proto.Message{})
	b := c.waitFor(27504)
	if b.Arr("buffs")[0].Int("val") != 3*monoAtkUnit || b.Arr("buffs")[1].Int("val") != 3*monoHPUnit {
		t.Fatalf("27504: %v", b)
	}
}

func TestMonopolyBossFightAndRanks(t *testing.T) {
	e, c, role, hero := monoEnv(t)
	c.send(27500, proto.Message{"id": 1})
	b := c.waitFor(27500)
	c.waitFor(27505)
	list := b.Arr("boss_list")
	if len(list) != 4 || list[0].Int("status") != 1 || list[0].Int("max_hp") != monoBossHP(1, 1) {
		t.Fatalf("27500: %v", b)
	}
	team := []proto.Message{{"pos": 1, "id": hero}}
	c.send(27501, proto.Message{"id": 1, "boss_id": 1, "formation_type": 1, "pos_info": team, "hallows_id": 0})
	if r := c.waitFor(27501); r.Int("flag") != 0 {
		t.Fatalf("locked boss: %v", r)
	}
	g, _, err := e.st.GuildOfRole(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	mg := &monoGuild{}
	mg.Dev[1] = 400
	if err := e.st.SaveGlobal(g.SrvID, monoGuildKey(g.GID), mg); err != nil {
		t.Fatal(err)
	}
	c.send(27400, proto.Message{}) // the allowance gives the tickets
	c.waitFor(27400)
	c.send(27501, proto.Message{"id": 1, "boss_id": 1, "formation_type": 1, "pos_info": team, "hallows_id": 0})
	if r := c.waitFor(27501); r.Int("flag") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatMonopolyBoss {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(27506)
	dmg := res.Int("all_dps")
	if res.Int("boss_id") != 1 || dmg <= 0 || len(res.Arr("award_list")) == 0 {
		t.Fatalf("27506: %v", res)
	}
	after := c.waitFor(27500)
	if hp := after.Arr("boss_list")[0].Int("hp"); hp != monoBossHP(1, 1)-dmg && after.Arr("boss_list")[0].Int("status") != 3 {
		t.Fatalf("hp after: %v (damage %d)", after, dmg)
	}
	c.send(27502, proto.Message{"id": 1})
	if r := c.waitFor(27502); len(r.Arr("rank_list")) != 1 || r.Arr("rank_list")[0].Int("dps") != dmg || r.Arr("rank_list")[0].Int("rank") != 1 {
		t.Fatalf("27502: %v", r)
	}
	c.send(27503, proto.Message{"id": 1, "num": 1})
	if r := c.waitFor(27503); len(r.Arr("guild_stage_rank")) != 1 || r.Int("r_rank") != 1 || r.Int("r_dps") != dmg {
		t.Fatalf("27503: %v", r)
	}
}
