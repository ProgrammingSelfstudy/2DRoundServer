package game

import (
	"sort"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestVoyageOrderLifecycle(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("vy", "出海人")
	var seeds []store.PartnerSeed
	var bids []int64 // the map has no order, and the second hero must come after a camp 1 one
	for bid := range partnerTable {
		bids = append(bids, bid)
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i] < bids[j] })
	for _, bid := range bids {
		if p := partnerTable[bid]; p.Camp == 1 && len(seeds) == 0 || p.Camp == 2 && len(seeds) == 1 {
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

// The voyage privileges: a refresh brings an order of the promised quality when the deal had none.
func TestVoyageEnsureQuality(t *testing.T) {
	var low []int64
	counts := map[int]int{}
	for _, c := range voyageCfg.Orders {
		counts[c.Quality]++
		if c.Quality < voyagePurple {
			low = append(low, c.ID)
		}
	}
	if len(low) < 3 || counts[voyagePurple] == 0 || counts[voyageOrange] == 0 {
		t.Fatalf("orders by quality: %v", counts)
	}
	qualityOf := func(bid int64) int { i, _ := voyageCfg.order(bid); return voyageCfg.Orders[i].Quality }
	zero := func(int) int { return 0 }
	mk := func() *voyageState {
		st := &voyageState{}
		for i, bid := range low[:3] {
			st.Orders = append(st.Orders, voyageOrder{ID: int64(i + 1), Bid: bid, Status: voyageUnget})
		}
		return st
	}
	for _, q := range []int{voyagePurple, voyageOrange} {
		st := mk()
		st.ensureQuality(q, zero)
		got := 0
		for _, o := range st.Orders {
			if qualityOf(o.Bid) >= q {
				got++
			}
		}
		if got != 1 || len(st.Orders) != 3 || st.Orders[2].ID != 3 || qualityOf(st.Orders[2].Bid) < q {
			t.Fatalf("quality %d: %+v", q, st.Orders)
		}
		seen := map[int64]bool{}
		for _, o := range st.Orders {
			if seen[o.Bid] {
				t.Fatalf("an order twice: %+v", st.Orders)
			}
			seen[o.Bid] = true
		}
		st.ensureQuality(q, zero) // one is there now: nothing changes
		if st2 := st; qualityOf(st2.Orders[2].Bid) < q {
			t.Fatalf("changed again: %+v", st.Orders)
		}
	}
	// orders already taken are not touched, and with none left to take nothing happens
	st := mk()
	for i := range st.Orders {
		st.Orders[i].Status = voyageUnderway
	}
	before := append([]voyageOrder(nil), st.Orders...)
	st.ensureQuality(voyageOrange, zero)
	for i := range before {
		if st.Orders[i].Bid != before[i].Bid {
			t.Fatalf("a taken order was swapped: %+v", st.Orders)
		}
	}
}

// A refresh with the purple privilege (103) always brings a purple order, however the deal fell; the count of the day follows.
func TestVoyagePrivilegeRefresh(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("vp", "刷新人二")
	p := &privilegeState{}
	p.set(voyagePrivilegePurple, privilegeForever)
	if err := e.st.SaveState(role.RID, privilegeModule, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		st := &voyageState{}
		if _, err := e.st.LoadState(role.RID, voyageModule, st); err != nil {
			t.Fatal(err)
		}
		st.Day = 0 // a new day: the free refresh and the count start again
		if err := e.st.SaveState(role.RID, voyageModule, st); err != nil {
			t.Fatal(err)
		}
		c.send(23800, proto.Message{})
		c.waitFor(23800)
		c.send(23804, proto.Message{})
		if r := c.waitFor(23804); r.Int("flag") != 1 {
			t.Fatalf("refresh %d: %v", i, r)
		}
		c.waitFor(23800)
		c.send(23800, proto.Message{}) // the refresh answers before it saves: this one is handled after the save
		c.waitFor(23800)
		saved := &voyageState{}
		if _, err := e.st.LoadState(role.RID, voyageModule, saved); err != nil {
			t.Fatal(err)
		}
		purple := false
		for _, o := range saved.Orders {
			if ci, ok := voyageCfg.order(o.Bid); ok && o.Status == voyageUnget && voyageCfg.Orders[ci].Quality >= voyagePurple {
				purple = true
			}
		}
		if !purple || saved.Refreshes != 1 {
			t.Fatalf("round %d: purple %v, refreshes %d", i, purple, saved.Refreshes)
		}
	}
}
