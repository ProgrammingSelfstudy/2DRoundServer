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

// warorder.json: the war order (战令: the holiday war order, orderaction) of the client (tools/extract_warorder.py).
//
//go:embed data/warorder.json
var warOrderJSON []byte

const (
	warOrderModule   = "warorder"
	warOrderDays     = 30  // the day tasks end on day 30 of the month
	warOrderLevPrice = 600 // diamonds per level bought (1000 struck down by 0.6 in the client's buy window)
)

// warOrderMonths is the period a calendar month runs. The client table only numbers the periods (its texts name the
// season of 5 to 10: September to February); the month of a period and the month as the window are our rules.
var warOrderMonths = map[time.Month]int64{
	time.September: 5, time.October: 6, time.November: 7, time.December: 8, time.January: 9, time.February: 10,
}

type warTask struct {
	ID    int64      `json:"id"`
	Kind  int64      `json:"kind"` // 1 day, 2 week, 3 period
	Desc  string     `json:"desc"`
	Label string     `json:"label"`
	Need  int64      `json:"need"`
	Param []any      `json:"param"`
	Award [][2]int64 `json:"award"`
	Min   int64      `json:"min"`
	Max   int64      `json:"max"`
	// the band of the role level the task is for (the new war order), 0 for no limit
	MinLev int64  `json:"min_lev"`
	MaxLev int64  `json:"max_lev"`
	Event  string `json:"-"` // the game event that counts it, "" when we have none
}

type warLev struct {
	Lev       int64      `json:"lev"`
	Exp       int64      `json:"exp"` // the experience in all at which the level is reached
	MinLev    int64      `json:"min_lev"`
	MaxLev    int64      `json:"max_lev"`
	Reward    [][2]int64 `json:"reward"`
	RmbReward [][2]int64 `json:"rmb_reward"`
}

type warPeriod struct {
	Levs    []warLev   `json:"levs"`
	Tasks   []*warTask `json:"tasks"`
	byID    map[int64]*warTask
	expItem int64
}

var warOrderCfg = func() map[int64]*warPeriod {
	var raw struct {
		Periods map[string]*warPeriod `json:"periods"`
	}
	if err := json.Unmarshal(warOrderJSON, &raw); err != nil || len(raw.Periods) == 0 {
		panic("game: bad warorder.json")
	}
	out := map[int64]*warPeriod{}
	for k, p := range raw.Periods {
		id, _ := strconv.ParseInt(k, 10, 64)
		p.byID = map[int64]*warTask{}
		for _, t := range p.Tasks {
			t.Event = warTaskEvent(t)
			p.byID[t.ID] = t
			if p.expItem == 0 && len(t.Award) > 0 {
				p.expItem = t.Award[0][0]
			}
		}
		out[id] = p
	}
	return out
}()

// warTaskEvent maps the client's event of a task to the events this server counts; the tasks about systems that report
// none (forging, the dial, the lucky coins...) get none and stay open.
func warTaskEvent(t *warTask) string {
	switch t.Label {
	case evtLoaded, evtFriendPresent, evtGuildDonate, questFastCombat, evtStarTower:
		return t.Label
	case "evt_recruit", "evt_recruit_high":
		return evtRecruit
	case "evt_guild_dun_fight":
		return evtGuildBoss
	case "evt_star_tower_pass":
		return evtStarTower
	case "evt_arena_fight_result": // the wins are not told apart from the challenges
		if !strings.Contains(t.Desc, "赢") && !strings.Contains(t.Desc, "胜") {
			return evtArena
		}
	case "evt_shipping": // taking an order, not finishing one
		if q, ok := firstParam(t); ok && strings.Contains(t.Desc, "接取") {
			switch q {
			case 3:
				return evtVoyagePurple
			case 4:
				return evtVoyageOrange
			}
		}
	}
	return ""
}

// firstParam is the only parameter of a task when it is a plain number (the quality of an order).
func firstParam(t *warTask) (int64, bool) {
	if len(t.Param) != 1 {
		return 0, false
	}
	f, ok := t.Param[0].(float64)
	return int64(f), ok
}

type warState struct {
	Month   int64 // the YYYYMM the state is of; a new month starts from nothing
	Prog    map[int64]int64
	Claimed []int64 // the tasks taken
	Exp     int64
	Levs    []int64 // the level rewards taken
	Opened  bool    // the client has asked, so the changes are pushed
	Lev     int64   // the role level the cycle began with (the new war order picks its bands by it)
}

// warNow says where the calendar is in the war order: the period, the day of the month (1-30) and the end of the month; a
// month without a period has none (period 0).
func warNow(now time.Time) (period, day, end, month int64) {
	t := now.In(dayZone)
	period = warOrderMonths[t.Month()]
	day = int64(min(t.Day(), warOrderDays))
	end = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, dayZone).Unix()
	return period, day, end, int64(t.Year()*100 + int(t.Month()))
}

func (s *warState) claimed(id int64) bool { return has(s.Claimed, id) }

func (s *warState) finish(t *warTask) int64 {
	switch {
	case s.claimed(t.ID):
		return 2
	case t.Event != "" && s.Prog[t.ID] >= t.Need:
		return 1
	}
	return 0
}

func (s *warState) taskMsg(t *warTask) proto.Message {
	return proto.Message{"id": t.ID, "type": t.Kind, "finish": s.finish(t), "target_val": t.Need, "value": min(s.Prog[t.ID], t.Need), "end_time": int64(0)}
}

// reached is the level the experience has: 0 before the first row.
func (s *warState) reached(p *warPeriod) int64 {
	lev := int64(0)
	for _, l := range p.Levs {
		if s.Exp >= l.Exp && l.Lev > lev {
			lev = l.Lev
		}
	}
	return lev
}

// onList says whether the task is on the list on this day.
func (t *warTask) onList(day int64) bool {
	return t.Kind == 3 || (day >= t.Min && day <= t.Max)
}

func loadWar(st *store.Store, role *store.Role, now time.Time) (*warState, *warPeriod, int64, int64, error) {
	period, day, end, month := warNow(now)
	state := &warState{}
	if _, err := st.LoadState(role.RID, warOrderModule, state); err != nil {
		return nil, nil, 0, 0, err
	}
	if state.Month != month {
		*state = warState{Month: month}
	}
	if state.Prog == nil {
		state.Prog = map[int64]int64{}
	}
	return state, warOrderCfg[period], day, end, nil
}

// RegisterWarOrder registers the war order (25300-25309): a month of day, week and month tasks that pay the period's
// experience item, whose total raises the level; each level has a free reward. The tasks are counted by the game events
// that exist, the others stay open. The advance card (paid track, the extra experience pack) needs payments, which we
// have none of, so its track stays locked. The months of the periods, the window and the price of a level are our rules.
func RegisterWarOrder(r *gateway.Router, st *store.Store) {
	r.Handle(25300, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, day, end, err := loadWar(st, role, s.Now())
		if err != nil {
			return err
		}
		period, _, _, _ := warNow(s.Now())
		if !state.Opened && p != nil {
			state.Opened = true
			if err := st.SaveState(role.RID, warOrderModule, state); err != nil {
				return err
			}
		}
		reply := proto.Message{"period": period, "cur_day": day, "end_time": end, "lev": int64(0), "exp": state.Exp,
			"rmb_status": 0, "exp_status": 0, "list": []proto.Message{}}
		if p != nil {
			reply["lev"] = state.reached(p)
			list := []proto.Message{}
			for _, t := range p.Tasks {
				if t.onList(day) {
					list = append(list, state.taskMsg(t))
				}
			}
			reply["list"] = list
		}
		return s.Send(25300, reply)
	})
	r.Handle(25302, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(25302, proto.Message{"code": 0, "msg": msg}) }
		state, p, day, _, err := loadWar(st, role, s.Now())
		if err != nil {
			return err
		}
		var t *warTask
		if p != nil {
			t = p.byID[req.Int("id")]
		}
		if t == nil || !t.onList(day) {
			return refuse("任务不存在")
		}
		switch state.finish(t) {
		case 2:
			return refuse("已经领取过了")
		case 0:
			return refuse("还没有达成")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, a := range t.Award {
			if a[0] == p.expItem {
				state.Exp += a[1]
			} else if ItemKnown(a[0]) {
				splitGains([]sellGain{{a[0], a[1]}}, 1, ga, gi)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		state.Claimed = append(state.Claimed, t.ID)
		if err := st.SaveState(role.RID, warOrderModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(25302, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(25301, proto.Message{"list": []proto.Message{state.taskMsg(t)}}); err != nil {
			return err
		}
		return s.Send(25305, proto.Message{"lev": state.reached(p), "exp": state.Exp})
	})
	levList := func(s *gateway.Session, state *warState, p *warPeriod) error {
		taken := []proto.Message{}
		ids := append([]int64(nil), state.Levs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			taken = append(taken, proto.Message{"id": id, "status": 1, "rmb_status": 0})
		}
		lev := int64(0)
		if p != nil {
			lev = state.reached(p)
		}
		return s.Send(25303, proto.Message{"lev": lev, "reward_list": taken})
	}
	r.Handle(25303, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, _, _, err := loadWar(st, role, s.Now())
		if err != nil {
			return err
		}
		return levList(s, state, p)
	})
	r.Handle(25304, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(25304, proto.Message{"flag": 0, "msg": msg}) }
		state, p, _, _, err := loadWar(st, role, s.Now())
		if err != nil {
			return err
		}
		if p == nil {
			return refuse("活动未开启")
		}
		reached := state.reached(p)
		var rows []*warLev
		for i := range p.Levs {
			l := &p.Levs[i]
			if l.Lev <= reached && !has(state.Levs, l.Lev) && (req.Int("id") == 0 || req.Int("id") == l.Lev) {
				rows = append(rows, l)
			}
		}
		if len(rows) == 0 {
			if id := req.Int("id"); id > reached {
				return refuse("等级不够")
			} else if id != 0 {
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
		if err := st.SaveState(role.RID, warOrderModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(25304, proto.Message{"flag": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		return levList(s, state, p)
	})
	r.Handle(25306, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(25306, proto.Message{"rmb_status": 0, "exp_status": 0, "list": []proto.Message{}})
	})
	r.Handle(25307, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(25307, proto.Message{"flag": 0, "msg": msg}) }
		state, p, _, _, err := loadWar(st, role, s.Now())
		if err != nil {
			return err
		}
		if p == nil {
			return refuse("活动未开启")
		}
		n, lev := req.Int("id"), state.reached(p)
		if n <= 0 || lev+n > int64(len(p.Levs)) {
			return refuse("已经满级了")
		}
		if err := st.Exchange(role.RID, map[int64]int64{3: warOrderLevPrice * n}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			return refuse("钻石不足")
		}
		state.Exp = p.Levs[lev+n-1].Exp
		if err := st.SaveState(role.RID, warOrderModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := s.Send(25307, proto.Message{"flag": 1, "msg": "购买成功"}); err != nil {
			return err
		}
		return s.Send(25305, proto.Message{"lev": state.reached(p), "exp": state.Exp})
	})
	r.Handle(25308, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(25308, proto.Message{"flag": 0, "msg": "请先激活进阶卡"})
	})
	r.Handle(25309, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		_, day, _, _ := warNow(s.Now())
		return s.Send(25309, proto.Message{"is_pop": 0, "cur_day": day})
	})
}

// warOrderEvent counts a game event for the tasks of today that wait for it. With push, the client is told which tasks
// became ready (25301) once it has opened the war order.
func warOrderEvent(s *gateway.Session, st *store.Store, role *store.Role, label string, n int64, push bool) error {
	if role == nil || n <= 0 {
		return nil
	}
	state, p, day, _, err := loadWar(st, role, s.Now())
	if err != nil || p == nil {
		return err
	}
	var changed []proto.Message
	for _, t := range p.Tasks {
		if t.Event != label || !t.onList(day) || state.claimed(t.ID) || state.Prog[t.ID] >= t.Need {
			continue
		}
		state.Prog[t.ID] = min(state.Prog[t.ID]+n, t.Need)
		changed = append(changed, state.taskMsg(t))
	}
	if len(changed) == 0 {
		return nil
	}
	if err := st.SaveState(role.RID, warOrderModule, state); err != nil {
		return err
	}
	if push && state.Opened {
		return s.Send(25301, proto.Message{"list": changed})
	}
	return nil
}
