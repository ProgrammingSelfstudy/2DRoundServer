package game

import (
	"encoding/json"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const dayChargeModule = "daycharge"

// dayCharge is the daily first top-up (Config.ChargeData.data_daily_reward): paying Goal diamonds in a day (VIP experience
// counts as the diamonds of the payment) gives a reward that depends on the days since the server opened.
var dayCharge = func() (d struct {
	Goal    int64
	Rewards []struct {
		Min, Max int64
		Items    [][2]int64
	}
}) {
	var f struct {
		Daily struct {
			Goal    int64
			Rewards []struct {
				Min, Max int64
				Items    [][2]int64
			}
		} `json:"daily"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || f.Daily.Goal == 0 || len(f.Daily.Rewards) != 7 {
		panic("game: bad charge.json daily")
	}
	d.Goal, d.Rewards = f.Daily.Goal, f.Daily.Rewards
	return d
}()

type dayChargeState struct {
	Day   int64 // day number the counts belong to
	Num   int64 // diamonds paid that day
	Taken bool
}

func (d *dayChargeState) roll(today int64) {
	if d.Day != today {
		d.Day, d.Num, d.Taken = today, 0, false
	}
}

// status: 0 not reached, 1 the reward waits, 2 taken.
func (d *dayChargeState) status() int64 {
	switch {
	case d.Taken:
		return 2
	case d.Num >= dayCharge.Goal:
		return 1
	}
	return 0
}

func init() {
	chargePaidHooks = append(chargePaidHooks, func(st *store.Store, role *store.Role, p ChargeProduct) error {
		d := &dayChargeState{}
		if _, err := st.LoadState(role.RID, dayChargeModule, d); err != nil {
			return err
		}
		d.roll(dayNo(chargeClock()))
		d.Num += p.VIPExp
		return st.SaveState(role.RID, dayChargeModule, d)
	})
}

// RegisterDayCharge registers the daily first top-up: 21010 the state, 21011 take the reward.
func RegisterDayCharge(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *dayChargeState, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil
		}
		d := &dayChargeState{}
		if _, err := st.LoadState(role.RID, dayChargeModule, d); err != nil {
			return nil, nil, err
		}
		d.roll(dayNo(s.Now()))
		return role, d, nil
	}
	r.Handle(21010, func(s *gateway.Session, _ proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(21010, proto.Message{"status": d.status(), "num": d.Num})
	})
	r.Handle(21011, func(s *gateway.Session, _ proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		switch d.status() {
		case 0:
			return s.Send(21011, proto.Message{"code": 0, "msg": "今日充值还没有达到"})
		case 2:
			return s.Send(21011, proto.Message{"code": 0, "msg": "今天已经领取过了"})
		}
		srv, err := st.Server(role.SrvID)
		if err != nil {
			return err
		}
		at := srv.OpenTime
		if at == 0 {
			at = srv.CreatedAt
		}
		day := openDay(time.Unix(at, 0), s.Now())
		ga, gi := map[int64]int64{}, map[int64]int64{}
		rw := dayCharge.Rewards[0] // a day before the first range takes the first one
		for _, r := range dayCharge.Rewards {
			if r.Min <= day {
				rw = r
			}
		}
		for _, it := range rw.Items {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Taken = true
		if err := st.SaveState(role.RID, dayChargeModule, d); err != nil {
			return err
		}
		if err := s.Send(21011, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(21010, proto.Message{"status": 2, "num": d.Num}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
