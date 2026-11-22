package game

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func newGrowthRole(e *env, platform string) (*store.Role, func() *client) {
	t := e.t
	t.Helper()
	account, err := e.st.CreateAccount(platform, "growth", "", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.IssueToken(account.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	role, err := e.st.CreateRole(account.ID, "symlf_1", "养成测试", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return role, func() *client {
		t.Helper()
		c := e.dial(true)
		c.send(1164, loginArgs(account.Account, token))
		if m := c.recv(1164); m.Int("code") != 1 {
			t.Fatalf("login: %v", m)
		}
		c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
		if m := c.recv(10192); m.Int("code") != 1 {
			t.Fatalf("enter: %v", m)
		}
		return c
	}
}

func growthReply(t *testing.T, frames []capturedReply, cmd uint16) proto.Message {
	t.Helper()
	for _, frame := range frames {
		if frame.cmd == cmd {
			return frame.msg
		}
	}
	t.Fatalf("missing %d in %v", cmd, replyCodes(frames))
	return nil
}

func TestH5HeroGrowthLoop(t *testing.T) {
	e := newEnv(t, time.Minute)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	role, connect := newGrowthRole(e, store.PlatformH5)
	rid := role.RID
	c := connect()
	seeds := []store.PartnerSeed{{BID: 30501, Star: 5}, {BID: 30501, Star: 5}, {BID: 30502, Star: 5},
		{BID: 30503, Star: 5}, {BID: 30504, Star: 5}}
	granted, err := e.st.GrantStarterPartners(rid, seeds)
	must(err)
	if !granted {
		t.Fatal("test roster was not added")
	}
	heroes, err := e.st.PartnersOf(rid)
	must(err)
	target, duplicate, trained := heroes[0], heroes[1], heroes[2]
	targetArgs := proto.Message{"partner_id": target.PartnerID}
	read := func(frames []capturedReply, cmd uint16) proto.Message {
		t.Helper()
		return growthReply(t, frames, cmd)
	}
	act := func(cmd uint16, req proto.Message, success int64) []capturedReply {
		t.Helper()
		frames := captureReplies(c, cmd, req)
		if m := read(frames, cmd); m.Int("result") != success {
			t.Fatalf("%d: %v", cmd, m)
		}
		if success == 0 && !slices.Equal(replyCodes(frames), []uint16{cmd}) {
			t.Fatalf("refused growth sent unexpected frames: %v", replyCodes(frames))
		}
		return frames
	}
	balances := func() (coin, exp int64, items map[int64]int64) {
		t.Helper()
		assets, err := e.st.RoleAssets(rid)
		must(err)
		items, err = e.st.RoleItems(rid)
		must(err)
		return assets[1], assets[22], items
	}
	must(e.st.Exchange(rid, nil, nil, map[int64]int64{1: 2_000_000, 22: 2_000_000}, map[int64]int64{10001: 3000, 90503: 1, 40101: 1}))
	must(e.st.UpgradePartner(rid, target.PartnerID, 1, 0, 29, 0, nil, nil))
	act(11004, targetArgs, 0)
	coinBefore, expBefore, _ := balances()
	level := act(11003, targetArgs, 1)
	leveled := read(level, 11002)
	if leveled.Int("lev") != 30 {
		t.Fatalf("upgrade must stop at level 30: %v", leveled)
	}
	coin, exp, bagBefore := balances()
	if coinBefore-coin != 5542 || expBefore-exp != 2275 {
		t.Fatalf("level 29 -> 30 cost: coin=%d exp=%d", coinBefore-coin, expBefore-exp)
	}
	act(11003, targetArgs, 0)
	coinBefore = coin
	advanced := act(11004, targetArgs, 1)
	h := read(advanced, 11002)
	if h.Int("lev") != 30 || h.Int("break_lev") != 1 || h.Int("power") <= leveled.Int("power") {
		t.Fatalf("first advancement: %v", h)
	}
	if read(advanced, 10306).Int("power") <= read(level, 10306).Int("power") {
		t.Fatal("role power did not increase after advancement")
	}
	coin, _, bag := balances()
	if coinBefore-coin != 10000 || bagBefore[10001]-bag[10001] != 50 {
		t.Fatalf("first advancement cost: coin=%d cards=%d", coinBefore-coin, bagBefore[10001]-bag[10001])
	}
	if h := read(act(11003, targetArgs, 1), 11002); h.Int("lev") != 35 {
		t.Fatalf("first advancement must unlock levels past 30: %v", h)
	}

	must(e.st.UpgradePartner(rid, target.PartnerID, 35, 1, 100, 5, nil, nil))
	must(e.st.UpgradePartner(rid, trained.PartnerID, 1, 0, 60, 2, nil, nil))
	must(e.st.EquipPiece(rid, trained.PartnerID, 1, 40101))
	const artifact = 10451
	const holyBase = 1010201
	must(e.st.SaveArtifacts(rid, &store.ArtifactDoc{Pieces: []store.ArtPiece{{ID: artifact*10000 + 1, Base: artifact, Partner: trained.PartnerID, Pos: 1}}}))
	must(e.st.SaveHoly(rid, &store.HolyDoc{Pieces: []store.HolyPiece{{ID: 1_000_000_001, Base: holyBase, Partner: trained.PartnerID, Slot: 1}}}))
	before, err := e.st.PartnersOf(rid)
	must(err)
	wantAssets, wantItems := refund(before[2])
	coinBefore, expBefore, bagBefore = balances()
	if got := read(act(11004, targetArgs, 0), 11004).Str("msg"); got != "英雄达到6星后可进阶" {
		t.Fatalf("star prerequisite: %q", got)
	}
	preview := func() proto.Message { return read(captureReplies(c, 11016, targetArgs), 11016) }
	hp, atk, _, _ := partnerStats(before[0])
	if m := preview(); m.Int("hp") != hp || m.Int("atk") != atk {
		t.Fatalf("ineligible break preview: %v", m)
	}
	star := proto.Message{
		"partner_id": target.PartnerID, "expend1": []proto.Message{{"partner_id": duplicate.PartnerID}},
		"expend2":     []proto.Message{{"partner_id": trained.PartnerID}, {"partner_id": heroes[3].PartnerID}, {"partner_id": heroes[4].PartnerID}},
		"item_expend": []proto.Message{{"item_id": 90503, "num": 1}},
	}
	lock := func(value int64) {
		t.Helper()
		out := act(11015, proto.Message{"partner_id": trained.PartnerID, "type": value}, 1)
		if push := read(out, 11002).Arr("is_lock"); len(push) != 1 || push[0].Int("is_lock") != value {
			t.Fatalf("lock state %d: %v", value, push)
		}
	}
	lock(1)
	if m := read(act(11005, star, 0), 11005); m.Str("msg") != "英雄已锁定" {
		t.Fatalf("locked material: %v", m)
	}
	planned := map[int64]store.Partner{target.PartnerID: before[0], trained.PartnerID: before[2]}
	if _, err := e.st.AscendH5Partner(rid, target.PartnerID, 5, []int64{trained.PartnerID}, nil, nil, nil, nil, planned); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("atomic lock check: %v", err)
	}
	stillHere, err := e.st.PartnerByID(rid, trained.PartnerID)
	must(err)
	if kept, err := e.st.PartnerByID(rid, target.PartnerID); err != nil || kept.Star != 5 || len(stillHere.Equips) != 1 {
		t.Fatalf("refused star-up modified heroes: %v %v %v", kept, stillHere, err)
	}
	if coin, exp, bag := balances(); coin != coinBefore || exp != expBefore || bag[90503] != 1 || bag[10001] != bagBefore[10001] {
		t.Fatalf("refused growth charged materials: %d, %d, %v", coin, exp, bag)
	}
	lock(0)
	out := act(11005, star, 1)
	up := read(out, 11002)
	if up.Int("star") != 6 || up.Int("power") <= partnerMessage(before[0]).Int("power") {
		t.Fatalf("star-up hero: %v", up)
	}
	if removed := read(out, 11006).Arr("expend2"); len(removed) != 4 {
		t.Fatalf("consumed heroes: %v", removed)
	}
	shown := rewardTotals(t, read(out, 12742))
	if shown[1] != wantAssets[1] || shown[22] != wantAssets[22] || shown[10001] != wantItems[10001] ||
		shown[40101] != 1 || shown[artifact] != 1 || shown[holyBase] != 1 {
		t.Fatalf("refund notice: %v", shown)
	}
	coin, exp, bag = balances()
	if coin-coinBefore != wantAssets[1] || exp-expBefore != wantAssets[22] || bag[10001]-bagBefore[10001] != wantItems[10001] ||
		bag[90503] != 0 || bag[40101] != 1 || bag[artifact] != 1 || bag[holyBase] != 1 {
		t.Fatalf("incorrect net gains: %d, %d, %v", coin-coinBefore, exp-expBefore, bag)
	}
	gear, err := e.st.ArtifactsOf(rid)
	must(err)
	if len(gear.Pieces) != 1 || gear.Pieces[0].Partner != 0 || gear.Pieces[0].Pos != 0 {
		t.Fatalf("returned artifact instance: %v", gear.Pieces)
	}
	holy, err := e.st.HolyOf(rid)
	must(err)
	if len(holy.Pieces) != 1 || holy.Pieces[0].Partner != 0 || holy.Pieces[0].Slot != 0 {
		t.Fatalf("returned holy equipment instance: %v", holy.Pieces)
	}
	after, err := e.st.PartnersOf(rid)
	must(err)
	power := read(out, 10306)
	previous := rolePower(before)
	if len(after) != 1 || power.Int("power") != rolePower(after) || power.Int("power") >= previous || power.Int("max_power") < previous {
		t.Fatalf("current and peak power after consuming heroes: %v, previous %d, roster %v", power, previous, after)
	}
	act(11005, star, 0)

	next := preview()
	if next.Int("hp") <= up.Int("hp") || next.Int("atk") <= up.Int("atk") {
		t.Fatalf("eligible break preview: %v", next)
	}
	cap := read(act(11004, targetArgs, 1), 11002)
	if cap.Int("break_lev") != 6 || cap.Int("lev") != 100 || cap.Int("hp") != next.Int("hp") || cap.Int("atk") != next.Int("atk") {
		t.Fatalf("six stars must unlock sixth advancement: %v", cap)
	}
	coin, _, bag = balances()
	if coinBefore+wantAssets[1]-coin != 500000 || bagBefore[10001]+wantItems[10001]-bag[10001] != 2000 {
		t.Fatalf("sixth advancement cost: %d, %v", coin, bag)
	}
	up = read(act(11003, targetArgs, 1), 11002)
	if up.Int("lev") != 101 || up.Int("power") <= cap.Int("power") {
		t.Fatalf("sixth advancement must unlock level 101: %v", up)
	}
	c = connect()
	relogin := captureReplies(c, 10300, proto.Message{})
	if m := read(relogin, 10306); m.Int("power") != up.Int("power") || m.Int("max_power") < previous {
		t.Fatalf("power after relogin: %v", m)
	}
}

func TestH5GrowthRosterVisibility(t *testing.T) {
	for _, platform := range []string{store.PlatformH5, "symlf"} {
		t.Run(platform, func(t *testing.T) {
			e := newEnv(t, time.Minute)
			role, connect := newGrowthRole(e, platform)
			seeds := []store.PartnerSeed{{BID: 30403, Star: 4}, {BID: 30501, Star: 5}, {BID: 30502, Star: 6}}
			if granted, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !granted {
				t.Fatalf("seed roster: %v, %v", granted, err)
			}
			heroes, err := e.st.PartnersOf(role.RID)
			if err != nil || len(heroes) != len(seeds) {
				t.Fatalf("stored roster: %v, %v", heroes, err)
			}
			h5 := platform == store.PlatformH5
			want := heroes
			if h5 {
				want = heroes[1:]
			}
			c := connect()
			check := func(cmd uint16, rows []proto.Message) {
				t.Helper()
				if len(rows) != len(want) {
					t.Fatalf("%d roster: %v", cmd, rows)
				}
				for i, row := range rows {
					if p := want[i]; row.Int("partner_id") != p.PartnerID || row.Int("bid") != p.BID || row.Int("star") != int64(p.Star) ||
						row.Int("lev") != int64(p.Lev) || row.Int("power") <= 0 || len(row.Arr("skills")) == 0 {
						t.Fatalf("%d hero %d: %v", cmd, i, row)
					}
				}
			}
			for _, cmd := range []uint16{11000, 11025} {
				c.send(cmd, proto.Message{})
				m := c.recv(cmd)
				check(cmd, m.Arr("partners"))
				if m.Int("num") != heroListMax || m.Int("buy_num") != 0 {
					t.Fatalf("%d slot info: %v", cmd, m)
				}
			}
			ids := []proto.Message{{"partner_id": 99999}}
			push := []proto.Message{}
			for _, h := range heroes {
				ids = append(ids, proto.Message{"partner_id": h.PartnerID})
				push = append(push, partnerMessage(h))
			}
			c.send(11026, proto.Message{"partner_ids": ids})
			details := c.recv(11026).Arr("partners")
			if len(details) != len(want) {
				t.Fatalf("11026 roster: %v", details)
			}
			for i, d := range details {
				if d.Int("partner_id") != want[i].PartnerID || d.Int("hp") <= 0 || d.Int("atk") <= 0 {
					t.Fatalf("11026 stats: %v", d)
				}
			}
			c.send(10315, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
			profile := c.recv(10315).Arr("partner_list")
			if len(profile) != len(want) {
				t.Fatalf("profile roster: %v", profile)
			}
			for _, p := range profile {
				if !slices.ContainsFunc(want, func(w store.Partner) bool {
					return p.Int("id") == w.PartnerID && p.Int("star") == int64(w.Star)
				}) {
					t.Fatalf("profile hero: %v", p)
				}
			}
			c.send(11061, proto.Message{"r_rid": role.RID, "r_srvid": role.SrvID, "partner_id": heroes[0].PartnerID})
			wantBID := heroes[0].BID
			if h5 {
				wantBID = 0
			}
			if m := c.recv(11061); m.Int("bid") != wantBID {
				t.Fatalf("lower-star hero detail: %v", m)
			}
			online := e.gw.Hub.Online(role.RID)
			if err := online.Send(11001, proto.Message{"partners": push}); err != nil {
				t.Fatal(err)
			}
			check(11001, c.recv(11001).Arr("partners"))
			if h5 {
				if err := online.Send(11001, proto.Message{"partners": push[:1]}); err != nil {
					t.Fatal(err)
				}
				c.expectNothing()
			}
			low := proto.Message{"partner_id": heroes[0].PartnerID, "type": 1}
			locked := captureReplies(c, 11015, low)
			wantCodes := []uint16{11002, 11015}
			if h5 {
				wantCodes = []uint16{11015}
			}
			if !slices.Equal(replyCodes(locked), wantCodes) || growthReply(t, locked, 11015).Int("result") != 1 {
				t.Fatalf("locking the lower-star hero: %v", locked)
			}
			if h5 {
				for _, cmd := range []uint16{11003, 11004, 11005} {
					f := captureReplies(c, cmd, low)
					m := growthReply(t, f, cmd)
					if !slices.Equal(replyCodes(f), []uint16{cmd}) || m.Int("result") != 0 || m.Str("msg") != "英雄不存在" {
						t.Fatalf("%d on a lower-star hero: %v", cmd, f)
					}
				}
				for _, cmd := range []uint16{11016, 11063} {
					c.send(cmd, low)
					c.expectNothing()
				}
			}
			if stored, err := e.st.PartnerByID(role.RID, heroes[0].PartnerID); err != nil || stored.Star != 4 || stored.Lev != 1 {
				t.Fatalf("lower-star hero in storage: %v, %v", stored, err)
			}
		})
	}
}

func TestH5RosterStarterPower(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, connect := newGrowthRole(e, store.PlatformH5)
	c := connect()
	frames := captureReplies(c, 11025, proto.Message{})
	if got := replyCodes(frames); !slices.Equal(got, []uint16{11025, 10306}) {
		t.Fatalf("initial roster pushes: %v", got)
	}
	heroes := frames[0].msg.Arr("partners")
	if len(heroes) != 1 || heroes[0].Int("bid") != starterH5 || heroes[0].Int("star") != 5 {
		t.Fatalf("H5 starter: %v", heroes)
	}
	power := heroes[0].Int("power")
	if m := frames[1].msg; power <= 0 || m.Int("power") != power || m.Int("max_power") != power {
		t.Fatalf("starter power: %v", m)
	}
	again := captureReplies(c, 11025, proto.Message{})
	if got := replyCodes(again); !slices.Equal(got, []uint16{11025}) {
		t.Fatalf("repeated roster pushes: %v", got)
	}
	if got := again[0].msg.Arr("partners"); len(got) != 1 || got[0].Int("partner_id") != heroes[0].Int("partner_id") {
		t.Fatalf("starter was granted more than once: %v", got)
	}
}

func TestH5BreakLimitData(t *testing.T) {
	for key, step := range growth.Brach {
		var typ, group, stage int
		if n, err := fmt.Sscanf(key, "%d_%d_%d", &typ, &group, &stage); err != nil || n != 3 {
			t.Fatalf("break key %q: %v", key, err)
		}
		minStar := h5BreakLimits[group][stage]
		if step.Limit && minStar != stage || !step.Limit && minStar != 0 {
			t.Fatalf("break step %s, limited %v, min stars %d", key, step.Limit, minStar)
		}
	}
	for _, c := range []struct {
		bid  int64
		step int
		star int
	}{{30402, 5, 5}, {30402, 6, 6}, {10401, 5, 5}, {30501, 6, 6}, {10508, 6, 6}} {
		p := store.Partner{BID: c.bid, BreakLev: c.step - 1, Star: c.star - 1}
		next := growth.Brach[brachKey(partnerTable[p.BID], c.step)]
		if msg := breakDenial(p, next, true); msg != fmt.Sprintf("英雄达到%d星后可进阶", c.star) {
			t.Fatalf("hero %d, break %d, star %d: %q", c.bid, c.step, p.Star, msg)
		}
		p.Star++
		if msg := breakDenial(p, next, true); msg != "" {
			t.Fatalf("hero %d, break %d, star %d: %q", c.bid, c.step, p.Star, msg)
		}
		if msg := breakDenial(p, next, false); msg != "该突破暂未开放" {
			t.Fatalf("native hero %d, break %d: %q", c.bid, c.step, msg)
		}
	}
}

func TestH5FiveStarRecipeDoesNotChangeNativeMaterials(t *testing.T) {
	p := store.Partner{PartnerID: 1, BID: 30501, Star: 5}
	owned := map[int64]store.Partner{
		2: {PartnerID: 2, BID: 30501, Star: 5},
		3: {PartnerID: 3, BID: 30502, Star: 5},
	}
	ids, souls := []int64{2, 3}, map[int64]int64{90503: 3}
	if _, _, msg := planStar(p, owned, ids, souls, true); msg != "" {
		t.Fatal(msg)
	}
	if _, _, msg := starPlan(p, owned, ids, souls); msg == "" {
		t.Fatal("native recipe still needs the named five-star Mr.1")
	}
	if _, _, msg := planStar(p, owned, ids, map[int64]int64{90500: 3}, true); msg == "" {
		t.Fatal("an any-camp soul must not fill a named camp")
	}
	owned[3] = store.Partner{PartnerID: 3, BID: 30502, Star: 4}
	if _, _, msg := planStar(p, owned, ids, souls, true); msg == "" {
		t.Fatal("the replacement still has to be five stars")
	}
}
