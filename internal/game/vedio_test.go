package game

import (
	"encoding/json"
	"reflect"
	"testing"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestVedioKeepSharePlay(t *testing.T) {
	e := newDungeonEnv(t)
	c, role := e.enter("vd", "录像员")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	c.send(20201, proto.Message{})
	rid := c.waitFor(20201).Arr("f_list")[0].Int("rid")
	c.send(20203, proto.Message{"rid": rid, "srv_id": "symlf_1", "is_auto": 0})
	c.waitFor(20203)
	scene := c.waitFor(20000)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20210)
	id := res.Int("replay_id")
	if id == 0 {
		t.Fatalf("the arena fight is not kept: %v", res)
	}

	c.send(19901, proto.Message{"type": vedioMine})
	list := c.waitFor(19901).Arr("replay_list")
	if len(list) != 1 || list[0].Int("id") != id || list[0].Str("a_name") != "录像员" || list[0].Int("a_rank") != 1 || list[0].Int("round") == 0 || len(list[0].Arr("a_plist")) == 0 || len(list[0].Arr("b_plist")) == 0 {
		t.Fatalf("my replays: %v", list)
	}
	if ret := list[0].Int("ret"); (ret == 1) != (res.Int("result") == 1) {
		t.Fatalf("ret %d, result %d", ret, res.Int("result"))
	}
	// nobody shared it yet
	c.send(19902, proto.Message{"type": vedioTabNew, "cond_type": 0, "start": 1, "num": vedioPage})
	if r := c.waitFor(19902); r.Int("len") != 0 || len(r.Arr("replay_list")) != 0 {
		t.Fatalf("public before the share: %v", r)
	}
	c.send(19905, proto.Message{"id": id, "channel": 2, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19905); r.Int("code") != 1 {
		t.Fatalf("share: %v", r)
	}
	for _, tab := range []int64{vedioTabNew, vedioTabHot, 1} {
		c.send(19902, proto.Message{"type": tab, "cond_type": 0, "start": 1, "num": vedioPage})
		if r := c.waitFor(19902); r.Int("len") != 1 || len(r.Arr("replay_list")) != 1 || r.Arr("replay_list")[0].Int("share") != 1 {
			t.Fatalf("public tab %d: %v", tab, r)
		}
	}
	// 3 is the solo tab: no replays of it are kept
	c.send(19902, proto.Message{"type": 3, "cond_type": 0, "start": 1, "num": vedioPage})
	if r := c.waitFor(19902); r.Int("len") != 0 {
		t.Fatalf("solo tab: %v", r)
	}

	c.send(19903, proto.Message{"id": id, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19903); r.Int("code") != 1 {
		t.Fatalf("like: %v", r)
	}
	c.send(19903, proto.Message{"id": id, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19903); r.Int("code") != 0 {
		t.Fatalf("like twice: %v", r)
	}
	c.send(19903, proto.Message{"id": 999, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19903); r.Int("code") != 0 {
		t.Fatalf("like of nothing: %v", r)
	}
	c.send(19906, proto.Message{})
	if r := c.waitFor(19906); r.Int("like") != 1 {
		t.Fatalf("likes today: %v", r)
	}

	c.send(19904, proto.Message{"id": id, "type": 1, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19904); r.Int("code") != 1 || r.Int("type") != 1 {
		t.Fatalf("collect: %v", r)
	}
	c.send(19901, proto.Message{"type": vedioCollect})
	if l := c.waitFor(19901).Arr("replay_list"); len(l) != 1 || l[0].Int("is_collect") != 1 || l[0].Int("flag") != 1 || l[0].Int("like") != 1 {
		t.Fatalf("collected: %v", l)
	}
	c.send(19904, proto.Message{"id": id, "type": 0, "srv_id": "symlf_1", "combat_type": combatArena})
	if r := c.waitFor(19904); r.Int("code") != 1 || r.Int("type") != 0 {
		t.Fatalf("let go: %v", r)
	}
	c.send(19901, proto.Message{"type": vedioCollect})
	if l := c.waitFor(19901).Arr("replay_list"); len(l) != 0 {
		t.Fatalf("collected after letting go: %v", l)
	}

	hero := list[0].Arr("a_plist")[0]
	c.send(19907, proto.Message{"replay_id": id, "partner_id": hero.Int("id"), "type": 1, "srv_id": "symlf_1", "combat_type": combatArena})
	if h := c.waitFor(19907); h.Int("bid") != hero.Int("bid") || h.Int("hp") != hero.Int("hp_max") || h.Int("atk") <= 0 || h.Int("speed") <= 0 || h.Int("dps") <= 0 || h.Int("dps") != hero.Int("dps") {
		t.Fatalf("hero card: %v", h)
	}
	c.send(19908, proto.Message{"replay_id": id, "srv_id": "symlf_1", "type": 1, "channel": 2, "hall_srv_id": ""})
	if r := c.waitFor(19908); r.Int("id") != id || r.Int("channel") != 2 || r.Int("round") != list[0].Int("round") {
		t.Fatalf("look: %v", r)
	}

	// play it: the fight of the replay is the one that was kept
	c.send(20036, proto.Message{"replay_id": 999, "replay_srv_id": "symlf_1"})
	if r := c.waitFor(20036); r.Int("code") != 0 {
		t.Fatalf("play nothing: %v", r)
	}
	c.send(20036, proto.Message{"replay_id": id, "replay_srv_id": "symlf_1"})
	if r := c.waitFor(20000); r.Int("combat_map") != fightMap || r.Int("combat_map") != scene.Int("combat_map") || r.Int("combat_type") != combatArena {
		t.Fatalf("replay scene before confirmation: %v", r)
	}
	if r := c.waitFor(20036); r.Int("code") != 1 {
		t.Fatalf("play: %v", r)
	}
	intro := c.waitFor(20027)
	actorCount := 0
	for _, obj := range intro.Arr("objects") {
		if obj.Int("pos") != spiritPos {
			actorCount++
		}
	}
	if intro.Int("combat_type") != combatArena || actorCount != len(list[0].Arr("a_plist"))+len(list[0].Arr("b_plist")) {
		t.Fatalf("replay intro: %v", intro)
	}
	c.send(20001, proto.Message{})
	c.waitFor(20001)
	if r := c.waitFor(20002); len(r.Arr("all_alive")) == 0 {
		t.Fatalf("round 1: %v", r)
	}
	c.send(20019, proto.Message{})
	if r := c.waitFor(20004); len(r.Arr("skill_plays")) == 0 {
		t.Fatalf("attacks of round 1: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	report := c.waitFor(20006)
	stats := report.Arr("hurt_statistics")
	if report.Int("combat_type") != combatArena+1000 || report.Int("result") != list[0].Int("ret") || len(report.Arr("item_rewards")) != 0 || len(stats) != 2 {
		t.Fatalf("replay report: %v", report)
	}
	attacker := stats[1].Arr("partner_hurts")
	if stats[1].Int("type") != 1 || len(attacker) == 0 || attacker[0].Int("dps") != hero.Int("dps") || attacker[0].Int("be_hurt") != hero.Int("be_hurt") {
		t.Fatalf("replay statistics don't match the archived lineup: %v", attacker)
	}
	after, _ := e.st.RoleAssets(role.RID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("replaying changed assets: %v -> %v", before, after)
	}
	c.send(19901, proto.Message{"type": vedioMine})
	if l := c.waitFor(19901).Arr("replay_list"); len(l) != 1 || l[0].Int("play") != 1 {
		t.Fatalf("play count: %v", l)
	}
}

func TestVedioFiltersBeforePaging(t *testing.T) {
	e := newDungeonEnv(t)
	c, role := e.enter("filter", "录像筛选")
	add := func(combat, stage int64, first, second replaySide, shares, likes int64) int64 {
		t.Helper()
		meta, err := json.Marshal(replayMeta{A: first, B: second})
		if err != nil {
			t.Fatal(err)
		}
		id, err := e.st.AddReplay(store.Replay{SrvID: role.SrvID, CombatType: combat, Stage: stage, Meta: string(meta)})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.BumpReplay(id, likes, shares, 0); err != nil {
			t.Fatal(err)
		}
		return id
	}
	empty := replaySide{}
	top := add(combatArena, 0, replaySide{Rank: 10}, empty, 1, 10)
	next := add(combatArena, 0, empty, replaySide{Rank: 50}, 1, 0)
	other := add(combatArena, 0, replaySide{Rank: 51}, empty, 1, 0)
	bottom := add(combatArena, 0, empty, replaySide{Rank: 350}, 1, 0)
	latestTop := add(combatArena, 0, empty, replaySide{Rank: 1}, 1, 0)
	add(combatArena, 0, replaySide{Rank: 1}, empty, 0, 99)
	level100 := add(8, 0, replaySide{Lev: 100}, empty, 1, 0)
	level151 := add(8, 0, empty, replaySide{Lev: 151}, 1, 0)
	final := add(combatChampion, 2, empty, empty, 1, 0)
	qualifier := add(combatChampion, champSize, empty, empty, 1, 0)
	weekly := add(combatCrossChampion, champSize, empty, empty, 1, 0)
	ladder := add(combatLadder, 0, empty, empty, 1, 0)
	check := func(tab, cond, start, num, total int64, want ...int64) {
		t.Helper()
		c.send(19902, proto.Message{"type": tab, "cond_type": cond, "start": start, "num": num})
		msg := c.waitFor(19902)
		list := msg.Arr("replay_list")
		if msg.Int("len") != total || len(list) != len(want) {
			t.Fatalf("tab %d condition %d: %v", tab, cond, msg)
		}
		for idx, id := range want {
			if list[idx].Int("id") != id {
				t.Fatalf("tab %d condition %d wanted %v, got %v", tab, cond, want, list)
			}
		}
	}
	check(1, 1, 1, 1, 2, latestTop)
	check(1, 1, 2, 1, 2, top)
	check(1, 1, 3, 1, 2)
	check(1, 2, 1, 30, 1, next)
	check(1, 3, 1, 30, 1, other)
	check(1, 4, 1, 30, 1, bottom)
	check(3, 1, 1, 30, 1, level151)
	check(3, 3, 1, 30, 1, level100)
	check(3, 4, 1, 30, 0)
	check(2, 1, 1, 30, 1, final)
	check(2, 2, 1, 30, 0)
	check(2, 6, 1, 30, 1, qualifier)
	check(7, 7, 1, 30, 1, weekly)
	check(5, 0, 1, 30, 1, ladder)
	check(vedioTabHot, 0, 1, 1, 11, top)
	check(vedioTabNew, 0, 1, 1, 11, ladder)
}
