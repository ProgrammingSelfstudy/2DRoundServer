package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func ladderTime(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, dayZone) } // 24th is a Saturday

func TestLadderClosedOutsideTheWeekend(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(ladderTime(21, 12))
	c, _ := e.enter("ld0", "天梯客")
	c.send(24312, proto.Message{})
	if r := c.waitFor(24312); r.Int("code") != 0 {
		t.Fatalf("open on a Wednesday: %v", r)
	}
	c.send(24300, proto.Message{})
	if r := c.waitFor(24300); r.Int("rank") != 0 {
		t.Fatalf("a place while closed: %v", r)
	}
	c.send(24301, proto.Message{})
	if r := c.waitFor(24301); len(r.Arr("f_list")) != 0 {
		t.Fatalf("foes while closed: %v", r)
	}
	c.send(24306, proto.Message{})
	if r := c.waitFor(24306); r.Int("code") != 0 {
		t.Fatalf("quick challenge while closed: %v", r)
	}
	e.setNow(ladderTime(24, 8))
	c.send(24312, proto.Message{})
	if r := c.waitFor(24312); r.Int("code") != 1 {
		t.Fatalf("closed on a Saturday: %v", r)
	}
}

func TestLadderFightBuyQuickAndRewards(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(ladderTime(24, 9))
	c, role := e.enter("ld1", "天梯客")
	if err := e.st.SetRoleLevel(role.RID, 60); err != nil {
		t.Fatal(err)
	}
	c.send(24300, proto.Message{})
	info := c.waitFor(24300)
	if info.Int("rank") != 1000 || info.Int("can_combat_num") != 10 || info.Int("best_rank") != 1000 {
		t.Fatalf("first standing: %v", info)
	}
	c.send(24301, proto.Message{})
	list := c.waitFor(24301).Arr("f_list")
	if len(list) != ladderFoes {
		t.Fatalf("foes: %v", list)
	}
	foe := list[0]
	if foe.Int("rank") >= 1000 || foe.Int("rank") < 991 || foe.Int("power") == 0 || foe.Int("look") == 0 {
		t.Fatalf("foe: %v", foe)
	}
	c.send(24302, proto.Message{"rid": foe.Int("rid"), "srv_id": foe.Str("srv_id")})
	if d := c.waitFor(24302); len(d.Arr("p_list")) == 0 || d.Int("rank") != foe.Int("rank") {
		t.Fatalf("foe detail: %v", d)
	}
	// a rid that is not on the ladder
	c.send(24303, proto.Message{"rid": 12345, "srv_id": "symlf_1"})
	if r := c.waitFor(24303); r.Int("code") != 0 {
		t.Fatalf("challenge of nobody: %v", r)
	}

	c.send(24303, proto.Message{"rid": foe.Int("rid"), "srv_id": foe.Str("srv_id")})
	if r := c.waitFor(24303); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	c.waitFor(20000)
	if intro := c.waitFor(20027); intro.Int("combat_type") != combatLadder {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(24307)
	won := res.Int("result") == 1
	if res.Int("replay_id") == 0 || len(res.Arr("reward")) == 0 {
		t.Fatalf("result: %v", res)
	}
	if won && (res.Int("atk_rank") != foe.Int("rank") || res.Int("atk_change_rank") != 1000-foe.Int("rank") || res.Int("is_change_best_rank") != 1) {
		t.Fatalf("won but the places did not change: %v", res)
	}
	if !won && (res.Int("atk_rank") != 1000 || res.Int("atk_change_rank") != 0) {
		t.Fatalf("lost but the rank moved: %v", res)
	}
	info = c.waitFor(24300)
	if info.Int("can_combat_num") != 9 || info.Int("rank") != res.Int("atk_rank") {
		t.Fatalf("standing after the fight: %v", info)
	}
	c.waitFor(24301)
	c.send(24310, proto.Message{})
	log := c.waitFor(24310).Arr("log_list")
	if len(log) != 1 || log[0].Int("type") != 1 || log[0].Int("ret") != res.Int("result") || log[0].Int("replay_id") != res.Int("replay_id") {
		t.Fatalf("my log: %v", log)
	}

	// a refresh waits for its cool-down
	c.send(24304, proto.Message{})
	if r := c.waitFor(24304); r.Int("code") != 1 {
		t.Fatalf("refresh: %v", r)
	}
	c.waitFor(24300)
	c.waitFor(24301)
	c.send(24304, proto.Message{})
	if r := c.waitFor(24304); r.Int("code") != 0 {
		t.Fatalf("refresh again: %v", r)
	}

	// the one-key challenge costs one and pays the prize
	c.send(24306, proto.Message{})
	if r := c.waitFor(24306); r.Int("code") != 1 {
		t.Fatalf("quick: %v", r)
	}
	if info = c.waitFor(24300); info.Int("can_combat_num") != 8 {
		t.Fatalf("after the quick challenge: %v", info)
	}
	// buying needs diamonds
	c.send(24305, proto.Message{})
	if r := c.waitFor(24305); r.Int("code") != 0 {
		t.Fatalf("buy without diamonds: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 100}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	c.send(24305, proto.Message{})
	if r := c.waitFor(24305); r.Int("code") != 1 {
		t.Fatalf("buy: %v", r)
	}
	if info = c.waitFor(24300); info.Int("can_combat_num") != 9 || info.Int("buy_combat_num") != 1 {
		t.Fatalf("after the buy: %v", info)
	}

	// the statues and the ranking
	c.send(24308, proto.Message{})
	if r := c.waitFor(24308).Arr("rank_list"); len(r) != 3 || r[0].Int("rank") != 1 {
		t.Fatalf("top three: %v", r)
	}
	c.send(24309, proto.Message{})
	if r := c.waitFor(24309); r.Int("rank") != info.Int("rank") || len(r.Arr("rank_list")) != 50 {
		t.Fatalf("ranking: %v", r)
	}

	// Sunday 23:00 sends the rank prize by mail, once
	e.setNow(ladderTime(25, 23))
	c.send(24300, proto.Message{})
	if r := c.waitFor(24300); r.Int("rank") != info.Int("rank") {
		t.Fatalf("rank after the close: %v", r)
	}
	prizes := func() int {
		mails, err := e.st.MailsForRole(role)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, m := range mails {
			if m.Subject == "天梯争霸奖励" {
				n++
			}
		}
		return n
	}
	if n := prizes(); n != 1 {
		t.Fatalf("rank prize mails: %d", n)
	}
	c.send(24300, proto.Message{})
	c.waitFor(24300)
	if n := prizes(); n != 1 {
		t.Fatalf("the prize was sent %d times", n)
	}
}
