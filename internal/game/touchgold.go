package game

import (
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const touchGoldModule = "touchgold"

// touchGoldRows are the three Midas-touch buttons (Config.ConvertData.data_exchange[0]): the diamonds each costs, how many minutes
// of the role's hook income it pays and the coins it pays at least; each can be used once per refresh. The refreshes are at
// 0:00, 9:00 and 19:00 (the client's touch_gold_tips). The original raised the coins with the VIP level; that is not done.
var touchGoldRows = [3]struct{ ID, Price, Minutes, MinCoin int64 }{{1, 0, 60, 10000}, {2, 20, 120, 20000}, {3, 50, 300, 50000}}

const touchGoldCoin = 1 // the coin asset in the hook income

var touchGoldHours = [3]int{0, 9, 19}

// touchGoldPeriod numbers the refresh period that holds t and gives the time the next one starts.
func touchGoldPeriod(t time.Time) (period, next int64) {
	n := t.In(dayZone)
	day := dayStart(n)
	slot := 0
	for i, h := range touchGoldHours {
		if n.Hour() >= h {
			slot = i
		}
	}
	period = dayNo(t)*3 + int64(slot)
	if slot == 2 {
		return period, day.AddDate(0, 0, 1).Unix()
	}
	return period, day.Add(time.Duration(touchGoldHours[slot+1]) * time.Hour).Unix()
}

type touchGoldState struct {
	Period int64
	Used   map[int64]bool
}

// RegisterTouchGold registers the Midas touch: 23606 the three buttons, 23607 use one.
func RegisterTouchGold(r *gateway.Router, st *store.Store) {
	gainOf := func(rid, minutes, minCoin int64) (int64, error) {
		d, err := dramaState(st, rid)
		if err != nil {
			return 0, err
		}
		coin := int64(0)
		for _, g := range minutesOf(hookRate(d), minutes) {
			if g.ID == touchGoldCoin {
				coin += g.Amount
			}
		}
		return max(coin, minCoin), nil
	}
	load := func(s *gateway.Session) (*store.Role, *touchGoldState, int64, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, 0, nil
		}
		d := &touchGoldState{}
		if _, err := st.LoadState(role.RID, touchGoldModule, d); err != nil {
			return nil, nil, 0, err
		}
		period, next := touchGoldPeriod(s.Now())
		if d.Period != period {
			d.Period, d.Used = period, map[int64]bool{}
		}
		if d.Used == nil {
			d.Used = map[int64]bool{}
		}
		return role, d, next, nil
	}
	window := func(role *store.Role, d *touchGoldState, next int64) (proto.Message, error) {
		list := []proto.Message{}
		for _, row := range touchGoldRows {
			gain, err := gainOf(role.RID, row.Minutes, row.MinCoin)
			if err != nil {
				return nil, err
			}
			list = append(list, proto.Message{"id": row.ID, "price": row.Price, "gain": gain, "num": b2i(d.Used[row.ID]), "max": 1})
		}
		return proto.Message{"ref_time": next, "camp_type": 0, "list": list}, nil
	}
	r.Handle(23606, func(s *gateway.Session, _ proto.Message) error {
		role, d, next, err := load(s)
		if role == nil {
			return err
		}
		m, err := window(role, d, next)
		if err != nil {
			return err
		}
		return s.Send(23606, m)
	})
	r.Handle(23607, func(s *gateway.Session, req proto.Message) error {
		role, d, next, err := load(s)
		if role == nil {
			return err
		}
		id := req.Int("id")
		refuse := func(msg string) error { return s.Send(23607, proto.Message{"code": 0, "msg": msg, "id": id}) }
		idx := -1
		for i, row := range touchGoldRows {
			if row.ID == id {
				idx = i
			}
		}
		switch {
		case idx < 0:
			return refuse("点金不存在")
		case d.Used[id]:
			return refuse("这一档已经点过了")
		}
		row := touchGoldRows[idx]
		gain, err := gainOf(role.RID, row.Minutes, row.MinCoin)
		if err != nil {
			return err
		}
		spend := map[int64]int64{}
		if row.Price > 0 {
			spend[3] = row.Price
		}
		if err := st.Exchange(role.RID, spend, map[int64]int64{}, map[int64]int64{touchGoldCoin: gain}, map[int64]int64{}); err != nil {
			if err == store.ErrConflict {
				return refuse("钻石不足")
			}
			return err
		}
		d.Used[id] = true
		if err := st.SaveState(role.RID, touchGoldModule, d); err != nil {
			return err
		}
		if err := s.Send(23607, proto.Message{"code": 1, "msg": "点金成功", "id": id}); err != nil {
			return err
		}
		m, err := window(role, d, next)
		if err != nil {
			return err
		}
		if err := s.Send(23606, m); err != nil {
			return err
		}
		return sendAssets(s, st, role.RID)
	})
}
