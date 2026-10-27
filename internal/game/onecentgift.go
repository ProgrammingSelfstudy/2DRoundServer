package game

import (
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	oneCentModule  = "onecentgift"
	oneCentCharge  = 301 // the 1 yuan gift
	oneCentPeriod1 = 1
)

// oneCentRewards are the rewards of the first period of the 1 yuan gift (Config.HolidayDimeData.data_award_list[1]): the first
// is paid at once, the others when the role's power reaches the number. The other periods run on dates of 2021 and are not used.
var oneCentRewards = func() []struct {
	ID, Power int64
	Items     [][2]int64
} {
	var f struct {
		OC []struct {
			ID, Power int64
			Items     [][2]int64
		} `json:"onecent"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.OC) != 4 {
		panic("game: bad charge.json onecent")
	}
	return f.OC
}()

type oneCentState struct {
	Bought bool
	Taken  []int64
}

// status is a reward's state: 0 not reached, 1 waits, 2 taken.
func (d *oneCentState) status(i int, power int64) int64 {
	r := oneCentRewards[i]
	switch {
	case has(d.Taken, r.ID):
		return 2
	case d.Bought && power >= r.Power:
		return 1
	}
	return 0
}

func (d *oneCentState) window(power int64) proto.Message {
	list := make([]proto.Message, len(oneCentRewards))
	for i, r := range oneCentRewards {
		list[i] = proto.Message{"id": r.ID, "finish": d.status(i, power)}
	}
	return proto.Message{"period": oneCentPeriod1, "is_buy": b2i(d.Bought), "end_time": 0, "list": list}
}

func init() {
	RegisterChargeGuard(oneCentCharge, func(st *store.Store, role *store.Role) error {
		d := &oneCentState{}
		if _, err := st.LoadState(role.RID, oneCentModule, d); err != nil {
			return err
		}
		if d.Bought {
			return errNotPayable("bought already")
		}
		return nil
	})
	RegisterChargeHook(oneCentCharge, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
		d := &oneCentState{}
		if _, err := st.LoadState(role.RID, oneCentModule, d); err != nil {
			return nil, nil, err
		}
		if d.Bought {
			return nil, nil, errNotPayable("bought already")
		}
		d.Bought = true
		return map[int64]int64{}, map[int64]int64{}, st.SaveState(role.RID, oneCentModule, d)
	})
}

// RegisterOneCentGift registers the 1 yuan gift: 16651 the page, 16652 take a reward. The gift itself is bought as a top-up order
// (product 301); its rewards come at once or when the power is reached.
func RegisterOneCentGift(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *oneCentState, int64, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, 0, nil
		}
		d := &oneCentState{}
		if _, err := st.LoadState(role.RID, oneCentModule, d); err != nil {
			return nil, nil, 0, err
		}
		heroes, err := st.PartnersOf(role.RID)
		if err != nil {
			return nil, nil, 0, err
		}
		return role, d, rolePower(heroes), nil
	}
	r.Handle(16651, func(s *gateway.Session, _ proto.Message) error {
		role, d, power, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(16651, d.window(power))
	})
	r.Handle(16652, func(s *gateway.Session, req proto.Message) error {
		role, d, power, err := load(s)
		if role == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(16652, proto.Message{"code": 0, "msg": msg}) }
		idx := -1
		for i, rw := range oneCentRewards {
			if rw.ID == req.Int("id") {
				idx = i
			}
		}
		switch {
		case idx < 0:
			return refuse("奖励不存在")
		case !d.Bought:
			return refuse("还没有购买")
		case d.status(idx, power) == 2:
			return refuse("已经领取过了")
		case d.status(idx, power) == 0:
			return refuse("战力不足")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range oneCentRewards[idx].Items {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Taken = append(d.Taken, oneCentRewards[idx].ID)
		if err := st.SaveState(role.RID, oneCentModule, d); err != nil {
			return err
		}
		if err := s.Send(16652, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(16651, d.window(power)); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
