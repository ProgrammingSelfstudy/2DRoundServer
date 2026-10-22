package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSeerChangeTargets(t *testing.T) {
	for _, bid := range []int64{10401, 10501} {
		p := partnerTable[bid]
		for _, to := range seerChangeTargets(bid, p.InitStar) {
			q := partnerTable[to]
			if to == bid || q.Camp != p.Camp || q.InitStar != p.InitStar {
				t.Fatalf("%d -> %d", bid, to)
			}
		}
		if len(seerChangeTargets(bid, p.InitStar)) == 0 {
			t.Fatalf("%d has no target", bid)
		}
	}
	if seerChangeTargets(10101, 1) != nil || seerChangeTargets(1, 5) != nil {
		t.Fatal("only 4-star and 5-star heroes of water, fire and wind")
	}
}

func TestSeerChange(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 4}, {BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatal(err)
	}
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	hs, _ := e.st.PartnersOf(role.RID)
	hero, weak := hs[0], hs[1]
	act := func(pid, action int64) proto.Message {
		c.send(23215, proto.Message{"partner_id": pid, "action": action})
		return c.recv(23215)
	}
	c.send(23214, proto.Message{})
	if r := c.recv(23214); r.Int("partner_id") != 0 {
		t.Fatalf("nothing pending: %v", r)
	}
	if r := act(hero.PartnerID, 1); r.Int("code") != 0 { // no essence
		t.Fatalf("no essence: %v", r)
	}
	e.give(c, role, map[int64]int64{14002: 45})
	if r := act(weak.PartnerID, 1); r.Int("code") != 0 { // 1 star
		t.Fatalf("1-star hero: %v", r)
	}
	// a started swap: the cost, the locked hero, the swap, then the answer with an empty msg
	start := func(pid int64) {
		c.send(23215, proto.Message{"partner_id": pid, "action": 1})
		c.recv(10302)
		c.recv(10500)
		if r := c.recv(11002); r.Int("partner_id") != pid || len(r.Arr("is_lock")) != 1 {
			t.Fatalf("locked hero: %v", r)
		}
	}
	want := seerChangeTargets(10401, 4)[0]
	start(hero.PartnerID)
	if r := c.recv(23214); r.Int("partner_id") != hero.PartnerID || r.Int("new_partner_bid") != want {
		t.Fatalf("pending: %v", r)
	}
	if r := c.recv(23215); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("swap: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[14002] != 25 {
		t.Fatalf("essence: %v", items)
	}
	if r := act(hero.PartnerID, 1); r.Int("code") != 0 { // one at a time
		t.Fatalf("twice: %v", r)
	}
	if r := act(hero.PartnerID, 0); r.Int("code") != 1 { // cancel
		t.Fatalf("cancel: %v", r)
	}
	c.recv(23214)
	if r := act(hero.PartnerID, 2); r.Int("code") != 0 { // nothing to save
		t.Fatalf("save nothing: %v", r)
	}
	start(hero.PartnerID)
	c.recv(23214)
	c.recv(23215)
	// saved: a new hero with a new partner id, the old one gone, then the swap cleared
	c.send(23215, proto.Message{"partner_id": hero.PartnerID, "action": 2})
	r := c.recv(11001)
	new := r.Arr("partners")[0]
	if new.Int("bid") != want || new.Int("partner_id") == hero.PartnerID {
		t.Fatalf("new hero: %v", r)
	}
	if r := c.recv(11006); len(r.Arr("expend2")) != 1 || r.Arr("expend2")[0].Int("partner_id") != hero.PartnerID {
		t.Fatalf("old hero gone: %v", r)
	}
	if r := c.recv(23214); r.Int("partner_id") != 0 {
		t.Fatalf("cleared: %v", r)
	}
	if r := c.recv(23215); r.Int("code") != 1 {
		t.Fatalf("save: %v", r)
	}
	if _, err := e.st.PartnerByID(role.RID, hero.PartnerID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old hero still there: %v", err)
	}
	p, _ := e.st.PartnerByID(role.RID, new.Int("partner_id"))
	if p.BID != want || p.Star != 4 || p.Lev != hero.Lev {
		t.Fatalf("hero: %+v", p)
	}
}

// A saved swap makes a new hero, and its worn equipment and formation place move with it.
func TestSeerChangeKeepsEquipmentAndFormation(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 4}}); err != nil || !ok {
		t.Fatal(err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	old := hs[0]
	e.give(c, role, map[int64]int64{40101: 1})
	if err := e.st.EquipPiece(role.RID, old.PartnerID, 1, 40101); err != nil {
		t.Fatal(err)
	}
	f := store.Formation{Type: 1, FormationType: 1, Slots: []store.FormationSlot{{Pos: 3, PartnerID: old.PartnerID}}}
	if err := e.st.SaveFormation(role.RID, f); err != nil {
		t.Fatal(err)
	}
	if err := e.st.StartPartnerChange(role.RID, old.PartnerID, 10509, nil, nil); err != nil {
		t.Fatal(err)
	}
	np, err := e.st.SavePartnerChange(role.RID, old.PartnerID)
	if err != nil {
		t.Fatal(err)
	}
	if np.PartnerID == old.PartnerID || np.BID != 10509 || len(np.Equips) != 1 || np.Equips[0].BaseID != 40101 {
		t.Fatalf("new hero: %+v", np)
	}
	got, err := e.st.Formation(role.RID, 1)
	if err != nil || len(got.Slots) != 1 || got.Slots[0].PartnerID != np.PartnerID || got.Slots[0].Pos != 3 {
		t.Fatalf("formation: %+v %v", got, err)
	}
}
