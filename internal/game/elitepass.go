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
	Month int64 // the YYYYMM of the wins; a new month starts from nothing
	Wins  int64
	Levs  []int64 // the level rewards taken
}

// elitePassNow is the period (odd months 1, even months 2: our rule, the client table only numbers them), the day of the
// month and the end of the month.
func elitePassNow(now time.Time) (period, day, end, month int64) {
	t := now.In(dayZone)
	period = 2 - int64(t.Month())%2
	return period, int64(t.Day()), time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, dayZone).Unix(), int64(t.Year()*100 + int(t.Month()))
}

func loadElitePass(st *store.Store, rid int64, now time.Time) (*elitePassState, error) {
	state := &elitePassState{}
	if _, err := st.LoadState(rid, elitePassModule, state); err != nil {
		return nil, err
	}
	if _, _, _, month := elitePassNow(now); state.Month != month {
		*state = elitePassState{Month: month}
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
		ids := append([]int64(nil), state.Levs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			list = append(list, proto.Message{"id": id, "award_status": 1, "rmb_award_status": 0})
		}
		return s.Send(24952, proto.Message{"period": period, "cur_day": day, "end_time": end, "lev": state.reached(levs),
			"rmb_status": 0, "win_count": state.Wins, "list": list})
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
			if l.Lev <= reached && !has(state.Levs, l.Lev) && (req.Int("id") == 0 || req.Int("id") == l.Lev) {
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
		for _, l := range rows {
			for _, a := range l.Reward {
				if ItemKnown(a[0]) {
					splitGains([]sellGain{{a[0], a[1]}}, 1, ga, gi)
				}
			}
			state.Levs = append(state.Levs, l.Lev)
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
