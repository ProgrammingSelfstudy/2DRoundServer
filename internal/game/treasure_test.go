package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func checkTreasureDraw(t *testing.T, board, reply proto.Message, count int) {
	t.Helper()
	points, prizes, positions := reply.Arr("awards1"), reply.Arr("awards2"), reply.Arr("awards3")
	if reply.Int("code") != 1 || len(points) != 1 || points[0].Int("bid") != 18 || points[0].Int("num") != int64(10*count) || len(prizes) != count || len(positions) != count {
		t.Fatalf("draw %d: %v", count, reply)
	}
	slots := map[int64]trSlot{}
	for _, sl := range board.Arr("rand_lists") {
		p := sl.Int("pos")
		slots[p] = trSlot{Pos: p, ID: sl.Int("id"), Status: sl.Int("status")}
	}
	for i, hit := range positions {
		pos := hit.Int("pos")
		sl := slots[pos]
		item := trItems[sl.ID]
		if sl.ID == 0 || sl.Status != 0 || prizes[i].Int("bid") != item.Item || prizes[i].Int("num") != item.Num {
			t.Fatalf("pull %d: slot %v, prize %v", i, sl, prizes[i])
		}
		if pos%2 == 1 {
			sl.Status = 1
			slots[pos] = sl
		}
	}
}

func TestTreasureHunt(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("tr1", "探宝人")
	c.send(16637, proto.Message{})
	r := c.waitFor(16637)
	dd := r.Arr("dial_data")
	if len(dd) != 2 || len(dd[0].Arr("rand_lists")) != 8 || dd[0].Int("type") != 1 || dd[1].Int("type") != 2 {
		t.Fatalf("init: %v", r)
	}
	c.send(16638, proto.Message{"type": 1, "type2": 1})
	if r := c.waitFor(16638); r.Int("code") != 0 {
		t.Fatalf("level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	c.send(16638, proto.Message{"type": 1, "type2": 1})
	if r := c.waitFor(16638); r.Int("code") != 0 {
		t.Fatalf("without the item: %v", r)
	}
	e.addItems(t, role, map[int64]int64{37001: 30})
	c.send(16638, proto.Message{"type": 1, "type2": 1})
	res := c.waitFor(16638)
	checkTreasureDraw(t, dd[0], res, 1)
	st := c.waitFor(16639)
	if l := c.waitFor(16641); l.Int("type") != 1 || len(l.Arr("log_list")) != 1 {
		t.Fatalf("log push: %v", l)
	}
	taken := 0
	for _, sl := range st.Arr("rand_lists") {
		taken += int(sl.Int("status"))
	}
	if st.Int("type") != 1 || st.Int("count") != 1 || st.Int("lucky") != 10 || taken != int(res.Arr("awards3")[0].Int("pos")%2) {
		t.Fatalf("after one draw: %v", st)
	}
	// the multi draw needs VIP 2
	c.send(16643, proto.Message{"type": 1, "type2": 2})
	if r := c.waitFor(16643); r.Int("code") != 0 {
		t.Fatalf("multi without vip: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 2); err != nil {
		t.Fatal(err)
	}
	c.send(16643, proto.Message{"type": 1, "type2": 2})
	res = c.waitFor(16643)
	checkTreasureDraw(t, st, res, 15)
	st = c.waitFor(16639)
	c.waitFor(16641)
	if st.Int("count") != 16 || st.Int("lucky") != 160 {
		t.Fatalf("after the multi draw: %v", st)
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[37001] != 30-1-12 {
		t.Fatalf("the draw items: %v", items)
	}
	// lucky gifts: 100 is reached, 300 is not
	c.send(16640, proto.Message{"type": 1, "id": 2})
	if r := c.waitFor(16640); r.Int("code") != 0 {
		t.Fatalf("lucky not reached: %v", r)
	}
	c.send(16640, proto.Message{"type": 1, "id": 1})
	if r := c.waitFor(16640); r.Int("code") != 1 {
		t.Fatalf("lucky gift: %v", r)
	}
	if st = c.waitFor(16639); len(st.Arr("lucky_award")) != 1 {
		t.Fatalf("claimed: %v", st)
	}
	c.send(16640, proto.Message{"type": 1, "id": 1})
	if r := c.waitFor(16640); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	// a free refresh once, then 30 diamonds
	c.send(16642, proto.Message{"type": 1})
	if r := c.waitFor(16642); r.Int("code") != 1 {
		t.Fatalf("free refresh: %v", r)
	}
	st = c.waitFor(16639)
	if st.Int("end_time") != actNow.Unix()+trCfg.FreeTime {
		t.Fatalf("next free refresh: %v", st)
	}
	for _, sl := range st.Arr("rand_lists") {
		if sl.Int("status") != 0 {
			t.Fatalf("the wheel was not renewed: %v", st)
		}
	}
	c.send(16642, proto.Message{"type": 1})
	if r := c.waitFor(16642); r.Int("code") != 0 {
		t.Fatalf("paid refresh without diamonds: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(16642, proto.Message{"type": 1})
	if r := c.waitFor(16642); r.Int("code") != 1 {
		t.Fatalf("paid refresh: %v", r)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 70 {
		t.Fatalf("diamonds: %v", a)
	}
	c.waitFor(16639)
	c.send(16637, proto.Message{})
	if l := c.waitFor(16637).Arr("dial_data")[0].Arr("log_list"); len(l) != 2 || l[0].Str("role_name") != "探宝人" {
		t.Fatalf("log: %v", l)
	}
	// Advanced draws use their own tickets, count, gifts and refresh clock.
	e.addItems(t, role, map[int64]int64{37002: 11})
	c.send(16638, proto.Message{"type": 2, "type2": 1})
	checkTreasureDraw(t, dd[1], c.waitFor(16638), 1)
	advanced := c.waitFor(16639)
	c.waitFor(16641)
	c.send(16638, proto.Message{"type": 2, "type2": 2})
	checkTreasureDraw(t, advanced, c.waitFor(16638), 10)
	advanced = c.waitFor(16639)
	c.waitFor(16641)
	if advanced.Int("lucky") != 110 || advanced.Int("count") != 11 || advanced.Int("end_time") != 0 {
		t.Fatalf("advanced: %v", advanced)
	}
	items, _ = e.st.RoleItems(role.RID)
	before := items[37001]
	c.send(16640, proto.Message{"type": 2, "id": 6})
	if resp := c.waitFor(16640); resp.Int("code") != 1 {
		t.Fatal(resp)
	}
	c.waitFor(16639)
	items, _ = e.st.RoleItems(role.RID)
	if items[37002] != 0 || items[37001] != before+10 {
		t.Fatalf("advanced tickets and gift: %v", items)
	}

}

func TestTreasureSettlementRollback(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	_, role := e.enter("tr_tx", "事务探宝")
	e.addItems(t, role, map[int64]int64{37001: 12})
	before, err := e.st.RoleAssets(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("unable to save treasure log")
	err = e.st.ExchangeAndSave(role.RID, nil, map[int64]int64{37001: 12}, map[int64]int64{18: 150}, nil, func(tx *store.Store) error {
		if err := tx.SaveState(role.RID, trModule, &trRole{Wheels: [2]trWheel{{Count: 15, Lucky: 150}, {}}}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	items, err := e.st.RoleItems(role.RID)
	if err != nil || items[37001] != 12 {
		t.Fatalf("tickets after rollback: %v, %v", items, err)
	}
	after, err := e.st.RoleAssets(role.RID)
	if err != nil || before[18] != after[18] {
		t.Fatalf("points after rollback: %v, %v", after, err)
	}
	if exists, err := e.st.LoadState(role.RID, trModule, &trRole{}); err != nil || exists {
		t.Fatalf("wheel after rollback: %v, %v", exists, err)
	}
}
