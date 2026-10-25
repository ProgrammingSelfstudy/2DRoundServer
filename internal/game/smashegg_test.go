package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func eggEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("eg", "砸蛋人")
	return e, c, role
}

func (e *env) eggGrant(t *testing.T, role *store.Role, assets, items map[int64]int64) {
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
		t.Fatal(err)
	}
}

func TestSmasheggInfoAndSmash(t *testing.T) {
	e, c, role := eggEnv(t)
	c.send(16680, proto.Message{})
	b := c.waitFor(16680)
	eggs := b.Arr("egg_status")
	if len(eggs) != 6 || b.Int("free_num") != 1 || b.Int("lucky") != 0 || b.Int("endtime") <= time.Now().Unix() {
		t.Fatalf("16680: %v", b)
	}
	c.send(16682, proto.Message{"pos": 1})
	if r := c.waitFor(16682); r.Int("code") != 0 {
		t.Fatalf("no hammer: %v", r)
	}
	e.eggGrant(t, role, nil, map[int64]int64{37004: 2})
	c.send(16682, proto.Message{"pos": 1})
	if r := c.waitFor(16682); r.Int("code") != 1 {
		t.Fatalf("smash: %v", r)
	}
	b = c.waitFor(16680)
	got := b.Arr("egg_status")[0]
	if got.Int("status") != 1 || len(got.Arr("show_reward")) != 1 || b.Int("lucky") != 1 {
		t.Fatalf("after smash: %v", b)
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[37004] != 1 {
		t.Fatalf("hammers: %v", items)
	}
	c.send(16682, proto.Message{"pos": 1})
	if r := c.waitFor(16682); r.Int("code") != 0 {
		t.Fatalf("smashed twice: %v", r)
	}
	c.send(16685, proto.Message{"type": 1})
	if r := c.waitFor(16685); len(r.Arr("log_list")) != 1 || r.Arr("log_list")[0].Str("role_name") != "砸蛋人" {
		t.Fatalf("own log: %v", r)
	}
	c.send(16685, proto.Message{"type": 2})
	if r := c.waitFor(16685); len(r.Arr("log_list")) != 1 {
		t.Fatalf("server log: %v", r)
	}
}

func TestSmasheggAllAndAutoRefresh(t *testing.T) {
	e, c, role := eggEnv(t)
	e.eggGrant(t, role, nil, map[int64]int64{37004: 5})
	c.send(16683, proto.Message{})
	if r := c.waitFor(16683); r.Int("code") != 0 {
		t.Fatalf("five hammers for six eggs: %v", r)
	}
	e.eggGrant(t, role, nil, map[int64]int64{37004: 1})
	c.send(16683, proto.Message{})
	if r := c.waitFor(16683); r.Int("code") != 1 {
		t.Fatalf("smash all: %v", r)
	}
	b := c.waitFor(16680)
	for _, eg := range b.Arr("egg_status") {
		if eg.Int("status") != 1 {
			t.Fatalf("not all smashed: %v", b)
		}
	}
	b = c.waitFor(16680) // the next six come at once
	for _, eg := range b.Arr("egg_status") {
		if eg.Int("status") != 0 {
			t.Fatalf("no new eggs: %v", b)
		}
	}
	if b.Int("free_num") != 1 {
		t.Fatalf("the automatic refresh costs the free one: %v", b)
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[37004] != 0 {
		t.Fatalf("hammers: %v", items)
	}
}

func TestSmasheggRefreshAndBuy(t *testing.T) {
	e, c, role := eggEnv(t)
	c.send(16681, proto.Message{})
	if r := c.waitFor(16681); r.Int("code") != 1 {
		t.Fatalf("free refresh: %v", r)
	}
	if b := c.waitFor(16680); b.Int("free_num") != 0 {
		t.Fatalf("free left: %v", b)
	}
	c.send(16681, proto.Message{})
	if r := c.waitFor(16681); r.Int("code") != 0 {
		t.Fatalf("paid refresh without diamonds: %v", r)
	}
	c.send(16684, proto.Message{"num": 3})
	if r := c.waitFor(16684); r.Int("code") != 0 {
		t.Fatalf("buy without diamonds: %v", r)
	}
	e.eggGrant(t, role, map[int64]int64{3: 1000}, nil)
	before, _ := e.st.RoleAssets(role.RID)
	c.send(16684, proto.Message{"num": 3})
	if r := c.waitFor(16684); r.Int("code") != 1 {
		t.Fatalf("buy: %v", r)
	}
	items, _ := e.st.RoleItems(role.RID)
	assets, _ := e.st.RoleAssets(role.RID)
	// the experience that comes with the hammers levels the role up, which pays some diamonds back, so only the drop is checked
	if items[37004] != 3 || assets[3] >= before[3] {
		t.Fatalf("bought: items %v, diamonds %v -> %v", items, before[3], assets[3])
	}
}

func TestSmasheggBestEgg(t *testing.T) {
	st := &eggState{Lucky: 8}
	st.deal(func(n int) int { return 0 })
	if !st.Eggs[0].Best || st.Lucky != 0 {
		t.Fatalf("full luck must give a best egg: %+v", st)
	}
	st = &eggState{}
	st.deal(func(n int) int { return 0 })
	for _, eg := range st.Eggs {
		if eg.Best {
			t.Fatalf("no luck, no best egg: %+v", st)
		}
	}
	st.Eggs[0].Best = true
	st.open(0, func(n int) int { return 0 })
	if st.Eggs[0].Prize != [2]int64{99956, 1} || st.Lucky != 1 {
		t.Fatalf("best prize: %+v", st)
	}
}
