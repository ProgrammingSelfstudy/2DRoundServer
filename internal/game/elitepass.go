package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// elitepass.json: the pass of the elite match (段位赛令牌活动) of the client (tools/extract_elitepass.py).
//
//go:embed data/elitepass.json
var elitePassJSON []byte

const elitePassModule = "elitepass"

// elitePassAdvanceCharge is the charge product of the advance card of the pass.
const elitePassAdvanceCharge = 909

type elitePassLev struct {
	Lev       int64      `json:"lev"`
	Wins      int64      `json:"wins"`
	Reward    [][2]int64 `json:"reward"`
	RmbReward [][2]int64 `json:"rmb_reward"`
}

var elitePassCfg = func() map[int64][]elitePassLev {
	var raw struct {
		Periods map[string][]elitePassLev `json:"periods"`
	}
	if err := json.Unmarshal(elitePassJSON, &raw); err != nil || len(raw.Periods) == 0 {
		panic("game: bad elitepass.json")
	}
	out := map[int64][]elitePassLev{}
	for k, v := range raw.Periods {
		id, _ := strconv.ParseInt(k, 10, 64)
		out[id] = v
	}
	return out
}()

type elitePassState struct {
	Month int64 // the 30-day period of the wins (elitePassNow); a new period starts from nothing
	Wins  int64
	Levs  []int64 // the level rewards taken
	// the advance card (product 909) of the period was paid, and the levels whose advance reward was taken
	Premium bool
	RmbLevs []int64
}

// elitePassStart is the first day of the period the original server showed on 2026-10-26 (capture original-2026-10-26:
// period 2, cur_day 16, end 2026-11-10 00:00); a period is 30 days and the two tables of the client take turns.
var elitePassStart = time.Date(2026, time.October, 11, 0, 0, 0, 0, dayZone)

const elitePassDays = 30

// elitePassNow is the period (the tables alternate 2, 1, 2, ...), the day of the period (the first is 1) and its end. The
// last value numbers the 30-day periods from the captured one, so a new period starts from nothing.
func elitePassNow(now time.Time) (period, day, end, cycle int64) {
	days := (now.Unix() - elitePassStart.Unix()) / 86400
	if now.Before(elitePassStart) && (now.Unix()-elitePassStart.Unix())%86400 != 0 {
		days-- // floor, not truncate toward zero
	}
	cycle = days / elitePassDays
	if days%elitePassDays < 0 {
		cycle--
	}
	day = days - cycle*elitePassDays + 1
	end = elitePassStart.AddDate(0, 0, int(cycle+1)*elitePassDays).Unix()
	return 2 - ((cycle%2)+2)%2, day, end, cycle
}

func loadElitePass(st *store.Store, rid int64, now time.Time) (*elitePassState, error) {
	state := &elitePassState{}
	if _, err := st.LoadState(rid, elitePassModule, state); err != nil {
		return nil, err
	}
	if _, _, _, cycle := elitePassNow(now); state.Month != cycle {
		*state = elitePassState{Month: cycle}
	}
	return state, nil
}

func (s *elitePassState) reached(levs []elitePassLev) int64 {
	lev := int64(0)
	for _, l := range levs {
		if s.Wins >= l.Wins && l.Lev > lev {
			lev = l.Lev
		}
	}
	return lev
}

// elitePassWin counts a win of the elite match for the pass.
func elitePassWin(st *store.Store, rid int64, now time.Time) error {
	state, err := loadElitePass(st, rid, now)
	if err != nil {
		return err
	}
	state.Wins++
	return st.SaveState(rid, elitePassModule, state)
}

// RegisterElitePass registers the pass of the elite match (24952-24955): the wins of the month raise the level and each
// level has a free reward. The paid track (the advance card) needs payments, which we have none of. The period of a month
// and the window are our rules.
func RegisterElitePass(r *gateway.Router, st *store.Store) {
	// the advance card (product 909), once a period; it is the period the confirmation falls in
	RegisterChargeGuard(elitePassAdvanceCharge, func(st *store.Store, role *store.Role) error {
		state, err := loadElitePass(st, role.RID, time.Now())
		switch {
		case err != nil:
			return err
		case state.Premium:
			return errNotPayable("本期已激活进阶卡")
		}
		return nil
	})
	RegisterChargeHook(elitePassAdvanceCharge, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
		state, err := loadElitePass(st, role.RID, time.Now())
		if err != nil {
			return nil, nil, err
		}
		state.Premium = true
		return nil, nil, st.SaveState(role.RID, elitePassModule, state)
	})
	r.Handle(24952, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, err := loadElitePass(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		period, day, end, _ := elitePassNow(s.Now())
		levs := elitePassCfg[period]
		list := []proto.Message{}
		seen := map[int64]bool{}
		ids := append(append([]int64(nil), state.Levs...), state.RmbLevs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				list = append(list, proto.Message{"id": id, "award_status": b2i(has(state.Levs, id)), "rmb_award_status": b2i(has(state.RmbLevs, id))})
			}
		}
		return s.Send(24952, proto.Message{"period": period, "cur_day": day, "end_time": end, "lev": state.reached(levs),
			"rmb_status": b2i(state.Premium), "win_count": state.Wins, "list": list})
	})
	r.Handle(24953, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(24953, proto.Message{"flag": 0, "msg": msg}) }
		state, err := loadElitePass(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		period, _, _, _ := elitePassNow(s.Now())
		levs := elitePassCfg[period]
		reached := state.reached(levs)
		var rows []elitePassLev
		for _, l := range levs {
			if l.Lev <= reached && (!has(state.Levs, l.Lev) || (state.Premium && !has(state.RmbLevs, l.Lev))) && (req.Int("id") == 0 || req.Int("id") == l.Lev) {
				rows = append(rows, l)
			}
		}
		if len(rows) == 0 {
			switch id := req.Int("id"); {
			case id > reached:
				return refuse("胜场不够")
			case id != 0:
				return refuse("已经领取过了")
			}
			return refuse("没有可领取的奖励")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		add := func(rows [][2]int64) {
			for _, a := range rows {
				if ItemKnown(a[0]) {
					splitGains([]sellGain{{a[0], a[1]}}, 1, ga, gi)
				}
			}
		}
		for _, l := range rows {
			if !has(state.Levs, l.Lev) {
				add(l.Reward)
				state.Levs = append(state.Levs, l.Lev)
			}
			if state.Premium && !has(state.RmbLevs, l.Lev) {
				add(l.RmbReward)
				state.RmbLevs = append(state.RmbLevs, l.Lev)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, elitePassModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(24953, proto.Message{"flag": 1, "msg": "领取成功"})
	})
	r.Handle(24954, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, err := loadElitePass(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		period, _, _, _ := elitePassNow(s.Now())
		red := int64(0)
		for _, l := range elitePassCfg[period] {
			if l.Wins <= state.Wins && !has(state.Levs, l.Lev) {
				red = 1
			}
		}
		return s.Send(24954, proto.Message{"flag": red})
	})
	r.Handle(24955, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		_, day, _, _ := elitePassNow(s.Now())
		return s.Send(24955, proto.Message{"is_pop": 0, "cur_day": day})
	})
}
