package game

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func enterArenaH5(t *testing.T, e *env, name string, role *store.Role) (*client, *store.Role) {
	t.Helper()
	account, err := e.st.FindOrCreateAccount(store.PlatformH5, name)
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.IssueToken(account.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if role == nil {
		role, err = e.st.CreateRole(account.ID, "symlf_1", name, 0, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.SetRoleLevel(role.RID, arenaLimitLev); err != nil {
			t.Fatal(err)
		}
	}
	c := e.dial(true)
	c.send(1164, loginArgs(name, token))
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("account login: %v", r)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if r := c.recv(10192); r.Int("code") != 1 {
		t.Fatalf("enter: %v", r)
	}
	return c, role
}

func arenaWireRounds(t *testing.T, c *client, result uint16) (proto.Message, []proto.Message) {
	t.Helper()
	c.send(20001, proto.Message{})
	c.waitFor(20001)
	var plays []proto.Message
	for i := 0; i < 120; i++ {
		cmd, msg := c.recvNext()
		switch cmd {
		case 20002:
			c.send(20019, proto.Message{})
			plays = append(plays, c.waitFor(20004))
			c.send(20005, proto.Message{})
		case result:
			return msg, plays
		}
	}
	t.Fatal("arena playback did not finish")
	return nil, nil
}

func TestArenaSavedDefenseAndReplayForBothPlayers(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, attacker := enterArenaH5(t, e, "arena-atk", nil)
	d, defender := enterArenaH5(t, e, "arena-def", nil)
	d.send(20200, proto.Message{})
	d.waitFor(20200)
	d.send(11000, proto.Message{})
	d.waitFor(11000)
	heroes, err := e.st.PartnersOf(defender.RID)
	if err != nil || len(heroes) == 0 || heroes[0].BID != starterH5 {
		t.Fatalf("defender roster: %v %v", heroes, err)
	}

	// Persist a pre-update opponent slot containing the campaign lineup.
	old, ok, err := oppFromRole(e.st, defender, arenaInitScore)
	if err != nil || !ok || len(old.Lineup) != 1 || old.Lineup[0].Pos != 1 {
		t.Fatalf("legacy opponent: %+v %v", old, err)
	}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetArenaSlots(attacker.RID, []store.ArenaSlot{{Idx: 1, OppRID: defender.RID, Info: string(raw)}}); err != nil {
		t.Fatal(err)
	}
	d.send(11212, proto.Message{"type": formArenaDefense, "formation_type": 3, "pos_info": []proto.Message{{"pos": 6, "id": heroes[0].PartnerID}}})
	if r := d.waitFor(11212); r.Int("code") != 1 {
		t.Fatalf("save defense: %v", r)
	}
	for kind, pos := range map[int64]int64{formDrama: 1, formArenaDefense: 6} {
		d.send(11211, proto.Message{"type": kind})
		team := d.waitFor(11211)
		if slots := team.Arr("pos_info"); team.Int("type") != kind || len(slots) != 1 || slots[0].Int("pos") != pos {
			t.Fatalf("formation %d: %v", kind, team)
		}
	}
	c.send(20201, proto.Message{})
	if list := c.waitFor(20201).Arr("f_list"); len(list) != 1 || list[0].Int("rid") != defender.RID || list[0].Arr("p_list")[0].Int("pos") != 6 {
		t.Fatalf("opponent list did not use defense: %v", list)
	}
	target := proto.Message{"rid": defender.RID, "srv_id": defender.SrvID}
	c.send(20202, target)
	if detail := c.waitFor(20202); detail.Int("formation_type") != 3 || detail.Arr("p_list")[0].Int("pos") != 6 {
		t.Fatalf("opponent details did not use defense: %v", detail)
	}
	assetsBefore, err := e.st.RoleAssets(attacker.RID)
	if err != nil {
		t.Fatal(err)
	}
	c.send(20203, target)
	if r := c.waitFor(20203); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	intro := c.waitFor(20027)
	var defenseCells []int64
	for _, o := range intro.Arr("objects") {
		if o.Int("group") == 2 {
			defenseCells = append(defenseCells, o.Int("pos"))
		}
	}
	if intro.Int("combat_type") != combatArena || !reflect.DeepEqual(defenseCells, []int64{10 + battleCell(3, 6)}) {
		t.Fatalf("wrong defenders in fight: %v", defenseCells)
	}
	c.send(20203, target)
	if r := c.waitFor(20203); r.Int("code") != 0 {
		t.Fatalf("second challenge to the same slot: %v", r)
	}
	settlement, played := arenaWireRounds(t, c, 20210)
	drops, id := settlement.Arr("items"), settlement.Int("replay_id")
	if len(played) == 0 || id <= 0 || len(drops) != 1 {
		t.Fatalf("settlement: %v", settlement)
	}
	assetsAfter, err := e.st.RoleAssets(attacker.RID)
	if err != nil || assetsAfter[drops[0].Int("bid")]-assetsBefore[drops[0].Int("bid")] != drops[0].Int("num") {
		t.Fatalf("reward missing from wallet: %v %v, %v", assetsBefore, assetsAfter, err)
	}

	for i, player := range []*client{c, d} {
		player.send(20222, proto.Message{})
		logs := player.waitFor(20222).Arr("log_list")
		wantResult, wantChange := settlement.Int("result"), settlement.Int("get_score")
		if i == 1 {
			wantResult, wantChange = 3-wantResult, settlement.Int("lose_score")
		}
		if len(logs) != 1 || logs[0].Int("type") != int64(i+1) || logs[0].Int("ret") != wantResult ||
			logs[0].Int("score") != wantChange || logs[0].Int("replay_id") != id {
			t.Fatalf("player %d log: %v", i+1, logs)
		}
		player.send(20036, proto.Message{"replay_id": id, "replay_srv_id": ""})
		if r := player.waitFor(20036); r.Int("code") != 1 {
			t.Fatalf("player %d replay: %v", i+1, r)
		}
		replayIntro := player.waitFor(20027)
		if !reflect.DeepEqual(replayIntro.Arr("objects"), intro.Arr("objects")) {
			t.Fatalf("player %d replay changed the lineup", i+1)
		}
		end, rounds := arenaWireRounds(t, player, 20006)
		if end.Int("combat_type") != 1000+combatArena || !reflect.DeepEqual(played, rounds) {
			t.Fatalf("player %d replay changed the fight: %v", i+1, end)
		}
	}
	assets, err := e.st.RoleAssets(attacker.RID)
	if err != nil || !reflect.DeepEqual(assetsAfter, assets) {
		t.Fatalf("replay changed the wallet: %v %v", assets, err)
	}
	c.send(20200, proto.Message{})
	if r := c.waitFor(20200); r.Int("can_combat_num") != arenaFreeCombats-1 || r.Int("score") != settlement.Int("score") {
		t.Fatalf("counted a battle twice: %v", r)
	}
}

func TestArenaTicketAndDisconnectKeepSingleSettlement(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 11, 10, 12, 0, 0, 0, dayZone))
	c, role := enterArenaH5(t, e, "arena-rejoin", nil)
	c.send(20201, proto.Message{})
	enemy := c.waitFor(20201).Arr("f_list")[0]
	target := proto.Message{"rid": enemy.Int("rid"), "srv_id": enemy.Str("srv_id")}
	a, err := e.st.ArenaPlayerOf(role.RID, arenaInitScore, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.FreeUsed = arenaFreeCombats
	if err := e.st.SaveArenaPlayer(a); err != nil {
		t.Fatal(err)
	}
	c.send(20203, target)
	if reply := c.waitFor(20203); reply.Int("code") != 0 || reply.Str("msg") != "挑战次数不足" {
		t.Fatalf("exhausted attempt: %v", reply)
	}
	c.send(20207, proto.Message{"num": 1})
	if reply := c.waitFor(20207); reply.Int("code") != 0 {
		t.Fatalf("bought a ticket without diamonds: %v", reply)
	}
	if a, err := e.st.ArenaPlayerOf(role.RID, arenaInitScore, 0); err != nil || a.Bought != 0 {
		t.Fatalf("failed ticket purchase changed the counter: %+v %v", a, err)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(20207, proto.Message{"num": 1})
	if reply := c.waitFor(20207); reply.Int("code") != 1 {
		t.Fatalf("buy: %v", reply)
	}
	c.send(20203, target)
	if reply := c.waitFor(20203); reply.Int("code") != 1 {
		t.Fatalf("challenge after buying a ticket: %v", reply)
	}
	c.waitFor(20027)
	c.c.Close() // no round acknowledgement or skip

	c, _ = enterArenaH5(t, e, "arena-rejoin", role)
	c.send(20208, proto.Message{})
	if r := c.waitFor(20208); r.Int("had_combat_num") != 1 {
		t.Fatalf("fight was not saved on disconnect: %v", r)
	}
	c.send(20222, proto.Message{})
	if r := c.waitFor(20222).Arr("log_list"); len(r) != 1 || r[0].Int("replay_id") <= 0 {
		t.Fatalf("fight has no replay on reconnect: %v", r)
	}
	c.send(20203, target)
	if reply := c.waitFor(20203); reply.Int("code") != 0 {
		t.Fatalf("another charge for the same match: %v", reply)
	}
	items, err := e.st.RoleItems(role.RID)
	if err != nil || items[arenaTicket] != 0 {
		t.Fatalf("ticket balance: %v %v", items, err)
	}
	assets, err := e.st.RoleAssets(role.RID)
	if err != nil || assets[3] != 100-arenaTicketGems {
		t.Fatalf("diamonds: %v %v", assets, err)
	}
	e.advance(24 * time.Hour)
	c.send(20200, proto.Message{})
	if r := c.waitFor(20200); r.Int("can_combat_num") != arenaFreeCombats || r.Int("buy_combat_num") != 0 {
		t.Fatalf("day counters did not reset: %v", r)
	}
}

func TestArenaSeasonRollsIdlePlayersOnRanking(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 11, 10, 12, 0, 0, 0, dayZone))
	c, _ := enterArenaH5(t, e, "arena-active", nil)
	d, idle := enterArenaH5(t, e, "arena-idle", nil)
	legacy, original := e.enter("arena-original", "原版玩家")
	legacy.send(20200, proto.Message{})
	legacy.waitFor(20200)
	old, err := e.st.ArenaPlayerOf(original.RID, arenaInitScore, 0)
	if err != nil {
		t.Fatal(err)
	}
	old.Score, old.Battles = 3000, 20
	if err := e.st.SaveArenaPlayer(old); err != nil {
		t.Fatal(err)
	}
	d.send(20200, proto.Message{})
	d.waitFor(20200)
	c.send(20221, proto.Message{})
	if r := c.waitFor(20221); r.Int("rank") != 0 || len(r.Arr("rank_list")) != 1 {
		t.Fatalf("looking at the leaderboard entered a role: %v", r)
	}
	c.send(20200, proto.Message{})
	c.waitFor(20200)
	a, err := e.st.ArenaPlayerOf(idle.RID, arenaInitScore, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.Score, a.Battles, a.ContWin = 2000, 10, 3
	if err := e.st.SaveArenaPlayer(a); err != nil {
		t.Fatal(err)
	}
	e.advance(7 * 24 * time.Hour)
	for i := 0; i < 2; i++ {
		c.send(20221, proto.Message{})
		list := c.waitFor(20221).Arr("rank_list")
		if len(list) != 2 || list[0].Int("rid") != idle.RID || list[0].Int("score") != 1400 {
			t.Fatalf("ranking on requested season: %v", list)
		}
	}
	d.send(20208, proto.Message{})
	if r := d.waitFor(20208); r.Int("had_combat_num") != 0 {
		t.Fatalf("previous season had unclaimed progress: %v", r)
	}
	if a, err := e.st.ArenaPlayerOf(original.RID, arenaInitScore, 0); err != nil || *a != *old {
		t.Fatalf("H5 ranking modified the original client's standing: %+v, %v", a, err)
	}
}

func TestArenaH5UsesFiveStarMinimumInDefenseMatchingAndCombat(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, attacker := enterArenaH5(t, e, "arena-mix-atk", nil)
	d, defender := enterArenaH5(t, e, "arena-mix-def", nil)
	legacy, legacyRole := e.enter("arena-mix-original", "原版玩家")
	if granted, err := e.st.GrantStarterPartners(legacyRole.RID, []store.PartnerSeed{{BID: starterH5, Star: 5}}); err != nil || !granted {
		t.Fatalf("legacy roster: %t %v", granted, err)
	}
	legacy.send(20200, proto.Message{})
	legacy.waitFor(20200)
	d.send(20200, proto.Message{})
	d.waitFor(20200)
	seed := func(role *store.Role, kind int64) []store.Partner {
		t.Helper()
		if granted, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: starterMale, Star: 4}, {BID: starterH5, Star: 5}, {BID: starterH5, Star: 6}}); err != nil || !granted {
			t.Fatalf("seed %s: %t %v", role.Name, granted, err)
		}
		heroes, err := e.st.PartnersOf(role.RID)
		if err != nil || len(heroes) != 3 {
			t.Fatalf("heroes: %v %v", heroes, err)
		}
		f := store.Formation{Type: kind, FormationType: 2,
			Slots: []store.FormationSlot{{Pos: 2, PartnerID: heroes[0].PartnerID}, {Pos: 4, PartnerID: heroes[1].PartnerID}, {Pos: 6, PartnerID: heroes[2].PartnerID}}}
		if err := e.st.SaveFormation(role.RID, f); err != nil {
			t.Fatal(err)
		}
		return heroes
	}
	attack := seed(attacker, formDrama)
	defense := seed(defender, formArenaDefense)
	d.send(11213, proto.Message{"type_list": []proto.Message{{"type": formArenaDefense}}})
	forms := d.waitFor(11213).Arr("info")
	if len(forms) != 1 || forms[0].Int("formation_type") != 2 {
		t.Fatalf("saved defense: %v", forms)
	}
	if slots := forms[0].Arr("pos_info"); len(slots) != 2 || slots[0].Int("id") != defense[1].PartnerID || slots[1].Int("id") != defense[2].PartnerID {
		t.Fatalf("defense slots: %v", slots)
	}
	d.send(11212, proto.Message{"type": formArenaDefense, "formation_type": 3, "pos_info": []proto.Message{{"pos": 1, "id": defense[0].PartnerID}}})
	if r := d.waitFor(11212); r.Int("code") != 0 {
		t.Fatalf("saved a four-star defender: %v", r)
	}

	checkLineup := func(heroes []proto.Message) {
		t.Helper()
		if len(heroes) != 2 || heroes[0].Int("star") != 5 || heroes[0].Int("pos") != 4 || heroes[1].Int("star") != 6 || heroes[1].Int("pos") != 6 {
			t.Fatalf("expected the five- and six-star heroes in their saved positions: %v", heroes)
		}
	}
	c.send(20201, proto.Message{})
	list := c.waitFor(20201).Arr("f_list")
	if len(list) != arenaOpponents || list[0].Int("rid") != defender.RID {
		t.Fatalf("nearby H5 opponent and robots: %v", list)
	}
	for i, enemy := range list {
		if i > 0 && enemy.Int("rid") < arenaRobotBase {
			t.Fatalf("H5 matched an original client: %v", enemy)
		}
		checkLineup(enemy.Arr("p_list"))
	}
	target := proto.Message{"rid": defender.RID, "srv_id": defender.SrvID}
	c.send(20202, target)
	detail := c.waitFor(20202)
	if detail.Int("formation_type") != 2 {
		t.Fatalf("defense formation type: %v", detail)
	}
	checkLineup(detail.Arr("p_list"))
	c.send(20203, target)
	if r := c.waitFor(20203); r.Int("code") != 1 {
		t.Fatalf("H5 challenge: %v", r)
	}
	intro := c.waitFor(20027)
	heroStars := map[int64][]int64{}
	for _, o := range intro.Arr("objects") {
		if o.Int("object_type") == 2 {
			heroStars[o.Int("group")] = append(heroStars[o.Int("group")], o.Int("star"))
		}
	}
	if !reflect.DeepEqual(heroStars, map[int64][]int64{1: {5, 6}, 2: {5, 6}}) {
		t.Fatalf("combat teams: %v", heroStars)
	}
	c.send(20062, proto.Message{})
	result := c.waitFor(20210)
	stats := result.Arr("hurt_statistics")
	if len(stats) != 1 {
		t.Fatalf("combat results: %v", result)
	}
	if heroes := stats[0].Arr("partner_hurts"); len(heroes) != 2 || heroes[0].Int("id") != attack[1].PartnerID || heroes[1].Int("id") != attack[2].PartnerID {
		t.Fatalf("combat result roster: %v", heroes)
	}
}

func TestArenaH5ReadsOldRobotSnapshots(t *testing.T) {
	old := arenaOpp{RID: arenaRobotBase, SrvID: "symlf_1", Robot: true, FormType: 1, Lineup: []arenaHero{
		{Pos: 2, BID: starterMale, Lev: 1, Star: 4}, {Pos: 6, BID: starterH5, Lev: 3, Star: 5},
	}}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	slot := store.ArenaSlot{OppRID: old.RID, Info: string(raw)}
	o, ok, err := arenaOpponentOnSlot(nil, old.SrvID, slot)
	if err != nil || !ok || !reflect.DeepEqual(o.Lineup, old.Lineup[1:]) || o.Power != rolePower([]store.Partner{{BID: starterH5, Lev: 3, Star: 5}}) {
		t.Fatalf("old robot with filtered roster: %+v %v %v", o, ok, err)
	}
}

func TestArenaOriginalClientUsesCampaignAndSettlesAfterPlayback(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, attacker := e.enter("arena-compat-atk", "挑战者")
	d, defender := e.enter("arena-compat-def", "防守者")
	if err := e.st.SetRoleLevel(attacker.RID, arenaLimitLev); err != nil {
		t.Fatal(err)
	}
	if granted, err := e.st.GrantStarterPartners(defender.RID, []store.PartnerSeed{{BID: starterMale, Star: 4}}); err != nil || !granted {
		t.Fatalf("defender: %t %v", granted, err)
	}
	heroes, err := e.st.PartnersOf(defender.RID)
	if err != nil || len(heroes) != 1 {
		t.Fatalf("defender roster: %v %v", heroes, err)
	}
	d.send(11212, proto.Message{"type": formArenaDefense, "formation_type": 3, "pos_info": []proto.Message{{"pos": 6, "id": heroes[0].PartnerID}}})
	if r := d.waitFor(11212); r.Int("code") != 1 {
		t.Fatalf("original client could not save a four-star defender: %v", r)
	}
	d.send(20200, proto.Message{})
	d.waitFor(20200)
	c.send(20201, proto.Message{})
	list := c.waitFor(20201).Arr("f_list")
	if len(list) != arenaOpponents || list[0].Int("rid") != defender.RID {
		t.Fatalf("original client's opponents: %v", list)
	}
	if heroes := list[0].Arr("p_list"); len(heroes) != 1 || heroes[0].Int("star") != 4 || heroes[0].Int("pos") != 1 {
		t.Fatalf("original client's opponent used a different lineup: %v", heroes)
	}
	c.send(20203, proto.Message{"rid": defender.RID, "srv_id": defender.SrvID})
	if r := c.waitFor(20203); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	intro := c.waitFor(20027)
	var defenseCells []int64
	for _, o := range intro.Arr("objects") {
		if o.Int("group") == 2 {
			if o.Int("star") != 4 {
				t.Fatalf("changed the original client's defender: %v", o)
			}
			defenseCells = append(defenseCells, o.Int("pos"))
		}
	}
	if !reflect.DeepEqual(defenseCells, []int64{10 + battleCell(1, 1)}) {
		t.Fatalf("original client's combat positions: %v", defenseCells)
	}
	c.send(20208, proto.Message{})
	if r := c.waitFor(20208); r.Int("had_combat_num") != 0 {
		t.Fatalf("battle settled before playback ended: %v", r)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20210)
	c.send(20208, proto.Message{})
	if r := c.waitFor(20208); r.Int("had_combat_num") != 1 {
		t.Fatalf("battle failed to settle after playback: %v", r)
	}
}

func TestArenaMatchRollbackOnReplayFailure(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := enterArenaH5(t, e, "arena-rollback", nil)
	c.send(20201, proto.Message{})
	c.waitFor(20201)
	slots, err := e.st.ArenaSlots(role.RID)
	if err != nil || len(slots) == 0 {
		t.Fatalf("opponents: %v %v", slots, err)
	}
	opp, ok, err := arenaOpponentOnSlot(e.st, role.SrvID, slots[0])
	if err != nil || !ok {
		t.Fatalf("opponent: %+v %v", opp, err)
	}
	assets, err := e.st.RoleAssets(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = commitArenaFight(e.st, role.RID, slots[0], opp, replaySide{}, 0, time.Unix(0, e.clk.Load()), true,
		func(*store.Store) int64 { return 0 })
	if err == nil {
		t.Fatal("settled a battle without its replay")
	}
	a, err := e.st.ArenaPlayerOf(role.RID, arenaInitScore, 0)
	if err != nil || a.FreeUsed != 0 || a.Battles != 0 || a.Score != arenaInitScore {
		t.Fatalf("partially updated standing: %+v %v", a, err)
	}
	after, err := e.st.ArenaSlots(role.RID)
	if err != nil || !reflect.DeepEqual(after, slots) {
		t.Fatalf("failed battle consumed an opponent: %v %v", after, err)
	}
	wallet, err := e.st.RoleAssets(role.RID)
	if err != nil || !reflect.DeepEqual(wallet, assets) {
		t.Fatalf("failed battle changed the wallet: %v %v", wallet, err)
	}
	if logs, err := e.st.ArenaLogs(role.RID); err != nil || len(logs) != 0 {
		t.Fatalf("failed battle added history: %v %v", logs, err)
	}
}
