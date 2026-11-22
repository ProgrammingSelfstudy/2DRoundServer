package game

import (
	"reflect"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func finishDailyDungeonFight(t *testing.T, c *client, combat int64) proto.Message {
	t.Helper()
	c.send(20001, nil)
	c.waitFor(20002)
	for range maxFightRounds {
		c.send(20019, nil)
		c.waitFor(20004)
		c.send(20005, nil)
		for range 30 {
			cmd, msg := c.recvNext()
			if cmd == 20006 {
				if msg.Int("combat_type") != combat {
					t.Fatalf("wrong combat result: %v", msg)
				}
				return msg
			}
			if cmd == 20002 {
				break
			}
		}
	}
	t.Fatal("no dungeon settlement after the last round")
	return nil
}

func enterDungeonRole(e *env, platform, account, name string) (*client, *store.Role) {
	e.t.Helper()
	a, err := e.st.FindOrCreateAccount(platform, account)
	if err != nil {
		e.t.Fatal(err)
	}
	role, err := e.st.CreateRole(a.ID, e.login.SrvID, name, 1, 1, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return loginDungeonRole(e, account, role), role
}

func loginDungeonRole(e *env, account string, role *store.Role) *client {
	e.t.Helper()
	token, err := e.st.IssueToken(role.AccountID, 3600)
	if err != nil {
		e.t.Fatal(err)
	}
	c := e.dial(true)
	c.send(1164, loginArgs(account, token))
	if m := c.recv(1164); m.Int("code") != 1 {
		e.t.Fatalf("login: %v", m)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if m := c.recv(10192); m.Int("code") != 1 {
		e.t.Fatalf("enter: %v", m)
	}
	return c
}

func strengthenDungeonHeroes(t *testing.T, s *store.Store, rid int64) {
	t.Helper()
	heroes, err := s.PartnersOf(rid)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("heroes: %v, %v", heroes, err)
	}
	for _, hero := range heroes {
		if err := s.UpgradePartner(rid, hero.PartnerID, hero.Lev, hero.BreakLev, 300, 5, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func mixedDungeonHeroes(t *testing.T, s *store.Store, rid int64) (low, high store.Partner) {
	t.Helper()
	// Awaken a four-star template and a native five-star template.
	seeds := []store.PartnerSeed{{BID: starterMale, Star: 5}, {BID: starterH5, Star: 6}}
	if ok, err := s.GrantStarterPartners(rid, seeds); err != nil || !ok {
		t.Fatalf("seed heroes: %v, %v", ok, err)
	}
	strengthenDungeonHeroes(t, s, rid)
	heroes, err := s.PartnersOf(rid)
	if err != nil || len(heroes) != 2 {
		t.Fatalf("read heroes: %v, %v", heroes, err)
	}
	return heroes[0], heroes[1]
}

func TestDailyDungeonsHeroRewardsByPlatform(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
		heroBIDs       []int64
		rewards        map[int64]int64
	}{
		{"H5", store.PlatformH5, []int64{starterMale, starterH5}, map[int64]int64{fiveStarRandomShard: 12}},
		{"original", "symlf", []int64{starterMale, starterH5}, map[int64]int64{threeStarRandomShard: 10, fourStarRandomShard: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newDungeonEnv(t)
			e.setNow(time.Date(2026, time.November, 22, 6, 0, 0, 0, dayZone))
			c, role := enterDungeonRole(e, tc.platform, "hero-dungeon", "英雄副本")
			if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
				t.Fatal(err)
			}
			low, high := mixedDungeonHeroes(t, e.st, role.RID)
			if err := e.st.SaveFormation(role.RID, store.Formation{
				Type: formDrama, FormationType: 1,
				Slots: []store.FormationSlot{{Pos: 2, PartnerID: low.PartnerID}, {Pos: 5, PartnerID: high.PartnerID}},
			}); err != nil {
				t.Fatal(err)
			}
			req := proto.Message{"id": 301}
			c.send(13031, req)
			if r := c.waitFor(13031); r.Int("code") != 1 {
				t.Fatalf("challenge: %v", r)
			}
			intro := c.waitFor(20027)
			var heroBIDs []int64
			for _, obj := range intro.Arr("objects") {
				if obj.Int("object_type") == 2 {
					heroBIDs = append(heroBIDs, obj.Int("object_bid"))
					if obj.Int("object_bid") == starterH5 && (obj.Int("star") != 6 || obj.Int("pos") != battleCell(1, 5)) {
						t.Fatalf("awakened hero lost its star or position: %v", obj)
					}
				}
			}
			if !reflect.DeepEqual(heroBIDs, tc.heroBIDs) || intro.Int("a_object_num") != int64(len(tc.heroBIDs)) {
				t.Fatalf("battle roster %v, want %v", heroBIDs, tc.heroBIDs)
			}
			result := finishDailyDungeonFight(t, c, combatStoneDungeon)
			got := map[int64]int64{}
			for _, reward := range result.Arr("item_rewards") {
				got[reward.Int("bid")] += reward.Int("num")
			}
			if result.Int("result") != 1 || len(result.Arr("item_rewards")) != len(tc.rewards) || !reflect.DeepEqual(got, tc.rewards) {
				t.Fatalf("battle result %v, want %v", result, tc.rewards)
			}
			var stats []int64
			for _, group := range result.Arr("hurt_statistics") {
				for _, h := range group.Arr("partner_hurts") {
					stats = append(stats, h.Int("bid"))
				}
			}
			if !reflect.DeepEqual(stats, tc.heroBIDs) {
				t.Fatalf("result roster %v, want %v", stats, tc.heroBIDs)
			}

			c.send(13032, req)
			if r := rewardTotals(t, c.waitFor(12742)); !reflect.DeepEqual(r, tc.rewards) {
				t.Fatalf("sweep rewards %v, want %v", r, tc.rewards)
			}
			if r := c.waitFor(13032); r.Int("code") != 1 {
				t.Fatalf("sweep: %v", r)
			}
			items, err := e.st.RoleItems(role.RID)
			if err != nil || len(items) != len(tc.rewards) {
				t.Fatalf("reward ledger: %v, %v", items, err)
			}
			for id, n := range tc.rewards {
				if items[id] != 2*n {
					t.Fatalf("item %d balance %d, want %d", id, items[id], 2*n)
				}
			}
		})
	}
}

func TestDailyDungeonsMaterialBattleSweepAndRelog(t *testing.T) {
	e, c, role := stoneEnvFor(t, store.PlatformH5)
	e.setNow(time.Date(2026, time.November, 22, 4, 57, 0, 0, dayZone))
	strengthenDungeonHeroes(t, e.st, role.RID)
	c.send(13031, proto.Message{"id": 102})
	if r := c.waitFor(13031); r.Int("code") != 0 || r.Str("msg") != "通关前置难度后开启" {
		t.Fatalf("previous stage gate: %v", r)
	}
	c.send(13031, proto.Message{"id": 101})
	if r := c.waitFor(13031); r.Int("code") != 1 {
		t.Fatalf("start material dungeon: %v", r)
	}
	if r := c.waitFor(20027); r.Int("combat_type") != combatStoneDungeon {
		t.Fatalf("intro: %v", r)
	}
	result := finishDailyDungeonFight(t, c, combatStoneDungeon)
	if result.Int("result") != 1 || len(result.Arr("item_rewards")) != 2 {
		t.Fatalf("material victory: %v", result)
	}
	c.send(13032, proto.Message{"id": 101})
	loot := c.waitFor(12742).Arr("asset_list")
	if len(loot) != 2 || loot[0].Int("bid") != 1 || loot[0].Str("num") != "100000" {
		t.Fatalf("sweep loot: %v", loot)
	}
	if r := c.waitFor(13032); r.Int("code") != 1 {
		t.Fatalf("sweep: %v", r)
	}
	c.c.Close()
	c = loginDungeonRole(e, "ua", role)
	c.send(13030, nil)
	state := c.waitFor(13030)
	if state.Arr("list")[0].Int("day_num") != 2 || len(state.Arr("pass_list")) != 1 || state.Arr("pass_list")[0].Int("id") != 101 {
		t.Fatalf("material dungeon after relogin: %v", state)
	}
	c.send(10300, nil)
	if r := c.waitFor(10302); r.Str("coin") != "200000" {
		t.Fatalf("material dungeon currency after relogin: %v", r)
	}
	c.send(10500, nil)
	bag := c.waitFor(10500).Arr("item_list")
	found := false
	for _, item := range bag {
		if item.Int("base_id") == 10001 && item.Int("quantity") == 20 {
			found = true
		}
	}
	if !found {
		t.Fatalf("material dungeon items after relogin: %v", bag)
	}
	if quests, err := e.st.QuestRows(role.RID, 20261122); err != nil || quests[90023].Value != 2 {
		t.Fatalf("daily quest after one challenge and one sweep: %v, %v", quests[90023], err)
	}
	e.advance(4 * time.Minute)
	c.send(13030, nil)
	if r := c.waitFor(13030); r.Arr("list")[0].Int("day_num") != 0 || len(r.Arr("pass_list")) != 1 {
		t.Fatalf("05:00 reset: %v", r)
	}
}

func TestDailyDungeonsExpeditionSweepBattleBoxAndRelog(t *testing.T) {
	e, c, role, hero := expEnvFor(t, store.PlatformH5)
	e.setNow(time.Date(2026, time.November, 22, 0, 1, 0, 0, dayZone))
	strengthenDungeonHeroes(t, e.st, role.RID)
	e.expSave(t, role, &expState{Day: 20261121, Diff: 1, Guard: expPointID(15)})
	c.send(24412, proto.Message{"difficulty": 1})
	if r := c.waitFor(24412); r.Int("code") != 1 {
		t.Fatalf("choose expedition: %v", r)
	}
	if r := c.waitFor(24414); r.Int("floor_id") != 4 || r.Arr("rewards")[0].Int("num") != 352500 {
		t.Fatalf("auto-sweep: %v", r)
	}
	if r := c.waitFor(24400); r.Int("guard_id") != 6 || len(r.Arr("reward")) != 1 {
		t.Fatalf("auto-sweep progress: %v", r)
	}
	for _, point := range []int64{6, 7} {
		c.send(24403, expTeam(hero))
		if r := c.waitFor(24403); r.Int("code") != 1 {
			t.Fatalf("start expedition point %d: %v", point, r)
		}
		c.waitFor(20027)
		if r := finishDailyDungeonFight(t, c, combatExpedit); r.Int("result") != 1 {
			t.Fatalf("expedition victory for point %d: %v", point, r)
		}
		c.waitFor(24400)
	}
	c.send(24402, proto.Message{"id": 8})
	if r := c.waitFor(24402); r.Int("code") != 1 {
		t.Fatalf("expedition box: %v", r)
	}
	c.send(24400, nil)
	state := c.waitFor(24400)
	c.send(24409, nil)
	hp := c.waitFor(24409)
	c.c.Close()
	c = loginDungeonRole(e, "ux", role)
	c.send(24400, nil)
	if r := c.waitFor(24400); !reflect.DeepEqual(state, r) || r.Int("guard_id") != 9 || len(r.Arr("reward")) != 2 {
		t.Fatalf("expedition state after relogin: %v -> %v", state, r)
	}
	c.send(24409, nil)
	if r := c.waitFor(24409); !reflect.DeepEqual(hp, r) {
		t.Fatalf("expedition HP after relogin: %v -> %v", hp, r)
	}
	c.send(24402, proto.Message{"id": 8})
	if r := c.waitFor(24402); r.Int("code") != 0 {
		t.Fatalf("duplicate box after relogin: %v", r)
	}
	balances, err := e.st.RoleAssets(role.RID)
	if err != nil || balances[1] != 564000 || balances[25] != 808 {
		t.Fatalf("expedition balances: %v, %v", balances, err)
	}
}

func TestDailyDungeonsExpeditionDoesNotSettleAnOldDaysBattle(t *testing.T) {
	e, c, role, hero := expEnvFor(t, store.PlatformH5)
	e.setNow(time.Date(2026, time.November, 22, 23, 59, 0, 0, dayZone))
	c.send(24412, proto.Message{"difficulty": 1})
	c.waitFor(24412)
	c.waitFor(24400)
	c.send(24403, expTeam(hero))
	c.waitFor(24403)
	c.waitFor(20027)
	e.advance(2 * time.Minute)
	c.send(24412, proto.Message{"difficulty": 1})
	if r := c.waitFor(24412); r.Int("code") != 1 {
		t.Fatalf("next day's difficulty: %v", r)
	}
	c.waitFor(24400)
	if r := finishDailyDungeonFight(t, c, combatExpedit); r.Int("result") != 0 || len(r.Arr("item_rewards")) != 0 {
		t.Fatalf("old battle settlement: %v", r)
	}
	if st := e.expState(t, role); st.Day != 20261123 || st.Guard != 1 || len(st.Hurt) != 0 || len(st.Got) != 0 {
		t.Fatalf("old battle changed next day's state: %+v", st)
	}
	if assets, err := e.st.RoleAssets(role.RID); err != nil || assets[1] != 0 || assets[25] != 0 {
		t.Fatalf("old battle paid rewards: %v, %v", assets, err)
	}
}

func TestDailyDungeonsExpeditionFiltersH5HeroesAndLoans(t *testing.T) {
	e := newDungeonEnv(t)
	now := time.Date(2026, time.November, 22, 6, 0, 0, 0, dayZone)
	e.setNow(now)
	highClient, h5 := enterDungeonRole(e, store.PlatformH5, "h5", "远征者")
	friend, original := enterDungeonRole(e, "symlf", "native", "好友")
	promoted, high := mixedDungeonHeroes(t, e.st, h5.RID)
	friendPromoted, _ := mixedDungeonHeroes(t, e.st, original.RID)
	wantLow := func(rid int64) store.Partner {
		t.Helper()
		heroes, err := e.st.Summon(rid, store.SummonResult{Seeds: []store.PartnerSeed{{BID: starterMale, Star: 4}}})
		if err != nil || len(heroes) != 1 {
			t.Fatalf("seed current four-star hero: %v (%v)", heroes, err)
		}
		return heroes[0]
	}
	low, friendLow := wantLow(h5.RID), wantLow(original.RID)
	if err := e.st.RequestFriend(original.RID, h5.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(original.RID, h5.RID, 50); err != nil {
		t.Fatal(err)
	}
	e.expSave(t, h5, &expState{
		Day: dayKey(now), Diff: 1, Guard: 1, Sent: low.PartnerID,
		Hurt: map[string]int64{itoa(low.PartnerID): 50, itoa(high.PartnerID): 75},
		Hired: []expHire{{RID: original.RID, SrvID: original.SrvID, PID: friendLow.PartnerID,
			BID: friendLow.BID, Lev: int64(friendLow.Lev), Star: int64(friendLow.Star), Per: 100}},
	})

	friend.send(24407, proto.Message{"id": friendLow.PartnerID})
	if r := friend.waitFor(24407); r.Int("code") != 1 {
		t.Fatalf("original dispatch: %v", r)
	}
	friend.send(24405, nil)
	if r := friend.waitFor(24405).Arr("list"); len(r) != 1 || r[0].Int("bid") != starterMale {
		t.Fatalf("original dispatch list: %v", r)
	}
	for _, cmd := range []uint16{24405, 24406} {
		highClient.send(cmd, nil)
		if r := highClient.waitFor(cmd); len(r.Arr("list")) != 0 {
			t.Fatalf("%d exposes a current four-star hero: %v", cmd, r)
		}
	}
	highClient.send(24409, nil)
	hp := highClient.waitFor(24409)
	own := hp.Arr("p_list")
	if len(hp.Arr("list")) != 0 || len(own) != 1 || own[0].Int("id") != high.PartnerID || own[0].Int("hp_per") != 75 {
		t.Fatalf("H5 expedition HP: %v", hp)
	}
	highClient.send(24401, proto.Message{"id": 1})
	guards := highClient.waitFor(24401).Arr("guards")
	guardStars := make(map[int64]int64, len(guards))
	for _, guard := range guards {
		guardStars[guard.Int("bid")] = guard.Int("star")
	}
	if len(guards) != 2 || guardStars[starterH5] != 6 || guardStars[starterMale] != 5 {
		t.Fatalf("H5 expedition guards: %v", guards)
	}
	highClient.send(24407, proto.Message{"id": low.PartnerID})
	if r := highClient.waitFor(24407); r.Int("code") != 0 {
		t.Fatalf("H5 dispatch accepted a current four-star hero: %v", r)
	}
	highClient.send(24408, proto.Message{"rid": original.RID, "srv_id": original.SrvID, "id": friendLow.PartnerID})
	if r := highClient.waitFor(24408); r.Int("code") != 0 {
		t.Fatalf("H5 hire accepted a current four-star hero: %v", r)
	}
	if assets, err := e.st.RoleAssets(original.RID); err != nil || assets[14] != 0 {
		t.Fatalf("rejected hire paid friendship points: %v, %v", assets, err)
	}

	highClient.send(24403, expTeam(low.PartnerID))
	if r := highClient.waitFor(24403); r.Int("code") != 0 {
		t.Fatalf("H5 expedition used a current four-star hero: %v", r)
	}
	loan := expTeam(friendLow.PartnerID)
	loan.Arr("pos_info")[0]["owner_id"] = original.RID
	highClient.send(24403, loan)
	if r := highClient.waitFor(24403); r.Int("code") != 0 {
		t.Fatalf("H5 expedition used an old four-star loan: %v", r)
	}

	highClient.send(24407, proto.Message{"id": promoted.PartnerID})
	if r := highClient.waitFor(24407); r.Int("code") != 1 {
		t.Fatalf("H5 promoted hero dispatch: %v", r)
	}
	friend.send(24407, proto.Message{"id": friendPromoted.PartnerID})
	if r := friend.waitFor(24407); r.Int("code") != 1 {
		t.Fatalf("five-star dispatch: %v", r)
	}
	highClient.send(24406, nil)
	if r := highClient.waitFor(24406).Arr("list"); len(r) != 1 || r[0].Int("id") != friendPromoted.PartnerID {
		t.Fatalf("H5 five-star offer: %v", r)
	}
	highClient.send(24408, proto.Message{"rid": original.RID, "srv_id": original.SrvID, "id": friendPromoted.PartnerID})
	if r := highClient.waitFor(24408); r.Int("code") != 1 {
		t.Fatalf("H5 five-star hire: %v", r)
	}
	if assets, err := e.st.RoleAssets(original.RID); err != nil || assets[14] != 10 {
		t.Fatalf("five-star hire payout: %v, %v", assets, err)
	}
	team := expTeam(promoted.PartnerID)
	team["pos_info"] = append(team.Arr("pos_info"), proto.Message{"pos": 2, "id": friendPromoted.PartnerID, "owner_id": original.RID})
	highClient.send(24403, team)
	if r := highClient.waitFor(24403); r.Int("code") != 1 {
		t.Fatalf("H5 five-star expedition: %v", r)
	}
	if intro := highClient.waitFor(20027); intro.Int("a_object_num") != 2 {
		t.Fatalf("H5 expedition team: %v", intro)
	} else {
		fighting := make(map[int64]int64)
		for _, obj := range intro.Arr("objects") {
			if obj.Int("object_type") != 2 {
				continue
			}
			if obj.Int("star") < 5 || obj.Int("group") == 1 && obj.Int("object_bid") != starterMale {
				t.Fatalf("H5 expedition intro contains an unexpected hero: %v", obj)
			}
			fighting[obj.Int("group")]++
		}
		if fighting[1] != 2 || fighting[2] != 2 {
			t.Fatalf("expected two promoted fighters and two guards: %v", intro)
		}
	}
}
