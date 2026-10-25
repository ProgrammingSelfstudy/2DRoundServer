package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestRefund(t *testing.T) {
	// level 3 gives back the cost of levels 1 and 2 (22 x(12+15), 1 x(53+60) in the client's table)
	a, i := refund(store.Partner{BID: 10101, Lev: 3})
	if a[22] != 27 || a[1] != 113 || len(i) != 0 {
		t.Fatalf("level refund: %v %v", a, i)
	}
	if a, i = refund(store.Partner{BID: 10101, Lev: 1}); len(a)+len(i) != 0 {
		t.Fatalf("a level-1 hero gives nothing back: %v %v", a, i)
	}
	// a talent given back: its learn stone plus the level-up (3 stones)
	_, i = refund(store.Partner{BID: 10101, Lev: 1, Talents: []store.Talent{{Pos: 1, SkillID: 700063}}})
	if i[16007] != 4 {
		t.Fatalf("talent refund: %v", i)
	}
}

func TestRebornAndOffer(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}, {BID: 10102, Star: 1}, {BID: 10103, Star: 1}, {BID: 10104, Star: 1}, {BID: 10401, Star: 6}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	if err := e.st.UpgradePartner(role.RID, hs[0].PartnerID, 1, 0, 3, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ChangeTalent(role.RID, hs[4].PartnerID, 1, 0, 700061, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	pid := hs[0].PartnerID
	count := func(id int64) proto.Message {
		c.send(11067, proto.Message{"partner_id": id})
		return c.recv(11067)
	}
	if r := count(pid); r.Int("day_num") != 0 || r.Int("end_time") != 0 {
		t.Fatalf("fresh: %v", r)
	}
	reborn := func(id int64) proto.Message {
		c.send(11068, proto.Message{"partner_id": id})
		return c.recv(11068)
	}
	if r := reborn(hs[1].PartnerID); r.Int("code") != 0 { // level 1, nothing to give back
		t.Fatalf("nothing to reborn: %v", r)
	}
	c.send(11068, proto.Message{"partner_id": pid})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11002); r.Int("lev") != 1 {
		t.Fatalf("hero after reborn: %v", r)
	}
	if r := c.recv(11068); r.Int("code") != 1 {
		t.Fatalf("reborn: %v", r)
	}
	if r := c.recv(11067); r.Int("day_num") != 1 || r.Int("end_time") == 0 {
		t.Fatalf("count push: %v", r)
	}
	if p, _ := e.st.PartnerByID(role.RID, pid); p.Lev != 1 {
		t.Fatalf("not saved: %v", p)
	}
	if n, _ := e.st.Counter(role.RID, "hero_reborn_"+itoaDay(e)); n != 1 {
		t.Fatalf("counter %d", n)
	}
	if r := reborn(pid); r.Int("code") != 0 { // level 1 again
		t.Fatalf("again: %v", r)
	}

	// offering: the talent hero (refund 1 stone), a locked one is refused
	if err := e.st.SetPartnerLocked(role.RID, hs[3].PartnerID, true); err != nil {
		t.Fatal(err)
	}
	ask := func(cmd uint16, ids ...int64) proto.Message {
		var l []proto.Message
		for _, id := range ids {
			l = append(l, proto.Message{"partner_id": id})
		}
		c.send(cmd, proto.Message{"list": l})
		return c.recv(cmd)
	}
	if r := ask(11075, hs[3].PartnerID); r.Int("code") != 0 || r.Str("msg") == "" {
		t.Fatalf("locked: %v", r)
	}
	if r := ask(11075, hs[4].PartnerID, hs[4].PartnerID); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	if r := ask(11075); r.Int("code") != 0 {
		t.Fatalf("empty: %v", r)
	}
	pre := ask(11075, hs[4].PartnerID)
	if pre.Int("code") != 1 || len(pre.Arr("list")) != 1 || pre.Arr("list")[0].Int("id") != 16007 || pre.Arr("list")[0].Int("num") != 1 {
		t.Fatalf("preview: %v", pre)
	}
	c.send(11076, proto.Message{"list": []proto.Message{{"partner_id": hs[4].PartnerID}}})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11076); r.Int("code") != 1 || len(r.Arr("list")) != 1 {
		t.Fatalf("offer: %v", r)
	}
	if _, err := e.st.PartnerByID(role.RID, hs[4].PartnerID); err != store.ErrNotFound {
		t.Fatalf("hero must be gone: %v", err)
	}
	if r := ask(11076, hs[4].PartnerID); r.Int("code") != 0 {
		t.Fatalf("already gone: %v", r)
	}
}

func itoaDay(e *env) string { return itoa(dayKey(time.Unix(0, e.clk.Load()))) }

func TestSkins(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// skin 107 and its upgrade 117 belong to hero 50504; the starter hero is someone else's
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 50504, Star: 5}, {BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero, other := hs[0].PartnerID, hs[1].PartnerID
	list := func() []proto.Message {
		c.send(11020, proto.Message{})
		return c.recv(11020).Arr("partner_skins")
	}
	if len(list()) != 0 {
		t.Fatal("no skins yet")
	}
	wear := func(pid, skin int64) proto.Message {
		c.send(11019, proto.Message{"partner_id": pid, "skin_id": skin})
		return c.recv(11019)
	}
	if r := wear(hero, 107); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("not owned: %v", r)
	}
	if err := e.st.GrantSkin(role.RID, 107, 0); err != nil {
		t.Fatal(err)
	}
	if l := list(); len(l) != 1 || l[0].Int("id") != 107 {
		t.Fatalf("list: %v", l)
	}
	if r := wear(other, 107); r.Int("result") != 0 {
		t.Fatalf("someone else's skin: %v", r)
	}
	c.send(11019, proto.Message{"partner_id": hero, "skin_id": 107})
	if r := c.recv(11002); r.Int("use_skin") != 107 {
		t.Fatalf("hero push: %v", r)
	}
	if r := c.recv(11019); r.Int("result") != 1 || r.Int("partner_id") != hero {
		t.Fatalf("wear: %v", r)
	}
	// upgrade 107 -> 117 costs item 23019 x1 and 10000000 coin (asset 1)
	up := func() proto.Message {
		c.send(11021, proto.Message{"partner_id": hero, "skin_id": 107})
		return c.recv(11021)
	}
	if r := up(); r.Int("result") != 0 {
		t.Fatalf("no material: %v", r)
	}
	e.give(c, role, map[int64]int64{23019: 1})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 10000000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	c.send(11021, proto.Message{"partner_id": hero, "skin_id": 107})
	c.recv(10302)
	c.recv(10500)
	if l := c.recv(11020).Arr("partner_skins"); len(l) != 1 || l[0].Int("id") != 117 {
		t.Fatalf("after upgrade: %v", l)
	}
	if r := c.recv(11002); r.Int("use_skin") != 117 {
		t.Fatalf("the worn skin follows the upgrade: %v", r)
	}
	if r := c.recv(11021); r.Int("result") != 1 {
		t.Fatalf("upgrade: %v", r)
	}
	if r := up(); r.Int("result") != 0 { // 107 is gone
		t.Fatalf("old skin: %v", r)
	}
	// taking it off
	c.send(11019, proto.Message{"partner_id": hero, "skin_id": 0})
	if r := c.recv(11002); r.Int("use_skin") != 0 {
		t.Fatalf("off: %v", r)
	}
	c.recv(11019)
}

func TestReturn(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// 10501: camp 1, first star 5; star 12 -> 10 gives 4 copies and the "12_1" materials
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10501, Star: 12}, {BID: 10501, Star: 5}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	hero, low := hs[0].PartnerID, hs[1].PartnerID
	if err := e.st.UpgradePartner(role.RID, hero, 1, 0, 300, 6, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ChangeTalent(role.RID, hero, 3, 0, 700061, nil, nil, nil, nil); err != nil { // slot 3 opens at star 13
		t.Fatal(err)
	}
	ask := func(cmd uint16, pid int64) proto.Message {
		c.send(cmd, proto.Message{"partner_id": pid})
		return c.recv(cmd)
	}
	if r := ask(11066, low); r.Int("code") != 0 || r.Str("msg") == "" {
		t.Fatalf("a 5-star hero cannot return: %v", r)
	}
	pre := ask(11066, hero)
	if pre.Int("code") != 1 {
		t.Fatalf("preview: %v", pre)
	}
	first := pre.Arr("list")[0]
	if first.Int("is_partner") != 1 || first.Int("id") != 10501 || first.Int("num") != 4 {
		t.Fatalf("preview copies: %v", pre.Arr("list"))
	}
	var stones int64
	for _, m := range pre.Arr("list") {
		if m.Int("id") == 16007 {
			stones = m.Int("num")
		}
	}
	if stones != 1 {
		t.Fatalf("the slot-3 talent comes back: %v", pre.Arr("list"))
	}
	if r := ask(11065, hero); r.Int("result") != 0 { // no 37005 yet
		t.Fatalf("no cost item: %v", r)
	}
	e.give(c, role, map[int64]int64{37005: 5})
	c.send(11065, proto.Message{"partner_id": hero})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(11001); len(r.Arr("partners")) != 4 {
		t.Fatalf("copies: %v", r)
	}
	if r := c.recv(11002); r.Int("star") != 10 || r.Int("lev") != 255 || r.Int("break_lev") != 6 {
		t.Fatalf("hero after return: %v", r)
	}
	if r := c.recv(11065); r.Int("result") != 1 || len(r.Arr("list")) != len(pre.Arr("list")) {
		t.Fatalf("return: %v", r)
	}
	if p, _ := e.st.PartnerByID(role.RID, hero); p.Star != 10 || len(p.Talents) != 0 {
		t.Fatalf("saved hero: %+v", p)
	}
	if hs, _ := e.st.PartnersOf(role.RID); len(hs) != 6 {
		t.Fatalf("2 + 4 copies, got %d", len(hs))
	}
	if r := ask(11065, hero); r.Int("result") != 0 { // star 10 is below the condition
		t.Fatalf("again: %v", r)
	}
}
