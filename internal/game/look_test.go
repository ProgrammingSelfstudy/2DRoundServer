package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestLookAndStory(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	if _, err := e.st.GrantStarterPartners(b.RID, []store.PartnerSeed{{BID: 10501, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	heroes, err := e.st.PartnersOf(b.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatal(err)
	}
	h := heroes[0]
	ca.send(11061, proto.Message{"r_rid": b.RID, "r_srvid": b.SrvID, "partner_id": h.PartnerID})
	r := ca.waitFor(11061)
	if r.Int("bid") != h.BID || r.Int("partner_id") != h.PartnerID || r.Int("r_rid") != b.RID || r.Int("power") == 0 {
		t.Fatalf("11061: %v", r)
	}
	ca.send(11062, proto.Message{"id": h.PartnerID, "srv_id": b.SrvID})
	if r := ca.waitFor(11062); r.Int("bid") != h.BID || r.Int("r_rid") != b.RID {
		t.Fatalf("11062: %v", r)
	}
	ca.send(11061, proto.Message{"r_rid": b.RID, "partner_id": h.PartnerID + 1000})
	if r := ca.waitFor(11061); r.Int("bid") != 0 {
		t.Fatalf("unknown hero: %v", r)
	}
	ca.send(11062, proto.Message{"id": 99999999})
	if r := ca.waitFor(11062); r.Int("bid") != 0 {
		t.Fatalf("unknown shared hero: %v", r)
	}
	ca.send(11102, proto.Message{"drama_bid": 1})
	if r := ca.waitFor(11102); r.Int("code") != 1 {
		t.Fatalf("skip: %v", r)
	}
	ca.send(11100, proto.Message{"drama_bid": 1, "step_id": 2})
	ca.waitFor(11100)
}

func TestGuideAndSceneAcks(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("a", "甲")
	c.send(11121, proto.Message{"id": 7, "n": 3})
	if r := c.waitFor(11121); r.Int("id") != 7 || r.Int("n") != 3 {
		t.Fatalf("11121: %v", r)
	}
	c.send(11122, proto.Message{"id": 7, "is_skip": 1})
	c.waitFor(11122)
	c.send(10956, proto.Message{"key": 1, "val": 2})
	c.waitFor(10956)
	c.send(20063, proto.Message{})
	if r := c.waitFor(20063); len(r.Arr("type_list")) != 0 {
		t.Fatalf("20063: %v", r)
	}
}
