package game

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func playTowerScript(c *client, resultCmd uint16) ([]proto.Message, proto.Message) {
	c.t.Helper()
	c.send(20001, proto.Message{})
	c.waitFor(20001)
	round := c.waitFor(20002)
	var script []proto.Message
	for i := 0; i < maxFightRounds; i++ {
		c.send(20019, proto.Message{})
		script = append(script, round, c.waitFor(20004))
		c.send(20005, proto.Message{})
		for {
			cmd, m := c.recvNext()
			if cmd == resultCmd {
				return script, m
			}
			if cmd == 20002 {
				round = m
				break
			}
		}
	}
	c.t.Fatal("tower battle did not finish")
	return nil, nil
}

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
	if _, err := starterGrant(e.st, role); err != nil {
		t.Fatal(err)
	}
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("starter heroes: %v, %v", heroes, err)
	}
	c.send(11212, proto.Message{"type": formStarTower, "formation_type": 2,
		"pos_info": []proto.Message{{"pos": 3, "id": heroes[0].PartnerID}}})
	if r := c.waitFor(11212); r.Int("code") != 1 {
		t.Fatalf("tower formation: %v", r)
	}
	c.send(11322, proto.Message{"tower": 1})
	if r := c.waitFor(11322); r.Int("result") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	intro := c.waitFor(20027)
	if intro.Int("combat_type") != combatStarTower {
		t.Fatalf("intro: %v", intro)
	}
	if forms := intro.Arr("formation"); len(forms) != 2 || forms[0].Int("formation_type") != 2 || intro.Int("a_object_num") != 1 {
		t.Fatalf("intro ignored tower formation: %v", intro)
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

func TestStarTowerReplayUsesRecordedRoundsAndStatistics(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	clear := func(account, name string, heroLevel int, duration time.Duration) (*client, *store.Role, proto.Message, []proto.Message, proto.Message) {
		c, role := e.enter(account, name)
		if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10502, Star: 5}}); err != nil {
			t.Fatal(err)
		}
		heroes, err := e.st.PartnersOf(role.RID)
		if err != nil || len(heroes) != 1 {
			t.Fatalf("partners: %v, %v", heroes, err)
		}
		h := heroes[0]
		if err := e.st.UpgradePartner(role.RID, h.PartnerID, h.Lev, h.BreakLev, heroLevel, h.BreakLev, nil, nil); err != nil {
			t.Fatal(err)
		}
		c.send(11322, proto.Message{"tower": 1})
		if reply := c.waitFor(11322); reply.Int("result") != 1 {
			t.Fatalf("start: %v", reply)
		}
		intro := c.waitFor(20027)
		e.advance(duration)
		rounds, res := playTowerScript(c, 11323)
		if res.Int("result") != 1 || res.Int("replay_id") <= 0 || res.Int("timer") != int64(duration/time.Second) {
			t.Fatalf("tower result: %v", res)
		}
		return c, role, intro, rounds, res
	}
	_, _, intro, rounds, fast := clear("fast", "最快", 250, 3*time.Second)
	viewer, role, _, _, low := clear("weak", "低战", 180, 8*time.Second)
	groups := fast.Arr("hurt_statistics")
	if len(groups) != 2 || groups[0].Int("type") != 2 || groups[1].Int("type") != 1 {
		t.Fatalf("both teams' statistics: %v", groups)
	}
	var foeLoss int64
	for _, enemy := range groups[0].Arr("partner_hurts") {
		foeLoss += enemy.Int("be_hurt")
	}
	header := groups[1].Arr("partner_hurts")[0]
	if foeLoss <= 0 || foeLoss != header.Int("dps") || header.Int("cure") != 0 {
		t.Fatalf("damage dealt should account for HP removed: %v", groups)
	}
	viewer.send(11325, proto.Message{"tower": 1})
	list := viewer.waitFor(11325)
	rows := list.Arr("tower_replay_data")
	if len(rows) != 2 || rows[0].Int("type") != 1 || rows[0].Int("replay_id") != fast.Int("replay_id") || rows[1].Int("type") != 2 ||
		rows[1].Int("replay_id") != low.Int("replay_id") || rows[0].Int("power") <= rows[1].Int("power") ||
		list.Int("m_replay_id") != low.Int("replay_id") || list.Int("my_time") != 8 {
		t.Fatalf("floor records: %v", list)
	}
	viewer.send(11325, proto.Message{"tower": 2})
	if other := viewer.waitFor(11325); other.Int("m_replay_id") != 0 || len(other.Arr("tower_replay_data")) != 0 {
		t.Fatalf("records must be scoped to the floor: %v", other)
	}
	before, _ := e.st.RoleAssets(role.RID)
	replay := proto.Message{"replay_id": fast.Int("replay_id"), "replay_srv_id": role.SrvID}
	viewer.send(20036, replay)
	viewer.waitFor(20000)
	if reply := viewer.waitFor(20036); reply.Int("code") != 1 {
		t.Fatalf("play another role's fastest clear: %v", reply)
	}
	if got := viewer.waitFor(20027); !reflect.DeepEqual(got, intro) {
		t.Fatalf("replay intro is different: %v", got)
	}
	viewer.send(11322, proto.Message{"tower": 2})
	if answer := viewer.waitFor(11322); answer.Int("result") != 0 {
		t.Fatalf("a new challenge could replace an active replay: %v", answer)
	}
	played, done := playTowerScript(viewer, 20006)
	if done.Int("combat_type") != 1007 || !reflect.DeepEqual(played, rounds) || !reflect.DeepEqual(done.Arr("hurt_statistics"), groups) {
		t.Fatalf("replay changed the recorded round or report: %v", done)
	}
	after, _ := e.st.RoleAssets(role.RID)
	progress, _ := e.st.StarTowerOf(role.RID)
	if !reflect.DeepEqual(before, after) || progress.MaxTower != 1 || progress.Used != 0 {
		t.Fatalf("playback changed the player's assets or progress: %+v", progress)
	}
}

func TestStarTowerStatusStatisticsUseEffectiveHPChanges(t *testing.T) {
	f := &fightSim{rng: rand.New(rand.NewSource(11)), units: []fightUnit{
		{ours: true, pos: 1, hp: 10, maxHP: 30, atk: 500},
		{ours: true, pos: 2, hp: 30, maxHP: 30, atk: 500},
		{pos: 10, hp: 25, maxHP: 25, atk: 500},
	}}
	target, healer, monster := &f.units[0], &f.units[1], &f.units[2]
	add := func(source *fightUnit, row buffRule) { f.addBuff(source, target, row) }
	add(monster, buffRule{Bid: 1, Kind: "dot", Pct: 100, Rounds: 1, Chance: 100})
	add(healer, buffRule{Bid: 2, Kind: "regen", Pct: 100, Rounds: 1, Chance: 100})
	add(target, buffRule{Bid: 3, Kind: "regen", Pct: 100, Rounds: 1, Chance: 100})
	f.startOfRound()
	if target.hp != 30 || target.taken != 9 || monster.damage != 9 || healer.healing != 29 || target.healing != 0 {
		t.Fatalf("stats must account for capped damage and overheal separately: %+v", f.units)
	}
}

func TestStarTowerDefeatKeepsStatisticsWithoutSettingARecord(t *testing.T) {
	e, c, role := towerEnv(t)
	limit := int64(len(towerCfg.Floors))
	if err := e.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: limit - 1}); err != nil {
		t.Fatal(err)
	}
	c.send(11322, proto.Message{"tower": limit})
	if start := c.waitFor(11322); start.Int("result") != 1 {
		t.Fatalf("start: %v", start)
	}
	c.waitFor(20027)
	_, defeat := playTowerScript(c, 11323)
	if defeat.Int("result") != 2 || len(defeat.Arr("hurt_statistics")) != 2 || defeat.Int("replay_id") <= 0 || defeat.Int("max_tower") != limit-1 {
		t.Fatalf("defeat must keep its report without clearing the floor: %v", defeat)
	}
	c.send(11325, proto.Message{"tower": limit})
	if list := c.waitFor(11325); list.Int("m_replay_id") != 0 || len(list.Arr("tower_replay_data")) != 0 {
		t.Fatalf("a defeat cannot set a clear record: %v", list)
	}
	c.send(20036, proto.Message{"replay_id": defeat.Int("replay_id")})
	if ack := c.waitFor(20036); ack.Int("code") != 1 {
		t.Fatalf("play defeat: %v", ack)
	}
	c.waitFor(20027)
	_, result := playTowerScript(c, 20006)
	if result.Int("result") != 2 || !reflect.DeepEqual(result.Arr("hurt_statistics"), defeat.Arr("hurt_statistics")) {
		t.Fatalf("defeat changed in playback: %v", result)
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
