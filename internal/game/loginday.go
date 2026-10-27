package game

import (
	_ "embed"
	"encoding/json"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// loginday.json is the client's eight-day login table (tools/extract_loginday.py): the rewards of each day.
//
//go:embed data/loginday.json
var loginDayJSON []byte

var loginDays = func() [][][2]int64 {
	var f struct {
		Days [][][2]int64 `json:"days"`
	}
	if err := json.Unmarshal(loginDayJSON, &f); err != nil || len(f.Days) != 8 {
		panic("game: bad loginday.json")
	}
	return f.Days
}()

const loginDayModule = "loginday"

// Day states the client understands (action_eight_login_window.lua): 1 not reached, 2 reached and
// not taken, 3 taken.
const (
	loginDayLocked = 1
	loginDayOpen   = 2
	loginDayTaken  = 3
)

type loginDayState struct {
	Taken []int64 `json:"taken"` // the days whose reward was taken
}

func (st *loginDayState) has(day int64) bool {
	for _, d := range st.Taken {
		if d == day {
			return true
		}
	}
	return false
}

// loginDayReached is how many of the eight days the role has reached: the day they were created
// is day 1, and a day is a calendar one in China time (our policy: the original rule is unknown;
// the client also has a seven-day table, login_days_data, which this server does not use).
func loginDayReached(role *store.Role, now time.Time) int64 {
	created := dayStart(time.Unix(role.CreatedAt, 0).In(dayZone))
	days := int64(dayStart(now.In(dayZone)).Sub(created)/(24*time.Hour)) + 1
	return max(1, min(days, int64(len(loginDays))))
}

// RegisterLoginDay registers the eight-day login gifts: 21100 (the state of each day) and 21101 (take
// a reached day's reward).
func RegisterLoginDay(r *gateway.Router, st *store.Store) {
	list := func(role *store.Role, state *loginDayState, now time.Time) []proto.Message {
		reached := loginDayReached(role, now)
		out := []proto.Message{}
		for day := int64(1); day <= int64(len(loginDays)); day++ {
			status := loginDayLocked
			switch {
			case state.has(day):
				status = loginDayTaken
			case day <= reached:
				status = loginDayOpen
			}
			out = append(out, proto.Message{"day": day, "status": status})
		}
		return out
	}
	r.Handle(21100, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state := &loginDayState{}
		if _, err := st.LoadState(role.RID, loginDayModule, state); err != nil {
			return err
		}
		return s.Send(21100, proto.Message{"status_list": list(role, state, s.Now()), "type": 1})
	})
	r.Handle(21101, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		day := req.Int("day")
		reply := func(code int, msg string) error {
			return s.Send(21101, proto.Message{"code": code, "msg": msg, "day": day})
		}
		state := &loginDayState{}
		if _, err := st.LoadState(role.RID, loginDayModule, state); err != nil {
			return err
		}
		switch {
		case day < 1 || day > int64(len(loginDays)):
			return reply(0, "没有这一天")
		case state.has(day):
			return reply(0, "奖励已经领取")
		case day > loginDayReached(role, s.Now()):
			return reply(0, "还没到这一天")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, g := range loginDays[day-1] {
			splitGains([]sellGain{{g[0], g[1]}}, 1, assets, items)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
			return err
		}
		state.Taken = append(state.Taken, day)
		if err := st.SaveState(role.RID, loginDayModule, state); err != nil {
			return err
		}
		if err := reply(1, "领取成功"); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
