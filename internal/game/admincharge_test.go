package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func chargeTestClock(t *testing.T) {
	t.Helper()
	old := chargeClock
	chargeClock = func() time.Time { return actNow }
	t.Cleanup(func() { chargeClock = old })
}

func chargeBarrier(c *client) { c.send(10905, proto.Message{}); c.waitFor(10905) }

func chargeOffline(t *testing.T, e *env, c *client, role *store.Role) {
	t.Helper()
	chargeBarrier(c)
	c.c.Close()
	deadline := time.Now().Add(time.Second)
	for e.gw.Hub.Online(role.RID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("role did not go offline")
		}
		time.Sleep(time.Millisecond)
	}
}

func chargeReenter(t *testing.T, e *env, account string, role *store.Role) *client {
	t.Helper()
	_, token := e.account(account)
	c := e.dial(true)
	c.send(1164, loginArgs(account, token))
	c.waitFor(1164)
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if c.waitFor(10192).Int("code") != 1 {
		t.Fatal("reentry failed")
	}
	chargeBarrier(c)
	return c
}

func chargePay(t *testing.T, e *env, role *store.Role, id int64) *store.PayOrder {
	t.Helper()
	o, err := CreateChargeOrder(e.st, role, id, actNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "test", "isolated test payment"); err != nil {
		t.Fatal(err)
	}
	return o
}

// Exercise registered reset callbacks and the same compare-before/audit store
// transaction used by HTTP, with a genuinely offline TCP role. HTTP permission
// and gateway checks are independently covered in internal/admin charge tests.
func resetChargeState(t *testing.T, e *env, role *store.Role, key, item string) {
	t.Helper()
	if e.gw.Hub.Online(role.RID) != nil {
		t.Fatal("reset fixture must be offline")
	}
	m, ok := FindAdminModule("role", key)
	if !ok {
		t.Fatal(key)
	}
	before, exists, err := e.st.RoleModule(role.RID, key)
	if err != nil || !exists {
		t.Fatalf("missing reset seed %s: %v", key, err)
	}
	var selected *AdminReset
	for i := range m.Resets {
		if m.Resets[i].Item == item {
			selected = &m.Resets[i]
			break
		}
	}
	if selected == nil {
		t.Fatalf("item not registered: %s/%s", key, item)
	}
	var after json.RawMessage
	if item != "all" {
		d := m.New()
		if err := json.Unmarshal(before, d); err != nil {
			t.Fatal(err)
		}
		if err := selected.Apply(e.st, role.RID, d, actNow); err != nil {
			t.Fatal(err)
		}
		after, err = json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
	}
	assets, _ := e.st.RoleAssets(role.RID)
	paid, _ := e.st.RoleCharge(role.RID)
	if err := e.st.CommitRoleModuleReset(role.RID, key, before, after, store.AuditLog{AdminID: 1, AdminName: "test", Action: "role.module.reset", DetailJSON: `{"reason":"isolated game regression"}`}); err != nil {
		t.Fatal(err)
	}
	afterAssets, _ := e.st.RoleAssets(role.RID)
	afterPaid, _ := e.st.RoleCharge(role.RID)
	if !reflect.DeepEqual(assets, afterAssets) || !reflect.DeepEqual(paid, afterPaid) {
		t.Fatal("reset changed paid totals or granted assets")
	}
}

func assertChargeProtocol(t *testing.T, c *client, cmd uint16, want proto.Message, got proto.Message) {
	t.Helper()
	b, err := proto.Encode(proto.DefaultOptions(), c.schema.Recv[cmd], want)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := proto.Decode(proto.DefaultOptions(), c.schema.Recv[cmd], b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized, got) {
		t.Fatalf("protocol %d differs:\nview=%v\ngame=%v", cmd, normalized, got)
	}
}

func readChargeView(t *testing.T, e *env, role *store.Role, key string) AdminChargeView {
	t.Helper()
	m, ok := FindAdminModule("role", key)
	if !ok || m.View == nil {
		t.Fatal("missing charge view " + key)
	}
	before, _ := e.st.RoleModules(role.RID)
	v, err := m.View(e.st, strconv.FormatInt(role.RID, 10), key, actNow)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := e.st.RoleModules(role.RID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("view wrote persisted state")
	}
	return v.(AdminChargeView)
}

func TestAdminChargeViewsMatchGameHandlers(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("chargeviews", "视图比对")
	chargeBarrier(c)
	chargePay(t, e, role, 8)
	chargePay(t, e, role, 8)
	today := dayNo(actNow)
	for _, fs := range firstSets {
		key := "firstcharge_" + fs.key
		if err := e.st.SaveState(role.RID, key, &firstChargeState{Start: [2]int64{today - 1, 0}, Taken: [2]int64{1, 0}, Last: [2]int64{today - 1, 0}, Chosen: 2}); err != nil {
			t.Fatal(err)
		}
		v := readChargeView(t, e, role, key)
		c.send(fs.show, proto.Message{})
		assertChargeProtocol(t, c, fs.show, v.Protocols[itoa64(int64(fs.show))].(proto.Message), c.waitFor(fs.show))
		chargeBarrier(c)
	}
	if err := e.st.SaveState(role.RID, dayChargeModule, &dayChargeState{Day: today - 1, Num: 900, Taken: true}); err != nil {
		t.Fatal(err)
	}
	v := readChargeView(t, e, role, dayChargeModule)
	c.send(21010, proto.Message{})
	assertChargeProtocol(t, c, 21010, v.Protocols["21010"].(proto.Message), c.waitFor(21010))
	chargeBarrier(c)
	rc, _ := e.st.RoleCharge(role.RID)
	if err := e.st.SaveState(role.RID, monthCardModule, &monthCardState{Base: [2]int64{rc.TotalCents, rc.TotalCents}, Start: [2]int64{today - 1, today - 2}, Last: [2]int64{today, today - 1}, Days: [2]int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	v = readChargeView(t, e, role, monthCardModule)
	c.send(16705, proto.Message{})
	assertChargeProtocol(t, c, 16705, v.Protocols["16705"].(proto.Message), c.waitFor(16705))
	chargeBarrier(c)
	if err := e.st.SaveState(role.RID, monthFundModule, &monthFundState{Start: [2]int64{today - 1, today - 2}, Last: [2]int64{today, today - 1}}); err != nil {
		t.Fatal(err)
	}
	v = readChargeView(t, e, role, monthFundModule)
	c.send(24700, proto.Message{})
	assertChargeProtocol(t, c, 24700, v.Protocols["24700"].(proto.Message), c.waitFor(24700))
	for i, fd := range monthFunds {
		c.send(24701, proto.Message{"id": fd.ID})
		assertChargeProtocol(t, c, 24701, v.Protocols["24701"].([]proto.Message)[i], c.waitFor(24701))
	}
	chargeBarrier(c)
	lg := &limitGiftState{Gifts: map[int64]*limitGiftEntry{}}
	for i, g := range limitGifts {
		lg.Gifts[g.ID] = &limitGiftEntry{Start: actNow.Unix() - 100, Bought: i%3 == 0}
	}
	if err := e.st.SaveState(role.RID, limitGiftModule, lg); err != nil {
		t.Fatal(err)
	}
	v = readChargeView(t, e, role, limitGiftModule)
	c.send(21210, proto.Message{})
	assertChargeProtocol(t, c, 21210, v.Protocols["21210"].(proto.Message), c.waitFor(21210))
	chargeBarrier(c)
	stale := &cycleGiftState{End: [3]int64{0, 1, 1}, Count: map[int64]int64{}}
	for _, info := range cycleInfos {
		stale.Count[info.ChargeID] = info.Limit
	}
	if err := e.st.SaveState(role.RID, cycleGiftModule, stale); err != nil {
		t.Fatal(err)
	}
	before, _, _ := e.st.RoleModule(role.RID, cycleGiftModule)
	v = readChargeView(t, e, role, cycleGiftModule)
	for i := int64(1); i <= 2; i++ {
		c.send(21007, proto.Message{"type": i})
		assertChargeProtocol(t, c, 21007, v.Protocols["21007"].([]proto.Message)[i-1], c.waitFor(21007))
	}
	after, _, _ := e.st.RoleModule(role.RID, cycleGiftModule)
	if !bytes.Equal(before, after) {
		t.Fatal("cycle view/handler persisted roll")
	}
}

func TestAdminFirstChargeResetsEachWindowAndNoDuplicateClaim(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("firstreset", "首充重置")
	chargeBarrier(c)
	chargePay(t, e, role, 8)
	chargePay(t, e, role, 8)
	for _, fs := range firstSets {
		c.send(fs.show, proto.Message{})
		c.waitFor(fs.show)
		c.send(fs.take, proto.Message{"id": 1})
		if c.waitFor(fs.take).Int("code") != 1 {
			t.Fatal("initial first-charge claim failed")
		}
		c.waitFor(fs.show)
		chargeOffline(t, e, c, role)
		resetChargeState(t, e, role, "firstcharge_"+fs.key, "all")
		c = chargeReenter(t, e, "firstreset", role)
		c.send(fs.show, proto.Message{})
		c.waitFor(fs.show)
		c.send(fs.take, proto.Message{"id": 1})
		if c.waitFor(fs.take).Int("code") != 1 {
			t.Fatal("reset did not reopen first-charge claim")
		}
		c.waitFor(fs.show)
		chargeBarrier(c)
		assets, _ := e.st.RoleAssets(role.RID)
		items, _ := e.st.RoleItems(role.RID)
		c.send(fs.take, proto.Message{"id": 1})
		if c.waitFor(fs.take).Int("code") != 0 {
			t.Fatal("duplicate first-charge claim succeeded")
		}
		chargeBarrier(c)
		after, _ := e.st.RoleAssets(role.RID)
		afterItems, _ := e.st.RoleItems(role.RID)
		if !reflect.DeepEqual(assets, after) || !reflect.DeepEqual(items, afterItems) {
			t.Fatal("duplicate claim paid again")
		}
	}
}

func TestAdminMonthCardResetsEachCardRequireNewPayment(t *testing.T) {
	for i := range monthCards {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			chargeTestClock(t)
			e := newEnv(t, 100*time.Hour)
			e.setNow(actNow)
			c, role := e.enter("cardreset", "月卡重置")
			chargeBarrier(c)
			chargePay(t, e, role, 21)
			c.send(16705, proto.Message{})
			c.waitFor(16705)
			c.send(16706, proto.Message{"card_type": i + 1})
			if c.waitFor(16706).Int("code") != 1 {
				t.Fatal("initial card claim")
			}
			c.waitFor(16705)
			chargeOffline(t, e, c, role)
			var before monthCardState
			e.st.LoadState(role.RID, monthCardModule, &before)
			rc, _ := e.st.RoleCharge(role.RID)
			resetChargeState(t, e, role, monthCardModule, strconv.Itoa(i))
			var after monthCardState
			e.st.LoadState(role.RID, monthCardModule, &after)
			j := 1 - i
			if after.Base[i] != rc.TotalCents || after.Start[i] != 0 || after.Last[i] != 0 || after.Days[i] != 0 || after.Base[j] != before.Base[j] || after.Start[j] != before.Start[j] || after.Last[j] != before.Last[j] || after.Days[j] != before.Days[j] {
				t.Fatal("card reset changed sibling or wrong base")
			}
			c = chargeReenter(t, e, "cardreset", role)
			c.send(16705, proto.Message{})
			if c.waitFor(16705).Int("card"+strconv.Itoa(i+1)+"_is_reward") != 0 {
				t.Fatal("historical payments reactivated reset card")
			}
			chargePay(t, e, role, 21)
			c.send(16706, proto.Message{"card_type": i + 1})
			if c.waitFor(16706).Int("code") != 1 {
				t.Fatal("new payment did not reactivate card")
			}
			c.waitFor(16705)
			chargeBarrier(c)
			assets, _ := e.st.RoleAssets(role.RID)
			c.send(16706, proto.Message{"card_type": i + 1})
			if c.waitFor(16706).Int("code") != 0 {
				t.Fatal("duplicate card claim")
			}
			chargeBarrier(c)
			again, _ := e.st.RoleAssets(role.RID)
			if !reflect.DeepEqual(assets, again) {
				t.Fatal("duplicate card assets")
			}
		})
	}
}

func TestAdminMonthFundResetsEachFundRequireNewOrder(t *testing.T) {
	for i, fd := range monthFunds {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			chargeTestClock(t)
			e := newEnv(t, 100*time.Hour)
			e.setNow(actNow)
			c, role := e.enter("fundreset", "基金重置")
			chargeBarrier(c)
			old := chargePay(t, e, role, fd.ChargeID)
			c.send(24702, proto.Message{"id": fd.ID})
			if c.waitFor(24702).Int("code") != 1 {
				t.Fatal("initial fund claim")
			}
			c.waitFor(24701)
			chargeOffline(t, e, c, role)
			resetChargeState(t, e, role, monthFundModule, strconv.Itoa(i))
			c = chargeReenter(t, e, "fundreset", role)
			c.send(24701, proto.Message{"id": fd.ID})
			if c.waitFor(24701).Int("status") != 0 {
				t.Fatal("fund remained active")
			}
			if _, err := ConfirmChargeOrder(e.st, old.OrderNo, "test", ""); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("old payment delivered twice: %v", err)
			}
			chargePay(t, e, role, fd.ChargeID)
			c.send(24702, proto.Message{"id": fd.ID})
			if c.waitFor(24702).Int("code") != 1 {
				t.Fatal("new fund order did not permit claim")
			}
			c.waitFor(24701)
			chargeBarrier(c)
			assets, _ := e.st.RoleAssets(role.RID)
			c.send(24702, proto.Message{"id": fd.ID})
			if c.waitFor(24702).Int("code") != 0 {
				t.Fatal("duplicate fund claim")
			}
			chargeBarrier(c)
			again, _ := e.st.RoleAssets(role.RID)
			if !reflect.DeepEqual(assets, again) {
				t.Fatal("duplicate fund assets")
			}
		})
	}
}

func TestAdminDayChargeResetRequiresFreshDailyCharge(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("dayreset", "每日重置")
	chargeBarrier(c)
	chargePay(t, e, role, 4)
	c.send(21011, proto.Message{})
	if c.waitFor(21011).Int("code") != 1 {
		t.Fatal("initial daily claim")
	}
	c.waitFor(21010)
	chargeOffline(t, e, c, role)
	resetChargeState(t, e, role, dayChargeModule, "today")
	c = chargeReenter(t, e, "dayreset", role)
	c.send(21010, proto.Message{})
	if got := c.waitFor(21010); got.Int("num") != 0 || got.Int("status") != 0 {
		t.Fatal("daily reset did not clear accumulation")
	}
	chargePay(t, e, role, 4)
	c.send(21011, proto.Message{})
	if c.waitFor(21011).Int("code") != 1 {
		t.Fatal("fresh payment did not requalify")
	}
	c.waitFor(21010)
	chargeBarrier(c)
	assets, _ := e.st.RoleAssets(role.RID)
	c.send(21011, proto.Message{})
	if c.waitFor(21011).Int("code") != 0 {
		t.Fatal("duplicate daily claim")
	}
	chargeBarrier(c)
	again, _ := e.st.RoleAssets(role.RID)
	if !reflect.DeepEqual(assets, again) {
		t.Fatal("daily reward duplicated")
	}
}

func TestAdminCycleResetEveryConfiguredChargeAndNoDuplicateDelivery(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("cyclereset", "限购重置")
	chargeBarrier(c)
	if err := e.st.SetRoleCreatedAt(role.RID, actNow.Add(-24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	role, _ = e.st.RoleByID(role.RID)
	for _, info := range cycleInfos {
		seed := &cycleGiftState{End: [3]int64{0, cycleEnd(1, actNow), cycleEnd(2, actNow)}, Count: map[int64]int64{}}
		for _, v := range cycleInfos {
			seed.Count[v.ChargeID] = v.Limit
		}
		chargeBarrier(c)
		if err := e.st.SaveState(role.RID, cycleGiftModule, seed); err != nil {
			t.Fatal(err)
		}
		if _, err := CreateChargeOrder(e.st, role, info.ChargeID, actNow); !errors.Is(err, ErrNotPayable) {
			t.Fatalf("limit not active: %d %v", info.ChargeID, err)
		}
		chargeOffline(t, e, c, role)
		resetChargeState(t, e, role, cycleGiftModule, itoa64(info.ChargeID))
		c = chargeReenter(t, e, "cyclereset", role)
		o := chargePay(t, e, role, info.ChargeID)
		assets, _ := e.st.RoleAssets(role.RID)
		items, _ := e.st.RoleItems(role.RID)
		if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "test", ""); !errors.Is(err, store.ErrConflict) {
			t.Fatal("cycle order delivered twice")
		}
		after, _ := e.st.RoleAssets(role.RID)
		afterItems, _ := e.st.RoleItems(role.RID)
		if !reflect.DeepEqual(assets, after) || !reflect.DeepEqual(items, afterItems) {
			t.Fatal("cycle delivery duplicated")
		}
		var state cycleGiftState
		e.st.LoadState(role.RID, cycleGiftModule, &state)
		for _, v := range cycleInfos {
			want := v.Limit
			if v.ChargeID == info.ChargeID {
				want = 1
			}
			if state.Count[v.ChargeID] != want {
				t.Fatal("cycle reset changed another product")
			}
		}
	}
}

func TestAdminLimitGiftResetEveryConfiguredID(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("limitreset", "限时逐项重置")
	chargeBarrier(c)
	if err := e.st.SetRoleLevel(role.RID, 200); err != nil {
		t.Fatal(err)
	}
	star := int64(0)
	for _, g := range limitGifts {
		if g.Kind == 1 {
			star = max(star, g.need)
		}
	}
	if _, err := e.st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{{BID: 70517, Star: int(star)}}}); err != nil {
		t.Fatal(err)
	}
	role, _ = e.st.RoleByID(role.RID)
	for _, g := range limitGifts {
		seed := &limitGiftState{Gifts: map[int64]*limitGiftEntry{}}
		for _, other := range limitGifts {
			seed.Gifts[other.ID] = &limitGiftEntry{Start: -1}
		}
		seed.Gifts[g.ID] = &limitGiftEntry{Start: actNow.Unix(), Bought: true}
		chargeBarrier(c)
		if err := e.st.SaveState(role.RID, limitGiftModule, seed); err != nil {
			t.Fatal(err)
		}
		chargeOffline(t, e, c, role)
		resetChargeState(t, e, role, limitGiftModule, itoa64(g.ID))
		var cleared limitGiftState
		e.st.LoadState(role.RID, limitGiftModule, &cleared)
		if cleared.Gifts[g.ID] != nil {
			t.Fatalf("gift %d not cleared", g.ID)
		}
		for _, other := range limitGifts {
			if other.ID != g.ID && !reflect.DeepEqual(cleared.Gifts[other.ID], seed.Gifts[other.ID]) {
				t.Fatal("gift reset changed another id")
			}
		}
		c = chargeReenter(t, e, "limitreset", role)
		c.send(21210, proto.Message{})
		list := c.waitFor(21210).Arr("gifts")
		offered := false
		for _, entry := range list {
			offered = offered || entry.Int("id") == g.ID
		}
		if g.Kind == 3 {
			if offered {
				t.Fatal("reset enabled an unsupported holy-equipment gift")
			}
			continue // The original game intentionally never offers this configured kind.
		}
		if !offered {
			t.Fatalf("gift %d was not offered after reset", g.ID)
		}
		o := chargePay(t, e, role, g.ChargeID)
		assets, _ := e.st.RoleAssets(role.RID)
		items, _ := e.st.RoleItems(role.RID)
		if _, err := ConfirmChargeOrder(e.st, o.OrderNo, "test", ""); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("gift %d duplicate payment: %v", g.ID, err)
		}
		after, _ := e.st.RoleAssets(role.RID)
		afterItems, _ := e.st.RoleItems(role.RID)
		if !reflect.DeepEqual(assets, after) || !reflect.DeepEqual(items, afterItems) {
			t.Fatal("gift order delivered twice")
		}
		c.send(21210, proto.Message{})
		for _, entry := range c.waitFor(21210).Arr("gifts") {
			if entry.Int("id") == g.ID {
				t.Fatal("bought gift still offered")
			}
		}
	}
}

func TestAdminChargeConcurrentConfirmRunsWritableHookOnce(t *testing.T) {
	chargeTestClock(t)
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("confirm-concurrent", "并发补单")
	chargeBarrier(c)
	if err := e.st.SetRoleCreatedAt(role.RID, actNow.Add(-24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	role, _ = e.st.RoleByID(role.RID)
	o, err := CreateChargeOrder(e.st, role, 401, actNow)
	if err != nil {
		t.Fatal(err)
	}
	oldHook := chargeHooks[401]
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int64
	chargeHooks[401] = func(st *store.Store, r *store.Role, p ChargeProduct, bought int64) (map[int64]int64, map[int64]int64, error) {
		a, i, err := oldHook(st, r, p, bought)
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return a, i, err
	}
	go func() { _, err := ConfirmChargeOrder(e.st, o.OrderNo, "one", ""); results <- err }()
	<-entered // First hook has already changed the cycle counter, order is still waiting.
	started := make(chan struct{})
	go func() { close(started); _, err := ConfirmChargeOrder(e.st, o.OrderNo, "two", ""); results <- err }()
	<-started
	secondEntered := false
	select {
	case <-entered:
		secondEntered = true
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	a, b := <-results, <-results
	chargeHooks[401] = oldHook // Both confirmations have finished accessing the hook map.
	if secondEntered || calls.Load() != 1 {
		t.Fatal("two confirmations executed a writable hook for one order")
	}
	if !((a == nil && errors.Is(b, store.ErrConflict)) || (b == nil && errors.Is(a, store.ErrConflict))) {
		t.Fatalf("confirmation results: %v / %v", a, b)
	}
	var d cycleGiftState
	e.st.LoadState(role.RID, cycleGiftModule, &d)
	rc, _ := e.st.RoleCharge(role.RID)
	if d.Count[401] != 1 || rc.Bought["401"] != 1 {
		t.Fatal("hook count and paid order count diverged")
	}
}
