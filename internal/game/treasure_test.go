package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

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
	if res.Int("code") != 1 || len(res.Arr("awards1")) != 1 || len(res.Arr("awards3")) != 1 {
		t.Fatalf("draw: %v", res)
	}
	st := c.waitFor(16639)
	if l := c.waitFor(16641); l.Int("type") != 1 || len(l.Arr("log_list")) != 1 {
		t.Fatalf("log push: %v", l)
	}
	taken := 0
	for _, sl := range st.Arr("rand_lists") {
		taken += int(sl.Int("status"))
	}
	if st.Int("type") != 1 || st.Int("count") != 1 || st.Int("lucky") != 10 || taken != 1 {
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
	if res.Int("code") != 1 {
		t.Fatalf("multi: %v", res)
	}
	var n int64
	for _, a := range res.Arr("awards2") {
		if a.Int("bid") != 18 {
			n += a.Int("num")
		}
	}
	if n != 14 {
		t.Fatalf("a multi draw gives 14 pulls beside the wheel: %v", res)
	}
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
}
