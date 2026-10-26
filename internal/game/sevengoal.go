package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// sevengoal.json: the seven-day goals (七日目标: the adventure diary and the magic box) of the client (tools/extract_sevengoal.py).
//
//go:embed data/sevengoal.json
var sevenGoalJSON []byte

const (
	sevenGoalModule = "sevengoal"
	sevenGoalDays   = 7
)

// sevenGoalPeriods is the order of the periods a role goes through, a week each (the odd ones are the diary, 冒险日记,
// and the even ones the box, 魔盒). The client table only numbers them; the order and the week are our rules.
var sevenGoalPeriods = []int64{2, 3, 5, 6, 8, 9, 10, 11, 12, 13}

type sevenGoal struct {
	ID    int64
	Day   int64 // 0 for the goals that count from the first day
	Desc  string
	Award [][2]int64
	Event string // the game event that counts it, "" when we have none
	Need  int64
	Rmb   int64 // the accumulated charge goals
}

type sevenLev struct {
	Lev    int64      `json:"lev"`
	Exp    int64      `json:"exp"`
	Reward [][2]int64 `json:"reward"`
}

type sevenPeriod struct {
	goals   []sevenGoal
	byID    map[int64]*sevenGoal
	levs    []sevenLev
	expItem int64
}

var sevenGoalCfg = func() map[int64]*sevenPeriod {
	var raw struct {
		Periods map[string]struct {
			Goals []struct {
				ID    int64      `json:"id"`
				Desc  string     `json:"desc"`
				Award [][2]int64 `json:"award"`
			} `json:"goals"`
			Charge []struct {
				ID    int64      `json:"id"`
				Desc  string     `json:"desc"`
				Award [][2]int64 `json:"award"`
				Rmb   int64      `json:"rmb"`
			} `json:"charge"`
			Levs    []sevenLev `json:"levs"`
			ExpItem int64      `json:"exp_item"`
		} `json:"periods"`
	}
	if err := json.Unmarshal(sevenGoalJSON, &raw); err != nil || len(raw.Periods) == 0 {
		panic("game: bad sevengoal.json")
	}
	out := map[int64]*sevenPeriod{}
	for k, p := range raw.Periods {
		id, _ := strconv.ParseInt(k, 10, 64)
		day := int64(0)
		sp := &sevenPeriod{byID: map[int64]*sevenGoal{}, levs: p.Levs, expItem: p.ExpItem}
		for _, g := range p.Goals {
			label, need := sevenGoalRule(g.Desc)
			sg := sevenGoal{ID: g.ID, Desc: g.Desc, Award: g.Award, Event: label, Need: need}
			// the first goal of every day is the login (ids 211, 221... only in the first periods, the later ones number straight
			// on), so a login starts the next day; the diamond goals 99xx at the end count from the first day
			if g.ID < 9000 {
				if strings.Contains(g.Desc, "登录") {
					day++
				}
				sg.Day = day
			}
			sp.goals = append(sp.goals, sg)
		}
		for _, g := range p.Charge {
			sp.goals = append(sp.goals, sevenGoal{ID: g.ID, Desc: g.Desc, Award: g.Award, Rmb: g.Rmb, Need: g.Rmb})
		}
		for i := range sp.goals {
			sp.byID[sp.goals[i].ID] = &sp.goals[i]
		}
		out[id] = sp
	}
	return out
}()

var sevenNumbers = map[rune]int64{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9, '十': 10}

// sevenGoalRule reads the event and the count of a goal from its text; the table has only the text. Goals about
// systems that report no event yet (the arena, the voyage, the guild...) get no event and stay open.
func sevenGoalRule(desc string) (string, int64) {
	label := ""
	switch {
	case strings.Contains(desc, "登录"):
		label = evtLoaded
	case strings.Contains(desc, "快速作战"):
		label = questFastCombat
	case strings.Contains(desc, "友情点"):
		label = evtFriendPresent
	case strings.Contains(desc, "召唤"):
		label = evtRecruit
	}
	need := int64(1)
	rs := []rune(desc)
	for i := 0; i < len(rs); i++ {
		if rs[i] >= '0' && rs[i] <= '9' {
			j := i
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			need, _ = strconv.ParseInt(string(rs[i:j]), 10, 64)
			break
		}
		if v, ok := sevenNumbers[rs[i]]; ok {
			need = v
			break
		}
	}
	return label, need
}

type sevenState struct {
	Period  int64
	Prog    map[int64]int64
	Claimed []int64
	Exp     int64
	Levs    []int64 // the level rewards taken
	Opened  bool    // the client has asked for the list, so it keeps one that pushes update
}

// sevenNow says where the role is in the seven days: the period, the day (1-7) and the end of the period; a role past
// the last period has none (period 0).
func sevenNow(role *store.Role, now time.Time) (period, day, end int64) {
	start := dayStart(time.Unix(role.CreatedAt, 0))
	days := int64(now.Sub(start) / (24 * time.Hour))
	if days < 0 {
		days = 0
	}
	week := days / sevenGoalDays
	if week >= int64(len(sevenGoalPeriods)) {
		return 0, 0, 0
	}
	return sevenGoalPeriods[week], days%sevenGoalDays + 1, start.AddDate(0, 0, int((week+1)*sevenGoalDays)).Unix()
}

func (s *sevenState) claimed(id int64) bool { return has(s.Claimed, id) }

func (s *sevenState) value(g *sevenGoal) int64 { return min(s.Prog[g.ID], g.Need) }

func (s *sevenState) finish(g *sevenGoal) int64 {
	switch {
	case s.claimed(g.ID):
		return 2
	case g.Event != "" && s.Prog[g.ID] >= g.Need:
		return 1
	}
	return 0
}

// reached is the highest level whose experience the role has, 0 before the first (the box needs 50 for its first level).
func (s *sevenState) reached(p *sevenPeriod) int64 {
	lev := int64(0)
	for _, l := range p.levs {
		if s.Exp >= l.Exp && l.Lev > lev {
			lev = l.Lev
		}
	}
	return lev
}

// lev is the level the client shows, never below 1.
func (s *sevenState) lev(p *sevenPeriod) int64 { return max(s.reached(p), 1) }

func (s *sevenState) goalMsg(g *sevenGoal) proto.Message {
	return proto.Message{"id": g.ID, "finish": s.finish(g), "target_val": g.Need, "value": s.value(g)}
}

// visible says whether the goal is on the list on this day.
func (g *sevenGoal) visible(day int64) bool { return g.Day <= day }

func loadSeven(st *store.Store, role *store.Role, now time.Time) (*sevenState, *sevenPeriod, int64, int64, error) {
	period, day, end := sevenNow(role, now)
	state := &sevenState{}
	if _, err := st.LoadState(role.RID, sevenGoalModule, state); err != nil {
		return nil, nil, 0, 0, err
	}
	if state.Period != period { // a new period starts from nothing
		*state = sevenState{Period: period}
	}
	if state.Prog == nil {
		state.Prog = map[int64]int64{}
	}
	return state, sevenGoalCfg[period], day, end, nil
}

// RegisterSevenGoal registers the seven-day goals (13604-13609): a week of goals a day for a new role, each paying
// the experience item of the period; the experience raises the level and each level has a reward. The goals are counted
// by the game events that exist (login, quick combat, summons, friend gifts); the others have no event yet. The periods,
// their order and the week are our rules, not captured. Charge goals need payments, which we have none of.
func RegisterSevenGoal(r *gateway.Router, st *store.Store) {
	r.Handle(13604, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, day, end, err := loadSeven(st, role, s.Now())
		if err != nil {
			return err
		}
		if !state.Opened && p != nil {
			state.Opened = true
			if err := st.SaveState(role.RID, sevenGoalModule, state); err != nil {
				return err
			}
		}
		reply := proto.Message{"period": state.Period, "cur_day": day, "end_time": end, "lev": int64(1), "exp": state.Exp, "list": []proto.Message{}}
		if p != nil {
			reply["lev"] = state.lev(p)
			list := []proto.Message{}
			for i := range p.goals {
				if g := &p.goals[i]; g.visible(day) {
					list = append(list, state.goalMsg(g))
				}
			}
			reply["list"] = list
		}
		return s.Send(13604, reply)
	})
	r.Handle(13606, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(13606, proto.Message{"code": 0, "msg": msg}) }
		state, p, day, _, err := loadSeven(st, role, s.Now())
		if err != nil {
			return err
		}
		var g *sevenGoal
		if p != nil {
			g = p.byID[req.Int("id")]
		}
		if g == nil || !g.visible(day) {
			return refuse("目标不存在")
		}
		switch state.finish(g) {
		case 2:
			return refuse("已经领取过了")
		case 0:
			return refuse("还没有达成")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, a := range g.Award {
			if ItemKnown(a[0]) {
				splitGains([]sellGain{{a[0], a[1]}}, 1, ga, gi)
			}
			if a[0] == p.expItem {
				state.Exp += a[1]
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		state.Claimed = append(state.Claimed, g.ID)
		if err := st.SaveState(role.RID, sevenGoalModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(13606, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(13605, proto.Message{"list": []proto.Message{state.goalMsg(g)}}); err != nil {
			return err
		}
		return s.Send(13609, proto.Message{"lev": state.lev(p), "exp": state.Exp})
	})
	levList := func(s *gateway.Session, state *sevenState) error {
		taken := []proto.Message{}
		ids := append([]int64(nil), state.Levs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			taken = append(taken, proto.Message{"id": id})
		}
		return s.Send(13607, proto.Message{"lev": 1, "reward_list": taken})
	}
	r.Handle(13607, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, _, _, err := loadSeven(st, role, s.Now())
		if err != nil {
			return err
		}
		if p != nil {
			return levList(s, state)
		}
		return s.Send(13607, proto.Message{"lev": 1, "reward_list": []proto.Message{}})
	})
	r.Handle(13608, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(13608, proto.Message{"flag": 0, "msg": msg}) }
		state, p, _, _, err := loadSeven(st, role, s.Now())
		if err != nil {
			return err
		}
		var row *sevenLev
		if p != nil {
			for i := range p.levs {
				if p.levs[i].Lev == req.Int("id") {
					row = &p.levs[i]
				}
			}
		}
		switch {
		case row == nil:
			return refuse("奖励不存在")
		case has(state.Levs, row.Lev):
			return refuse("已经领取过了")
		case row.Lev > state.reached(p):
			return refuse("等级不够")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, a := range row.Reward {
			if ItemKnown(a[0]) {
				splitGains([]sellGain{{a[0], a[1]}}, 1, ga, gi)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		state.Levs = append(state.Levs, row.Lev)
		if err := st.SaveState(role.RID, sevenGoalModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(13608, proto.Message{"flag": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		return levList(s, state)
	})
}

// sevenGoalEvent counts a game event for the goals of today that wait for it. With push, the client is told which goals
// became ready (13605) once it has opened the list; before that it asks for the whole list anyway.
func sevenGoalEvent(s *gateway.Session, st *store.Store, role *store.Role, label string, n int64, push bool) error {
	if role == nil || n <= 0 {
		return nil
	}
	state, p, day, _, err := loadSeven(st, role, s.Now())
	if err != nil || p == nil {
		return err
	}
	var changed []proto.Message
	for i := range p.goals {
		g := &p.goals[i]
		if g.Event != label || g.visible(day) == false || (g.Day != 0 && g.Day != day) || state.claimed(g.ID) || state.Prog[g.ID] >= g.Need {
			continue
		}
		state.Prog[g.ID] = min(state.Prog[g.ID]+n, g.Need)
		changed = append(changed, state.goalMsg(g))
	}
	if len(changed) == 0 {
		return nil
	}
	if err := st.SaveState(role.RID, sevenGoalModule, state); err != nil {
		return err
	}
	if push && state.Opened {
		return s.Send(13605, proto.Message{"list": changed})
	}
	return nil
}
