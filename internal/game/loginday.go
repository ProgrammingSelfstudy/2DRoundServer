package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// loginday.json is the client's eight-day login table (tools/extract_loginday.py): the rewards of each day.
//
//go:embed data/loginday.json
var loginDayJSON []byte

//go:embed data/sevenloginday.json
var sevenLoginDayJSON []byte

var (
	loginDays      = parseLoginDays(loginDayJSON, 8)
	sevenLoginDays = parseLoginDays(sevenLoginDayJSON, 7)
)

func parseLoginDays(data []byte, count int) [][][2]int64 {
	var rewards struct {
		Days [][][2]int64 `json:"days"`
	}
	if err := json.Unmarshal(data, &rewards); err != nil || len(rewards.Days) != count {
		panic("game: bad login day rewards")
	}
	return rewards.Days
}

// loginDayIcon is the id of the eight-day login in the client's function table; its icon
// (open_type 2) shows only when the server lists it in the activity status (10922).
const (
	loginDayIcon      = 508
	sevenLoginDayIcon = 505
)

// Day states the client understands (action_eight_login_window.lua): 1 not reached, 2 reached and
// not taken, 3 taken.
const (
	loginDayLocked = 1
	loginDayOpen   = 2
	loginDayTaken  = 3
)

// loginDayReached is how many of the eight days the role has reached: the day they were created
// is day 1, and a day is a calendar one in China time (our policy: the original rule is unknown).
func loginDayReached(role *store.Role, now time.Time) int64 {
	created := dayStart(time.Unix(role.CreatedAt, 0).In(dayZone))
	days := int64(dayStart(now.In(dayZone)).Sub(created)/(24*time.Hour)) + 1
	return max(1, min(days, int64(len(loginDays))))
}

var growthIconIDs = [...]int64{503, 604, 605, 1503, 602, 603, 2503, 3503, 4503, 617}

func growthIconPeriods(role *store.Role, now time.Time) (seven, war int64) {
	seven, _, _ = sevenNow(role, now)
	if sevenGoalCfg[seven] == nil {
		seven = 0
	}
	war = warIconPeriod(now)
	return
}

func growthIconPeriod(id, seven, war int64) int64 {
	switch id {
	case 503, 604, 605, 2503, 3503, 4503:
		return seven
	case 1503, 602:
		switch seven {
		case 2, 5, 8, 10, 12:
			return seven
		}
	case 603:
		switch seven {
		case 3, 6, 9, 11, 13:
			return seven
		}
	case 617:
		return war
	}
	return 0
}

func activityIcon(id int64, status int) proto.Message {
	return proto.Message{"id": id, "status": status, "int_args": []proto.Message{}, "ext_args": []proto.Message{}}
}

func pushGrowthIconChanges(s *gateway.Session, role *store.Role, previous, now time.Time) error {
	prevSeven, prevWar := growthIconPeriods(role, previous)
	seven, war := growthIconPeriods(role, now)
	if seven == prevSeven && war == prevWar {
		return nil
	}
	for _, id := range growthIconIDs {
		period := growthIconPeriod(id, seven, war)
		if period != growthIconPeriod(id, prevSeven, prevWar) {
			if err := s.Send(10923, activityIcon(id, b2i(period != 0))); err != nil {
				return err
			}
		}
	}
	return nil
}

// RegisterLoginDay registers activity icons (10922/10924/10925) and login gifts (21100/21101).
// A gateway offers exactly one login track because neither request identifies it on the wire.
func RegisterLoginDay(r *gateway.Router, st *store.Store, sevenDayLogin bool) {
	icon, loginType, days := int64(loginDayIcon), 1, loginDays
	load, claim := st.LoginDay, st.ClaimLoginDay
	if sevenDayLogin {
		icon, loginType, days = sevenLoginDayIcon, 0, sevenLoginDays
		load, claim = st.SevenLoginDay, st.ClaimSevenLoginDay
	}
	iconOpen := func(rid int64) (bool, error) {
		state, err := load(rid)
		return len(state.Taken) < len(days), err
	}
	list := func(role *store.Role, state store.LoginDayState, now time.Time) []proto.Message {
		reached := loginDayReached(role, now)
		out := []proto.Message{}
		for day := int64(1); day <= int64(len(days)); day++ {
			status := loginDayLocked
			switch {
			case state.Has(day):
				status = loginDayTaken
			case day <= reached:
				status = loginDayOpen
			}
			out = append(out, proto.Message{"day": day, "status": status})
		}
		return out
	}
	icons := func(cmd uint16) gateway.Handler {
		return func(s *gateway.Session, _ proto.Message) error {
			role := s.Role()
			if role == nil {
				return s.Send(cmd, proto.Message{"act_list": []proto.Message{}})
			}
			now := s.Now()
			if cmd == 10922 { // asked once at login: the moment to offer the limited gifts the role reached
				if err := CheckLimitGifts(s, st, now); err != nil {
					return err
				}
			}
			open, err := iconOpen(role.RID)
			if err != nil {
				return err
			}
			list := []proto.Message{activityIcon(loginDayIcon, b2i(open && icon == loginDayIcon)), activityIcon(sevenLoginDayIcon, b2i(open && icon == sevenLoginDayIcon))}
			daysRankOpen, err := daysRankIconOpen(st, role.SrvID, now)
			if err != nil {
				return err
			}
			list = append(list, activityIcon(drIcon, b2i(daysRankOpen)))
			seven, war := growthIconPeriods(role, now)
			for _, id := range growthIconIDs {
				list = append(list, activityIcon(id, b2i(growthIconPeriod(id, seven, war) != 0)))
			}
			festival, err := readFestivalIcons(st, role, now)
			if err != nil {
				return err
			}
			for _, id := range festivalIconIDs {
				list = append(list, activityIcon(id, b2i(festival.open(id))))
			}
			return s.Send(cmd, proto.Message{"act_list": list})
		}
	}
	r.Handle(10922, icons(10922))
	r.Handle(10924, icons(10924))
	r.Handle(10925, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		id, status := req.Int("id"), 0
		if role != nil {
			now := s.Now()
			switch {
			case id == drIcon:
				open, err := daysRankIconOpen(st, role.SrvID, now)
				if err != nil {
					return err
				}
				status = b2i(open)
			case id == icon:
				open, err := iconOpen(role.RID)
				if err != nil {
					return err
				}
				status = b2i(open)
			case slices.Contains(festivalIconIDs[:], id):
				festival, err := readFestivalIcons(st, role, now)
				if err != nil {
					return err
				}
				status = b2i(festival.open(id))
			default:
				seven, war := growthIconPeriods(role, now)
				status = b2i(growthIconPeriod(id, seven, war) != 0)
			}
		}
		return s.Send(10925, activityIcon(id, status))
	})
	r.Handle(21100, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, err := load(role.RID)
		if err != nil {
			return err
		}
		return s.Send(21100, proto.Message{"status_list": list(role, state, s.Now()), "type": loginType})
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
		if day < 1 || day > int64(len(days)) {
			return reply(0, "没有这一天")
		}
		if day > loginDayReached(role, s.Now()) {
			state, err := load(role.RID)
			if err != nil {
				return err
			}
			if state.Has(day) {
				return reply(0, "奖励已经领取")
			}
			return reply(0, "还没到这一天")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(toGains(days[day-1]), 1, assets, items)
		state, err := claim(role.RID, day, assets, items)
		if errors.Is(err, store.ErrConflict) {
			return reply(0, "奖励已经领取")
		}
		if err != nil {
			return err
		}
		if err := reply(1, "领取成功"); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if len(state.Taken) >= len(days) && (sevenDayLogin || isH5RoleSession(s)) {
			return s.Send(10923, activityIcon(icon, 0))
		}
		return nil
	})
}

// Register the persisted state without loading or changing gameplay data.
func init() {
	registerAdminModule(AdminModule{Key: store.LoginDayStateKey, Title: "登录天数/登录奖励", Window: "10922–10925、21100/21101", Scope: "role", New: adminState[store.LoginDayState]})
	registerAdminModule(AdminModule{Key: store.SevenLoginDayStateKey, Title: "七天登录奖励", Window: "10922–10925、21100/21101", Scope: "role", New: adminState[store.LoginDayState]})
}
