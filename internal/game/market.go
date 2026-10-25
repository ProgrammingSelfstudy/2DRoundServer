package game

import (
	_ "embed"
	"encoding/json"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// market_gold.json: the gold (coin) market of the client (tools/extract_market_gold.py).
//
//go:embed data/market_gold.json
var marketGoldJSON []byte

type marketGood struct {
	BaseID    int64 `json:"base_id"`
	Catalg    int   `json:"catalg"`
	Price     int64 `json:"price"`
	LimitNum  int64 `json:"limit_num"`
	LimitType int   `json:"limit_type"` // 1 per day, 2 per week (the client's tips)
	OpenLev   int64 `json:"open_lev"`
	Sort      int   `json:"sort"`
}

var marketCfg = func() struct {
	ItemSale float64 `json:"item_sale"`
	Goods    []marketGood
	Hide     []struct{ ID, OpenLev, Price int64 } `json:"hide"`
	byBase   map[int64]marketGood
	hide     map[int64]struct{ OpenLev, Price int64 }
} {
	var c struct {
		ItemSale float64 `json:"item_sale"`
		Goods    []marketGood
		Hide     []struct{ ID, OpenLev, Price int64 } `json:"hide"`
		byBase   map[int64]marketGood
		hide     map[int64]struct{ OpenLev, Price int64 }
	}
	if err := json.Unmarshal(marketGoldJSON, &c); err != nil || len(c.Goods) == 0 || c.ItemSale <= 0 {
		panic("game: bad market_gold.json")
	}
	c.byBase = map[int64]marketGood{}
	c.hide = map[int64]struct{ OpenLev, Price int64 }{}
	for _, g := range c.Goods {
		c.byBase[g.BaseID] = g
	}
	for _, h := range c.Hide {
		c.hide[h.ID] = struct{ OpenLev, Price int64 }{h.OpenLev, h.Price}
	}
	sort.SliceStable(c.Goods, func(i, j int) bool { return c.Goods[i].Sort < c.Goods[j].Sort })
	return c
}()

// marketKey is the shop_buys row of a market good (negative, so it never meets an exchange shop good).
func marketKey(base int64) int64 { return -base }

// marketWindow is the days a good's purchase limit counts: today, or this Monday to Sunday.
func marketWindow(g marketGood, s *gateway.Session) (from, to int64) {
	shop := shopGood{}
	if g.LimitType == 2 {
		shop.Week = g.LimitNum
	} else {
		shop.Day = g.LimitNum
	}
	from, to, _ = shop.limitWindow(s.Now())
	return
}

// RegisterMarket registers the gold market: 23500 the goods of a tab, 23501 buy, 23502 sell back,
// 23516 the price of items, 23520 the purchase counts. Our rules, not captured: prices never move (the
// client's text says they move with trading and reset at 0:00), the stock is unlimited, and selling a market
// good pays 0.9 of its price, as the client's text says.
func RegisterMarket(r *gateway.Router, st *store.Store) {
	goodMessage := func(g marketGood) proto.Message {
		return proto.Message{"id": g.BaseID, "base_id": g.BaseID, "cur_price": g.Price, "margin": 1000}
	}
	list := func(role *store.Role, catalg int64) []proto.Message {
		out := []proto.Message{}
		for _, g := range marketCfg.Goods {
			if (catalg == 0 || int64(g.Catalg) == catalg) && int64(role.Lev) >= g.OpenLev {
				out = append(out, goodMessage(g))
			}
		}
		return out
	}
	for _, cmd := range []uint16{23500, 23518} {
		cmd := cmd
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			if s.Role() == nil {
				return nil
			}
			return s.Send(cmd, proto.Message{"catalg": req.Int("catalg"), "goods": list(s.Role(), req.Int("catalg"))})
		})
	}
	r.Handle(23520, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		out := []proto.Message{}
		for _, g := range marketCfg.Goods {
			if g.LimitNum == 0 {
				continue
			}
			from, to := marketWindow(g, s)
			bought, err := st.ShopBought(role.RID, from, to)
			if err != nil {
				return err
			}
			if n := bought[marketKey(g.BaseID)]; n > 0 {
				out = append(out, proto.Message{"item_id": g.BaseID, "count": n})
			}
		}
		return s.Send(23520, proto.Message{"limit_data": out})
	})
	r.Handle(23516, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		out := []proto.Message{}
		for _, b := range req.Arr("base_ids") {
			if g, ok := marketCfg.byBase[b.Int("base_id")]; ok {
				out = append(out, proto.Message{"source": 2, "base_id": g.BaseID, "assets": 0, "price": g.Price})
			}
		}
		return s.Send(23516, proto.Message{"market_price": out})
	})
	r.Handle(23501, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		g, ok := marketCfg.byBase[req.Int("base_id")]
		num := req.Int("num")
		if !ok || num <= 0 || int64(role.Lev) < g.OpenLev {
			return s.Send(23501, proto.Message{"flag": 0})
		}
		from, to := marketWindow(g, s)
		err := st.ShopBuy(role.RID, marketKey(g.BaseID), dayKey(s.Now()), num, from, to, g.LimitNum, map[int64]int64{1: g.Price * num}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{g.BaseID: num})
		if err != nil {
			if err == store.ErrConflict {
				return s.Send(23501, proto.Message{"flag": 0})
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(23501, proto.Message{"flag": 1})
	})
	r.Handle(23502, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		base, num := req.Int("id"), req.Int("num")
		var unit int64
		if g, ok := marketCfg.byBase[base]; ok && int64(role.Lev) >= g.OpenLev {
			unit = int64(float64(g.Price) * marketCfg.ItemSale)
		} else if h, ok := marketCfg.hide[base]; ok && int64(role.Lev) >= h.OpenLev {
			unit = h.Price
		}
		if unit <= 0 || num <= 0 {
			return s.Send(23502, proto.Message{"flag": 0})
		}
		err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{base: num}, map[int64]int64{1: unit * num}, map[int64]int64{})
		if err != nil {
			if err == store.ErrConflict {
				return s.Send(23502, proto.Message{"flag": 0})
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(23502, proto.Message{"flag": 1})
	})
}
