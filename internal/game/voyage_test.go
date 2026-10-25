package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestVoyageOrderLifecycle(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("vy", "出海人")
	var seeds []store.PartnerSeed
	for bid, p := range partnerTable {
		if p.Camp == 1 && len(seeds) == 0 || p.Camp == 2 && len(seeds) == 1 {
			seeds = append(seeds, store.PartnerSeed{BID: bid, Star: 5})
		}
	}
	if ok, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !ok {
		t.Fatal(err, ok)
	}
	c.send(23800, proto.Message{})
	b := c.waitFor(23800)
	list := b.Arr("order_list")
	if len(list) != 6 || b.Int("free_times") != 0 {
		t.Fatalf("23800: %v", b)
	}
	// 10001: two heroes, a 2 star one and one of camp 1; costs 2000 coins, takes two hours
	st := &voyageState{}
	if _, err := e.st.LoadState(role.RID, voyageModule, st); err != nil {
		t.Fatal(err)
	}
	st.Orders[0] = voyageOrder{ID: 77, Bid: 10001, Status: voyageUnget}
	if err := e.st.SaveState(role.RID, voyageModule, st); err != nil {
		t.Fatal(err)
	}
	parts, _ := e.st.PartnersOf(role.RID)
	ids := []proto.Message{{"partner_id": parts[0].PartnerID}, {"partner_id": parts[1].PartnerID}}
	c.send(23802, proto.Message{"order_id": 77, "assign_ids": ids})
	if r := c.waitFor(23802); r.Int("flag") != 0 {
		t.Fatalf("no coins: %v", r)
	}
	e.eggGrant(t, role, map[int64]int64{7: 5000, voyageDiamond: 100}, nil)
	c.send(23802, proto.Message{"order_id": 77, "assign_ids": ids})
	if r := c.waitFor(23802); r.Int("flag") != 1 {
		t.Fatalf("receive: %v", r)
	}
	if r := c.waitFor(23801); r.Int("status") != voyageUnderway || r.Int("end_time") <= time.Now().Unix() || len(r.Arr("assign_ids")) != 2 {
		t.Fatalf("23801: %v", r)
	}
	c.send(23802, proto.Message{"order_id": 77, "assign_ids": ids})
	if r := c.waitFor(23802); r.Int("flag") != 0 {
		t.Fatalf("twice: %v", r)
	}
	c.send(23803, proto.Message{"order_id": 77, "type": 0})
	if r := c.waitFor(23803); r.Int("flag") != 0 {
		t.Fatalf("collect early: %v", r)
	}
	c.send(23803, proto.Message{"order_id": 77, "type": 1})
	if r := c.waitFor(23803); r.Int("flag") != 1 || r.Int("order_id") != 77 {
		t.Fatalf("speed up: %v", r)
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[29903] != 20 {
		t.Fatalf("award: %v", items)
	}
	assets, _ := e.st.RoleAssets(role.RID)
	if assets[7] != 3000 {
		t.Fatalf("coins: %v", assets)
	}
}

func TestVoyageRefresh(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, _ := e.enter("vr", "刷新人")
	c.send(23800, proto.Message{})
	c.waitFor(23800)
	c.send(23804, proto.Message{})
	if r := c.waitFor(23804); r.Int("flag") != 1 {
		t.Fatalf("refresh: %v", r)
	}
	if b := c.waitFor(23800); len(b.Arr("order_list")) != 6 || b.Int("free_times") != 1 {
		t.Fatalf("after refresh: %v", b)
	}
	c.send(23804, proto.Message{})
	c.waitFor(23804)
	c.waitFor(23800)
	c.send(23804, proto.Message{})
	if r := c.waitFor(23804); r.Int("flag") != 0 {
		t.Fatalf("third without anything: %v", r)
	}
	c.send(23805, proto.Message{})
	if r := c.waitFor(23805); r.Int("flag") != 0 {
		t.Fatalf("23805: %v", r)
	}
}
