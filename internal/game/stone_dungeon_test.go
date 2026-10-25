package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func stoneEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ua", "挑战者")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10503, Star: 5}, {BID: 10509, Star: 5}, {BID: 20511, Star: 5}, {BID: 30506, Star: 5}}); err != nil || !ok {
		t.Fatalf("heroes: %v %v", ok, err)
	}
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	return e, c, role
}

func TestStoneDungeonState(t *testing.T) {
	e, c, role := stoneEnv(t)
	c.send(13030, proto.Message{})
	r := c.waitFor(13030)
	if len(r.Arr("list")) != 5 || r.Arr("list")[0].Int("type") != 1 || r.Arr("list")[1].Int("type") != 4 || len(r.Arr("pass_list")) != 0 {
		t.Fatalf("13030: %v", r)
	}
	if _, err := e.st.MarkStoneCleared(role.RID, 101); err != nil {
		t.Fatal(err)
	}
	c.send(13030, proto.Message{})
	if r := c.waitFor(13030); len(r.Arr("pass_list")) != 1 || r.Arr("pass_list")[0].Int("id") != 101 {
		t.Fatalf("pass list: %v", r)
	}
}

func TestStoneDungeonRules(t *testing.T) {
	e, c, role := stoneEnv(t)
	c.send(13031, proto.Message{"id": 999})
	if r := c.waitFor(13031); r.Int("code") != 0 {
		t.Fatalf("unknown dungeon: %v", r)
	}
	c.send(13031, proto.Message{"id": 102}) // the one before is not cleared
	if r := c.waitFor(13031); r.Int("code") != 0 {
		t.Fatalf("locked: %v", r)
	}
	c.send(13031, proto.Message{"id": 501}) // a rune dungeon, level 70
	if r := c.waitFor(13031); r.Int("code") != 0 {
		t.Fatalf("level gate: %v", r)
	}
	c.send(13032, proto.Message{"id": 101}) // nothing cleared yet
	if r := c.waitFor(13032); r.Int("code") != 0 {
		t.Fatalf("sweep before the first win: %v", r)
	}
	// two free attempts, then a third needs VIP and gems
	if _, err := e.st.MarkStoneCleared(role.RID, 101); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		c.send(13032, proto.Message{"id": 101})
		if r := c.waitFor(13032); r.Int("code") != 1 || r.Int("id") != 101 {
			t.Fatalf("sweep %d: %v", i+1, r)
		}
	}
	items, _ := e.st.RoleAssets(role.RID)
	if items[1] != 200000 { // two sweeps of 100000 coin
		t.Fatalf("coin after two sweeps: %v", items)
	}
	c.send(13032, proto.Message{"id": 101})
	if r := c.waitFor(13032); r.Int("code") != 0 {
		t.Fatalf("third attempt without VIP: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 1); err != nil {
		t.Fatal(err)
	}
	c.send(13032, proto.Message{"id": 101})
	if r := c.waitFor(13032); r.Int("code") != 0 {
		t.Fatalf("third attempt without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 60})
	c.send(13032, proto.Message{"id": 101})
	if r := c.waitFor(13032); r.Int("code") != 1 {
		t.Fatalf("third attempt: %v", r)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 10 {
		t.Fatalf("gems after paying 50: %v", a)
	}
	c.send(13030, proto.Message{})
	var state proto.Message
	for state == nil || state.Arr("list")[0].Int("day_num") != 3 {
		state = c.waitFor(13030)
	}
	// the day turns at 5:00: free attempts again
	c.settle()
	e.advance(24 * time.Hour)
	c.send(13030, proto.Message{})
	if r := c.waitFor(13030); r.Arr("list")[0].Int("day_num") != 0 {
		t.Fatalf("next day: %v", r)
	}
}

func TestStoneDungeonFight(t *testing.T) {
	e, c, role := stoneEnv(t)
	c.send(13031, proto.Message{"id": 101})
	if r := c.waitFor(13031); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatStoneDungeon {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	if res.Int("combat_type") != combatStoneDungeon || (res.Int("result") != 1 && res.Int("result") != 2) {
		t.Fatalf("result: %v", res)
	}
	cleared, _ := e.st.StoneCleared(role.RID)
	if res.Int("result") == 1 {
		if !cleared[101] || len(res.Arr("item_rewards")) != 2 {
			t.Fatalf("a win: %v %v", res, cleared)
		}
	} else if cleared[101] {
		t.Fatalf("a loss cleared it")
	}
	counts, _ := e.st.StoneCounts(role.RID, summonDay(time.Unix(0, e.clk.Load())))
	if counts[1] != 1 {
		t.Fatalf("attempts: %v", counts)
	}
}
