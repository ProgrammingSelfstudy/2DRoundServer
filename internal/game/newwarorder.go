package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// newwarorder.json: the new war order (海贼夺宝, neworderaction) of the client (tools/extract_newwarorder.py).
//
//go:embed data/newwarorder.json
var newWarOrderJSON []byte

const (
	newWarOrderModule = "newwarorder"
	newWarOrderOpen   = 7  // days after the role was created that it opens (the client text: "created 7 days, visible on day 8")
	newWarOrderDays   = 30 // days of a cycle ("the cycle lasts 30 days, it starts again after")
)

var newWarOrderCfg = func() map[int64]*warPeriod {
	var raw struct {
		Periods map[string]*warPeriod `json:"periods"`
	}
	if err := json.Unmarshal(newWarOrderJSON, &raw); err != nil || len(raw.Periods) == 0 {
		panic("game: bad newwarorder.json")
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

// newWarNow says where the role is in the cycles: the period (they go round 1, 2, 3), the day of the cycle (1-30), the end
// of the cycle and the number of the cycle; a role not yet open has none (period 0). The order of the periods is our rule.
func newWarNow(role *store.Role, now time.Time) (period, day, end, cycle int64) {
	days := int64(now.Sub(dayStart(time.Unix(role.CreatedAt, 0))) / (24 * time.Hour))
	if days < newWarOrderOpen {
		return 0, 0, 0, 0
	}
	n := (days - newWarOrderOpen) / newWarOrderDays
	start := dayStart(time.Unix(role.CreatedAt, 0)).AddDate(0, 0, int(newWarOrderOpen+(n+1)*newWarOrderDays))
	return n%int64(len(newWarOrderCfg)) + 1, (days-newWarOrderOpen)%newWarOrderDays + 1, start.Unix(), n + 1
}

// inBand says whether a role of that level sees the task or level row.
func inBand(lev, lo, hi int64) bool { return hi == 0 || (lev >= lo && lev <= hi) }

func (s *warState) levs(p *warPeriod) []warLev {
	var out []warLev
	for _, l := range p.Levs {
		if inBand(s.Lev, l.MinLev, l.MaxLev) {
			out = append(out, l)
		}
	}
	return out
}

func (s *warState) visible(t *warTask, day int64) bool {
	return t.onList(day) && inBand(s.Lev, t.MinLev, t.MaxLev)
}

// newWarAdvanceCharge is the charge product of the advance card of the new war order.
const newWarAdvanceCharge = 911

func loadNewWar(st *store.Store, role *store.Role, now time.Time) (*warState, *warPeriod, int64, int64, error) {
	period, day, end, cycle := newWarNow(role, now)
	state := &warState{}
	if _, err := st.LoadState(role.RID, newWarOrderModule, state); err != nil {
		return nil, nil, 0, 0, err
	}
	if state.Month != cycle {
		*state = warState{Month: cycle, Lev: int64(role.Lev)}
	}
	if state.Prog == nil {
		state.Prog = map[int64]int64{}
	}
	return state, newWarOrderCfg[period], day, end, nil
}

func (s *warState) newReached(p *warPeriod) int64 {
	lev := int64(0)
	for _, l := range s.levs(p) {
		if s.Exp >= l.Exp && l.Lev > lev {
			lev = l.Lev
		}
	}
	return lev
}

// RegisterNewWarOrder registers the new war order (28700-28708): the same as the war order, but it opens a week after the
// role was created, runs cycles of 30 days that go round three periods, has day and week tasks only, and the tasks and
// rewards are in bands of the role level (the level the cycle began with). The paid track needs payments, which we have none of.
func RegisterNewWarOrder(r *gateway.Router, st *store.Store) {
	// the advance card (product 911), once a cycle; it is the cycle the confirmation falls in
	RegisterChargeGuard(newWarAdvanceCharge, func(st *store.Store, role *store.Role) error {
		state, p, _, _, err := loadNewWar(st, role, time.Now())
		switch {
		case err != nil:
			return err
		case p == nil:
			return errNotPayable("海贼夺宝未开启")
		case state.Premium:
			return errNotPayable("本期已激活进阶卡")
		}
		return nil
	})
	RegisterChargeHook(newWarAdvanceCharge, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
		state, _, _, _, err := loadNewWar(st, role, time.Now())
		if err != nil {
			return nil, nil, err
		}
		state.Premium = true
		return nil, nil, st.SaveState(role.RID, newWarOrderModule, state)
	})
	r.Handle(28700, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, day, end, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		period, _, _, _ := newWarNow(role, s.Now())
		if !state.Opened && p != nil {
			state.Opened = true
			if err := st.SaveState(role.RID, newWarOrderModule, state); err != nil {
				return err
			}
		}
		reply := proto.Message{"period": period, "cur_day": day, "end_time": end, "lev": int64(0), "exp": state.Exp, "period_lev": state.Lev,
			"day_lev": state.Lev, "week_lev": state.Lev, "rmb_status": b2i(state.Premium), "list": []proto.Message{}}
		if p != nil {
			reply["lev"] = state.newReached(p)
			list := []proto.Message{}
			for _, t := range p.Tasks {
				if state.visible(t, day) {
					list = append(list, state.taskMsg(t))
				}
			}
			reply["list"] = list
		}
		return s.Send(28700, reply)
	})
	r.Handle(28702, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(28702, proto.Message{"code": 0, "msg": msg}) }
		state, p, day, _, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		var t *warTask
		if p != nil {
			t = p.byID[req.Int("id")]
		}
		if t == nil || !state.visible(t, day) {
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
		if err := st.SaveState(role.RID, newWarOrderModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(28702, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(28701, proto.Message{"list": []proto.Message{state.taskMsg(t)}}); err != nil {
			return err
		}
		return s.Send(28705, proto.Message{"lev": state.newReached(p), "exp": state.Exp})
	})
	levList := func(s *gateway.Session, state *warState, p *warPeriod) error {
		taken := state.takenLevels()
		lev := int64(0)
		if p != nil {
			lev = state.newReached(p)
		}
		return s.Send(28703, proto.Message{"lev": lev, "reward_list": taken})
	}
	r.Handle(28703, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, _, _, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		return levList(s, state, p)
	})
	r.Handle(28704, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(28704, proto.Message{"flag": 0, "msg": msg}) }
		state, p, _, _, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		if p == nil {
			return refuse("活动未开启")
		}
		reached := state.newReached(p)
		var rows []warLev
		for _, l := range state.levs(p) {
			if l.Lev <= reached && state.open(l) && (req.Int("id") == 0 || req.Int("id") == l.Lev) {
				rows = append(rows, l)
			}
		}
		if len(rows) == 0 {
			switch id := req.Int("id"); {
			case id > reached:
				return refuse("等级不够")
			case id != 0:
				return refuse("已经领取过了")
			}
			return refuse("没有可领取的奖励")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, l := range rows {
			state.takeLevel(l, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, newWarOrderModule, state); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(28704, proto.Message{"flag": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		return levList(s, state, p)
	})
	r.Handle(28706, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		state, _, _, _, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		return s.Send(28706, proto.Message{"rmb_status": b2i(state.Premium), "list": []proto.Message{}})
	})
	r.Handle(28707, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		_, day, _, _ := newWarNow(role, s.Now())
		return s.Send(28707, proto.Message{"is_pop": 0, "cur_day": day})
	})
	r.Handle(28708, func(s *gateway.Session, _ proto.Message) error { // the red dot of a level or task ready
		role := s.Role()
		if role == nil {
			return nil
		}
		state, p, day, _, err := loadNewWar(st, role, s.Now())
		if err != nil {
			return err
		}
		red := int64(0)
		if p != nil {
			for _, l := range state.levs(p) {
				if l.Lev <= state.newReached(p) && !has(state.Levs, l.Lev) {
					red = 1
				}
			}
			for _, t := range p.Tasks {
				if state.visible(t, day) && state.finish(t) == 1 {
					red = 1
				}
			}
		}
		return s.Send(28708, proto.Message{"flag": red})
	})
}

// newWarOrderEvent counts a game event for the tasks that wait for it, pushing the ready ones (28701) once the client opened it.
func newWarOrderEvent(s *gateway.Session, st *store.Store, role *store.Role, label string, n int64, push bool) error {
	if role == nil || n <= 0 {
		return nil
	}
	state, p, day, _, err := loadNewWar(st, role, s.Now())
	if err != nil || p == nil {
		return err
	}
	var changed []proto.Message
	for _, t := range p.Tasks {
		if t.Event != label || !state.visible(t, day) || state.claimed(t.ID) || state.Prog[t.ID] >= t.Need {
			continue
		}
		state.Prog[t.ID] = min(state.Prog[t.ID]+n, t.Need)
		changed = append(changed, state.taskMsg(t))
	}
	if len(changed) == 0 {
		return nil
	}
	if err := st.SaveState(role.RID, newWarOrderModule, state); err != nil {
		return err
	}
	if push && state.Opened {
		return s.Send(28701, proto.Message{"list": changed})
	}
	return nil
}
