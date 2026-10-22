package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// shops.json holds the client's exchange shop goods (tools/extract_shops.py).
//
//go:embed data/shops.json
var shopsJSON []byte

type shopGood struct {
	Type  int64 `json:"type"` // the shop (MallConst.MallType)
	Item  int64 `json:"item"`
	Num   int64 `json:"num"`
	Pay   any   `json:"pay"` // an asset label name, or an item id
	Price int64 `json:"price"`
	Day   int64 `json:"day"` // limits per day / week / month; 0 = none, 9999 = unlimited
	Week  int64 `json:"week"`
	Month int64 `json:"month"`
}

var shopGoods = func() map[int64]shopGood {
	var f struct {
		Goods map[int64]shopGood `json:"goods"`
	}
	if err := json.Unmarshal(shopsJSON, &f); err != nil || len(f.Goods) == 0 {
		panic("game: bad shops.json")
	}
	return f.Goods
}()

// payID resolves a good's payment to an id for the ledgers: an asset label
// name becomes its asset id, a number is an item id.
func (g shopGood) payID() (int64, bool) {
	switch v := g.Pay.(type) {
	case string:
		return AssetLabelID(v)
	case float64:
		return int64(v), true
	}
	return 0, false
}

// limitWindow is the days (YYYYMMDD, inclusive) a good's purchase limit counts
// over today, and the limit. Our reading: a daily limit below 9999 counts today,
// else a weekly one counts this week (Monday to Sunday), else a monthly one this
// calendar month; no limit counts the role's whole history (from 0) for display.
func (g shopGood) limitWindow(now time.Time) (from, to, limit int64) {
	t := now.In(dayZone)
	today := dayKey(now)
	switch {
	case g.Day > 0 && g.Day < 9999:
		return today, today, g.Day
	case g.Week > 0:
		mon := t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
		return dayKey(mon), dayKey(mon.AddDate(0, 0, 6)), g.Week
	case g.Month > 0:
		first := time.Date(t.Year(), t.Month(), 1, 12, 0, 0, 0, dayZone)
		return dayKey(first), dayKey(first.AddDate(0, 1, -1)), g.Month
	}
	return 0, today, 0
}

// RegisterShop registers the exchange shops: 13401 (what the role bought of a
// shop) and 13402 (buy). The client holds the goods and prices itself and only
// asks the server for the purchase counts, which come in `ext` key 1.
func RegisterShop(r *gateway.Router, st *store.Store) {
	boughtExt := func(n int64) []proto.Message { return []proto.Message{{"key": 1, "val": n}} }
	r.Handle(13401, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ := req.Int("type")
		list := []proto.Message{}
		for _, eid := range sortedShopIDs() {
			g := shopGoods[eid]
			if g.Type != typ {
				continue
			}
			from, to, _ := g.limitWindow(s.Now())
			got, err := st.ShopBought(role.RID, from, to)
			if err != nil {
				return err
			}
			if got[eid] > 0 {
				list = append(list, proto.Message{"item_id": eid, "ext": boughtExt(got[eid])})
			}
		}
		return s.Send(13401, proto.Message{"code": 1, "msg": "", "type": typ, "is_half": 0, "item_list": list})
	})
	// 13403 / 13405 belong to the shops that deal a random, refreshing list. The tavern shop
	// (VarietyShop, type 4) answers with the one list we captured; the others (the recovery
	// shop, the skill shop) we have no goods for, so they are empty and refreshing says so.
	// Without an answer the tab kept showing the list of the page before it. refresh_time is
	// an hour on, so the client does not ask again at once.
	r.Handle(13403, func(s *gateway.Session, req proto.Message) error {
		if req.Int("type") == varietyShopType {
			return varietyShow(st, 13403, false)(s, req)
		}
		return s.Send(13403, proto.Message{"type": req.Int("type"), "code": 1, "msg": "", "count": 0, "free_count": 0, "num": 0,
			"refresh_time": s.Now().Unix() + 3600, "item_list": []proto.Message{}})
	})
	r.Handle(13405, func(s *gateway.Session, req proto.Message) error {
		if req.Int("type") == varietyShopType {
			return varietyShow(st, 13405, true)(s, req)
		}
		return s.Send(13405, proto.Message{"type": req.Int("type"), "code": 0, "msg": "该商店暂未开放", "count": 0, "free_count": 0, "num": 0,
			"refresh_time": 0, "item_list": []proto.Message{}})
	})
	r.Handle(13407, func(s *gateway.Session, req proto.Message) error {
		if req.Int("type") == varietyShopType {
			return varietyBuy(st)(s, req)
		}
		return s.Send(13407, proto.Message{"code": 0, "msg": "该商店暂未开放", "num": req.Int("num"), "order": req.Int("order"), "type": req.Int("type")})
	})
	r.Handle(13402, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		eid, num := req.Int("eid"), req.Int("num")
		g, ok := shopGoods[eid]
		reply := func(code int, msg string, bought int64) error {
			ext := boughtExt(bought)
			if g.Day == 0 && g.Week == 0 && g.Month == 0 { // the original sends no count for a good without a limit (capture 2026-10-21b)
				ext = []proto.Message{}
			}
			return s.Send(13402, proto.Message{"code": code, "type": g.Type, "eid": eid, "is_half": 0, "ext": ext, "msg": msg})
		}
		pay, payOK := g.payID()
		switch {
		case !ok || !payOK || !ItemKnown(g.Item):
			return reply(0, "商品不存在", 0)
		case num <= 0 || num > maxUseAtOnce:
			return reply(0, "购买数量不正确", 0)
		}
		from, to, limit := g.limitWindow(s.Now())
		spendA, spendI, gainA, gainI := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{pay, g.Price * num}}, 1, spendA, spendI)
		splitGains([]sellGain{{g.Item, g.Num * num}}, 1, gainA, gainI)
		err := st.ShopBuy(role.RID, eid, dayKey(s.Now()), num, from, to, limit, spendA, spendI, gainA, gainI)
		if err != nil {
			if !errors.Is(err, store.ErrConflict) {
				return err
			}
			got, _ := st.ShopBought(role.RID, from, to)
			return reply(0, "货币不足或已达购买上限", got[eid])
		}
		got, err := st.ShopBought(role.RID, from, to)
		if err != nil {
			return err
		}
		if err := reply(1, "", got[eid]); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}

func sortedShopIDs() []int64 {
	ids := make([]int64, 0, len(shopGoods))
	for id := range shopGoods {
		ids = append(ids, id)
	}
	sortInt64s(ids)
	return ids
}

func sortInt64s(a []int64) { sort.Slice(a, func(i, j int) bool { return a[i] < a[j] }) }
