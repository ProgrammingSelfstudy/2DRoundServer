package game

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func newH5ShopTestGateway(t *testing.T) (*store.Store, func() *client, func(time.Time)) {
	t.Helper()
	env := newGameEnv(t, time.Minute)
	env.setNow(time.Date(2026, 10, 21, 10, 0, 0, 0, dayZone))
	env.connect = serveTestPipe(t, env.gw, false)
	return env.st, func() *client { return env.dial(true) }, env.setNow
}

func createShopTestRole(t *testing.T, st *store.Store, platform, name string) (*store.Account, *store.Role, string) {
	t.Helper()
	a, err := st.FindOrCreateAccount(platform, "shopqa")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.IssueToken(a.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(a.ID, "symlf_1", name, 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return a, role, token
}

func TestH5ShopGatewayPurchaseAndExchange(t *testing.T) {
	st, dial, setNow := newH5ShopTestGateway(t)
	a, role, token := createShopTestRole(t, st, store.PlatformH5, "商城测试")
	enter := func() *client {
		c := dial()
		c.send(1164, loginArgs(a.Account, token))
		if r := c.recv(1164); r.Int("code") != 1 {
			t.Fatalf("login: %v", r)
		}
		c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
		if r := c.recv(10192); r.Int("code") != 1 {
			t.Fatalf("enter: %v", r)
		}
		return c
	}
	c := enter()
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{1: 60000, 24: 100}, nil); err != nil {
		t.Fatal(err)
	}
	read := func(currency, offer, balance, period, remaining int64) {
		t.Helper()
		c.send(proto.H5ShopCatalog, proto.Message{})
		reply := c.recv(proto.H5ShopCatalog)
		if reply.Int("code") != 1 || len(reply.Arr("sections")) != 4 {
			t.Fatalf("catalog: %v", reply)
		}
		found := false
		for _, section := range reply.Arr("sections") {
			if section.Int("currency_id") != currency {
				continue
			}
			if section.Int("balance") != balance {
				t.Fatalf("currency %d balance %d, want %d", currency, section.Int("balance"), balance)
			}
			for _, g := range section.Arr("offers") {
				if g.Int("offer_id") == offer {
					found = g.Int("period") == period && g.Int("remaining") == remaining
				}
			}
		}
		if !found {
			t.Fatalf("offer %d missing or has the wrong quota", offer)
		}
	}
	refuse := func(req proto.Message, message string) {
		t.Helper()
		c.send(proto.H5ShopBuy, req)
		if r := c.recv(proto.H5ShopBuy); r.Int("code") != 0 || r.Str("msg") != message {
			t.Fatalf("refusal: %v, want %q", r, message)
		}
	}
	buy := func(req proto.Message, asset string, balance, owned int64) {
		t.Helper()
		c.send(proto.H5ShopBuy, req)
		wallet := c.recv(10302)
		got := wallet.Int(asset)
		if value := wallet.Str(asset); value != "" {
			got, _ = strconv.ParseInt(value, 10, 64)
		}
		if got != balance {
			t.Fatalf("after purchase %s=%d, want %d", asset, got, balance)
		}
		delivered := int64(0)
		for _, slot := range c.recv(10500).Arr("item_list") {
			if slot.Int("base_id") == req.Int("item_id") {
				delivered += slot.Int("quantity")
			}
		}
		if delivered != owned {
			t.Fatalf("quantity=%d, want %d", delivered, owned)
		}
		r := c.recv(proto.H5ShopBuy)
		if r.Int("code") != 1 || r.Str("request_id") != req.Str("request_id") || r.Int("offer_id") != req.Int("offer_id") ||
			r.Int("item_id") != req.Int("item_id") || r.Int("currency_id") != req.Int("currency_id") ||
			r.Int("cost") != req.Int("price")*req.Int("count") || r.Int("amount") != req.Int("bundle")*req.Int("count") {
			t.Fatalf("receipt: %v", r)
		}
	}

	order := shopTestOrder
	read(1, -10152, 60000, 2, 2)
	refuse(order(1, -10152, 1, 10152, 1, 1, 1), "商品信息已变化，请重新确认")
	refuse(order(2, 1031, 43, 37004, 1, 5000, 1), "商品暂不可购买")  // voucher shop is outside H5
	refuse(order(3, -10151, 1, 10151, 1, 8000, 1), "商品暂不可购买") // the bag cannot deliver this legacy item
	refuse(order(4, -10152, 1, 10152, 1, 20000, 0), "购买请求无效")
	refuse(order(10, -10152, 1, 10152, 1, 20000, 1001), "购买请求无效")
	keys := order(5, -10152, 1, 10152, 1, 20000, 2)
	buy(keys, "coin", 20000, 2)
	read(1, -10152, 20000, 2, 0)
	refuse(order(6, -10152, 1, 10152, 1, 20000, 1), "已达购买上限")
	c.send(23501, proto.Message{"base_id": 10152, "num": 1})
	if r := c.recv(23501); r.Int("flag") != 0 {
		t.Fatalf("market must respect purchases made through the H5 shop: %v", r)
	}
	refuse(order(7, -10431, 1, 10431, 1, 30000, 1), "余额不足")
	c.c.Close()
	c = enter()
	buy(keys, "coin", 20000, 2)
	refuse(order(5, -10152, 1, 10152, 1, 20000, 1), "购买请求已使用")
	read(1, -10152, 20000, 2, 0)
	buy(order(8, 31008, 24, 24908, 50, 20, 3), "recruithigh_hero", 40, 150)
	read(24, 31008, 40, 0, 0)
	setNow(time.Date(2026, 10, 26, 0, 0, 0, 0, dayZone))
	read(1, -10152, 20000, 2, 2)
	buy(order(9, -10152, 1, 10152, 1, 20000, 1), "coin", 0, 3)
	read(1, -10152, 0, 2, 1)
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{1: 20000}, nil); err != nil {
		t.Fatal(err)
	}
	c.send(23501, proto.Message{"base_id": 10152, "num": 1})
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(23501); r.Int("flag") != 1 {
		t.Fatalf("market purchase: %v", r)
	}
	read(1, -10152, 0, 2, 0)
}

func shopTestOrder(id, eid, currency, item, bundle, price, count int64) proto.Message {
	return proto.Message{"request_id": fmt.Sprintf("%032x", id), "offer_id": eid, "currency_id": currency,
		"item_id": item, "bundle": bundle, "price": price, "count": count}
}

func TestH5ShopGatewayAccessAndFiveStarOffers(t *testing.T) {
	st, dial, _ := newH5ShopTestGateway(t)
	legacy, legacyRole, legacyToken := createShopTestRole(t, st, "symlf", "旧版商城")
	h5, h5Role, h5Token := createShopTestRole(t, st, store.PlatformH5, "网页商城")
	for _, role := range []*store.Role{legacyRole, h5Role} {
		if err := st.Exchange(role.RID, nil, nil, map[int64]int64{8: 1000, 24: 30}, nil); err != nil {
			t.Fatal(err)
		}
	}
	c := dial()
	login := func(a *store.Account, token string, code int64) {
		t.Helper()
		args := loginArgs(a.Account, token)
		for _, arg := range args.Arr("args") {
			if arg.Str("key") == "platform" {
				arg["val"] = store.PlatformH5
			}
		}
		c.send(1164, args)
		if r := c.recv(1164); r.Int("code") != code {
			t.Fatalf("login: %v, want code %d", r, code)
		}
	}
	enter := func(role *store.Role) {
		t.Helper()
		c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
		if r := c.recv(10192); r.Int("code") != 1 {
			t.Fatalf("enter: %v", r)
		}
	}
	refuse := func(order proto.Message, message string) {
		t.Helper()
		c.send(proto.H5ShopBuy, order)
		if r := c.recv(proto.H5ShopBuy); r.Int("code") != 0 || r.Str("msg") != message || r.Str("request_id") != order.Str("request_id") {
			t.Fatalf("refusal: %v, want %q", r, message)
		}
	}
	deny := func() {
		t.Helper()
		c.send(proto.H5ShopCatalog, proto.Message{})
		if r := c.recv(proto.H5ShopCatalog); r.Int("code") != 0 || len(r.Arr("sections")) != 0 || r.Str("msg") != "请先登录 H5 角色" {
			t.Fatalf("catalog access: %v", r)
		}
		refuse(shopTestOrder(1, 31008, 24, 24908, 50, 20, 1), "请先登录 H5 角色")
	}
	deny() // no login
	login(legacy, legacyToken, codeOK)
	enter(legacyRole)
	deny() // the platform string in the request does not change the authenticated account
	c.send(13402, proto.Message{"eid": 31040, "num": 1})
	if r := c.recv(13402); r.Int("code") != 1 || r.Int("type") != 31 || r.Int("eid") != 31040 {
		t.Fatalf("legacy four-star purchase: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	c.recv(10501)
	if items, err := st.RoleItems(legacyRole.RID); err != nil || items[24800] != 30 || items[24908] != 0 {
		t.Fatalf("legacy items: %v, %v", items, err)
	}
	if balances, err := st.RoleAssets(legacyRole.RID); err != nil || balances[24] != 27 {
		t.Fatalf("legacy wallet: %v, %v", balances, err)
	}

	login(h5, h5Token, codeFail)
	deny() // the session still belongs to the original native account
	c = dial()
	login(h5, h5Token, codeOK)
	deny() // an authenticated account still needs to enter its role
	enter(h5Role)
	c.send(proto.H5ShopCatalog, proto.Message{})
	r := c.recv(proto.H5ShopCatalog)
	if r.Int("code") != 1 {
		t.Fatalf("H5 catalog: %v", r)
	}
	listed := make(map[int64]proto.Message)
	for _, section := range r.Arr("sections") {
		for _, offer := range section.Arr("offers") {
			listed[offer.Int("offer_id")] = offer
		}
	}
	for id, item := range map[int64]int64{6003: 29105, 6006: 29905, 31008: 24908, 17001: 25908, 17006: 72001} {
		if got := listed[id]; got.Int("item_id") != item {
			t.Errorf("offer %d: got %v, want item %d", id, got, item)
		}
	}
	for _, id := range []int64{6001, 6002, 31040, 31055} {
		if _, ok := listed[id]; ok {
			t.Errorf("catalog lists four-star offer %d", id)
		}
	}
	refuse(shopTestOrder(2, 6001, 8, 29404, 10, 1000, 1), "商品暂不可购买")
	refuse(shopTestOrder(3, 31040, 24, 24800, 30, 3, 1), "商品暂不可购买")
	for _, offer := range []int64{6001, 31040} {
		c.send(13402, proto.Message{"eid": offer, "num": 1})
		if reply := c.recv(13402); reply.Int("code") != 0 || reply.Int("eid") != offer || reply.Str("msg") == "" {
			t.Fatalf("legacy exchange sold a low-star shard to an H5 role: %v", reply)
		}
	}
	if balances, err := st.RoleAssets(h5Role.RID); err != nil || balances[8] != 1000 || balances[24] != 30 {
		t.Fatalf("H5 wallet after refusals: %v, %v", balances, err)
	}
	if items, err := st.RoleItems(h5Role.RID); err != nil || len(items) != 0 {
		t.Fatalf("H5 bag after refusals: %v, %v", items, err)
	}
}

func TestH5ShopRechecksHeroItemEligibilityOnPurchase(t *testing.T) {
	st, _, _ := newH5ShopTestGateway(t)
	_, role, _ := createShopTestRole(t, st, store.PlatformH5, "商城测试")
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{8: 2000}, nil); err != nil {
		t.Fatal(err)
	}
	if !h5ShopItemAllowed(29900) {
		t.Error("the legendary five-star shard recipe should be eligible without an ID allowlist")
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, dayZone)
	for _, item := range []int64{24000, 29404} {
		order := store.H5ShopOrder{OfferID: item, ItemID: item, CurrencyID: 8, Bundle: 1, Price: 1000}
		offers := map[int64]h5ShopOffer{order.OfferID: {order: order}}
		req := shopTestOrder(item, order.OfferID, order.CurrencyID, order.ItemID, order.Bundle, order.Price, 1)
		if _, msg, err := buyH5Shop(st, role, now, offers, req); err != nil || msg != "商品暂不可购买" {
			t.Errorf("stale offer for low-star item %d: %q, %v", item, msg, err)
		}
	}
	if balances, err := st.RoleAssets(role.RID); err != nil || balances[8] != 2000 {
		t.Errorf("refused offers changed the wallet: %v, %v", balances, err)
	}
	if items, err := st.RoleItems(role.RID); err != nil || len(items) != 0 {
		t.Errorf("refused offers changed the bag: %v, %v", items, err)
	}
	if bought, err := st.ShopBought(role.RID, 0, dayKey(now)); err != nil || len(bought) != 0 {
		t.Errorf("refused offers updated purchase counts: %v, %v", bought, err)
	}
}

func TestH5ShopDailyAndMonthlyQuotaWindows(t *testing.T) {
	sections := h5ShopSections()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1}); err != nil {
		t.Fatal(err)
	}
	a, err := st.FindOrCreateAccount(store.PlatformH5, "shopqa")
	if err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(a.ID, "symlf_1", "商城测试", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{27: 100000}, nil); err != nil {
		t.Fatal(err)
	}
	offers := make(map[int64]h5ShopOffer)
	for _, g := range sections[3].offers {
		offers[g.order.OfferID] = g
	}
	start := time.Date(2026, 10, 31, 23, 59, 0, 0, dayZone)
	for i, eid := range []int64{17006, 17001} {
		g := offers[eid].order
		req := proto.Message{"offer_id": eid, "item_id": g.ItemID, "currency_id": g.CurrencyID, "bundle": g.Bundle, "price": g.Price, "count": 1}
		check := func(at time.Time, key int, message string) {
			t.Helper()
			req["request_id"] = fmt.Sprintf("%032x", key)
			if _, got, err := buyH5Shop(st, role, at, offers, req); err != nil || got != message {
				t.Fatalf("offer %d at %s: message %q, err %v", eid, at, got, err)
			}
		}
		check(start, 10*i, "")
		check(start, 10*i+1, "已达购买上限")
		check(start.Add(time.Minute), 10*i+2, "")
	}
	balances, err := st.RoleAssets(role.RID)
	if err != nil || balances[27] != 12500 {
		t.Fatalf("honor balance: %v, %v", balances, err)
	}
	items, err := st.RoleItems(role.RID)
	if err != nil || items[72001] != 200 || items[25908] != 100 {
		t.Fatalf("honor delivery: %v, %v", items, err)
	}
}
