package game

import (
	"encoding/json"
	"errors"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// varietyShopType is MallConst.MallType.VarietyShop, the tavern's random shop (杂货店).
const varietyShopType = 4

// varietyFreeRefresh is the free refreshes a role has a day (free_count 5 in the capture).
const varietyFreeRefresh = 5

// varietyGood is one good of the tavern shop as the original server listed it.
type varietyGood struct {
	item, num, price, payType, discount, discountType, score int64
	attr                                                     [][2]int64 // main_attr (attr_type, val) of an equipment piece
}

// pay is what the good costs: the discounted figure when it has a discount.
func (g varietyGood) pay() int64 {
	if g.discountType > 0 {
		return g.discount
	}
	return g.price
}

// varietyPool is every good we saw the original server deal for the tavern shop: the six
// of the capture of 2026-10-21b (13403) and the six after the first refresh (13405). The
// original deals a random list from a pool we do not know, so ours is these twelve; the
// first six are what a new role sees. payType 1 is coins (asset 1), 3 diamonds (asset 3).
var varietyPool = []varietyGood{
	{10001, 1000, 1000, 3, 500, 5, 0, nil},
	{10001, 300, 300, 3, 210, 7, 0, nil},
	{10001, 100, 100, 3, 0, 0, 0, nil},
	{10403, 1, 250, 3, 150, 6, 0, nil},
	{10103, 1, 100000, 1, 10000, 1, 0, nil},
	{10451, 1, 200, 3, 0, 0, 0, nil},
	{40111, 1, 2085, 3, 1875, 9, 1774, [][2]int64{{101, 887}}},
	{37001, 10, 500, 3, 300, 6, 0, nil},
	{40111, 1, 18750000, 1, 0, 0, 1774, [][2]int64{{101, 887}}},
	{10401, 1, 200000, 1, 20000, 1, 0, nil},
	{26802, 30, 600, 3, 0, 0, 0, nil},
	{25804, 30, 3000000, 1, 1800000, 6, 0, nil},
}

// varietyShelf is the six goods on the shelf: the pool index and whether it was bought.
type varietyShelf []struct {
	G int  `json:"g"`
	B bool `json:"b,omitempty"`
}

func (v varietyShelf) encode() string { b, _ := json.Marshal(v); return string(b) }

// varietyFirstShelf is the shelf of a new day: the captured first list.
func varietyFirstShelf() varietyShelf {
	v := make(varietyShelf, 6)
	for i := range v {
		v[i].G = i
	}
	return v
}

// varietyDeal is a fresh random shelf: six different goods of the pool.
func varietyDeal() varietyShelf {
	idx := make([]int, len(varietyPool))
	for i := range idx {
		idx[i] = i
	}
	v := make(varietyShelf, 6)
	for i := range v {
		j := i + int(summonRand()*float64(len(idx)-i))%(len(idx)-i)
		idx[i], idx[j] = idx[j], idx[i]
		v[i].G = idx[i]
	}
	return v
}

// varietyLoad is the role's shop for now: a new day (the free-draw day, which turns at
// 05:00 China time) gives the free refreshes back and the first shelf again.
func varietyLoad(st *store.Store, rid int64, now time.Time) (*store.VarietyShop, varietyShelf, error) {
	day := summonDay(now)
	v, err := st.VarietyShop(rid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	if err != nil || v.Day != day {
		v = &store.VarietyShop{Day: day, FreeLeft: varietyFreeRefresh, Goods: varietyFirstShelf().encode()}
		if err := st.SaveVarietyShop(rid, *v); err != nil {
			return nil, nil, err
		}
	}
	var shelf varietyShelf
	if err := json.Unmarshal([]byte(v.Goods), &shelf); err != nil {
		return nil, nil, err
	}
	return v, shelf, nil
}

// varietyReply is the answer of 13403 and 13405 for the shop: the shelf and the free
// refreshes left.
func varietyReply(free int64, shelf varietyShelf) proto.Message {
	list := make([]proto.Message, 0, len(shelf))
	for i, e := range shelf {
		g := varietyPool[e.G]
		attr := []proto.Message{}
		for _, a := range g.attr {
			attr = append(attr, proto.Message{"attr_type": a[0], "val": a[1]})
		}
		bought := 0
		if e.B {
			bought = 1
		}
		list = append(list, proto.Message{
			"discount": g.discount, "discount_type": g.discountType, "has_buy": bought, "item_desc": "",
			"item_id": g.item, "item_num": g.num, "limit_count": 1, "limit_day": 0, "limit_month": 0,
			"limit_week": 0, "main_attr": attr, "order": i + 1, "pay_type": g.payType,
			"pay_type2": 0, "price": g.price, "price2": 0, "score": g.score, "type": 3,
		})
	}
	return proto.Message{"type": varietyShopType, "code": 1, "msg": "", "count": 0, "free_count": free,
		"num": len(list), "refresh_time": 0, "item_list": list}
}

// varietyShow answers 13403 (open) or 13405 (refresh) for the tavern shop (type 4).
func varietyShow(st *store.Store, cmd uint16, refresh bool) func(*gateway.Session, proto.Message) error {
	return func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		v, shelf, err := varietyLoad(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		if refresh {
			if v.FreeLeft <= 0 {
				return s.Send(cmd, proto.Message{"type": varietyShopType, "code": 0, "msg": "刷新次数不足", "count": 0,
					"free_count": 0, "num": 0, "refresh_time": 0, "item_list": []proto.Message{}})
			}
			v.FreeLeft--
			shelf = varietyDeal()
			v.Goods = shelf.encode()
			if err := st.SaveVarietyShop(role.RID, *v); err != nil {
				return err
			}
		}
		return s.Send(cmd, varietyReply(v.FreeLeft, shelf))
	}
}

// varietyBuy answers 13407 (buy the good at an order) for the tavern shop (type 4).
func varietyBuy(st *store.Store) func(*gateway.Session, proto.Message) error {
	return func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		order := int(req.Int("order"))
		reply := func(code int, msg string) error {
			return s.Send(13407, proto.Message{"code": code, "msg": msg, "num": 1, "order": order, "type": varietyShopType})
		}
		v, shelf, err := varietyLoad(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		if order < 1 || order > len(shelf) {
			return reply(0, "商品不存在")
		}
		e := shelf[order-1]
		g := varietyPool[e.G]
		pay := map[int64]int64{1: 1, 3: 3}[g.payType] // coins and diamonds
		switch {
		case e.B:
			return reply(0, "已购买")
		case pay == 0 || !ItemKnown(g.item):
			return reply(0, "商品不存在")
		}
		spendA, spendI, gainA, gainI := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{pay, g.pay()}}, 1, spendA, spendI)
		splitGains([]sellGain{{g.item, g.num}}, 1, gainA, gainI)
		next := append(varietyShelf(nil), shelf...)
		next[order-1].B = true
		if err := st.VarietyBuy(role.RID, v.Goods, next.encode(), spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "货币不足")
			}
			return err
		}
		// the original's order: the assets, the item, then the answer with an empty msg
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return reply(1, "")
	}
}
