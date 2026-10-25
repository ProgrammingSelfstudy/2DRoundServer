package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestCrystalOf(t *testing.T) {
	mk := func(id int64, lev int, slot int64) store.Partner {
		return store.Partner{PartnerID: id, Lev: lev, ResSlot: slot}
	}
	// the capture: levels 165, 40 and three of 30; five more of 30 sit in slots and do not count
	all := []store.Partner{mk(388, 165, 0), mk(13, 40, 0), mk(844, 30, 0), mk(709, 30, 0), mk(522, 30, 0),
		mk(904, 30, 1), mk(761, 30, 2), mk(579, 30, 3), mk(564, 30, 4), mk(248, 30, 5)}
	lev, core := crystalOf(all)
	var ids []int64
	for _, p := range core {
		ids = append(ids, p.PartnerID)
	}
	if lev != 30 || len(ids) != 5 || ids[0] != 522 || ids[1] != 709 || ids[2] != 844 || ids[3] != 13 || ids[4] != 388 {
		t.Fatalf("crystal %d %v", lev, ids)
	}
	if l, c := crystalOf(all[:4]); l != 0 || c != nil {
		t.Fatalf("four heroes make no crystal: %d %v", l, c)
	}
	// the break step shown at level 30 and 35 is 1, as in the capture (10503 is type 5, break id 501)
	if b := resonateBreak(store.Partner{BID: 10503}, 30); b != 1 {
		t.Fatalf("break at 30: %d", b)
	}
}

func TestResonanceCrystal(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	seeds := []store.PartnerSeed{}
	for i := 0; i < 7; i++ {
		seeds = append(seeds, store.PartnerSeed{BID: 10503, Star: 5})
	}
	if ok, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	for i, lev := range []int{10, 20, 20, 25, 30} { // the core: the crystal is level 10
		if err := e.st.UpgradePartner(role.RID, hs[i].PartnerID, 1, 0, lev, 0, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	low := hs[5].PartnerID
	state := func() proto.Message {
		c.send(26425, proto.Message{})
		return c.recv(26425)
	}
	st0 := state()
	if st0.Int("lev") != 10 || len(st0.Arr("con_list")) != 5 || st0.Arr("con_list")[0].Int("id") != hs[0].PartnerID || len(st0.Arr("res_list")) != 1 || st0.Int("max_cystal_lev") != 550 {
		t.Fatalf("state: %v", st0)
	}
	put := func(id, pos int64) proto.Message {
		c.send(26426, proto.Message{"partner_id": id, "pos": pos})
		return c.recv(26426)
	}
	if r := put(low, 2); r.Int("result") != 0 || r.Str("msg") == "" { // only slot 1 is open
		t.Fatalf("locked slot: %v", r)
	}
	if r := put(hs[4].PartnerID, 1); r.Int("result") != 0 { // a core hero
		t.Fatalf("core hero: %v", r)
	}
	c.send(26426, proto.Message{"partner_id": low, "pos": 1})
	if m := c.recv(11002); m.Int("lev") != 10 || m.Int("resonate_lev") != 1 {
		t.Fatalf("hero in the slot shows the crystal's level: %v", m)
	}
	if st1 := c.recv(26425); st1.Arr("res_list")[0].Int("id") != low {
		t.Fatalf("state after put: %v", st1)
	}
	if r := c.recv(26426); r.Int("result") != 1 {
		t.Fatalf("put: %v", r)
	}
	if p, _ := e.st.PartnerByID(role.RID, low); p.Lev != 1 || p.ShownLev != 10 {
		t.Fatalf("the real level stays: %+v", p)
	}

	// the weakest core hero grows from 10 to 15 (levels 10 -> 15 cost the table's sums): the crystal
	// is still 20 / 20 / 25 / 30 behind it -> level 15 -> the hero in the slot follows
	need := map[int64]int64{}
	for lev := 10; lev < 15; lev++ {
		for _, g := range growth.Costs[lev-1] {
			need[g.ID] += g.Amount
		}
	}
	var atts []store.Attachment
	for id, n := range need {
		atts = append(atts, store.Attachment{Kind: store.AttachAsset, ItemID: id, Num: n * 2})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	c.send(11003, proto.Message{"partner_id": hs[0].PartnerID})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11002); r.Int("partner_id") != hs[0].PartnerID {
		t.Fatalf("first the hero: %v", r)
	}
	if r := c.recv(11002); r.Int("partner_id") != low || r.Int("lev") != 15 {
		t.Fatalf("then the hero in the slot follows: %v", r)
	}
	if r := c.recv(11003); r.Int("result") != 1 {
		t.Fatalf("level up: %v", r)
	}

	// take it out: a day of cool-down; ending it costs gems; putting in again works after
	c.send(26427, proto.Message{"partner_id": low, "pos": 1})
	if r := c.recv(11002); r.Int("partner_id") != low || r.Int("lev") != 1 || r.Int("resonate_lev") != 0 {
		t.Fatalf("back to its own level: %v", r)
	}
	st2 := c.recv(26425)
	if cool := st2.Arr("res_list")[0].Int("cool_time"); cool < time.Now().Unix()+86000 || st2.Arr("res_list")[0].Int("id") != 0 {
		t.Fatalf("cooling: %v", st2.Arr("res_list"))
	}
	if r := c.recv(26427); r.Int("result") != 1 {
		t.Fatalf("take off: %v", r)
	}
	if r := put(low, 1); r.Int("result") != 0 {
		t.Fatalf("cooling slot: %v", r)
	}
	c.send(26428, proto.Message{"pos": 1})
	if r := c.recv(26428); r.Int("result") != 0 { // no gems
		t.Fatalf("flush without gems: %v", r)
	}
	gems := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 3, Num: 800}}}, role.RID)
	c.send(10801, proto.Message{"id": gems.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	c.send(26428, proto.Message{"pos": 1})
	c.recv(10302)
	c.recv(10500)
	c.recv(26425)
	if r := c.recv(26428); r.Int("result") != 1 || r.Int("pos") != 1 {
		t.Fatalf("flush: %v", r)
	}
	// unlock slot 2 with gems (500 for the first one), only the next slot, only once
	c.send(26429, proto.Message{"pos": 3, "type": 2})
	if r := c.recv(26429); r.Int("result") != 0 {
		t.Fatalf("skipping a slot: %v", r)
	}
	c.send(26429, proto.Message{"pos": 2, "type": 2})
	c.recv(10302)
	c.recv(10500)
	if st3 := c.recv(26425); len(st3.Arr("res_list")) != 2 || st3.Int("gold_count") != 1 {
		t.Fatalf("after unlock: %v", st3)
	}
	if r := c.recv(26429); r.Int("result") != 1 || r.Int("pos") != 2 {
		t.Fatalf("unlock: %v", r)
	}
}
