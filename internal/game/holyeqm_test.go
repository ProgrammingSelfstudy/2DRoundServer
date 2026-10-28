package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func holyBagOf(t *testing.T, c *client) []proto.Message {
	t.Helper()
	c.send(10501, proto.Message{})
	var out []proto.Message
	for _, it := range c.until(10501).Arr("item_list") {
		if holySlot(it.Int("base_id")) > 0 {
			out = append(out, it)
		}
	}
	return out
}

func TestHolyEqm(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 6}}); err != nil {
		t.Fatal(err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero := hs[0].PartnerID
	_, atk0, _, _ := partnerStats(hs[0])
	const base = 1010201 // an earring with two random attributes (suit 101)
	e.give(c, role, map[int64]int64{base: 3, 10030: 1000})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 100000000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.until(10801)
	bag := holyBagOf(t, c)
	if len(bag) != 3 {
		t.Fatalf("pieces: %d", len(bag))
	}
	p := bag[0]
	if len(p.Arr("main_attr")) != 3 || len(p.Arr("holy_eqm_attr")) != 2 || p.Arr("holy_eqm_attr")[0].Int("pos") != 1 {
		t.Fatalf("piece: %v", p)
	}
	ids := map[int64]bool{}
	for _, a := range p.Arr("holy_eqm_attr") {
		ids[a.Int("attr_id")] = true
		rg := holyTable.Bases[base].Range[holyAttrKeys[a.Int("attr_id")]]
		if a.Int("attr_val") < rg[0] || a.Int("attr_val") > rg[1] {
			t.Fatalf("value out of range %v: %v", rg, a)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("same attribute twice: %v", p)
	}
	// wear: the hero shows it, attack grows by the main attack
	c.send(11093, proto.Message{"partner_id": hero, "holy_eqm_id": p.Int("id"), "type": 1})
	if r := c.until(11093); r.Int("result") != 1 {
		t.Fatalf("wear: %v", r)
	}
	if hv := c.until(11091); len(hv.Arr("holy_eqm")) != 1 || hv.Arr("holy_eqm")[0].Int("id") != p.Int("id") {
		t.Fatalf("hero push: %v", hv)
	}
	if got := len(holyBagOf(t, c)); got != 2 {
		t.Fatalf("bag after wearing: %d", got)
	}
	c.send(11092, proto.Message{"partner_ids": []proto.Message{{"partner_id": hero}}})
	if r := c.until(11092); len(r.Arr("partner_ids")[0].Arr("holy_eqm")) != 1 {
		t.Fatalf("11092: %v", r)
	}
	hs, _ = e.st.PartnersOf(role.RID)
	_, atk1, _, _ := partnerStats(hs[0])
	if atk1 <= atk0 {
		t.Fatalf("attack %d -> %d", atk0, atk1)
	}
	// wash with nothing locked (the capture's request has an empty pos): costs the base's price, counts a wash, and the
	// new attributes wait next to the old ones (position + 100) until 11090 saves them
	before, _ := e.st.RoleItems(role.RID)
	c.send(11094, proto.Message{"partner_id": hero, "holy_eqm_id": p.Int("id"), "pos": []proto.Message{}})
	if r := c.until(11094); r.Int("result") != 1 || r.Str("msg") != "洗练成功" {
		t.Fatalf("wash: %v", r)
	}
	after, _ := e.st.RoleItems(role.RID)
	if after[10030] != before[10030]-holyTable.Bases[base].Price[1][1] {
		t.Fatalf("price: %v -> %v", before, after)
	}
	d, _ := e.st.HolyOf(role.RID)
	i := holyFind(d, p.Int("id"))
	old := append([]store.HolyAttr(nil), d.Pieces[i].Rand...)
	if d.Pieces[i].Washes != 1 || len(old) != 2 || len(d.Pieces[i].Wash) != 2 {
		t.Fatalf("washed: %+v", d.Pieces[i])
	}
	if l := holyRandMsgs(d.Pieces[i]); len(l) != 4 || l[0].Int("pos") != 1 || l[2].Int("pos") != 101 || l[3].Int("pos") != 102 {
		t.Fatalf("shown: %v", l)
	}
	c.send(11090, proto.Message{"partner_id": hero, "holy_eqm_id": p.Int("id"), "type": 1})
	if r := c.until(11090); r.Int("result") != 1 || r.Str("msg") != "" {
		t.Fatalf("save: %v", r)
	}
	d, _ = e.st.HolyOf(role.RID)
	if i = holyFind(d, p.Int("id")); len(d.Pieces[i].Wash) != 0 || len(d.Pieces[i].Rand) != 2 {
		t.Fatalf("saved: %+v", d.Pieces[i])
	}
	// a plan keeps the worn piece
	c.send(25221, proto.Message{"id": 1, "partner_id": hero, "name": "方案甲", "holy_eqm": []proto.Message{{"partner_id": hero, "item_id": p.Int("id")}}})
	if r := c.until(25221); r.Int("code") != 1 {
		t.Fatalf("plan: %v", r)
	}
	if r := c.until(25220); r.Int("num") != 3 || len(r.Arr("holy_eqm_set_cell")) != 1 || r.Arr("holy_eqm_set_cell")[0].Str("name") != "方案甲" {
		t.Fatalf("plans: %v", r)
	}
	c.send(25221, proto.Message{"id": 4, "partner_id": hero, "name": "x", "holy_eqm": []proto.Message{}})
	if r := c.until(25221); r.Int("code") != 0 {
		t.Fatalf("closed plan: %v", r)
	}
	// sell one in the bag: preview and sale (9 of 10030 each); a worn one cannot be sold
	bag = holyBagOf(t, c)
	c.send(11088, proto.Message{"item_ids": []proto.Message{{"item_id": bag[0].Int("id")}}})
	if r := c.until(11088); r.Int("code") != 1 || len(r.Arr("list")) != 1 || r.Arr("list")[0].Int("num") != 9 {
		t.Fatalf("preview: %v", r)
	}
	c.send(11089, proto.Message{"item_ids": []proto.Message{{"item_id": p.Int("id")}}})
	if r := c.until(11089); r.Int("code") != 0 {
		t.Fatalf("worn sale: %v", r)
	}
	before, _ = e.st.RoleItems(role.RID)
	c.send(11089, proto.Message{"item_ids": []proto.Message{{"item_id": bag[0].Int("id")}}})
	gain := c.until(12742) // the gain comes before the answer, which has an empty msg (capture original-2026-10-27)
	if l := gain.Arr("asset_list"); len(l) != 1 || l[0].Int("bid") != 10030 || l[0].Str("num") != "9" {
		t.Fatalf("gain: %v", gain)
	}
	if r := c.until(11089); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("sale: %v", r)
	}
	after, _ = e.st.RoleItems(role.RID)
	if after[10030] != before[10030]+9 || after[base] != before[base]-1 {
		t.Fatalf("sale ledger: %v -> %v", before, after)
	}
	// take everything off
	c.send(11093, proto.Message{"partner_id": hero, "holy_eqm_id": 0, "type": 0})
	if r := c.until(11093); r.Int("result") != 1 {
		t.Fatalf("take off: %v", r)
	}
	if got := len(holyBagOf(t, c)); got != 2 {
		t.Fatalf("bag at the end: %d", got)
	}
}
