package game

import (
	"errors"
	"regexp"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type h5ShopOffer struct {
	order store.H5ShopOrder
	name  string
	level int64
	rule  shopGood
}

type h5ShopSection struct {
	name       string
	currencyID int64
	offers     []h5ShopOffer
}

func h5ShopShowsItem(item int64) bool {
	if item >= 24000 && item < 29000 {
		// The five camp ranges encode (star - 1) in the hundreds digit of
		// a hero card ID, as in heroCardItem. The matching shard adds 500.
		hundreds := item % 1000 / 100
		return hundreds == 4 || hundreds == 9
	}
	if itemTable[item].Type == 102 {
		// The five-star random and camp shard items have separate IDs.
		switch item {
		case 29105, 29205, 29305, 29405, 29505, 29905, 29906, 29907:
			return true
		}
		return false
	}
	return true
}

func h5ShopSections() []h5ShopSection {
	sections := []h5ShopSection{
		{name: "贝里市场", currencyID: 1},
		{name: "竞技兑换", currencyID: 8},
		{name: "声望兑换", currencyID: 24},
		{name: "荣誉兑换", currencyID: 27},
	}
	add := func(section *h5ShopSection, eid, item, amount, price, level int64, rule shopGood) {
		name, known := ItemName(item)
		if !known || name == "" || name == "获取途径用" || !ItemListedInBackpack(item) || isArtifact(item) ||
			!h5ShopShowsItem(item) || amount <= 0 || price <= 0 {
			return
		}
		section.offers = append(section.offers, h5ShopOffer{
			order: store.H5ShopOrder{OfferID: eid, ItemID: item, CurrencyID: section.currencyID, Bundle: amount, Price: price},
			name:  name,
			level: level,
			rule:  rule,
		})
	}
	for _, g := range marketCfg.Goods {
		rule := shopGood{Day: g.LimitNum}
		if g.LimitType == 2 {
			rule = shopGood{Week: g.LimitNum}
		}
		add(&sections[0], marketKey(g.BaseID), g.BaseID, 1, g.Price, g.OpenLev, rule)
	}
	exchange := map[int64]*h5ShopSection{6: &sections[1], 31: &sections[2], 17: &sections[3]}
	for _, eid := range sortedShopIDs() {
		g := shopGoods[eid]
		section := exchange[g.Type]
		if section == nil {
			continue
		}
		if pay, ok := g.payID(); ok && pay == section.currencyID {
			add(section, eid, g.Item, g.Num, g.Price, 0, g)
		}
	}
	return sections
}

func h5ShopPeriod(rule shopGood) int64 {
	switch {
	case rule.Day > 0 && rule.Day < 9999:
		return 1
	case rule.Week > 0:
		return 2
	case rule.Month > 0:
		return 3
	}
	return 0
}

func h5ShopCatalog(st *store.Store, role *store.Role, now time.Time, sections []h5ShopSection) (proto.Message, error) {
	balances, err := st.RoleAssets(role.RID)
	if err != nil {
		return nil, err
	}
	windows := make(map[[2]int64]map[int64]int64)
	list := make([]proto.Message, 0, len(sections))
	for _, section := range sections {
		offers := make([]proto.Message, 0, len(section.offers))
		for _, g := range section.offers {
			if int64(role.Lev) < g.level {
				continue
			}
			remaining := int64(0)
			from, to, limit := g.rule.limitWindow(now)
			if limit > 0 {
				window := [2]int64{from, to}
				bought, loaded := windows[window]
				if !loaded {
					bought, err = st.ShopBought(role.RID, from, to)
					if err != nil {
						return nil, err
					}
					windows[window] = bought
				}
				remaining = max(0, limit-bought[g.order.OfferID])
			}
			offers = append(offers, proto.Message{
				"offer_id": g.order.OfferID, "item_id": g.order.ItemID, "name": g.name,
				"bundle": g.order.Bundle, "price": g.order.Price, "period": h5ShopPeriod(g.rule), "remaining": remaining,
			})
		}
		name, _ := ItemName(section.currencyID)
		list = append(list, proto.Message{"name": section.name, "currency_id": section.currencyID,
			"currency_name": name, "balance": max(0, balances[section.currencyID]), "offers": offers})
	}
	return proto.Message{"code": 1, "sections": list}, nil
}

var h5ShopRequestID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func buyH5Shop(st *store.Store, role *store.Role, now time.Time, offers map[int64]h5ShopOffer, req proto.Message) (store.H5ShopOrder, string, error) {
	requestID := req.Str("request_id")
	order := store.H5ShopOrder{
		OfferID: req.Int("offer_id"), ItemID: req.Int("item_id"), CurrencyID: req.Int("currency_id"),
		Bundle: req.Int("bundle"), Price: req.Int("price"), Count: req.Int("count"),
	}
	if !h5ShopRequestID.MatchString(requestID) || order.Count < 1 || order.Count > maxUseAtOnce {
		return store.H5ShopOrder{}, "购买请求无效", nil
	}
	saved, err := st.H5ShopReceipt(role.RID, requestID)
	switch {
	case err == nil && saved == order:
		return saved, "", nil
	case err == nil:
		return store.H5ShopOrder{}, "购买请求已使用", nil
	case !errors.Is(err, store.ErrNotFound):
		return store.H5ShopOrder{}, "", err
	}
	g, ok := offers[order.OfferID]
	if !ok || int64(role.Lev) < g.level {
		return store.H5ShopOrder{}, "商品暂不可购买", nil
	}
	expected := g.order
	expected.Count = order.Count
	if expected != order {
		return store.H5ShopOrder{}, "商品信息已变化，请重新确认", nil
	}
	from, to, limit := g.rule.limitWindow(now)
	saved, err = st.BuyH5Shop(role.RID, requestID, expected, dayKey(now), from, to, limit)
	switch {
	case errors.Is(err, store.ErrShopLimit):
		return store.H5ShopOrder{}, "已达购买上限", nil
	case errors.Is(err, store.ErrConflict):
		return store.H5ShopOrder{}, "余额不足", nil
	case errors.Is(err, store.ErrShopOrder):
		return store.H5ShopOrder{}, "购买请求已使用", nil
	}
	return saved, "", err
}

func RegisterH5Shop(r *gateway.Router, st *store.Store) {
	sections := h5ShopSections()
	offers := make(map[int64]h5ShopOffer)
	for _, section := range sections {
		for _, g := range section.offers {
			offers[g.order.OfferID] = g
		}
	}
	getRole := func(s *gateway.Session) *store.Role {
		role, account := s.Role(), s.Account()
		if role != nil && account != nil && account.Platform == store.PlatformH5 && account.ID == role.AccountID {
			return role
		}
		return nil
	}
	r.Handle(proto.H5ShopCatalog, func(s *gateway.Session, _ proto.Message) error {
		role := getRole(s)
		if role == nil {
			return s.Send(proto.H5ShopCatalog, proto.Message{"code": 0, "msg": "请先登录 H5 角色"})
		}
		catalog, err := h5ShopCatalog(st, role, s.Now(), sections)
		if err != nil {
			return err
		}
		return s.Send(proto.H5ShopCatalog, catalog)
	})
	r.Handle(proto.H5ShopBuy, func(s *gateway.Session, req proto.Message) error {
		requestID := req.Str("request_id")
		if len(requestID) != 32 {
			requestID = ""
		}
		refuse := func(msg string) error {
			return s.Send(proto.H5ShopBuy, proto.Message{"request_id": requestID, "code": 0, "msg": msg})
		}
		role := getRole(s)
		if role == nil {
			return refuse("请先登录 H5 角色")
		}
		order, message, err := buyH5Shop(st, role, s.Now(), offers, req)
		if err != nil {
			return err
		}
		if message != "" {
			return refuse(message)
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(proto.H5ShopBuy, proto.Message{
			"request_id": requestID, "code": 1, "offer_id": order.OfferID, "item_id": order.ItemID,
			"amount": order.Bundle * order.Count, "currency_id": order.CurrencyID, "cost": order.Price * order.Count,
		})
	})
}
