package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestWarOrderRules(t *testing.T) {
	for _, c := range []struct {
		task  warTask
		event string
	}{
		{warTask{Label: "evt_cli_loaded", Desc: "登录冒险大陆一次"}, evtLoaded},
		{warTask{Label: "evt_recruit_high", Desc: "进行一次先知召唤"}, evtRecruit},
		{warTask{Label: "evt_shipping", Desc: "接取1次橙色远航", Param: []any{4.0}}, evtVoyageOrange},
		{warTask{Label: "evt_shipping", Desc: "接取1次紫色远航", Param: []any{3.0}}, evtVoyagePurple},
		{warTask{Label: "evt_shipping", Desc: "接取1次红色远航", Param: []any{5.0}}, ""},
		{warTask{Label: "evt_shipping", Desc: "完成远航任务300次"}, ""},
		{warTask{Label: "evt_arena_fight_result", Desc: "挑战10次竞技场"}, evtArena},
		{warTask{Label: "evt_arena_fight_result", Desc: "赢得10次竞技场"}, ""},
		{warTask{Label: "evt_dial", Desc: "转动转盘"}, ""},
	} {
		if got := warTaskEvent(&c.task); got != c.event {
			t.Fatalf("%s %v: %q, want %q", c.task.Label, c.task.Param, got, c.event)
		}
	}
	for id, p := range warOrderCfg {
		if len(p.Levs) != 40 || p.expItem == 0 || len(p.Tasks) == 0 {
			t.Fatalf("period %d: %d levels, exp item %d, %d tasks", id, len(p.Levs), p.expItem, len(p.Tasks))
		}
	}
	for m, id := range warOrderMonths {
		if warOrderCfg[id] == nil {
			t.Fatalf("%v: no period %d", m, id)
		}
	}
}

func TestWarOrderFlow(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("wo1", "战令人")
	e.setNow(time.Date(2026, time.October, 3, 12, 0, 0, 0, dayZone))
	c.send(10300, proto.Message{})
	c.send(25300, proto.Message{})
	r := c.waitFor(25300)
	if r.Int("period") != 6 || r.Int("cur_day") != 3 || r.Int("lev") != 0 || r.Int("end_time") != time.Date(2026, time.November, 1, 0, 0, 0, 0, dayZone).Unix() {
		t.Fatalf("open: %v", r)
	}
	var login proto.Message
	for _, g := range r.Arr("list") {
		if g.Int("id") == 41167 {
			t.Fatalf("a task of day 1 is listed on day 3: %v", g)
		}
		if g.Int("type") == 1 && g.Str("desc") == "" && g.Int("finish") == 1 {
			login = g
		}
	}
	if login == nil {
		t.Fatalf("no finished login task: %v", r.Arr("list"))
	}
	c.send(25302, proto.Message{"id": login.Int("id")})
	if r := c.waitFor(25302); r.Int("code") != 1 {
		t.Fatalf("claim: %v", r)
	}
	if r := c.waitFor(25301); r.Arr("list")[0].Int("finish") != 2 {
		t.Fatalf("task after the claim: %v", r)
	}
	r = c.waitFor(25305)
	if r.Int("exp") != 100 || r.Int("lev") != 0 {
		t.Fatalf("exp: %v", r)
	}
	c.send(25302, proto.Message{"id": login.Int("id")})
	if r := c.waitFor(25302); r.Int("code") != 0 {
		t.Fatalf("a second claim: %v", r)
	}
	// a level bought pays diamonds and sets the experience; the reward of the level is then taken once
	c.send(25307, proto.Message{"id": 1})
	if r := c.waitFor(25307); r.Int("flag") != 0 {
		t.Fatalf("buying without diamonds: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 2000})
	c.send(25307, proto.Message{"id": 2})
	if r := c.waitFor(25307); r.Int("flag") != 1 {
		t.Fatalf("buy: %v", r)
	}
	if r := c.waitFor(25305); r.Int("lev") != 2 || r.Int("exp") != 2000 {
		t.Fatalf("after the buy: %v", r)
	}
	c.send(25304, proto.Message{"id": 3})
	if r := c.waitFor(25304); r.Int("flag") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	c.send(25304, proto.Message{"id": 0})
	if r := c.waitFor(25304); r.Int("flag") != 1 {
		t.Fatalf("claim all: %v", r)
	}
	if r := c.waitFor(25303); len(r.Arr("reward_list")) != 2 || r.Int("lev") != 2 {
		t.Fatalf("taken: %v", r)
	}
	c.send(25304, proto.Message{"id": 0})
	if r := c.waitFor(25304); r.Int("flag") != 0 {
		t.Fatalf("nothing left: %v", r)
	}
	c.send(25308, proto.Message{})
	if r := c.waitFor(25308); r.Int("flag") != 0 {
		t.Fatalf("the extra pack without the card: %v", r)
	}
	// a month without a war order, and a new month starting from nothing
	e.setNow(time.Date(2026, time.August, 3, 12, 0, 0, 0, dayZone))
	c.send(25300, proto.Message{})
	if r := c.waitFor(25300); r.Int("period") != 0 || len(r.Arr("list")) != 0 {
		t.Fatalf("august: %v", r)
	}
	e.setNow(time.Date(2026, time.November, 3, 12, 0, 0, 0, dayZone))
	c.send(25300, proto.Message{})
	if r := c.waitFor(25300); r.Int("period") != 7 || r.Int("exp") != 0 {
		t.Fatalf("november: %v", r)
	}
}
