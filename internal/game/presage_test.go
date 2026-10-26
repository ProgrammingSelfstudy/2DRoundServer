package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func presageEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ps", "松果人")
	return e, c, role
}

func (e *env) presageState(t *testing.T, role *store.Role) *presageState {
	st := &presageState{}
	if _, err := e.st.LoadState(role.RID, presageModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestPresageDealRules(t *testing.T) {
	n := newRng().Intn
	for i := 0; i < 200; i++ {
		for kind := 1; kind <= 2; kind++ {
			ids := presageDeal(kind, int64(i), n)
			seen := map[int64]bool{}
			for _, id := range ids {
				if _, ok := presageCfg.byID[id]; !ok || seen[id] {
					t.Fatalf("kind %d deal %v", kind, ids)
				}
				seen[id] = true
			}
			if len(ids) != 3 {
				t.Fatalf("kind %d deal %v", kind, ids)
			}
		}
	}
	for i := 0; i < 100; i++ {
		goals := 0
		for _, id := range presageDeal(2, presageCfg.mustLuxury-1, n) {
			goals += presageCfg.byID[id].Goal
		}
		if goals < 2 {
			t.Fatalf("pity result with %d best prizes", goals)
		}
	}
}

func TestPresageOpenRefreshBuy(t *testing.T) {
	e, c, role := presageEnv(t)
	c.send(16690, proto.Message{})
	b := c.waitFor(16690)
	data := b.Arr("predict_data")
	if len(data) != 2 || len(data[0].Arr("rand_lists")) != 3 || b.Int("status_1") != 0 || b.Int("time") <= 0 {
		t.Fatalf("16690: %v", b)
	}
	c.send(16691, proto.Message{"type": 1})
	if r := c.waitFor(16691); r.Int("code") != 0 {
		t.Fatalf("refresh before open: %v", r)
	}
	c.send(16694, proto.Message{"type": 1})
	if r := c.waitFor(16694); r.Int("code") != 1 || r.Int("type") != 1 {
		t.Fatalf("open: %v", r)
	}
	c.send(16694, proto.Message{"type": 1})
	if r := c.waitFor(16694); r.Int("code") != 0 {
		t.Fatalf("opened twice: %v", r)
	}
	// two free refreshes, then the third costs a pine coin or diamonds
	for i := int64(1); i <= 2; i++ {
		c.send(16691, proto.Message{"type": 1})
		r := c.waitFor(16691)
		if r.Int("code") != 1 || r.Int("ref_count") != i || r.Int("count") != i || len(r.Arr("rand_lists")) != 3 {
			t.Fatalf("free refresh %d: %v", i, r)
		}
	}
	gold := func() int64 {
		a, _ := e.st.RoleAssets(role.RID)
		return a[presageGold]
	}
	before := gold()
	e.addItems(t, role, map[int64]int64{presageCfg.coin: 1})
	c.send(16691, proto.Message{"type": 1})
	if r := c.waitFor(16691); r.Int("code") != 1 || r.Int("ref_count") != 3 {
		t.Fatalf("coin refresh: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[presageCfg.coin] != 0 || gold() != before {
		t.Fatalf("coin refresh spent %v gold %d -> %d", items, before, gold())
	}
	c.send(16691, proto.Message{"type": 1})
	if r := c.waitFor(16691); r.Int("code") != 0 {
		t.Fatalf("refresh without coins or diamonds: %v", r)
	}

	// buying the shown three: coins first, the rest in diamonds
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{presageGold: 1000000}, map[int64]int64{presageCfg.coin: 5}); err != nil {
		t.Fatal(err)
	}
	st := e.presageState(t, role)
	shown := st.Kinds[1].Rand
	have, _ := e.st.RoleItems(role.RID)
	c.send(16693, proto.Message{"type": 1})
	r := c.waitFor(16693)
	if r.Int("code") != 1 || r.Int("flag") != 1 || r.Int("ref_count") != 1 || len(r.Arr("rand_lists")) != 3 {
		t.Fatalf("buy: %v", r)
	}
	after, _ := e.st.RoleItems(role.RID)
	if after[presageCfg.coin] != 0 {
		t.Fatalf("coins left %v", after)
	}
	wantGold := int64(1000000) + before - (presageCfg.Prize["1"].Gold[1] - 5*presageCfg.price)
	if got := gold(); got != wantGold {
		t.Fatalf("gold %d, want %d (shown %v, had %v)", got, wantGold, shown, have)
	}
	c.send(16693, proto.Message{"type": 1})
	if r := c.waitFor(16693); r.Int("code") != 0 {
		t.Fatalf("bought twice: %v", r)
	}
}

func TestPresageBuyCoinsAndDayReset(t *testing.T) {
	e, c, role := presageEnv(t)
	c.send(16692, proto.Message{"num": 1})
	if r := c.waitFor(16692); r.Int("code") != 0 {
		t.Fatalf("no diamonds: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{presageGold: 5000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	c.send(16692, proto.Message{"num": 2})
	if r := c.waitFor(16692); r.Int("code") != 1 {
		t.Fatalf("buy coins: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[presageCfg.coin] != 2 {
		t.Fatalf("coins %v", items)
	}
	c.send(16694, proto.Message{"type": 2})
	c.waitFor(16694)
	c.send(16691, proto.Message{"type": 2})
	c.waitFor(16691)
	st := e.presageState(t, role)
	st.Day-- // yesterday's treasure
	if err := e.st.SaveState(role.RID, presageModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(16690, proto.Message{})
	b := c.waitFor(16690)
	if b.Int("ref_2") != 0 || b.Int("status_2") != 0 {
		t.Fatalf("the day did not reset: %v", b)
	}
	if got := e.presageState(t, role).Kinds[2].Count; got != 1 {
		t.Fatalf("pity count lost on reset: %d", got)
	}
}
