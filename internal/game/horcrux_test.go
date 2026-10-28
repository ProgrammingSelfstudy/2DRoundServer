package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestHorcrux(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10502, Star: 5}, {BID: 10401, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero, other := hs[0].PartnerID, hs[1].PartnerID
	if hs[0].BID != 10502 {
		t.Fatal("order", hs[0].BID)
	}
	cmd := func(cmd uint16, pid int64, _ string) proto.Message {
		c.send(cmd, proto.Message{"partner_id": pid})
		r := c.until(cmd)
		return r
	}
	if r := cmd(11404, other, ""); r.Int("result") != 0 { // 10401 has no horcrux in the table
		t.Fatalf("no horcrux: %v", r)
	}
	if _, ok := horcruxTable.Heroes[10401]; ok {
		t.Skip("10401 has a horcrux")
	}
	if r := cmd(11404, hero, ""); r.Int("result") != 1 {
		t.Fatalf("can awaken: %v", r)
	}
	if r := cmd(11402, hero, ""); r.Int("result") != 0 || r.Str("msg") != "资产不足" {
		t.Fatalf("awaken without materials: %v", r)
	}
	e.give(c, role, map[int64]int64{10051: 100, 10052: 100, 37005: 100})
	_, atk0, _, _ := partnerStats(hs[0])
	if r := cmd(11402, hero, ""); r.Int("result") != 1 {
		t.Fatalf("awaken: %v", r)
	}
	if r := cmd(11402, hero, ""); r.Int("result") != 0 {
		t.Fatalf("twice: %v", r)
	}
	if left, _ := e.st.RoleItems(role.RID); left[10051] != 80 {
		t.Fatalf("awaken cost: %v", left)
	}
	hs, _ = e.st.PartnersOf(role.RID)
	if hs[0].Horcrux.Status != 1 || hs[0].Horcrux.Lev != 0 {
		t.Fatalf("state: %+v", hs[0].Horcrux)
	}
	_, atk1, _, _ := partnerStats(hs[0])
	if atk1 != atk0+500 { // attack slot of level 0 (attr6)
		t.Fatalf("attack %d -> %d", atk0, atk1)
	}
	// strengthen: level 0 costs 10 coin (asset 1), which the role has not
	if r := cmd(11403, hero, ""); r.Int("result") != 0 || r.Str("msg") != "资产不足" {
		t.Fatalf("no coin: %v", r)
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 1000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.until(10801)
	if r := cmd(11403, hero, ""); r.Int("result") != 1 {
		t.Fatalf("strengthen: %v", r)
	}
	hs, _ = e.st.PartnersOf(role.RID)
	if hs[0].Horcrux.Lev != 1 {
		t.Fatalf("level: %+v", hs[0].Horcrux)
	}
	// take the level back: costs 37005 x10, pays 100 coin
	before, _ := e.st.RoleItems(role.RID)
	if r := cmd(11405, hero, ""); r.Int("code") != 1 {
		t.Fatalf("fall back: %v", r)
	}
	after, _ := e.st.RoleItems(role.RID)
	if after[37005] != before[37005]-10 {
		t.Fatalf("fall back cost: %v -> %v", before, after)
	}
	hs, _ = e.st.PartnersOf(role.RID)
	if hs[0].Horcrux.Lev != 0 || hs[0].Horcrux.Status != 1 {
		t.Fatalf("after falling back: %+v", hs[0].Horcrux)
	}
	if r := cmd(11405, hero, ""); r.Int("code") != 0 {
		t.Fatalf("nothing to take back: %v", r)
	}
}
