package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestVedioKeepSharePlay(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("vd", "录像员")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	c.send(20201, proto.Message{})
	rid := c.waitFor(20201).Arr("f_list")[0].Int("rid")
	c.send(20203, proto.Message{"rid": rid, "srv_id": "symlf_1", "is_auto": 0})
	c.waitFor(20203)
	c.waitFor(20000)
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
	if len(list) != 1 || list[0].Int("id") != id || list[0].Str("a_name") != "录像员" || len(list[0].Arr("a_plist")) == 0 || len(list[0].Arr("b_plist")) == 0 {
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
	if h := c.waitFor(19907); h.Int("bid") != hero.Int("bid") || h.Int("hp") != hero.Int("hp_max") {
		t.Fatalf("hero card: %v", h)
	}
	c.send(19908, proto.Message{"replay_id": id, "srv_id": "symlf_1", "type": 1, "channel": 2, "hall_srv_id": ""})
	if r := c.waitFor(19908); r.Int("id") != id || r.Int("channel") != 2 {
		t.Fatalf("look: %v", r)
	}

	// play it: the fight of the replay is the one that was kept
	c.send(20036, proto.Message{"replay_id": 999, "replay_srv_id": "symlf_1"})
	if r := c.waitFor(20036); r.Int("code") != 0 {
		t.Fatalf("play nothing: %v", r)
	}
	c.send(20036, proto.Message{"replay_id": id, "replay_srv_id": "symlf_1"})
	if r := c.waitFor(20036); r.Int("code") != 1 {
		t.Fatalf("play: %v", r)
	}
	c.waitFor(20000)
	intro := c.waitFor(20027)
	if intro.Int("combat_type") != combatArena || len(intro.Arr("objects")) != len(list[0].Arr("a_plist"))+len(list[0].Arr("b_plist")) {
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
	c.send(19901, proto.Message{"type": vedioMine})
	if l := c.waitFor(19901).Arr("replay_list"); len(l) != 1 || l[0].Int("play") != 1 {
		t.Fatalf("play count: %v", l)
	}
}
