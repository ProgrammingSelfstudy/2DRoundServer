package game

import (
	"errors"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestChargeTiersAndOrders(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ch1", "充值人")
	c.send(16700, proto.Message{})
	r := c.waitFor(16700)
	list := r.Arr("list")
	if len(list) != 7 || list[0].Int("id") != 3 || list[0].Int("need_rmb") != 600 || list[0].Int("is_first") != 1 || list[0].Int("add_gold") != 60 {
		t.Fatalf("16700: %v", r) // the original showed 3, 4, 6, 7, 8, 20, 21 with the first purchase's extra (capture 2026-10-21b)
	}
	// products
	c.send(21016, proto.Message{"charge_id": 4})
	if r := c.waitFor(21016); r.Int("status") != 1 {
		t.Fatalf("21016 payable: %v", r)
	}
	c.send(21016, proto.Message{"charge_id": 101}) // a fund: nothing delivers it yet
	if r := c.waitFor(21016); r.Int("status") != 0 {
		t.Fatalf("21016 not payable: %v", r)
	}
	c.send(10947, proto.Message{"charge_id": 101, "notify_url": ""})
	if r := c.waitFor(10947); r.Int("code") != 0 {
		t.Fatalf("10947 not payable: %v", r)
	}
	c.send(10947, proto.Message{"charge_id": 4, "notify_url": ""})
	r = c.waitFor(10947)
	if r.Int("code") != 1 || !strings.HasPrefix(r.Str("url"), "http://pay.test/pay/order/P") {
		t.Fatalf("10947: %v", r)
	}
	no := strings.TrimPrefix(r.Str("url"), "http://pay.test/pay/order/")
	o, err := e.st.PayOrderByNo(no)
	if err != nil || o.RID != role.RID || o.ChargeID != 4 || o.AmountCents != 3000 || o.Status != store.PayWaiting {
		t.Fatalf("the order: %v %+v", err, o)
	}
	// not delivered before it is confirmed
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 0 {
		t.Fatalf("diamonds before the payment: %v", a)
	}
	o, err = ConfirmChargeOrder(e.st, no, "tester", "wx 30")
	if err != nil || o.Status != store.PayDone {
		t.Fatalf("confirm: %v %+v", err, o)
	}
	// the first purchase of the tier gives the first extra: 300 + 300
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 600 {
		t.Fatalf("diamonds after the first purchase: %v", a)
	}
	if _, err := ConfirmChargeOrder(e.st, no, "tester", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a second confirm: %v", err)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 600 {
		t.Fatalf("delivered twice: %v", a)
	}
	rc, _ := e.st.RoleCharge(role.RID)
	if rc.TotalCents != 3000 || rc.VIPExp != 300 || rc.Bought["4"] != 1 {
		t.Fatalf("totals: %+v", rc)
	}
	// the second purchase gives the later extra: 300 + 58
	o2, err := CreateChargeOrder(e.st, role, 4, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o2.OrderNo, "tester", ""); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 600+358 {
		t.Fatalf("diamonds after the second purchase: %v", a)
	}
	// the page marks the tier as no longer first, and the VIP experience shows in 10301
	c.send(16700, proto.Message{})
	if r := c.waitFor(16700); r.Arr("list")[1].Int("is_first") != 0 || r.Arr("list")[1].Int("add_gold") != 58 {
		t.Fatalf("16700 after: %v", r)
	}
	if err := SendChargeState(e.gw.Hub.Online(role.RID), e.st, role.RID); err != nil {
		t.Fatal(err)
	}
	if b := c.waitFor(10301); b.Int("vip_exp") != 600 {
		t.Fatalf("10301: %v", b)
	}
	// 600 experience reaches the level the VIP table starts at 600
	if r, _ := e.st.RoleByID(role.RID); r.VIPLev != vipLevelFor(600) || r.VIPLev == 0 {
		t.Fatalf("vip level %d for 600", r.VIPLev)
	}
	// at most five orders wait
	for i := 0; i < maxOpenOrders; i++ {
		if _, err := CreateChargeOrder(e.st, role, 3, actNow); err != nil {
			t.Fatalf("order %d: %v", i, err)
		}
	}
	if _, err := CreateChargeOrder(e.st, role, 3, actNow); !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("a sixth order: %v", err)
	}
}

func TestAccumulatedTopUp(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ca1", "累充人")
	c.send(16712, proto.Message{})
	r := c.waitFor(16712)
	if r.Int("charge_sum") != 0 || len(r.Arr("list")) != 10 || r.Arr("list")[0].Int("id") != 10 || r.Arr("list")[0].Int("status") != 0 {
		t.Fatalf("16712 before: %v", r)
	}
	o, err := CreateChargeOrder(e.st, role, 4, actNow) // 30 yuan: 300 experience
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	c.send(16712, proto.Message{})
	r = c.waitFor(16712)
	status := map[int64]int64{}
	for _, x := range r.Arr("list") {
		status[x.Int("id")] = x.Int("status")
	}
	if r.Int("charge_sum") != 300 || status[1] != 1 || status[2] != 0 {
		t.Fatalf("16712 after: %v", r)
	}
	c.send(16713, proto.Message{"id": 2})
	if r := c.waitFor(16713); r.Int("code") != 0 {
		t.Fatalf("a reward not reached: %v", r)
	}
	coins, _ := e.st.RoleAssets(role.RID)
	c.send(16713, proto.Message{"id": 1})
	r = c.waitFor(16713)
	if r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] != coins[1]+1000 {
		t.Fatalf("coins %d -> %d", coins[1], after[1])
	}
	for _, x := range r.Arr("list") {
		if x.Int("id") == 1 && x.Int("status") != 2 {
			t.Fatalf("not marked taken: %v", r)
		}
	}
	c.send(16713, proto.Message{"id": 1})
	if r := c.waitFor(16713); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
}
