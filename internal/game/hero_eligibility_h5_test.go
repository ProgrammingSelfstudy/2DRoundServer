package game

import (
	"slices"
	"testing"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func seedH5EligibilityRoster(test *testing.T, env *env, rid int64, seeds ...store.PartnerSeed) []store.Partner {
	test.Helper()
	if granted, err := env.st.GrantStarterPartners(rid, seeds); err != nil || !granted {
		test.Fatalf("seed roster: %v (%v)", granted, err)
	}
	heroes, err := env.st.PartnersOf(rid)
	if err != nil || len(heroes) != len(seeds) {
		test.Fatalf("read roster: %v (%v)", heroes, err)
	}
	return heroes
}

func TestH5RosterPushMatchesReadEligibility(test *testing.T) {
	env := newDungeonEnv(test)
	cli, role := enterDungeonRole(env, store.PlatformH5, "hero-eligibility", "升星英雄")
	const unknownBID = 99999999
	heroes := seedH5EligibilityRoster(test, env, role.RID,
		store.PartnerSeed{BID: 10401, Star: 4}, store.PartnerSeed{BID: unknownBID, Star: 5}, store.PartnerSeed{BID: 10401, Star: 5})
	cli.send(11000, nil)
	if roster := cli.recv(11000).Arr("partners"); len(roster) != 1 || roster[0].Int("partner_id") != heroes[2].PartnerID {
		test.Fatalf("roster should only show the known promoted hero: %v", roster)
	}
	cli.send(11061, proto.Message{"r_rid": role.RID, "r_srvid": role.SrvID, "partner_id": heroes[2].PartnerID})
	if info := cli.recv(11061); info.Int("bid") != 10401 || info.Int("star") != 5 {
		test.Fatalf("inspection of promoted hero: %v", info)
	}

	session := env.gw.Hub.Online(role.RID)
	messages := []proto.Message{partnerMessage(heroes[0]), partnerMessage(heroes[1]), partnerMessage(heroes[2])}
	sent := make(chan error, 1)
	go func() {
		if err := session.Send(11001, proto.Message{"partners": messages}); err != nil {
			sent <- err
			return
		}
		for _, msg := range messages {
			if err := session.Send(11002, msg); err != nil {
				sent <- err
				return
			}
		}
		sent <- nil
	}()
	if push := cli.recv(11001).Arr("partners"); len(push) != 1 || push[0].Int("partner_id") != heroes[2].PartnerID {
		test.Fatalf("batch push exposed ineligible heroes: %v", push)
	}
	if push := cli.recv(11002); push.Int("partner_id") != heroes[2].PartnerID {
		test.Fatalf("single push exposed an ineligible hero: %v", push)
	}
	if err := <-sent; err != nil {
		test.Fatal(err)
	}
}

func TestH5EquipmentRequiresKnownHeroTemplate(test *testing.T) {
	env := newDungeonEnv(test)
	cli, role := enterDungeonRole(env, store.PlatformH5, "hero-gear", "英雄装备")
	heroes := seedH5EligibilityRoster(test, env, role.RID,
		store.PartnerSeed{BID: 99999999, Star: 5}, store.PartnerSeed{BID: 10401, Star: 5})
	const gear = 40101
	if err := env.st.Exchange(role.RID, nil, nil, nil, map[int64]int64{gear: 1}); err != nil {
		test.Fatal(err)
	}
	for idx, hero := range heroes {
		frames := captureReplies(cli, 11010, proto.Message{"partner_id": hero.PartnerID, "item_id": equipmentStackID(gear)})
		if reply := growthReply(test, frames, 11010); reply.Int("result") != int64(idx) {
			test.Fatalf("equip hero template %d: %v", hero.BID, reply)
		}
		if items, err := env.st.RoleItems(role.RID); err != nil || items[gear] != 1-int64(idx) {
			test.Fatalf("equipment balance after hero %d: %v (%v)", hero.BID, items, err)
		}
	}
}

func TestH5FormationUsesEligibleHeroes(test *testing.T) {
	env := newDungeonEnv(test)
	cli, role := enterDungeonRole(env, store.PlatformH5, "hero-form", "英雄阵容")
	heroes := seedH5EligibilityRoster(test, env, role.RID,
		store.PartnerSeed{BID: 10401, Star: 4}, store.PartnerSeed{BID: 99999999, Star: 5}, store.PartnerSeed{BID: 10401, Star: 5})
	promoted := heroes[2]
	for _, kind := range []int64{formDrama, formArenaDefense, 2, formGuildBoss} {
		for idx, hero := range heroes {
			cli.send(11212, proto.Message{"type": kind, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero.PartnerID}}})
			if reply := cli.recv(11212); reply.Int("code") != int64(b2i(idx == 2)) {
				test.Errorf("formation %d save for hero %d/%d: %v", kind, hero.BID, hero.Star, reply)
			}
		}
	}
	oldSlots := []store.FormationSlot{{Pos: 1, PartnerID: heroes[0].PartnerID}, {Pos: 3, PartnerID: heroes[1].PartnerID}, {Pos: 5, PartnerID: promoted.PartnerID}}
	if err := env.st.SaveFormation(role.RID, store.Formation{Type: formDrama, FormationType: 1, Slots: oldSlots}); err != nil {
		test.Fatal(err)
	}
	cli.send(11211, proto.Message{"type": formDrama})
	if slots := cli.recv(11211).Arr("pos_info"); len(slots) != 1 || slots[0].Int("id") != promoted.PartnerID || slots[0].Int("pos") != 5 {
		test.Errorf("campaign read did not filter old slots: %v", slots)
	}
	cli.send(20060, proto.Message{"combat_type": combatDrama})
	if hook := cli.waitFor(20061).Arr("partner_list"); len(hook) != 1 || hook[0].Int("bid") != promoted.BID || hook[0].Int("star") != 5 {
		test.Errorf("idle battle roster: %v", hook)
	}
	cli.send(13003, proto.Message{"is_auto": 1})
	if reply := cli.recv(13003); reply.Int("code") != 1 {
		test.Fatalf("campaign battle: %v", reply)
	}
	intro := cli.waitFor(20027)
	var fighterIDs []int64
	for _, hero := range intro.Arr("objects") {
		if hero.Int("object_type") == 2 {
			fighterIDs = append(fighterIDs, hero.Int("object_id"))
		}
	}
	if !slices.Equal(fighterIDs, []int64{promoted.PartnerID}) {
		test.Errorf("campaign battle roster: %v", fighterIDs)
	}
	cli.send(20062, nil)
	result := cli.waitFor(13016).Arr("hurt_statistics")
	if len(result) != 1 || len(result[0].Arr("partner_hurts")) != 1 || result[0].Arr("partner_hurts")[0].Int("id") != promoted.PartnerID {
		test.Errorf("campaign result roster: %v", result)
	}
}

func TestH5DisplayedPowerMatchesDungeonEligibility(test *testing.T) {
	env := newDungeonEnv(test)
	cli, role := enterDungeonRole(env, store.PlatformH5, "hero-power", "英雄战力")
	if err := env.st.SetRoleLevel(role.RID, 20); err != nil {
		test.Fatal(err)
	}
	heroes := seedH5EligibilityRoster(test, env, role.RID, store.PartnerSeed{BID: 10401, Star: 4})
	if err := env.st.UpgradePartner(role.RID, heroes[0].PartnerID, 1, 0, 300, 5, nil, nil); err != nil {
		test.Fatal(err)
	}
	readPower := func() proto.Message { return growthReply(test, captureReplies(cli, 10300, nil), 10306) }
	if power := readPower(); power.Int("power") != 0 || power.Int("max_power") != 0 {
		test.Errorf("ineligible hero counted in 10306: %v", power)
	}
	cli.send(13003, nil)
	if reply := cli.recv(13003); reply.Int("code") != 0 || reply.Str("msg") != "没有可以出战的英雄" {
		test.Fatalf("campaign accepted an ineligible hero: %v", reply)
	}
	cli.send(13031, proto.Message{"id": 101})
	if reply := cli.waitFor(13031); reply.Int("code") != 0 || reply.Str("msg") != "战力不足" {
		test.Fatalf("ineligible hero counted in dungeon power: %v", reply)
	}
	granted, err := env.st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{{BID: 10401, Star: 5}}})
	if err != nil {
		test.Fatal(err)
	}
	if err := env.st.UpgradePartner(role.RID, granted[0].PartnerID, 1, 0, 300, 5, nil, nil); err != nil {
		test.Fatal(err)
	}
	promoted, err := env.st.PartnerByID(role.RID, granted[0].PartnerID)
	if err != nil {
		test.Fatal(err)
	}
	want := partnerMessage(*promoted).Int("power")
	if power := readPower(); want < stoneCfg.byID[101].Power || power.Int("power") != want || power.Int("max_power") != want {
		test.Errorf("10306 should report promoted hero's power %d: %v", want, power)
	}
	cli.send(13031, proto.Message{"id": 101})
	if reply := cli.waitFor(13031); reply.Int("code") != 1 {
		test.Fatalf("promoted hero's power did not qualify for dungeon: %v", reply)
	}
}
