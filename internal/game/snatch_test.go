package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSnatch(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("sn1", "夺宝人")
	c.send(25700, proto.Message{})
	if r := c.waitFor(25700); len(r.Arr("holiday_snatch_info")) != 0 {
		t.Fatalf("no activity open: %v", r)
	}
	c.send(25701, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(25701); r.Int("code") != 0 {
		t.Fatalf("a join without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: snatchCamp, BID: 99329, Status: 1, Spec: actSpecJSON(t, false)})
	round, _, _, _ := snatchRound(actNow)
	goods := snatchGoods(round)
	c.send(25700, proto.Message{})
	r := c.waitFor(25700)
	if r.Int("state") != 1 || len(r.Arr("holiday_snatch_info")) != len(goods) || r.Int("state_time") != actNow.Unix()+2*3600 {
		t.Fatalf("open: %v", r)
	}
	c.send(25701, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(25701); r.Int("code") != 0 {
		t.Fatalf("no tickets: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{37006: 1000})
	c.send(25701, proto.Message{"pos": 1, "num": 5})
	if r := c.waitFor(25701); r.Int("code") != 1 {
		t.Fatalf("join: %v", r)
	}
	c.waitFor(10500)
	r = c.waitFor(25700)
	first := r.Arr("holiday_snatch_info")[0]
	if first.Int("num") != 5 || first.Int("state") != 0 || first.Arr("ext")[0].Int("val") != 5 {
		t.Fatalf("after join: %v", first)
	}

	// buying out a good draws it at once: the player holds every slot
	rest := goods[0].Limit - 5
	if rest > goods[0].LimitRole-5 {
		t.Skip("the first good cannot be bought out by one role")
	}
	c.send(25701, proto.Message{"pos": 1, "num": rest})
	if r := c.waitFor(25701); r.Int("code") != 1 {
		t.Fatalf("buy out: %v", r)
	}
	c.waitFor(10500)
	r = c.waitFor(25700)
	if first := r.Arr("holiday_snatch_info")[0]; first.Int("state") != 2 {
		t.Fatalf("drawn: %v", first)
	}
	c.send(25702, proto.Message{"pos": 1})
	if r := c.waitFor(25702); r.Int("is_win") != 1 || r.Int("win_num") < 1 || len(r.Arr("buy_nums")) != int(goods[0].Limit) || r.Str("win_name") != "夺宝人" {
		t.Fatalf("opening: %v", r)
	}
	c.send(25701, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(25701); r.Int("code") != 0 {
		t.Fatalf("a join after the draw: %v", r)
	}
	c.send(25703, proto.Message{})
	if r := c.waitFor(25703); len(r.Arr("logs")) != 1 || r.Arr("logs")[0].Str("win_name") != "夺宝人" {
		t.Fatalf("server logs: %v", r)
	}

	// a second good that is not full: the window ends and robots fill it
	c.send(25701, proto.Message{"pos": 2, "num": 3})
	c.waitFor(25701)
	c.waitFor(10500)
	c.waitFor(25700)
	e.setNow(actNow.Add(3 * time.Hour)) // past 14:00
	c.send(25700, proto.Message{})
	r = c.waitFor(25700)
	if r.Int("state") != 0 {
		t.Fatalf("closed: %v", r)
	}
	c.send(25704, proto.Message{})
	r = c.waitFor(25704)
	if len(r.Arr("logs")) != 2 {
		t.Fatalf("my records: %v", r)
	}
	if mails, _ := e.st.MailsForRole(role); len(mails) < 3 { // the tickets, the prize, the second good's mail
		t.Fatalf("mails: %d", len(mails))
	}
}
