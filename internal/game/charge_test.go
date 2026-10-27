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

func TestMonthCards(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("mc1", "月卡人")
	c.send(16705, proto.Message{})
	r := c.waitFor(16705)
	if r.Int("card2_is_reward") != 0 || r.Int("card2_acc") != 0 || r.Int("card1_is_reward") != 0 {
		t.Fatalf("before: %v", r)
	}
	c.send(16706, proto.Message{"card_type": 2})
	if r := c.waitFor(16706); r.Int("code") != 0 {
		t.Fatalf("take without a card: %v", r)
	}
	pay := func(id int64) {
		t.Helper()
		o, err := CreateChargeOrder(e.st, role, id, actNow)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
			t.Fatal(err)
		}
	}
	pay(3) // 6 yuan
	c.send(16705, proto.Message{})
	if r := c.waitFor(16705); r.Int("card2_acc") != 6 || r.Int("card2_is_reward") != 0 {
		t.Fatalf("6 yuan: %v", r)
	}
	pay(4) // 30 yuan more: 36, the card of 30 is on
	c.send(16705, proto.Message{})
	r = c.waitFor(16705)
	if r.Int("card2_is_reward") != 1 || r.Int("card1_is_reward") != 0 || r.Int("card2_end_time") != dayStart(actNow).Unix()+30*86400 {
		t.Fatalf("the card is on: %v", r)
	}
	c.send(16707, proto.Message{})
	if r := c.waitFor(16707); r.Int("status") != 1 {
		t.Fatalf("16707: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(16706, proto.Message{"card_type": 2})
	if r := c.waitFor(16706); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if w := c.waitFor(16705); w.Int("card2_is_reward") != 2 || w.Int("card2_days") != 1 {
		t.Fatalf("after the take: %v", w)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+120 {
		t.Fatalf("diamonds %d -> %d", before[3], after[3])
	}
	c.send(16706, proto.Message{"card_type": 2})
	if r := c.waitFor(16706); r.Int("code") != 0 {
		t.Fatalf("twice a day: %v", r)
	}
	// the next day again; after 30 days the run is over and the paid sum counts from the activation
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(16708, proto.Message{})
	if r := c.waitFor(16708); r.Int("code") != 1 {
		t.Fatalf("16708 the next day: %v", r)
	}
	c.waitFor(16705)
	e.setNow(actNow.Add(31 * 24 * time.Hour))
	c.send(16705, proto.Message{})
	if r := c.waitFor(16705); r.Int("card2_is_reward") != 0 || r.Int("card2_acc") != 0 {
		t.Fatalf("after the run: %v", r)
	}
}

func TestFirstChargeGifts(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("fc1", "首充人")
	status := func() map[int64]int64 {
		t.Helper()
		c.send(21000, proto.Message{})
		out := map[int64]int64{}
		for _, g := range c.waitFor(21000).Arr("first_gift") {
			out[g.Int("id")] = g.Int("status")
		}
		return out
	}
	if st := status(); len(st) != 6 || st[1] != 0 || st[4] != 0 {
		t.Fatalf("before: %v", st)
	}
	c.send(21001, proto.Message{"id": 1})
	if r := c.waitFor(21001); r.Int("code") != 0 {
		t.Fatalf("take before paying: %v", r)
	}
	o, err := CreateChargeOrder(e.st, role, 3, actNow) // 6 yuan: 60 VIP experience, the first tier
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	if st := status(); st[1] != 1 || st[2] != 0 || st[4] != 0 {
		t.Fatalf("paid 6: %v", st)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(21001, proto.Message{"id": 2}) // the second day's gift waits for its day
	if r := c.waitFor(21001); r.Int("code") != 0 {
		t.Fatalf("day 2 on day 1: %v", r)
	}
	c.send(21001, proto.Message{"id": 1})
	if r := c.waitFor(21001); r.Int("code") != 1 {
		t.Fatalf("take 1: %v", r)
	}
	if w := c.waitFor(21000); w.Arr("first_gift")[0].Int("status") != 2 {
		t.Fatalf("after the take: %v", w)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3] {
		t.Fatalf("the gift is items, diamonds %d -> %d", before[3], after[3])
	}
	c.send(21001, proto.Message{"id": 1})
	if r := c.waitFor(21001); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	if st := status(); st[2] != 0 {
		t.Fatalf("same day: %v", st)
	}
	e.setNow(actNow.Add(24 * time.Hour))
	if st := status(); st[1] != 2 || st[2] != 1 || st[3] != 0 {
		t.Fatalf("day 2: %v", st)
	}
	// a late take keeps the order: day 3 only after day 2, even when the days have passed
	e.setNow(actNow.Add(5 * 24 * time.Hour))
	c.send(21001, proto.Message{"id": 3})
	if r := c.waitFor(21001); r.Int("code") != 0 {
		t.Fatalf("day 3 before day 2: %v", r)
	}
}

func TestFirstChargeWindows(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("fc2", "首充二")
	c.send(21012, proto.Message{})
	if r := c.waitFor(21012); r.Int("choosen_status") != 0 || len(r.Arr("first_gift")) != 6 {
		t.Fatalf("21012 before: %v", r)
	}
	c.send(21014, proto.Message{"id": 1})
	if r := c.waitFor(21014); r.Int("code") != 0 {
		t.Fatalf("choose before paying: %v", r)
	}
	o, err := CreateChargeOrder(e.st, role, 3, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	c.send(21012, proto.Message{})
	if r := c.waitFor(21012); r.Int("choosen_status") != 1 || r.Arr("first_gift")[0].Int("status") != 1 {
		t.Fatalf("21012 paid: %v", r)
	}
	c.send(21014, proto.Message{"id": 2})
	if r := c.waitFor(21014); r.Int("code") != 1 {
		t.Fatalf("choose: %v", r)
	}
	c.waitFor(11001)
	if r := c.waitFor(21012); r.Int("choosen_status") != 2 || r.Int("has_choosen_id") != 2 {
		t.Fatalf("after the choice: %v", r)
	}
	ps, _ := e.st.PartnersOf(role.RID)
	found := false
	for _, p := range ps {
		found = found || p.BID == 30506
	}
	if !found {
		t.Fatalf("the hero was not given: %v", ps)
	}
	c.send(21014, proto.Message{"id": 1})
	if r := c.waitFor(21014); r.Int("code") != 0 {
		t.Fatalf("a second choice: %v", r)
	}
	c.send(21013, proto.Message{"id": 1})
	if r := c.waitFor(21013); r.Int("code") != 1 {
		t.Fatalf("21013: %v", r)
	}
	c.send(21030, proto.Message{})
	if r := c.waitFor(21030); r.Arr("first_gift")[0].Int("status") != 1 {
		t.Fatalf("21030: %v", r)
	}
	c.send(21032, proto.Message{}) // 10000 experience asked: 6 yuan is not enough
	if r := c.waitFor(21032); r.Arr("first_gift")[0].Int("status") != 0 {
		t.Fatalf("21032: %v", r)
	}
}

func TestCycleGifts(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	old := chargeClock
	chargeClock = func() time.Time { return actNow }
	t.Cleanup(func() { chargeClock = old })
	c, role := e.enter("cg1", "礼包人")
	c.send(21007, proto.Message{"type": 1})
	r := c.waitFor(21007)
	if len(r.Arr("first_gift")) != 5 || r.Arr("first_gift")[0].Int("id") != 401 || r.Arr("first_gift")[0].Int("count") != 0 || r.Int("ref_time") != leWeekEnd(actNow) {
		t.Fatalf("21007 weekly: %v", r)
	}
	c.send(21007, proto.Message{"type": 2})
	if r := c.waitFor(21007); len(r.Arr("first_gift")) != 7 {
		t.Fatalf("21007 monthly: %v", r)
	}
	c.send(21016, proto.Message{"charge_id": 401})
	if r := c.waitFor(21016); r.Int("status") != 1 {
		t.Fatalf("21016: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	buy := func() error {
		o, err := CreateChargeOrder(e.st, role, 401, actNow)
		if err != nil {
			return err
		}
		_, err = ConfirmChargeOrder(e.st, o.OrderNo, "t", "")
		return err
	}
	if err := buy(); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+300 {
		t.Fatalf("diamonds %d -> %d", before[3], after[3])
	}
	c.send(21007, proto.Message{"type": 1})
	if r := c.waitFor(21007); r.Arr("first_gift")[0].Int("count") != 1 {
		t.Fatalf("after one: %v", r)
	}
	for i := 1; i < 10; i++ {
		if err := buy(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CreateChargeOrder(e.st, role, 401, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("the 11th: %v", err)
	}
	// the next week it is open again
	next := actNow.Add(7 * 24 * time.Hour)
	chargeClock = func() time.Time { return next }
	e.setNow(next)
	if err := buy(); err != nil {
		t.Fatalf("next week: %v", err)
	}
}

func TestMonthFund(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	old := chargeClock
	chargeClock = func() time.Time { return actNow }
	t.Cleanup(func() { chargeClock = old })
	c, role := e.enter("mf1", "基金人")
	c.send(24700, proto.Message{})
	r := c.waitFor(24700)
	if ids := r.Arr("ids"); len(ids) != 2 || ids[0].Int("id") != 101 || ids[0].Int("status") != 0 {
		t.Fatalf("24700: %v", r)
	}
	c.send(24702, proto.Message{"id": 101})
	if r := c.waitFor(24702); r.Int("code") != 0 {
		t.Fatalf("take before buying: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	o, err := CreateChargeOrder(e.st, role, 102, actNow) // the 128 yuan fund
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+1280 {
		t.Fatalf("the diamonds of the price: %d -> %d", before[3], after[3])
	}
	if _, err := CreateChargeOrder(e.st, role, 102, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("buying a running fund: %v", err)
	}
	c.send(24701, proto.Message{"id": 101})
	r = c.waitFor(24701)
	if r.Int("status") != 1 || r.Int("current_day") != 1 || r.Int("group_id") != 1 || r.Int("endtime") != dayStart(actNow).Unix()+30*86400 {
		t.Fatalf("24701: %v", r)
	}
	mid, _ := e.st.RoleAssets(role.RID)
	c.send(24702, proto.Message{"id": 101})
	if r := c.waitFor(24702); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if r := c.waitFor(24701); r.Int("status") != 2 {
		t.Fatalf("after the take: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != mid[3]+400 { // day 1 of group 1 is 400 diamonds
		t.Fatalf("day 1: %d -> %d", mid[3], after[3])
	}
	c.send(24702, proto.Message{"id": 101})
	if r := c.waitFor(24702); r.Int("code") != 0 {
		t.Fatalf("twice a day: %v", r)
	}
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(24701, proto.Message{"id": 101})
	if r := c.waitFor(24701); r.Int("status") != 1 || r.Int("current_day") != 2 {
		t.Fatalf("day 2: %v", r)
	}
	// after the 30 days it is over and can be bought again
	e.setNow(actNow.Add(30 * 24 * time.Hour))
	chargeClock = func() time.Time { return actNow.Add(30 * 24 * time.Hour) }
	c.send(24701, proto.Message{"id": 101})
	if r := c.waitFor(24701); r.Int("status") != 0 {
		t.Fatalf("after 30 days: %v", r)
	}
	if _, err := CreateChargeOrder(e.st, role, 102, actNow); err != nil {
		t.Fatalf("buying again: %v", err)
	}
}

func TestLimitGifts(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	old := chargeClock
	chargeClock = func() time.Time { return actNow }
	t.Cleanup(func() { chargeClock = old })
	c, role := e.enter("lg1", "限时人")
	c.send(21210, proto.Message{})
	if r := c.waitFor(21210); len(r.Arr("gifts")) != 0 {
		t.Fatalf("nothing is reached: %v", r)
	}
	if _, err := CreateChargeOrder(e.st, role, 211, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("a gift that is not offered: %v", err)
	}
	if err := e.st.SetRoleLevel(role.RID, 60); err != nil {
		t.Fatal(err)
	}
	c.send(21210, proto.Message{})
	if push := c.waitFor(21211); push.Int("id") != 10 { // level 60: the 48 gift, not 18 too
		t.Fatalf("21211: %v", push)
	}
	r := c.waitFor(21210)
	g := r.Arr("gifts")
	if len(g) != 1 || g[0].Int("id") != 10 || g[0].Int("end_time") != actNow.Unix()+7200 {
		t.Fatalf("21210: %v", r)
	}
	if _, err := CreateChargeOrder(e.st, role, 211, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("the passed 18 gift: %v", err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	o, err := CreateChargeOrder(e.st, role, 212, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+680 || after[1] != before[1]+1000000 {
		t.Fatalf("the gift: %v -> %v", before, after)
	}
	if _, err := CreateChargeOrder(e.st, role, 212, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("a second time: %v", err)
	}
	c.send(21210, proto.Message{})
	if r := c.waitFor(21210); len(r.Arr("gifts")) != 0 {
		t.Fatalf("after buying: %v", r)
	}
}

func TestLimitGiftOnLevelUp(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("lg2", "升级人")
	o, err := CreateChargeOrder(e.st, role, 3, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetRoleLevel(role.RID, 50); err != nil { // the level went up since the session began
		t.Fatal(err)
	}
	c.send(21001, proto.Message{"id": 1}) // any answer that pushes the resources notices the new level
	c.waitFor(21001)
	if push := c.waitFor(21211); push.Int("id") != 10 {
		t.Fatalf("21211: %v", push)
	}
}

func TestOneCentGiftAndLoginInit(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("oc1", "一分钱")
	c.send(21021, proto.Message{})
	if r := c.waitFor(21021); r.Int("flag") != 1 {
		t.Fatalf("21021: %v", r)
	}
	c.send(16651, proto.Message{})
	r := c.waitFor(16651)
	if r.Int("is_buy") != 0 || len(r.Arr("list")) != 4 || r.Arr("list")[0].Int("finish") != 0 {
		t.Fatalf("16651: %v", r)
	}
	c.send(16652, proto.Message{"id": 1})
	if r := c.waitFor(16652); r.Int("code") != 0 {
		t.Fatalf("before buying: %v", r)
	}
	o, err := CreateChargeOrder(e.st, role, 301, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateChargeOrder(e.st, role, 301, actNow); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("twice: %v", err)
	}
	c.send(16651, proto.Message{})
	r = c.waitFor(16651)
	if r.Int("is_buy") != 1 || r.Arr("list")[0].Int("finish") != 1 || r.Arr("list")[3].Int("finish") != 0 {
		t.Fatalf("bought: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(16652, proto.Message{"id": 1})
	if r := c.waitFor(16652); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+200 {
		t.Fatalf("diamonds %d -> %d", before[3], after[3])
	}
}

func TestDayCharge(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	old := chargeClock
	chargeClock = func() time.Time { return actNow }
	t.Cleanup(func() { chargeClock = old })
	c, role := e.enter("dc1", "日首充")
	c.send(21010, proto.Message{})
	if r := c.waitFor(21010); r.Int("status") != 0 || r.Int("num") != 0 {
		t.Fatalf("21010: %v", r)
	}
	pay := func(id int64) {
		o, err := CreateChargeOrder(e.st, role, id, actNow)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "t", ""); err != nil {
			t.Fatal(err)
		}
	}
	pay(3) // 6 yuan: 60 of 180
	c.send(21010, proto.Message{})
	if r := c.waitFor(21010); r.Int("status") != 0 || r.Int("num") != 60 {
		t.Fatalf("after 6 yuan: %v", r)
	}
	c.send(21011, proto.Message{})
	if r := c.waitFor(21011); r.Int("code") != 0 {
		t.Fatalf("early take: %v", r)
	}
	pay(4) // 30 yuan more
	c.send(21010, proto.Message{})
	if r := c.waitFor(21010); r.Int("status") != 1 {
		t.Fatalf("reached: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(21011, proto.Message{})
	if r := c.waitFor(21011); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+200 {
		t.Fatalf("diamonds %d -> %d", before[3], after[3])
	}
	c.waitFor(21010) // the push after the take
	// the next day it starts again
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(21010, proto.Message{})
	if r := c.waitFor(21010); r.Int("status") != 0 || r.Int("num") != 0 {
		t.Fatalf("next day: %v", r)
	}
}

func TestTouchGold(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(dayStart(actNow).Add(10 * time.Hour)) // after the 9:00 refresh
	c, role := e.enter("tg1", "点金手")
	c.send(23606, proto.Message{})
	r := c.waitFor(23606)
	l := r.Arr("list")
	if len(l) != 3 || l[0].Int("price") != 0 || l[1].Int("price") != 20 || l[0].Int("num") != 0 || r.Int("ref_time") != dayStart(actNow).Add(19*time.Hour).Unix() {
		t.Fatalf("23606: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(23607, proto.Message{"id": 1})
	if r := c.waitFor(23607); r.Int("code") != 1 {
		t.Fatalf("free touch: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[1] < before[1]+10000 {
		t.Fatalf("coins %d -> %d", before[1], after[1])
	}
	c.send(23607, proto.Message{"id": 1})
	if r := c.waitFor(23607); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	c.send(23607, proto.Message{"id": 3}) // 50 diamonds that the role does not have
	if r := c.waitFor(23607); r.Int("code") != 0 {
		t.Fatalf("no diamonds: %v", r)
	}
	e.setNow(dayStart(actNow).Add(19*time.Hour + time.Minute))
	c.send(23606, proto.Message{})
	if r := c.waitFor(23606); r.Arr("list")[0].Int("num") != 0 {
		t.Fatalf("after the refresh: %v", r)
	}
}

func TestActivityBox(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ab1", "活跃人")
	c.send(20300, proto.Message{})
	if r := c.waitFor(20300); len(r.Arr("activity_box")) != 0 {
		t.Fatalf("20300: %v", r)
	}
	c.send(20301, proto.Message{"activity": 25})
	if r := c.waitFor(20301); r.Int("code") != 0 {
		t.Fatalf("no points: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{9: 30}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	c.send(20301, proto.Message{"activity": 50})
	if r := c.waitFor(20301); r.Int("code") != 0 {
		t.Fatalf("too few points: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(20301, proto.Message{"activity": 25})
	if r := c.waitFor(20301); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[3] != before[3]+20 || after[1] != before[1]+10000 {
		t.Fatalf("box: %v -> %v", before, after)
	}
	c.send(20301, proto.Message{"activity": 25})
	if r := c.waitFor(20301); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(20300, proto.Message{})
	if r := c.waitFor(20300); len(r.Arr("activity_box")) != 0 {
		t.Fatalf("next day: %v", r)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[9] != 0 {
		t.Fatalf("the points of the day before: %v", after)
	}
}
