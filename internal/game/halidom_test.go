package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestHalidom(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("hd", "信念人")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10402, Star: 4}, {BID: 10403, Star: 1}, {BID: 10404, Star: 4}}); err != nil || !ok {
		t.Fatalf("heroes: %v %v", ok, err)
	}
	heroes := func() map[int64]store.Partner {
		l, _ := e.st.PartnersOf(role.RID)
		m := map[int64]store.Partner{}
		for _, h := range l {
			m[h.PartnerID] = h
		}
		return m
	}
	var star4, star1 []int64
	for id, h := range heroes() {
		if h.Star == 4 && h.BID != 10401 {
			star4 = append(star4, id)
		}
		if h.Star == 1 {
			star1 = append(star1, id)
		}
	}
	ids := func(l ...int64) []proto.Message {
		var out []proto.Message
		for _, id := range l {
			out = append(out, proto.Message{"id": id})
		}
		return out
	}

	c.send(22200, proto.Message{})
	if r := c.waitFor(22200); len(r.Arr("list")) != 0 {
		t.Fatalf("22200: %v", r)
	}
	// the role must be at the open level, and pay with the right heroes
	c.send(22202, proto.Message{"id": 1, "list": ids(star4[0])})
	if r := c.waitFor(22202); r.Int("code") != 0 {
		t.Fatalf("unlock below the open level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, int(halidomCfg.OpenLev)); err != nil {
		t.Fatal(err)
	}
	c.send(22202, proto.Message{"id": 1, "list": ids(star1[0])})
	if r := c.waitFor(22202); r.Int("code") != 0 {
		t.Fatalf("unlock with a 1 star hero: %v", r)
	}
	c.send(22202, proto.Message{"id": 1, "list": ids(star4[0])})
	if r := c.waitFor(22202); r.Int("code") != 1 {
		t.Fatalf("unlock: %v", r)
	}
	if r := c.waitFor(22201); r.Int("id") != 1 || r.Int("lev") != 1 || len(r.Arr("all_attr")) == 0 {
		t.Fatalf("22201: %v", r)
	}
	if _, ok := heroes()[star4[0]]; ok {
		t.Fatal("the hero was not consumed")
	}
	c.send(22202, proto.Message{"id": 1, "list": ids(star4[1])})
	if r := c.waitFor(22202); r.Int("code") != 0 {
		t.Fatalf("unlock twice: %v", r)
	}

	// level up: two clicks of 10 exp make a level
	c.send(22203, proto.Message{"id": 1})
	if r := c.waitFor(22203); r.Int("code") != 0 {
		t.Fatalf("no materials: %v", r)
	}
	e.addItems(t, role, map[int64]int64{17001: 100})
	e.grantAssets(c, role, map[int64]int64{1: 1000000})
	c.send(22203, proto.Message{"id": 1})
	if r := c.waitFor(22203); r.Int("code") != 1 {
		t.Fatalf("click: %v", r)
	}
	if r := c.waitFor(22201); r.Int("lev") != 1 || r.Int("exp") != 10 {
		t.Fatalf("after one click: %v", r)
	}
	c.send(22203, proto.Message{"id": 1})
	c.waitFor(22203)
	if r := c.waitFor(22201); r.Int("lev") != 2 || r.Int("exp") != 0 {
		t.Fatalf("after two clicks: %v", r)
	}
	if r := c.waitFor(22205); r.Int("id") != 1 {
		t.Fatalf("22205: %v", r)
	}

	// step: needs the level of its condition, then the items and a hero of the camp
	c.send(22204, proto.Message{"id": 1})
	if r := c.waitFor(22204); r.Int("code") != 0 {
		t.Fatalf("step at level 2: %v", r)
	}
	hdMu.Lock()
	st := &hdState{}
	e.st.LoadState(role.RID, halidomModule, st)
	st.Halidoms["1"].Lev = 10
	e.st.SaveState(role.RID, halidomModule, st)
	hdMu.Unlock()
	c.send(22204, proto.Message{"id": 1, "list2": ids(star4[1])})
	if r := c.waitFor(22204); r.Int("code") != 0 {
		t.Fatalf("step without items: %v", r)
	}
	e.addItems(t, role, map[int64]int64{17011: 50})
	c.send(22204, proto.Message{"id": 1, "list2": ids(star4[1])})
	if r := c.waitFor(22204); r.Int("code") != 1 {
		t.Fatalf("step: %v", r)
	}
	if r := c.waitFor(22201); r.Int("step") != 1 {
		t.Fatalf("after the step: %v", r)
	}
	if _, ok := heroes()[star4[1]]; ok {
		t.Fatal("the step hero was not consumed")
	}
}
