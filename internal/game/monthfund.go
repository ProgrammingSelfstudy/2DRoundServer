package game

import (
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	monthFundModule = "monthfund"
	monthFundDays   = 30
)

// monthFunds are the two funds of the fund window (Config.MonthFundData): bought with the top-up product charge, they pay the
// reward of the day for 30 days. The client does not say which reward group a fund uses; the one with the diamonds that fit the
// price is taken (6800 in 128 yuan, 13600 in 328 yuan).
var monthFunds = func() []struct {
	ID, ChargeID, Group int64
} {
	var f struct {
		MF struct {
			Funds []struct {
				ID       int64
				ChargeID int64 `json:"charge_id"`
			}
			Award [][][][2]int64
		} `json:"month_fund"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.MF.Funds) != 2 || len(f.MF.Award) != 8 {
		panic("game: bad charge.json month_fund")
	}
	monthFundAward = f.MF.Award
	groups := []int64{1, 3}
	out := make([]struct{ ID, ChargeID, Group int64 }, 2)
	for i, fd := range f.MF.Funds {
		out[i].ID, out[i].ChargeID, out[i].Group = fd.ID, fd.ChargeID, groups[i]
	}
	return out
}()

var monthFundAward [][][][2]int64 // by group (1-based), by day (1-based), the rewards

type monthFundState struct {
	Start [2]int64 // day number of the day it was bought, 0 = never
	Last  [2]int64 // day number of the last day taken
}

// view is the fund as the window shows it: status 0 not bought, 1 the day's reward waits, 2 it was taken; day is the day of the run.
func (d *monthFundState) view(i int, today int64) (status, day int64) {
	if d.Start[i] == 0 || today >= d.Start[i]+monthFundDays {
		return 0, 0
	}
	day = today - d.Start[i] + 1
	if d.Last[i] == today {
		return 2, day
	}
	return 1, day
}

func monthFundIndex(id int64) int {
	for i, f := range monthFunds {
		if f.ID == id {
			return i
		}
	}
	return -1
}

func monthFundMessage(d *monthFundState, i int, today int64) proto.Message {
	status, day := d.view(i, today)
	end := int64(0)
	if status != 0 {
		end = (d.Start[i]+monthFundDays)*86400 - 8*3600
	}
	return proto.Message{"id": monthFunds[i].ID, "current_day": day, "group_id": monthFunds[i].Group, "endtime": end, "status": status}
}

func init() {
	for i, f := range monthFunds {
		i, f := i, f
		RegisterChargeGuard(f.ChargeID, func(st *store.Store, role *store.Role) error {
			d := &monthFundState{}
			if _, err := st.LoadState(role.RID, monthFundModule, d); err != nil {
				return err
			}
			if status, _ := d.view(i, dayNo(chargeClock())); status != 0 {
				return ErrFundActive
			}
			return nil
		})
		RegisterChargeHook(f.ChargeID, func(st *store.Store, role *store.Role, p ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			d := &monthFundState{}
			if _, err := st.LoadState(role.RID, monthFundModule, d); err != nil {
				return nil, nil, err
			}
			today := dayNo(chargeClock())
			if status, _ := d.view(i, today); status != 0 {
				return nil, nil, ErrFundActive
			}
			d.Start[i], d.Last[i] = today, 0
			if err := st.SaveState(role.RID, monthFundModule, d); err != nil {
				return nil, nil, err
			}
			return map[int64]int64{3: p.VIPExp}, map[int64]int64{}, nil // "the diamonds and the VIP experience of the price, at once"
		})
	}
}

// ErrFundActive is the answer to a fund that is running; it is a kind of ErrNotPayable.
var ErrFundActive = errNotPayable("the fund is running")

// RegisterMonthFund registers the fund window: 24700 the funds that are open, 24701 one fund's state, 24702 take the day's reward.
// A fund is bought as a top-up order; when the operator confirms it the 30 days start that day.
func RegisterMonthFund(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *monthFundState, int64, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, 0, nil
		}
		d := &monthFundState{}
		if _, err := st.LoadState(role.RID, monthFundModule, d); err != nil {
			return nil, nil, 0, err
		}
		return role, d, dayNo(s.Now()), nil
	}
	r.Handle(24700, func(s *gateway.Session, _ proto.Message) error {
		role, d, today, err := load(s)
		if role == nil {
			return err
		}
		ids := []proto.Message{}
		for i, f := range monthFunds {
			status, _ := d.view(i, today)
			ids = append(ids, proto.Message{"id": f.ID, "status": status, "show": 1})
		}
		return s.Send(24700, proto.Message{"ids": ids})
	})
	r.Handle(24701, func(s *gateway.Session, req proto.Message) error {
		role, d, today, err := load(s)
		if role == nil {
			return err
		}
		i := monthFundIndex(req.Int("id"))
		if i < 0 {
			return nil
		}
		return s.Send(24701, monthFundMessage(d, i, today))
	})
	r.Handle(24702, func(s *gateway.Session, req proto.Message) error {
		role, d, today, err := load(s)
		if role == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(24702, proto.Message{"code": 0, "msg": msg}) }
		i := monthFundIndex(req.Int("id"))
		if i < 0 {
			return refuse("基金不存在")
		}
		status, day := d.view(i, today)
		switch status {
		case 0:
			return refuse("还没有购买基金")
		case 2:
			return refuse("今天已经领取过了")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range monthFundAward[monthFunds[i].Group-1][day-1] {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Last[i] = today
		if err := st.SaveState(role.RID, monthFundModule, d); err != nil {
			return err
		}
		if err := s.Send(24702, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(24701, monthFundMessage(d, i, today)); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
