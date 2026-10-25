package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestTalent(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// a star-6 hero opens slot 1 only, a star-5 hero none; 10101 is career 2
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 6}, {BID: 10101, Star: 5}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero, weak := hs[0].PartnerID, hs[1].PartnerID
	talentCmd := func(cmd uint16, pid, pos, skill int64) proto.Message {
		c.send(cmd, proto.Message{"partner_id": pid, "pos": pos, "skill_id": skill})
		return c.recv(cmd)
	}
	// 700061 (no career limit) costs one 16007; nothing in the bag yet
	if r := talentCmd(11096, hero, 1, 700061); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("no stone: %v", r)
	}
	e.give(c, role, map[int64]int64{16007: 5, 10450: 30000})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 3, Num: 500}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	if r := talentCmd(11096, weak, 1, 700061); r.Int("result") != 0 {
		t.Fatalf("slot closed at star 5: %v", r)
	}
	if r := talentCmd(11096, hero, 2, 700061); r.Int("result") != 0 {
		t.Fatalf("slot 2 closed at star 6: %v", r)
	}
	if r := talentCmd(11096, hero, 1, 12345); r.Int("result") != 0 {
		t.Fatalf("not a talent skill: %v", r)
	}
	if r := talentCmd(11096, 99999, 1, 700061); r.Int("result") != 0 {
		t.Fatalf("not own: %v", r)
	}
	c.send(11096, proto.Message{"partner_id": hero, "pos": 1, "skill_id": 700061})
	c.recv(10302)
	c.recv(10500)
	r := c.recv(11096)
	if r.Int("result") != 1 || r.Int("partner_id") != hero || len(r.Arr("dower_skill")) != 1 || r.Arr("dower_skill")[0].Int("skill_id") != 700061 {
		t.Fatalf("learn: %v", r)
	}
	if r := talentCmd(11096, hero, 1, 700131); r.Int("result") != 0 {
		t.Fatalf("slot taken: %v", r)
	}
	// level up: 3 stones and 10000 of 10450 per step
	c.send(11097, proto.Message{"partner_id": hero, "pos": 1})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11097); r.Int("result") != 1 || r.Arr("dower_skill")[0].Int("skill_id") != 700063 {
		t.Fatalf("level up: %v", r)
	}
	// 11099 reports it
	c.send(11099, proto.Message{"partner_ids": []proto.Message{{"partner_id": hero}, {"partner_id": weak}}})
	got := c.recv(11099).Arr("partner_ids")
	if len(got) != 2 || len(got[0].Arr("dower_skill")) != 1 || len(got[1].Arr("dower_skill")) != 0 {
		t.Fatalf("11099: %v", got)
	}
	// forget: 100 gems, refunds 10450 per the table; the slot empties
	c.send(11098, proto.Message{"partner_id": hero, "pos": 1})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11098); r.Int("result") != 1 || len(r.Arr("dower_skill")) != 0 {
		t.Fatalf("forget: %v", r)
	}
	if p, _ := e.st.PartnerByID(role.RID, hero); len(p.Talents) != 0 {
		t.Fatalf("not forgotten: %v", p.Talents)
	}
	if r := talentCmd(11098, hero, 1, 0); r.Int("result") != 0 {
		t.Fatalf("forget an empty slot: %v", r)
	}
}
