package game

import (
	_ "embed"
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// stoneshop.json: the mysterious store (神秘商店, 16688, 16689) of the client (tools/extract_stoneshop.py).
//
//go:embed data/stoneshop.json
var stoneShopJSON []byte

const (
	ssModule = "stoneshop"
	ssBID    = 993028 // ActionRankCommonType.mysterious_store
	ssPerDay = 1      // ActonExchangeType.Perday
	ssAll    = 3      // ActonExchangeType.Activity: a limit for the whole activity
)

var ssCfg = func() (c struct {
	Item  int64
	Goods []struct {
		ID, SubType, Day, All int64
		Expend, Award         [][2]int64
	}
}) {
	if err := json.Unmarshal(stoneShopJSON, &c); err != nil || len(c.Goods) == 0 {
		panic("game: bad stoneshop.json")
	}
	return
}()

type ssBought struct{ Day, All int64 }

type ssRole struct {
	Start int64
	Date  int64
	Buy   map[int64]*ssBought
}

// RegisterStoneShop registers the mysterious store: an operator opens the activity 993028; goods are bought with one
// item, with a limit for each day or for the whole activity.
func RegisterStoneShop(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *ssRole, bool, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, false, nil
		}
		d := &ssRole{}
		e, open := openByBID(st, role.SrvID, ssBID, s.Now())
		if !open {
			return role, d, false, nil
		}
		if _, err := st.LoadState(role.RID, ssModule, d); err != nil {
			return nil, nil, false, err
		}
		if d.Start != e.Row.StartAt || d.Buy == nil {
			*d = ssRole{Start: e.Row.StartAt, Buy: map[int64]*ssBought{}}
		}
		if day := dayKey(s.Now()); d.Date != day {
			d.Date = day
			for _, b := range d.Buy {
				b.Day = 0
			}
		}
		return role, d, true, nil
	}
	info := func(d *ssRole) []proto.Message {
		out := []proto.Message{}
		for _, g := range ssCfg.Goods {
			b := d.Buy[g.ID]
			if b == nil {
				b = &ssBought{}
			}
			out = append(out, proto.Message{"id": g.ID, "day_num": b.Day, "all_num": b.All, "s_day_num": 0, "s_all_num": 0})
		}
		return out
	}
	r.Handle(16688, func(s *gateway.Session, _ proto.Message) error {
		role, d, _, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(16688, proto.Message{"buy_info": info(d)})
	})
	r.Handle(16689, func(s *gateway.Session, req proto.Message) error {
		role, d, open, err := load(s)
		if role == nil {
			return err
		}
		id, num := req.Int("id"), max(1, req.Int("num"))
		refuse := func(msg string) error {
			return s.Send(16689, proto.Message{"code": 0, "msg": msg, "id": id, "num": num})
		}
		idx := -1
		for i, g := range ssCfg.Goods {
			if g.ID == id {
				idx = i
			}
		}
		if !open {
			return refuse("活动未开启")
		}
		if idx < 0 {
			return refuse("兑换不存在")
		}
		g := ssCfg.Goods[idx]
		b := d.Buy[id]
		if b == nil {
			b = &ssBought{}
		}
		switch {
		case g.SubType == ssPerDay && b.Day+num > g.Day, g.SubType == ssAll && b.All+num > g.All:
			return refuse("兑换次数不足")
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		for _, p := range g.Expend {
			splitGains([]sellGain{{p[0], p[1] * num}}, 1, spendA, spendI)
		}
		for _, p := range g.Award {
			splitGains([]sellGain{{p[0], p[1] * num}}, 1, gainA, gainI)
		}
		if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("兑换材料不足")
			}
			return err
		}
		b.Day += num
		b.All += num
		d.Buy[id] = b
		if err := st.SaveState(role.RID, ssModule, d); err != nil {
			return err
		}
		if err := s.Send(16689, proto.Message{"code": 1, "msg": "兑换成功", "id": id, "num": num}); err != nil {
			return err
		}
		if err := s.Send(16688, proto.Message{"buy_info": info(d)}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
