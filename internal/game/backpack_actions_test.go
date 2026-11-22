package game

import (
	"errors"
	"maps"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func enterBackpackH5(t *testing.T, e *env, name string) (*client, *store.Role) {
	t.Helper()
	token, role := newBackpackH5Role(t, e, name)
	return loginBackpackRole(t, e.dial(true), name, token, role), role
}

func newBackpackH5Role(t *testing.T, e *env, name string) (string, *store.Role) {
	t.Helper()
	acc, err := e.st.FindOrCreateAccount(store.PlatformH5, name)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := e.st.IssueToken(acc.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	role, err := e.st.CreateRole(acc.ID, "symlf_1", name, 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return tok, role
}

func loginBackpackRole(t *testing.T, c *client, name, token string, role *store.Role) *client {
	t.Helper()
	c.send(1164, loginArgs(name, token))
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("login: %v", r)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if r := c.recv(10192); r.Int("code") != 1 {
		t.Fatalf("enter: %v", r)
	}
	return c
}

func TestH5BackpackActionsUseAndCompose(t *testing.T) {
	e := newEnv(t, time.Minute)
	const name = "背包"
	token, role := newBackpackH5Role(t, e, name)
	c := loginBackpackRole(t, e.mustDialWS(e.wsServer(gateway.WSOptions{})), name, token, role)
	e.give(c, role, map[int64]int64{99955: 1, 29440: 1, 29901: 51, 10428: 50, 10034: 1})
	c.send(10560, proto.Message{"items": []proto.Message{{"bid": 99955}, {"bid": 29901}, {"bid": 10428}, {"bid": 10034}}})
	details := c.recv(10560).Arr("items")
	if len(details) != 4 || details[0].Str("description") == "" || details[0].Int("usable") != 1 ||
		len(details[0].Arr("rewards")) != 8 || details[1].Int("compose_cmd") != 11008 ||
		details[1].Arr("cost")[0].Int("num") != 50 || details[2].Int("compose_id") != 10421 ||
		details[2].Int("compose_cmd") != 10523 || details[3].Int("sellable") != 1 {
		t.Fatalf("item details: %v", details)
	}

	c.send(10515, proto.Message{"id": 99955, "quantity": 1, "args": []proto.Message{}})
	if r := c.recv(10515); r.Int("flag") != 1 || r.Int("base_id") != 99955 {
		t.Fatalf("gift: %v", r)
	}
	if assets := c.recv(10302); assets.Str("coin") != "50000000" {
		t.Fatalf("gift did not credit currency: %v", assets)
	}
	c.recv(10500)

	c.send(10523, proto.Message{"id": 10421, "num": 1})
	if r := c.recv(10523); r.Int("flag") != 1 {
		t.Fatalf("runes: %v", r)
	}
	c.recv(10500)

	c.send(11008, proto.Message{"bid": 29901, "num": 2})
	if r := c.recv(11008); r.Int("result") != 0 {
		t.Fatalf("accepted too few shards: %v", r)
	}
	c.send(11008, proto.Message{"bid": 29901, "num": 1})
	if r := c.recv(11008); r.Int("result") != 1 || r.Arr("partners")[0].Int("partner_bid") != 70502 {
		t.Fatalf("fixed shard: %v", r)
	}
	if h := c.recv(11001).Arr("partners")[0]; h.Int("bid") != 70502 || h.Int("star") != 5 || h.Int("lev") != 1 {
		t.Fatalf("new hero push: %v", h)
	}
	c.recv(10500)
	c.recv(10306)

	c.send(10515, proto.Message{"id": 29440, "quantity": 1, "args": []proto.Message{}})
	if r := c.recv(10515); r.Int("flag") != 1 {
		t.Fatalf("hero card: %v", r)
	}
	if h := c.recv(11001).Arr("partners")[0]; h.Int("bid") != 70531 || h.Int("star") != 5 {
		t.Fatalf("hero card created wrong hero: %v", h)
	}
	c.recv(10500)
	c.recv(10306)
	ledger, err := e.st.RoleItems(role.RID)
	if err != nil || ledger[99955] != 0 || ledger[10428] != 0 || ledger[10421] != 1 ||
		ledger[29901] != 1 || ledger[29440] != 0 || ledger[10450] != 50000 {
		t.Fatalf("persisted item ledger: %v (%v)", ledger, err)
	}
}

func TestH5BackpackFiltersHeroActionsAndRoster(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := enterBackpackH5(t, e, "筛选")
	if _, err := starterGrant(e.st, role); err != nil {
		t.Fatal(err)
	}
	heroes, err := e.st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{
		{BID: 10401, Star: 4}, {BID: 70501, Star: 6},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []uint16{11000, 11025} {
		c.send(cmd, proto.Message{})
		got := c.recv(cmd).Arr("partners")
		if len(got) != 2 || got[0].Int("star") != 5 || got[1].Int("star") != 6 {
			t.Fatalf("roster %d: %v", cmd, got)
		}
	}
	c.send(11026, proto.Message{"partner_ids": []proto.Message{
		{"partner_id": heroes[0].PartnerID}, {"partner_id": heroes[1].PartnerID},
	}})
	if got := c.recv(11026).Arr("partners"); len(got) != 1 || got[0].Int("partner_id") != heroes[1].PartnerID {
		t.Fatalf("filtered hero details: %v", got)
	}

	sess := e.gw.Hub.Online(role.RID)
	push := proto.Message{"partners": []proto.Message{partnerMessage(heroes[0]), partnerMessage(heroes[1])}}
	if err := sess.Send(11001, push); err != nil {
		t.Fatal(err)
	}
	if got := c.recv(11001).Arr("partners"); len(got) != 1 || got[0].Int("star") != 6 || len(push.Arr("partners")) != 2 {
		t.Fatalf("filtered hero update: %v, original %v", got, push)
	}
	if err := sess.Send(11002, partnerMessage(heroes[0])); err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(11002, partnerMessage(heroes[1])); err != nil {
		t.Fatal(err)
	}
	if got := c.recv(11002); got.Int("star") != 6 || got.Int("partner_id") != heroes[1].PartnerID {
		t.Fatalf("single hero update: %v", got)
	}

	stock := map[int64]int64{24000: 1, 294036: 1, 24800: 30, 40101: 1}
	e.give(c, role, stock)
	c.send(10560, proto.Message{"items": []proto.Message{
		{"bid": 24000}, {"bid": 294036}, {"bid": 24800}, {"bid": 24800}, {"bid": 99999999},
	}})
	if got := c.recv(10560).Arr("items"); len(got) != 3 || got[0].Int("usable") != 0 || got[0].Str("use_name") != "" ||
		got[1].Int("usable") != 0 || got[1].Str("use_name") != "" || got[2].Int("compose_cmd") != 0 || got[2].Int("sellable") != 1 {
		t.Fatalf("action eligibility: %v", got)
	}
	for _, card := range []int64{24000, 294036, 29440} {
		c.send(10515, proto.Message{"id": card, "quantity": 1})
		if r := c.recv(10515); r.Int("flag") != 0 {
			t.Fatalf("used ineligible or unowned hero card %d: %v", card, r)
		}
	}
	c.send(11008, proto.Message{"bid": 24800, "num": 1})
	if r := c.recv(11008); r.Int("result") != 0 || len(r.Arr("partners")) != 0 {
		t.Fatalf("composed ineligible hero shards: %v", r)
	}
	c.send(11010, proto.Message{"partner_id": heroes[0].PartnerID, "item_id": 40101001})
	if r := c.recv(11010); r.Int("result") != 0 {
		t.Fatalf("equipped a hidden hero: %v", r)
	}
	c.send(11010, proto.Message{"partner_id": heroes[1].PartnerID, "item_id": 40101002})
	if r := c.recv(11010); r.Int("result") != 0 {
		t.Fatalf("accepted an invalid H5 equipment stack ID: %v", r)
	}
	if got, err := e.st.RoleItems(role.RID); err != nil || !maps.Equal(got, stock) {
		t.Fatalf("refused H5 actions changed stock: %v (%v)", got, err)
	}
	if got, err := e.st.PartnersOf(role.RID); err != nil || len(got) != 3 {
		t.Fatalf("refused H5 actions changed stored heroes: %v (%v)", got, err)
	}
}

func TestBackpackActionsPreserveNativeProtocols(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("native-bag", "原生")
	c.send(11000, proto.Message{})
	roster := c.recv(11000).Arr("partners")
	if len(roster) != 1 || roster[0].Int("star") != 4 {
		t.Fatalf("native starter roster: %v", roster)
	}

	stock := map[int64]int64{29440: 1, 29901: 50, 40101: 1}
	e.give(c, role, stock)
	c.send(10560, proto.Message{"items": []proto.Message{{"bid": 29440}}})
	c.send(11008, proto.Message{"bid": 29901, "num": 1})
	c.send(1169, proto.Message{})
	c.recv(1169) // The H5-only commands sent before the heartbeat have no native reply.
	c.send(10515, proto.Message{"id": 29440, "quantity": 1})
	if r := c.recv(10515); r.Int("flag") != 0 || r.Str("msg") != "该物品暂不能使用" {
		t.Fatalf("native card use changed: %v", r)
	}
	req := proto.Message{"storage": 3, "args": []proto.Message{{"id": 40101001, "bid": 40101, "num": 1}}}
	c.send(10521, req)
	if r := c.recv(10521); len(r.Arr("items")) != 0 {
		t.Fatalf("native equipment sale preview changed: %v", r)
	}
	c.send(10522, req)
	if r := c.recv(10522); r.Int("flag") != 0 || r.Str("msg") != "该背包暂不支持出售" {
		t.Fatalf("native equipment sale changed: %v", r)
	}
	if got, err := e.st.RoleItems(role.RID); err != nil || !maps.Equal(got, stock) {
		t.Fatalf("native refused actions changed stock: %v (%v)", got, err)
	}

	c.send(11010, proto.Message{"partner_id": roster[0].Int("partner_id"), "item_id": 40101002})
	if r := c.recv(11010); r.Int("result") != 1 {
		t.Fatalf("native wear no longer accepts the legacy piece ID: %v", r)
	}
	if got := c.recv(11012).Arr("eqms"); len(got) != 1 || got[0].Int("base_id") != 40101 {
		t.Fatalf("native equipment update: %v", got)
	}
	if got := c.recv(10501).Arr("item_list"); len(got) != 0 {
		t.Fatalf("native equipment bag after wear: %v", got)
	}
	c.send(1169, proto.Message{})
	c.recv(1169) // No H5 role-power push follows the native equipment update.
}

func TestH5BackpackCatalogLargePage(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := enterBackpackH5(t, e, "图鉴")
	ids := make([]int64, 0, len(itemTable))
	for id := range itemTable {
		ids = append(ids, id)
	}
	cost := func(id int64) int {
		return len(backpackActions.Descriptions[id]) + len(itemNames[id]) + len(giftTable[id].Items)*8
	}
	sort.Slice(ids, func(i, j int) bool {
		if cost(ids[i]) == cost(ids[j]) {
			return ids[i] < ids[j]
		}
		return cost(ids[i]) > cost(ids[j])
	})
	const pageSize = 64
	rows := make([]proto.Message, 0, pageSize+1)
	for _, id := range ids[:pageSize+1] {
		rows = append(rows, proto.Message{"bid": id})
	}
	c.send(10560, proto.Message{"items": rows})
	got := c.recv(10560).Arr("items")
	if len(got) != pageSize {
		t.Fatalf("catalog returned %d details, want %d", len(got), pageSize)
	}
	for i, row := range got {
		if row.Int("bid") != ids[i] {
			t.Fatalf("row %d: %v, want %d", i, row, ids[i])
		}
	}
}

func TestH5BackpackLargeCardGrantReservesStarterSlot(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := enterBackpackH5(t, e, "批量")
	const card = 29440
	stock := map[int64]int64{card: heroListMax}
	if err := e.st.Exchange(role.RID, nil, nil, nil, stock); err != nil {
		t.Fatal(err)
	}
	c.send(10515, proto.Message{"id": card, "quantity": heroListMax})
	if r := c.recv(10515); r.Int("flag") != 0 || r.Str("msg") != "英雄背包已满，无法放入这么多英雄" {
		t.Fatalf("grant occupied the pending starter slot: %v", r)
	}
	if got, err := e.st.RoleItems(role.RID); err != nil || !maps.Equal(got, stock) {
		t.Fatalf("refused grant changed stock: %v (%v)", got, err)
	}

	c.send(10515, proto.Message{"id": card, "quantity": heroListMax - 1})
	if r := c.recv(10515); r.Int("flag") != 1 {
		t.Fatalf("bulk grant: %v", r)
	}
	var added int
	for added < heroListMax-1 {
		batch := c.recv(11001).Arr("partners")
		if len(batch) == 0 {
			t.Fatal("empty hero grant update")
		}
		for _, hero := range batch {
			if hero.Int("bid") != 70531 || hero.Int("star") != 5 {
				t.Fatalf("unexpected hero in bulk grant: %v", hero)
			}
		}
		added += len(batch)
	}
	if added != heroListMax-1 {
		t.Fatalf("sent %d heroes, want %d", added, heroListMax-1)
	}
	c.recv(10500)
	c.recv(10306)
	c.send(11025, proto.Message{})
	roster := c.recv(11025).Arr("partners")
	if len(roster) != heroListMax || roster[len(roster)-1].Int("bid") != starterH5 {
		t.Fatalf("roster has %d heroes after granting the starter", len(roster))
	}
	if got, err := e.st.RoleItems(role.RID); err != nil || !maps.Equal(got, map[int64]int64{card: 1}) {
		t.Fatalf("bulk grant stock: %v (%v)", got, err)
	}
}

func TestH5BackpackSaleRejectsInvalidBatches(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := enterBackpackH5(t, e, "出售")
	stock := map[int64]int64{40101: 1, 10034: 100000}
	if err := e.st.Exchange(role.RID, nil, nil, nil, stock); err != nil {
		t.Fatal(err)
	}
	gear := proto.Message{"id": 40101001, "bid": 40101, "num": 1}
	for _, tc := range []struct {
		name string
		req  proto.Message
	}{
		{"wrong stack id", proto.Message{"storage": 3, "args": []proto.Message{{"id": 40101002, "bid": 40101, "num": 1}}}},
		{"aggregate exceeds stock", proto.Message{"storage": 3, "args": []proto.Message{gear, gear}}},
		{"wrong storage", sale([2]int64{40101, 1})},
		{"gain overflows quote", sale([2]int64{10034, 50000}, [2]int64{10034, 50000})},
	} {
		c.send(10521, tc.req)
		if got := c.recv(10521).Arr("items"); len(got) != 0 {
			t.Fatalf("%s: invalid sale was priced: %v", tc.name, got)
		}
		c.send(10522, tc.req)
		if r := c.recv(10522); r.Int("flag") != 0 {
			t.Fatalf("%s: invalid sale was executed: %v", tc.name, r)
		}
	}
	c.send(10521, sale([2]int64{10034, 65536}))
	if got := c.recv(10521).Arr("items"); len(got) != 0 {
		t.Fatalf("preview accepted an amount that confirmation cannot encode: %v", got)
	}
	if got, err := e.st.RoleItems(role.RID); err != nil || !maps.Equal(got, stock) {
		t.Fatalf("refused sales changed stock: %v (%v)", got, err)
	}
	if got, err := e.st.RoleAssets(role.RID); err != nil || len(got) != 0 {
		t.Fatalf("refused sales changed assets: %v (%v)", got, err)
	}
}

func TestH5BackpackEquipmentOwnershipAndPower(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := enterBackpackH5(t, e, "装备")
	foreign, other := enterBackpackH5(t, e, "他人")
	if _, err := starterGrant(e.st, role); err != nil {
		t.Fatal(err)
	}
	if _, err := starterGrant(e.st, other); err != nil {
		t.Fatal(err)
	}
	owned, _ := e.st.PartnersOf(role.RID)
	theirs, _ := e.st.PartnersOf(other.RID)
	pid := owned[0].PartnerID
	if err := e.st.SetPartnerLocked(role.RID, pid, true); err != nil {
		t.Fatal(err)
	}
	before := rolePower(owned)
	e.give(c, role, map[int64]int64{40101: 1, 40201: 1})
	e.give(foreign, other, map[int64]int64{40102: 1})

	wear := func(c *client, p, item int64) proto.Message {
		c.send(11010, proto.Message{"partner_id": p, "item_id": item})
		return c.recv(11010)
	}
	if r := wear(c, theirs[0].PartnerID, 40101001); r.Int("result") != 0 {
		t.Fatalf("accepted another hero: %v", r)
	}
	if r := wear(foreign, theirs[0].PartnerID, 40101001); r.Int("result") != 0 {
		t.Fatalf("accepted another player's bag stock: %v", r)
	}
	if r := wear(c, pid, 0); r.Int("result") != 1 {
		t.Fatalf("locked hero cannot wear: %v", r)
	}
	h := c.recv(11012)
	if h.Int("partner_id") != pid || h.Int("atk") <= partnerMessage(owned[0]).Int("atk") || len(h.Arr("eqms")) != 2 {
		t.Fatalf("equipment state: %v", h)
	}
	if items := c.recv(10501).Arr("item_list"); len(items) != 0 {
		t.Fatalf("worn gear also in bag: %v", items)
	}
	peak := c.recv(10306).Int("power")
	if peak <= before {
		t.Fatalf("role power: %d -> %d", before, peak)
	}
	quote := proto.Message{"storage": 3, "args": []proto.Message{{"id": 40101001, "bid": 40101, "num": 1}}}
	c.send(10521, quote)
	if r := c.recv(10521); len(r.Arr("items")) != 0 {
		t.Fatalf("worn equipment quoted for sale: %v", r)
	}
	c.send(10522, quote)
	if r := c.recv(10522); r.Int("flag") != 0 {
		t.Fatalf("sold another copy of worn gear: %v", r)
	}

	c.send(11011, proto.Message{"partner_id": pid, "pos_id": 0})
	if r := c.recv(11011); r.Int("result") != 1 || len(c.recv(11012).Arr("eqms")) != 0 {
		t.Fatalf("take all off: %v", r)
	}
	if bag := c.recv(10501).Arr("item_list"); len(bag) != 2 {
		t.Fatalf("unequipped gear missing from bag: %v", bag)
	}
	if p := c.recv(10306); p.Int("power") != before || p.Int("max_power") != peak {
		t.Fatalf("unequipped power: %v", p)
	}
	c.send(10521, quote)
	if r := c.recv(10521).Arr("items"); len(r) != 1 || r[0].Int("num") != 250 {
		t.Fatalf("equipment sale quote: %v", r)
	}
	c.send(10522, quote)
	if r := c.recv(10522); r.Int("flag") != 1 {
		t.Fatalf("sell unequipped: %v", r)
	}
	if r := c.recv(10302); r.Str("coin") != "250" {
		t.Fatalf("sale proceeds: %v", r)
	}
	c.recv(10500)
	equips := c.recv(10501).Arr("item_list")
	if len(equips) != 1 || equips[0].Int("base_id") != 40201 {
		t.Fatalf("wrong gear sold: %v", equips)
	}
	if p, err := e.st.PartnerByID(role.RID, pid); err != nil || !p.Locked || len(p.Equips) != 0 {
		t.Fatalf("hero lock or slot state changed: %+v (%v)", p, err)
	}
	if items, _ := e.st.RoleItems(other.RID); items[40102] != 1 {
		t.Fatalf("other player's inventory changed: %v", items)
	}
	acc, err := e.st.FindOrCreateAccount(store.PlatformH5, "装备")
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.IssueToken(acc.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	c = loginBackpackRole(t, e.dial(true), "装备", token, role)
	c.send(10501, proto.Message{})
	if bag := c.recv(10501).Arr("item_list"); len(bag) != 1 || bag[0].Int("base_id") != 40201 {
		t.Fatalf("equipment after relogin: %v", bag)
	}
}

func TestBackpackAtomicEquipmentAndCapacity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "inventory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Platform: "symlf", ZoneID: 1, Status: 1}); err != nil {
		t.Fatal(err)
	}
	acc, err := st.FindOrCreateAccount(store.PlatformH5, "atomic")
	if err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(acc.ID, "symlf_1", "背包", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := starterGrant(st, role); err != nil {
		t.Fatal(err)
	}
	heroes, err := st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("starter hero: %v (%v)", heroes, err)
	}
	pid := heroes[0].PartnerID
	if err := st.Exchange(role.RID, nil, nil, nil, map[int64]int64{40101: 1, 29901: 50}); err != nil {
		t.Fatal(err)
	}
	changes := []store.Equip{{Slot: 1, BaseID: 40101}, {Slot: 3, BaseID: 40201}}
	if err := st.ChangeEquipment(role.RID, pid, []store.Equip{}, changes); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("unowned second item should fail: %v", err)
	}
	if p, _ := st.PartnerByID(role.RID, pid); len(p.Equips) != 0 {
		t.Fatalf("first slot was changed by failed batch: %v", p.Equips)
	}
	seed := []store.PartnerSeed{{BID: 70502, Star: 5}}
	if _, err := st.ComposePartners(role.RID, 29901, 50, seed, func(int) int { return 1 }); !errors.Is(err, store.ErrHeroCapacity) {
		t.Fatalf("full hero bag accepted shards: %v", err)
	}
	if ledger, _ := st.RoleItems(role.RID); ledger[29901] != 50 || ledger[40101] != 1 {
		t.Fatalf("failed operations deducted items: %v", ledger)
	}
	if err := st.EquipPiece(role.RID, pid, 1, 40101); err != nil {
		t.Fatal(err)
	}
	if err := st.ChangeEquipment(role.RID, pid, []store.Equip{}, []store.Equip{{Slot: 1}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale loadout was used: %v", err)
	}
	if err := st.Exchange(role.RID, nil, nil, nil, map[int64]int64{29901: 50}); err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := st.ComposePartners(role.RID, 29901, 50, seed, func(int) int { return 2 })
			results <- err
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a != nil {
		a, b = b, a
	}
	if a != nil || !errors.Is(b, store.ErrHeroCapacity) {
		t.Fatalf("concurrent composition results: %v and %v", a, b)
	}
	if got, err := st.PartnersOf(role.RID); err != nil || len(got) != 2 {
		t.Fatalf("two operations claimed one slot: %v (%v)", got, err)
	}
	if got, err := st.RoleItems(role.RID); err != nil || got[29901] != 50 {
		t.Fatalf("concurrent composition changed the wrong quantity: %v (%v)", got, err)
	}
	if got := shardPools[29105]; len(got) == 0 {
		t.Fatal("no heroes for generic camp shard")
	} else {
		for _, bid := range got {
			hero := partnerTable[bid]
			if hero.Camp != 1 || hero.InitStar != 5 {
				t.Fatalf("wrong camp or star in shard pool: %d", bid)
			}
		}
	}
}
