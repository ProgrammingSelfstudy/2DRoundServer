package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func eliteStateOf(times, since, lucky int64, claimed string) store.EliteSummon {
	return store.EliteSummon{Times: times, Since: since, Lucky: lucky, Claimed: claimed}
}

func TestEliteSummon(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	old := summonRand
	summonRand = func() float64 { return 0.999 } // the last outcome: a 3-star hero
	t.Cleanup(func() { summonRand = old })
	state := func() proto.Message {
		c.send(23230, proto.Message{})
		return c.recv(23230)
	}
	first := eliteTable.Wish[0]
	if r := state(); r.Int("camp_id") != 50051 || r.Int("times") != 0 || r.Int("must_count") != 110 || r.Int("lucky_bid") != first.Item || len(r.Arr("reward_list")) != 0 {
		t.Fatalf("state: %v", r)
	}
	draw := func(times int64) proto.Message {
		c.send(23231, proto.Message{"times": times, "recruit_type": 4})
		return nil
	}
	draw(1) // no scroll
	if r := c.recv(23231); r.Int("flag") != 0 || r.Str("msg") == "" {
		t.Fatalf("no scroll: %v", r)
	}
	e.give(c, role, map[int64]int64{10408: 21})

	// one draw: the assets, the scrolls, the hero, the result with flag 3, the new state
	draw(1)
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11001); len(r.Arr("partners")) != 1 {
		t.Fatalf("hero: %v", r)
	}
	r := c.recv(23201)
	if r.Int("flag") != 3 || r.Int("group_id") != 50051 || r.Int("times") != 1 || len(r.Arr("partner_bids")) != 1 || len(r.Arr("rewards")) != 1 {
		t.Fatalf("23201: %v", r)
	}
	if st := c.recv(23230); st.Int("times") != 1 || st.Int("must_count") != 109 {
		t.Fatalf("state after: %v", st)
	}
	if items, _ := e.st.RoleItems(role.RID); items[10408] != 20 {
		t.Fatalf("scrolls: %v", items)
	}

	// ten draws
	draw(10)
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11001); len(r.Arr("partners")) != 10 {
		t.Fatalf("ten heroes: %v", r)
	}
	c.recv(23201)
	if st := c.recv(23230); st.Int("times") != 11 || st.Int("must_count") != 99 {
		t.Fatalf("state after ten: %v", st)
	}

	// the draw that reaches the guarantee is the up hero and starts the count again
	if err := e.st.SaveEliteSummon(role.RID, eliteStateOf(11, 109, first.Item, "")); err != nil {
		t.Fatal(err)
	}
	draw(1)
	c.recv(10302)
	c.recv(10500)
	c.recv(11001)
	r = c.recv(23201)
	if got := r.Arr("partner_bids")[0].Int("partner_bid"); got != first.BID {
		t.Fatalf("the up hero was due: %v", r)
	}
	if st := c.recv(23230); st.Int("must_count") != 110 || st.Int("times") != 12 {
		t.Fatalf("after the up hero: %v", st)
	}
}

func TestElitePickAndAwards(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(23233, proto.Message{"lucky_bid": 12345})
	if r := c.recv(23233); r.Int("flag") != 0 {
		t.Fatalf("not a wish hero: %v", r)
	}
	pick := eliteTable.Wish[3]
	c.send(23233, proto.Message{"lucky_bid": pick.Item})
	if r := c.recv(23233); r.Int("flag") != 1 {
		t.Fatalf("pick: %v", r)
	}
	if r := c.recv(23230); r.Int("lucky_bid") != pick.Item {
		t.Fatalf("picked: %v", r)
	}
	claim := func(id int64) proto.Message {
		c.send(23232, proto.Message{"award_id": id})
		return nil
	}
	claim(1) // 30 draws needed
	if r := c.recv(23232); r.Int("flag") != 0 {
		t.Fatalf("too early: %v", r)
	}
	if err := e.st.SaveEliteSummon(role.RID, eliteStateOf(30, 0, pick.Item, "")); err != nil {
		t.Fatal(err)
	}
	claim(1)
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(23232); r.Int("flag") != 1 || r.Int("award_id") != 1 {
		t.Fatalf("claim: %v", r)
	}
	if r := c.recv(23230); len(r.Arr("reward_list")) != 1 || r.Arr("reward_list")[0].Int("id") != 1 {
		t.Fatalf("claimed list: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[10408] != 3 {
		t.Fatalf("reward: %v", items)
	}
	claim(1)
	if r := c.recv(23232); r.Int("flag") != 0 {
		t.Fatalf("twice: %v", r)
	}
}
