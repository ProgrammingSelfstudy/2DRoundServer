package game

import (
	"2droundserver/internal/store"
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestStarterHeroFollowsTheSex(t *testing.T) {
	for _, c := range []struct {
		sex int
		bid int64
	}{{0, 10401}, {1, 10401}, {2, 20402}} {
		seeds := starterSeeds(c.sex)
		info, ok := partnerTable[c.bid]
		if len(seeds) != 1 || seeds[0].BID != c.bid || seeds[0].Star != info.InitStar || !ok || len(info.Stars[itoa(int64(info.InitStar))].Skills) == 0 {
			t.Fatalf("sex %d: %+v", c.sex, seeds)
		}
	}
}

func TestHeroListGrantsTheStarterOnce(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	want := starterSeeds(role.Sex)[0].BID
	for _, cmd := range []uint16{11000, 11025} {
		c.send(cmd, proto.Message{})
		r := c.recv(cmd)
		ps := r.Arr("partners")
		if len(ps) != 1 || r.Int("num") != heroListMax || heroListMax != 300 {
			t.Fatalf("%d: %v", cmd, r)
		}
		first := ps[0]
		if first.Int("bid") != want || first.Int("lev") != 1 || len(first.Arr("skills")) == 0 || first.Int("partner_id") == 0 {
			t.Fatalf("first hero: %v", first)
		}
		if cmd == 11000 && (first.Int("atk") != partnerTable[want].Attr["atk"] || first.Int("hp") != partnerTable[want].Attr["hp_max"]) {
			t.Fatalf("11000 carries stats: %v", first)
		}
	}
	// details by id: only the asked heroes, owned by the role
	all, _ := e.st.PartnersOf(role.RID)
	c.send(11026, proto.Message{"partner_ids": []proto.Message{{"partner_id": all[0].PartnerID}, {"partner_id": 99999}}})
	d := c.recv(11026).Arr("partners")
	if len(d) != 1 || d[0].Int("partner_id") != all[0].PartnerID || d[0].Int("atk") != partnerTable[want].Attr["atk"] {
		t.Fatalf("11026: %v", d)
	}
	if len(all) != 1 {
		t.Fatalf("asking twice must not grant twice: %v", all)
	}
}

func TestHeroLevelUpAndBreak(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(11025, proto.Message{})
	c.recv(11025)
	hero, _ := e.st.PartnersOf(role.RID)
	id := hero[0].PartnerID // 10101: caps 30 / 40, one break step
	act := func(cmd uint16) proto.Message {
		c.send(cmd, proto.Message{"partner_id": id})
		return c.recv(cmd)
	}
	// a step that worked: the assets, the items, the hero (11002), then the answer
	grew := func(cmd uint16) (hero, reply proto.Message) {
		c.send(cmd, proto.Message{"partner_id": id})
		c.recv(10302)
		c.recv(10500)
		hero = c.recv(11002)
		return hero, c.recv(cmd)
	}
	if r := act(11003); r.Int("result") != 0 || r.Str("msg") == "" { // pays nothing it does not have
		t.Fatalf("no materials: %v", r)
	}
	if r := act(11004); r.Int("result") != 0 { // level 1 is below the cap of 30
		t.Fatalf("break too early: %v", r)
	}
	// level 1 -> 2 costs 12 of asset 22 and 53 of asset 1
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 22, Num: 100000}, {Kind: "asset", ItemID: 1, Num: 1000},
		{Kind: "item", ItemID: 10001, Num: 100}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)

	// the role holds enough for five levels, so one 11003 raises five (the client's "升5级")
	var want22, want1 int64
	for lv := 1; lv <= 5; lv++ {
		for _, c := range growth.Costs[lv-1] {
			if c.ID == 22 {
				want22 += c.Amount
			} else if c.ID == 1 {
				want1 += c.Amount
			}
		}
	}
	hero11002, r := grew(11003)
	if r.Int("result") != 1 || r.Str("msg") != "" || hero11002.Int("lev") != 6 || hero11002.Int("partner_id") != id {
		t.Fatalf("level up: %v %v", r, hero11002)
	}
	as, _ := e.st.RoleAssets(role.RID)
	if as[22] != 100000-want22 || as[1] != 1000-want1 {
		t.Fatalf("assets after five levels: %v (want %d, %d spent)", as, want22, want1)
	}

	// jump to the cap, then a level up is refused and a break works
	if err := e.st.UpgradePartner(role.RID, id, 6, 0, 30, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r := act(11003); r.Int("result") != 0 {
		t.Fatalf("level up at the cap: %v", r)
	}
	if r := act(11004); r.Int("result") != 0 { // costs 10000 coin; the role has less
		t.Fatalf("break without enough coin: %v", r)
	}
	m = e.sendMail(store.Mail{Subject: "g2", Attachments: []store.Attachment{{Kind: "asset", ItemID: 1, Num: 20000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	hero11002, r = grew(11004)
	if r.Int("result") != 1 || hero11002.Int("break_lev") != 1 || hero11002.Int("lev") != 30 {
		t.Fatalf("break: %v %v", r, hero11002)
	}
	if got, _ := e.st.RoleItems(role.RID); got[10001] != 50 {
		t.Fatalf("break took 50 of item 10001: %v", got)
	}
	if r := act(11004); r.Int("result") != 0 { // 10101 has one break step only
		t.Fatalf("second break: %v", r)
	}
	// level 31 is allowed now (cap 40): the role can pay some of the five levels one 11003
	// asks for, and gets as many as it can pay
	hero11002, r = grew(11003)
	if r.Int("result") != 1 || hero11002.Int("lev") <= 30 || hero11002.Int("lev") > 35 {
		t.Fatalf("level up after the break: %v %v", r, hero11002)
	}
}

func TestHeroStatsGrow(t *testing.T) {
	p1 := store.Partner{BID: 10101, Lev: 1, Star: 1}
	hp1, atk1, _, _ := partnerStats(p1)
	if atk1 != 33 || hp1 != 165 {
		t.Fatalf("level 1: %d %d", atk1, hp1)
	}
	p11 := store.Partner{BID: 10101, Lev: 11, Star: 1}
	hp, atk, def, speed := partnerStats(p11)
	if atk != 33+3314*10/1000 || hp != 165+16525*10/1000 || def != 14+2040*10/1000 || speed != 33+1667*10/1000 {
		t.Fatalf("level 11: %d %d %d %d", hp, atk, def, speed)
	}
	// the first break step multiplies atk and hp by 1.16
	_, atkB, _, _ := partnerStats(store.Partner{BID: 10101, Lev: 11, Star: 1, BreakLev: 1})
	if atkB != atk*1160/1000 {
		t.Fatalf("broken: %d vs %d", atkB, atk)
	}
}

func TestHeroStarUp(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// 10501 star 5 -> 6 takes: one more 10501 and one 10405 (both star 5) and three
	// star-5 heroes of camp 1
	seeds := []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10501, Star: 5}, {BID: 10405, Star: 5},
		{BID: 10401, Star: 5}, {BID: 10402, Star: 5}, {BID: 10403, Star: 5}, {BID: 10404, Star: 4}}
	if ok, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	ids := func(from, to int) []proto.Message {
		var out []proto.Message
		for _, h := range hs[from:to] {
			out = append(out, proto.Message{"partner_id": h.PartnerID})
		}
		return out
	}
	up := func(spec, camp []proto.Message) proto.Message {
		c.send(11005, proto.Message{"partner_id": hs[0].PartnerID, "expend1": spec, "expend2": camp, "item_expend": []proto.Message{}})
		return c.recv(11005)
	}
	// too few: the camp slots miss one hero
	if r := up(ids(1, 3), ids(3, 5)); r.Int("result") != 0 {
		t.Fatalf("missing material: %v", r)
	}
	// a star-4 hero cannot fill a star-5 slot
	if r := up(ids(1, 3), append(ids(3, 5), ids(6, 7)...)); r.Int("result") != 0 {
		t.Fatalf("wrong star: %v", r)
	}
	// the hero itself, twice the same hero, or someone else's id are refused
	if r := up(ids(0, 1), ids(3, 6)); r.Int("result") != 0 {
		t.Fatalf("itself: %v", r)
	}
	if r := up(append(ids(1, 3), ids(1, 2)...), ids(3, 6)); r.Int("result") != 0 {
		t.Fatalf("same hero twice: %v", r)
	}
	if got, _ := e.st.PartnersOf(role.RID); len(got) != 7 || got[0].Star != 5 {
		t.Fatalf("a refusal must change nothing: %v", got)
	}

	if r := up(ids(1, 3), ids(3, 6)); r.Int("result") != 1 {
		t.Fatalf("star up: %v", r)
	}
	if gone := c.recv(11006).Arr("expend2"); len(gone) != 5 {
		t.Fatalf("11006: %v", gone)
	}
	if ref := c.recv(11001).Arr("partners"); len(ref) != 1 || ref[0].Int("star") != 6 || ref[0].Int("partner_id") != hs[0].PartnerID {
		t.Fatalf("11001: %v", ref)
	}
	c.recv(10302)
	c.recv(10500)
	left, _ := e.st.PartnersOf(role.RID)
	if len(left) != 2 || left[0].Star != 6 || left[1].BID != 10404 {
		t.Fatalf("heroes after: %v", left)
	}
}

func TestStarPlanCost(t *testing.T) {
	// 10501 star 10 -> 11: one star-6 10501 and a star-9 hero of any camp, and 15000 of item 10001
	p := store.Partner{PartnerID: 1, BID: 10501, Star: 10}
	owned := map[int64]store.Partner{1: p, 2: {PartnerID: 2, BID: 10501, Star: 6}, 3: {PartnerID: 3, BID: 10201, Star: 9}}
	consumed, cost, msg := starPlan(p, owned, []int64{2, 3}, false)
	if msg != "" || len(consumed) != 2 || len(cost) != 1 || cost[0] != (sellGain{10001, 15000}) {
		t.Fatalf("%v %v %q", consumed, cost, msg)
	}
	// an extra hero that fills no slot is refused
	owned[4] = store.Partner{PartnerID: 4, BID: 10201, Star: 9}
	if _, _, msg := starPlan(p, owned, []int64{2, 3, 4}, false); msg == "" {
		t.Fatal("surplus hero accepted")
	}
	// the top star and the fusion path are refused
	if _, _, msg := starPlan(store.Partner{BID: 10101, Star: 1}, nil, nil, false); msg == "" {
		t.Fatal("10101 has no star 2")
	}
}

func TestBreakPreview(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	h, _ := e.st.PartnersOf(role.RID)
	now := partnerMessage(h[0])
	c.send(11016, proto.Message{"partner_id": h[0].PartnerID})
	r := c.recv(11016)
	// 10101's first break step multiplies atk and hp by 1.16
	if r.Int("atk") != now.Int("atk")*1160/1000 || r.Int("hp") <= now.Int("hp") {
		t.Fatalf("preview %v vs now %v", r, now)
	}
}

func TestBuyHeroSlots(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	buy := func() proto.Message { c.send(11009, proto.Message{}); return c.recv(11009) }
	if r := buy(); r.Int("result") != 0 { // 100 gems needed
		t.Fatalf("poor: %v", r)
	}
	mailTo(t, e, c, role.RID, store.Attachment{Kind: "asset", ItemID: 3, Num: 1000})
	r := buy()
	if r.Int("result") != 1 || r.Int("num") != heroListMax+5 || r.Int("buy_num") != 1 {
		t.Fatalf("first purchase: %v", r)
	}
	c.recv(10302)
	if as, _ := e.st.RoleAssets(role.RID); as[3] != 900 {
		t.Fatalf("gems: %v", as)
	}
	if r := buy(); r.Int("result") != 1 || r.Int("num") != heroListMax+10 { // 150 gems
		t.Fatalf("second: %v", r)
	}
	c.recv(10302)
	if as, _ := e.st.RoleAssets(role.RID); as[3] != 750 {
		t.Fatalf("gems after the second: %v", as)
	}
	// the hero list reports the new capacity and the count
	c.send(11025, proto.Message{})
	if l := c.recv(11025); l.Int("num") != heroListMax+10 || l.Int("buy_num") != 2 {
		t.Fatalf("list: %v", l)
	}
	// ten purchases at most
	mailTo(t, e, c, role.RID, store.Attachment{Kind: "asset", ItemID: 3, Num: 5000})
	for i := 2; i < 10; i++ {
		if r := buy(); r.Int("result") != 1 {
			t.Fatalf("purchase %d: %v", i+1, r)
		}
		c.recv(10302)
	}
	if r := buy(); r.Int("result") != 0 || r.Int("buy_num") != 10 {
		t.Fatalf("eleventh: %v", r)
	}
	if heroSlotCapacity(10) != heroListMax+50 {
		t.Fatal("capacity")
	}
}

func TestHeroResetAndHolyEquipmentAnswers(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	c.send(11067, proto.Message{"partner_id": 7})
	if r := c.recv(11067); r.Int("partner_id") != 7 || r.Int("end_time") != 0 || r.Int("day_num") != 0 {
		t.Fatalf("11067: %v", r)
	}
	c.send(11092, proto.Message{"partner_ids": []proto.Message{{"partner_id": 7}, {"partner_id": 8}}})
	ps := c.recv(11092).Arr("partner_ids")
	if len(ps) != 2 || ps[0].Int("partner_id") != 7 || ps[1].Int("partner_id") != 8 || len(ps[0].Arr("holy_eqm")) != 0 {
		t.Fatalf("11092: %v", ps)
	}
}

func TestDoubleTimeAndReplayStatus(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	c.send(10397, proto.Message{"status": 1})
	c.send(21322, proto.Message{})
	if r := c.recv(21322); r.Int("code") != 0 {
		t.Fatalf("21322: %v", r)
	}
}

func TestHeroLevelUpPartlyAffordable(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(11025, proto.Message{})
	c.recv(11025)
	hero, _ := e.st.PartnersOf(role.RID)
	id := hero[0].PartnerID
	var cost1, cost2 []sellGain = growth.Costs[0], growth.Costs[1]
	// exactly the first two levels' worth of every material
	need := map[int64]int64{}
	for _, g := range append(append([]sellGain{}, cost1...), cost2...) {
		need[g.ID] += g.Amount
	}
	var atts []store.Attachment
	for id, n := range need {
		kind := "item"
		if t, ok := itemTable[id]; ok && t.Type == 7 {
			kind = "asset"
		}
		atts = append(atts, store.Attachment{Kind: kind, ItemID: id, Num: n})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	c.send(11003, proto.Message{"partner_id": id})
	c.recv(10302)
	c.recv(10500)
	if h := c.recv(11002); h.Int("lev") != 3 {
		t.Fatalf("only two of five levels are paid for: %v", h)
	}
	if r := c.recv(11003); r.Int("result") != 1 {
		t.Fatalf("two levels: %v", r)
	}
	c.send(11003, proto.Message{"partner_id": id}) // nothing left
	if r := c.recv(11003); r.Int("result") != 0 {
		t.Fatalf("nothing left: %v", r)
	}
}

func TestHeroAttributeList(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("ua", "船长")
	c.send(11000, proto.Message{})
	heroes := c.waitFor(11000).Arr("partners")
	if len(heroes) == 0 {
		t.Fatal("no starter hero")
	}
	h := heroes[0]
	c.send(11063, proto.Message{"partner_id": h.Int("partner_id")})
	r := c.waitFor(11063)
	if r.Int("partner_id") != h.Int("partner_id") || r.Int("atk") != h.Int("atk") || r.Int("hp") != h.Int("hp") || r.Int("speed") != h.Int("speed") {
		t.Fatalf("11063 %v differs from the entry %v", r, h)
	}
	c.send(11063, proto.Message{"partner_id": 999999}) // not ours: no answer, the next one still comes
	c.send(11063, proto.Message{"partner_id": h.Int("partner_id")})
	if r := c.waitFor(11063); r.Int("partner_id") != h.Int("partner_id") {
		t.Fatalf("after a foreign id: %v", r)
	}
}
