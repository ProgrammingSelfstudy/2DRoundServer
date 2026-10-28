package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// until reads frames until the command comes and returns it.
func (c *client) until(cmd uint16) proto.Message {
	c.t.Helper()
	for i := 0; i < 40; i++ {
		got, m := c.recvNext()
		if got == cmd {
			return m
		}
	}
	c.t.Fatalf("command %d never came", cmd)
	return nil
}

func artBag(t *testing.T, c *client) []proto.Message {
	t.Helper()
	c.send(10500, proto.Message{})
	var out []proto.Message
	for _, it := range c.until(10500).Arr("item_list") {
		if isArtifact(it.Int("base_id")) {
			out = append(out, it)
		}
	}
	return out
}

func TestArtifact(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 6}}); err != nil {
		t.Fatal(err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero := hs[0].PartnerID
	_, atk0, _, _ := partnerStats(hs[0])
	// three pieces of tier 2 (one flat attribute, one skill), 50 of the tier 1 and enough materials
	e.give(c, role, map[int64]int64{10452: 3, 10451: 50, 10450: 100000})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 10000000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.until(10801)
	bag := artBag(t, c)
	if len(bag) != 53 {
		t.Fatalf("pieces: %d", len(bag))
	}
	var tier2 []proto.Message
	for _, it := range bag {
		if it.Int("base_id") == 10452 {
			tier2 = append(tier2, it)
		}
	}
	p := tier2[0]
	if p.Int("id")/artSerial != 10452 || len(p.Arr("extra_attr")) != 1 || len(p.Arr("extra")) != 1 || p.Arr("extra")[0].Int("extra_k") != 1 {
		t.Fatalf("piece: %v", p)
	}
	// wear it: the hero's list shows it, the bag loses one
	c.send(11030, proto.Message{"partner_id": hero, "pos_id": 1, "artifact_id": p.Int("id"), "type": 1})
	if r := c.until(11030); r.Int("result") != 1 {
		t.Fatalf("wear: %v", r)
	}
	hv := c.until(11031)
	if len(hv.Arr("artifacts")) != 1 || hv.Arr("artifacts")[0].Int("artifact_pos") != 1 || hv.Arr("artifacts")[0].Int("id") != p.Int("id") {
		t.Fatalf("hero view: %v", hv)
	}
	if bag = artBag(t, c); len(bag) != 52 {
		t.Fatalf("bag after wearing: %d", len(bag))
	}
	hs, _ = e.st.PartnersOf(role.RID)
	_, atk1, _, _ := partnerStats(hs[0])
	flat := p.Arr("extra_attr")[0]
	if flat.Int("attr_id") == 101 && atk1 != atk0+flat.Int("attr_val") {
		t.Fatalf("attack %d -> %d with %v", atk0, atk1, flat)
	}
	// the same piece cannot be worn twice; a second one takes the position and the first goes back
	c.send(11030, proto.Message{"partner_id": hero, "pos_id": 1, "artifact_id": p.Int("id"), "type": 1})
	if r := c.until(11030); r.Int("result") != 0 {
		t.Fatalf("twice: %v", r)
	}
	c.send(11030, proto.Message{"partner_id": hero, "pos_id": 1, "artifact_id": tier2[1].Int("id"), "type": 1})
	if r := c.until(11030); r.Int("result") != 1 {
		t.Fatalf("swap: %v", r)
	}
	if bag = artBag(t, c); len(bag) != 52 {
		t.Fatalf("bag after swap: %d", len(bag))
	}
	c.send(11030, proto.Message{"partner_id": hero, "pos_id": 1, "artifact_id": tier2[1].Int("id"), "type": 0})
	if r := c.until(11030); r.Int("result") != 1 {
		t.Fatalf("take off: %v", r)
	}
	if bag = artBag(t, c); len(bag) != 53 {
		t.Fatalf("bag after taking off: %d", len(bag))
	}
	// recast: new skills wait in extra_k 3, saving them makes them the piece's
	c.send(11033, proto.Message{"partner_id": 0, "artifact_id": p.Int("id"), "skills": []proto.Message{}, "luck_item": 0})
	if r := c.until(11033); r.Int("result") != 1 {
		t.Fatalf("recast: %v", r)
	}
	var pend int64
	for _, it := range artBag(t, c) {
		if it.Int("id") == p.Int("id") {
			for _, x := range it.Arr("extra") {
				if x.Int("extra_k") == 3 {
					pend = x.Int("extra_v")
				}
			}
		}
	}
	if pend == 0 {
		t.Fatal("no pending skill")
	}
	c.send(11034, proto.Message{"partner_id": 0, "artifact_id": p.Int("id"), "type": 0})
	if r := c.until(11034); r.Int("result") != 1 {
		t.Fatalf("save: %v", r)
	}
	for _, it := range artBag(t, c) {
		if it.Int("id") == p.Int("id") {
			if len(it.Arr("extra")) != 1 || it.Arr("extra")[0].Int("extra_k") != 1 || it.Arr("extra")[0].Int("extra_v") != pend {
				t.Fatalf("saved: %v", it)
			}
		}
	}
	// decompose one: 300 of 10450 come back (data_artifact_resolve), the piece is gone
	before, _ := e.st.RoleItems(role.RID)
	c.send(11035, proto.Message{"artifact_id": []proto.Message{{"id": tier2[2].Int("id")}}})
	if r := c.until(11035); r.Int("result") != 1 {
		t.Fatalf("decompose: %v", r)
	}
	after, _ := e.st.RoleItems(role.RID)
	if after[10450]-before[10450] != 300 || after[10452] != before[10452]-1 {
		t.Fatalf("ledger: %v -> %v", before, after)
	}
	// compose: two of tier 1 into tier 2 (20%): either a new piece or the melting value
	var mats []proto.Message
	for _, it := range artBag(t, c) {
		if it.Int("base_id") == 10451 && len(mats) < 2 {
			mats = append(mats, proto.Message{"artifact_id": it.Int("id")})
		}
	}
	e.st.SetRoleLevel(role.RID, 60)
	c.send(11036, proto.Message{"item_id": 10452, "expends": mats})
	r := c.until(11036)
	if r.Int("result") != 1 {
		t.Fatalf("compose: %v", r)
	}
	d, _ := e.st.ArtifactsOf(role.RID)
	if (r.Int("flag") == 1) != (d.Lucky == 0) {
		t.Fatalf("flag %d with melting value %d", r.Int("flag"), d.Lucky)
	}
	if r.Int("flag") == 0 && d.Lucky != 4 {
		t.Fatalf("melting value %d", d.Lucky)
	}
	// one material is not enough
	c.send(11036, proto.Message{"item_id": 10452, "expends": mats[:1]})
	if r := c.until(11036); r.Int("result") != 0 {
		t.Fatalf("one material: %v", r)
	}
}

func TestSyncArtifacts(t *testing.T) {
	d := &store.ArtifactDoc{Next: map[int64]int64{}}
	if !syncArtifacts(d, map[int64]int64{10457: 2, 10451: 1}) || len(d.Pieces) != 3 {
		t.Fatalf("%v", d.Pieces)
	}
	for _, a := range d.Pieces {
		if a.Base == 10457 && (len(a.Attr) != 3 || len(a.Skills) != 3) {
			t.Fatalf("top tier: %+v", a)
		}
		seen := map[int64]bool{}
		for _, x := range a.Attr {
			if seen[x.ID] {
				t.Fatalf("attribute twice: %+v", a)
			}
			seen[x.ID] = true
		}
		sk := map[int64]bool{}
		for _, s := range a.Skills {
			if s == 0 || sk[s] {
				t.Fatalf("skills: %+v", a)
			}
			sk[s] = true
		}
	}
	if syncArtifacts(d, map[int64]int64{10457: 2, 10451: 1}) {
		t.Fatal("changed again")
	}
	syncArtifacts(d, map[int64]int64{10457: 1})
	if len(d.Pieces) != 1 || d.Pieces[0].ID != 10457*artSerial+1 {
		t.Fatalf("shrunk: %+v", d.Pieces)
	}
}
