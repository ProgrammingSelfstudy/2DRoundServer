package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const treasureShopType = 16
const treasurePoints = 18
const treasureShopKey = "treasure_shop"

type treasureShopGood struct{ item, amount, price, score, attr, value int64 }

// Slot order, quantities, prices and equipment attributes from the type-16 shelves in
// original-2026-10-26/login-main-city and original-2026-10-27/{21-planes-afk-2,idle-5min}.
var treasureShelves = [...][8]treasureShopGood{
	{
		{25902, 50, 8000, 0, 0, 0},
		{24908, 50, 8000, 0, 0, 0},
		{40110, 1, 3125, 1552, 101, 776},
		{40309, 1, 1825, 1318, 104, 3294},
		{40207, 1, 625, 1057, 104, 2641},
		{40107, 1, 625, 704, 101, 352},
		{40209, 1, 1825, 1977, 104, 4941},
		{40407, 1, 625, 468, 101, 234},
	},
	{
		{26903, 50, 9000, 0, 0, 0},
		{40110, 1, 3125, 1552, 101, 776},
		{40210, 1, 3125, 2329, 104, 5822},
		{27700, 20, 300, 0, 0, 0},
		{40308, 1, 1075, 1083, 104, 2707},
		{40307, 1, 625, 705, 104, 1761},
		{40209, 1, 1825, 1977, 104, 4941},
		{40407, 1, 625, 468, 101, 234},
	},
	{
		{25902, 50, 8000, 0, 0, 0},
		{40110, 1, 3125, 1552, 101, 776},
		{40210, 1, 3125, 2329, 104, 5822},
		{40409, 1, 1825, 878, 101, 439},
		{40308, 1, 1075, 1083, 104, 2707},
		{40207, 1, 625, 1057, 104, 2641},
		{40109, 1, 1825, 1316, 101, 658},
		{40108, 1, 1075, 1082, 101, 541},
	},
}

type treasureShopState struct {
	Shelf              int
	Expires, Refreshes int64
	Sold               uint8
}

func (v treasureShopState) goods(h5 bool) [8]treasureShopGood {
	goods := treasureShelves[v.Shelf]
	// H5 only carries 5-star hero fragments. Use the other captured fourth-slot offer
	// on the shelf that otherwise contains 3-star Tashigi.
	if h5 && !h5ShopItemAllowed(goods[3].item) {
		goods[3] = treasureShelves[0][3]
	}
	return goods
}

func (v treasureShopState) message(h5 bool) proto.Message {
	var items []proto.Message
	for i, g := range v.goods(h5) {
		var attrs []proto.Message
		if g.attr != 0 {
			attrs = append(attrs, proto.Message{"attr_type": g.attr, "val": g.value})
		}
		items = append(items, proto.Message{"order": i + 1, "type": 3, "item_id": g.item, "item_num": g.amount, "pay_type": treasurePoints,
			"price": g.price, "has_buy": (v.Sold >> i) & 1, "limit_count": 1, "score": g.score, "main_attr": attrs})
	}
	return proto.Message{"code": 1, "type": treasureShopType, "count": v.Refreshes, "free_count": 0, "num": len(items), "refresh_time": v.Expires, "item_list": items}
}

var errShelfChanged = errors.New("treasure shop shelf changed")

func (previous treasureShopState) commit(st *store.Store, rid int64, next treasureShopState, spend, gain map[int64]int64) error {
	return st.ExchangeAndSave(rid, spend, nil, nil, gain, func(tx *store.Store) error {
		var current treasureShopState
		if _, err := tx.LoadState(rid, treasureShopKey, &current); err != nil {
			return err
		}
		if current != previous {
			return errShelfChanged
		}
		return tx.SaveState(rid, treasureShopKey, next)
	})
}

func handleTreasureShop(st *store.Store, s *gateway.Session, cmd uint16, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	order := req.Int("order")
	refuse := func(msg string) error {
		return s.Send(cmd, proto.Message{"code": 0, "msg": msg, "type": treasureShopType, "order": order, "num": 0})
	}
	fresh, err := st.RoleByID(role.RID)
	if err != nil {
		return err
	}
	if fresh.Lev < 10 {
		return refuse("人物等级10级开启")
	}
	var previous treasureShopState
	if _, err := st.LoadState(role.RID, treasureShopKey, &previous); err != nil {
		return err
	}
	next := previous
	now := s.Now()
	expired := now.Unix() >= previous.Expires
	view := &s.TreasureShopView
	if cmd != 13403 && (expired || view.RID != role.RID || view.Expires != previous.Expires || view.Refreshes != previous.Refreshes) {
		return refuse("货架已更新，请重新查看")
	}
	var spend, gain map[int64]int64
	h5 := isH5RoleSession(s)
	switch cmd {
	case 13403:
		if expired {
			next = treasureShopState{Shelf: previous.Shelf, Expires: nextMidnight(now)}
			if previous.Expires != 0 {
				next.Shelf = (next.Shelf + 1) % len(treasureShelves)
			}
		}
	case 13405:
		next.Shelf = (previous.Shelf + 1) % len(treasureShelves)
		next.Sold = 0
		next.Refreshes++
		spend = map[int64]int64{treasurePoints: 50}
	case 13407:
		if order < 1 || order > int64(len(treasureShelves[0])) || req.Int("num") != 1 || req.Int("buy_type") != 1 {
			return refuse("兑换数量或商品无效")
		}
		bit := uint8(1 << (order - 1))
		if previous.Sold&bit != 0 {
			return refuse("本轮已兑换")
		}
		g := previous.goods(h5)[order-1]
		spend = map[int64]int64{treasurePoints: g.price}
		gain = map[int64]int64{g.item: g.amount}
		next.Sold |= bit
	}
	if next != previous {
		err := previous.commit(st, role.RID, next, spend, gain)
		if errors.Is(err, errShelfChanged) {
			return refuse("货架已更新，请重新查看")
		}
		if errors.Is(err, store.ErrConflict) {
			return refuse("探宝积分不足")
		}
		if err != nil {
			return err
		}
	}
	if cmd != 13403 {
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
	}
	if cmd == 13407 {
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(cmd, proto.Message{"code": 1, "type": treasureShopType, "order": order, "num": 1})
	}
	view.RID, view.Expires, view.Refreshes = role.RID, next.Expires, next.Refreshes
	return s.Send(cmd, next.message(h5))
}

func init() {
	registerAdminModule(AdminModule{Key: treasureShopKey, Title: "探宝商城", Window: "13403/13405/13407", Scope: "role", New: adminState[treasureShopState]})
}
