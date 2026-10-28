package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestEmpower(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(time.Date(2026, 10, 20, 12, 0, 0, 0, dayZone))
	c, role := e.enter("em1", "赋能人")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10502, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	src := hs[0].PartnerID
	c.send(26420, proto.Message{})
	if r := c.waitFor(26420); r.Int("num") != 5 || r.Int("star") != 0 {
		t.Fatalf("before the stone: %v", r)
	}
	req := proto.Message{"partner_id": src, "skills": []proto.Message{}}
	c.send(26421, req)
	if r := c.waitFor(26421); r.Int("result") != 0 {
		t.Fatalf("without enough offering stars: %v", r)
	}
	// 5 heroes of 6 stars have been offered at the most: 30 stars, star 6 for the new hero
	if err := e.st.RaiseMaxStars(role.RID, 30); err != nil {
		t.Fatal(err)
	}
	c.send(26420, proto.Message{})
	if r := c.waitFor(26420); r.Int("star") != 6 || r.Int("num") != 5 {
		t.Fatalf("star: %v", r)
	}
	c.send(26423, req)
	if r := c.waitFor(26423); r.Int("power") == 0 || r.Int("partner_id") != src {
		t.Fatalf("preview: %v", r)
	}
	c.send(26421, req)
	if r := c.waitFor(26421); r.Int("result") != 0 || r.Str("msg") != "材料不足" {
		t.Fatalf("without materials: %v", r)
	}
	cost := empowerCfg.Cost[6]
	e.give(c, role, map[int64]int64{cost.Item[0][0]: 3})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 5000000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.until(10801)
	c.send(26421, req)
	if r := c.until(26421); r.Int("result") != 1 {
		t.Fatalf("empower: %v", r)
	}
	r := c.until(26424)
	hs, _ = e.st.PartnersOf(role.RID)
	if len(hs) != 2 {
		t.Fatalf("heroes: %d", len(hs))
	}
	nh := hs[1]
	if nh.PartnerID != r.Int("partner_id") || nh.BID != 10502 || nh.Star != 6 || nh.Lev != partnerTable[10502].Stars["6"].LevMax || !nh.Locked ||
		nh.EndTime != time.Date(2026, 10, 21, 12, 0, 0, 0, dayZone).Unix() {
		t.Fatalf("new hero: %+v", nh)
	}
	c.send(26420, proto.Message{})
	if r := c.until(26420); r.Int("num") != 4 {
		t.Fatalf("left: %v", r)
	}
	// an empowered hero is not a source
	c.send(26421, proto.Message{"partner_id": nh.PartnerID, "skills": []proto.Message{}})
	if r := c.until(26421); r.Int("result") != 0 {
		t.Fatalf("source: %v", r)
	}
	// a day on, it is gone, with what it wore back in the bag
	e.setNow(time.Date(2026, 10, 21, 12, 0, 1, 0, dayZone))
	c.send(11000, proto.Message{})
	list := c.until(11000)
	if len(list.Arr("partners")) != 1 {
		t.Fatalf("after a day: %v", list)
	}
	if d, _ := e.st.EmpowerOf(role.RID); len(d.Heroes) != 0 {
		t.Fatalf("state: %+v", d)
	}
	// the count starts again the next day
	c.send(26420, proto.Message{})
	if r := c.until(26420); r.Int("num") != 5 {
		t.Fatalf("new day: %v", r)
	}
}
