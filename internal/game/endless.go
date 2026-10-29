package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// endless.json: the endless trial of the client (tools/extract_endless.py).
//
//go:embed data/endless.json
var endlessJSON []byte

const (
	endlessModule  = "endless_trail"
	endlessRankMod = "endless_rank"
	combatEndless  = 11 // BattleConst.Fight_Type.Endless
	endOld         = 5  // Endless_trailEvent.endless_type.old; 1-4 are the water, fire, wind, light-dark modes
	endHireBase    = 2000000
	endRankShown   = 50
)

type endItem = [][]int64

type endBuff struct {
	GroupID int64 `json:"group_id"`
	Weight  int64 `json:"weight"`
	Round   int64 `json:"round"`
	SkillID int64 `json:"skill_id"`
}

type endFirst struct {
	ID      int64   `json:"id"`
	Items   endItem `json:"items"`
	LimitID int64   `json:"limit_id"`
}

type endFloor struct {
	ID    int64   `json:"id"`
	Items endItem `json:"items"`
}

type endRankReward struct {
	Items endItem `json:"items"`
	Max   int64   `json:"max"`
	Min   int64   `json:"min"`
}

type endConf struct {
	Const       map[string]json.RawMessage `json:"const"`
	Buffs       []map[string]endBuff       `json:"buffs"`
	First       [][]endFirst               `json:"first"`
	Floors      [][]endFloor               `json:"floors"`
	RankRewards [][]endRankReward          `json:"rank_rewards"`
}

func (c *endConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var endCfg = func() *endConf {
	c := &endConf{}
	if err := json.Unmarshal(endlessJSON, c); err != nil || len(c.First) != 5 || len(c.Floors) != 5 || len(c.Buffs) == 0 {
		panic("game: bad endless.json")
	}
	return c
}()

// endMode is a role's record in one mode.
type endMode struct {
	Max, DayPass, Start int64
	Taken               map[string]bool // first rewards taken
	Paid                map[string]bool // rounds whose daily reward was paid today
	Claimed             bool
}

type endHero struct {
	Flag, RID, PID, BID, Lev, Star, Per int64
	SrvID                               string
}

type endBuffHold struct{ ID, Group, Count int64 }

type endRun struct {
	Active   bool
	Type     int64
	Round    int64 // the round being fought or about to be
	Heroes   []endHero
	Buffs    []endBuffHold
	Rest     int64 // rounds left until the next buff choice
	Opts     []endBuffHold
	Choosing bool
	Acc      [][2]int64
}

type endSent struct{ PID int64 }

type endState struct {
	Modes                    map[string]*endMode
	Day                      int64
	Chosen                   int64 // the mode started today (0 = none)
	Run                      endRun
	Sent                     []int64   // heroes dispatched as support
	Hired                    []endHero // friends' heroes hired today
	Form                     []pfSlot
	FormType                 int64
	Hallows                  int64
	SupportDay, SupportToday int64
}

type endRankRow struct {
	RID   int64
	Name  string
	SrvID string
	Val   int64
	At    int64
}

type endRanks struct{ Lists map[string][]endRankRow }

type endSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterEndless registers the endless trial (23900-23913): rounds of fights with the same team, a buff to choose every
// five, first-clear rewards every five rounds, a reward for every round passed (once a day), rankings and support
// heroes of friends. The enemies are scaled copies of the role's own best heroes, stronger every round; a fight
// is played per round and the next one follows at once (or after the buff choice). Our rules, not captured.
func RegisterEndless(r *gateway.Router, db *store.Store) {
	e := &endSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(23900, e.info)
	r.Handle(23901, e.start)
	r.Handle(23902, e.battle)
	r.Handle(23903, e.first)
	r.Handle(23904, e.takeFirst)
	r.Handle(23905, e.mySent)
	r.Handle(23906, e.hired)
	r.Handle(23907, e.friendHeroes)
	r.Handle(23908, e.dispatch)
	r.Handle(23909, e.hire)
	r.Handle(23910, e.buffState)
	r.Handle(23911, e.chooseBuff)
	r.Handle(23912, func(s *gateway.Session, _ proto.Message) error { return s.Send(23912, proto.Message{"flag": 0}) })
	r.Handle(23913, e.rank)
	fightExitHooks.Store(db, e.exit)
}

// fightExitHooks run when the client leaves a fight (20008).
var fightExitHooks sync.Map // *store.Store -> func(*gateway.Session)

func (e *endSvc) with(s *gateway.Session, fn func(role *store.Role, st *endState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &endState{}
	if _, err := e.st.LoadState(role.RID, endlessModule, st); err != nil {
		return err
	}
	if st.Modes == nil {
		st.Modes = map[string]*endMode{}
	}
	if day := dayKey(s.Now()); st.Day != day { // a new day: the starts move back, the day's counters clear
		if err := e.payRankRewards(role, st); err != nil {
			return err
		}
		st.Day, st.Chosen, st.Hired = day, 0, nil
		for _, m := range st.Modes {
			m.DayPass, m.Paid, m.Claimed = 0, map[string]bool{}, false
			m.Start = max(1, m.Max-endCfg.num("backoff_num"))
		}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, endlessModule, st)
}

func (st *endState) mode(t int64) *endMode {
	k := strconv.FormatInt(t, 10)
	m := st.Modes[k]
	if m == nil {
		m = &endMode{Start: 1, Taken: map[string]bool{}, Paid: map[string]bool{}}
		st.Modes[k] = m
	}
	if m.Taken == nil {
		m.Taken = map[string]bool{}
	}
	if m.Paid == nil {
		m.Paid = map[string]bool{}
	}
	if m.Start < 1 {
		m.Start = 1
	}
	return m
}

func (e *endSvc) gain(s *gateway.Session, role *store.Role, items endItem) error {
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l), 1, ga, gi)
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if s == nil {
		return nil
	}
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, e.st)
}

// ---- rotation and ranks

// selectType is the new mode on offer now: they take turns every three days from the client's start date.
func endSelect(now time.Time) (cur, next int64, nextTime int64) {
	start := time.Date(2020, 3, 13, 0, 0, 0, 0, dayZone)
	days := int64(now.Sub(start).Hours() / 24)
	if days < 0 {
		days = 0
	}
	slot := days / 3
	// next_time is the seconds left: the client counts it down itself (a capture of the original server: 194424)
	return slot%4 + 1, (slot+1)%4 + 1, start.Add(time.Duration((slot+1)*3*24)*time.Hour).Unix() - now.Unix()
}

func (e *endSvc) ranks(srv string) (*endRanks, error) {
	r := &endRanks{}
	if _, err := e.st.LoadGlobal(srv, endlessRankMod, r); err != nil {
		return nil, err
	}
	if r.Lists == nil {
		r.Lists = map[string][]endRankRow{}
	}
	return r, nil
}

func (r *endRanks) sorted(t int64) []endRankRow {
	list := append([]endRankRow(nil), r.Lists[strconv.FormatInt(t, 10)]...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Val != list[j].Val {
			return list[i].Val > list[j].Val
		}
		return list[i].At < list[j].At
	})
	return list
}

func (e *endSvc) noteRank(role *store.Role, t, val, now int64) error {
	r, err := e.ranks(role.SrvID)
	if err != nil {
		return err
	}
	k := strconv.FormatInt(t, 10)
	for i := range r.Lists[k] {
		if r.Lists[k][i].RID == role.RID {
			if val > r.Lists[k][i].Val {
				r.Lists[k][i].Val, r.Lists[k][i].At = val, now
			}
			return e.st.SaveGlobal(role.SrvID, endlessRankMod, r)
		}
	}
	r.Lists[k] = append(r.Lists[k], endRankRow{RID: role.RID, Name: role.Name, SrvID: role.SrvID, Val: val, At: now})
	return e.st.SaveGlobal(role.SrvID, endlessRankMod, r)
}

func (e *endSvc) rankOf(role *store.Role, t int64) (int64, []proto.Message, error) {
	r, err := e.ranks(role.SrvID)
	if err != nil {
		return 0, nil, err
	}
	list := r.sorted(t)
	out := []proto.Message{}
	var mine int64
	for i, row := range list {
		if row.RID == role.RID {
			mine = int64(i + 1)
		}
		if i < endRankShown {
			out = append(out, proto.Message{"idx": i + 1, "name": row.Name, "rid": row.RID, "srv_id": row.SrvID, "val": row.Val})
		}
	}
	return mine, out, nil
}

// payRankRewards pays the rank rewards at the day's turn, for the rank the role holds then (the client: at 00:00).
func (e *endSvc) payRankRewards(role *store.Role, st *endState) error {
	if st.Day == 0 {
		return nil
	}
	for t := int64(1); t <= 5; t++ {
		if st.Modes[strconv.FormatInt(t, 10)] == nil {
			continue
		}
		rank, _, err := e.rankOf(role, t)
		if err != nil || rank == 0 {
			continue
		}
		for _, r := range endCfg.RankRewards[t-1] {
			if rank >= r.Min && rank <= r.Max {
				if err := e.gain(nil, role, r.Items); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *endSvc) rank(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		mine, list, err := e.rankOf(role, req.Int("type"))
		if err != nil {
			return err
		}
		return s.Send(23913, proto.Message{"my_idx": mine, "rank_list": list})
	})
}

// ---- the state

func (e *endSvc) heroMessages(role *store.Role, list []endHero) []proto.Message {
	out := []proto.Message{}
	all, _ := e.st.PartnersOf(role.RID)
	byID := map[int64]store.Partner{}
	for _, h := range all {
		byID[h.PartnerID] = h
	}
	for _, h := range list {
		pp := store.Partner{PartnerID: h.PID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
		if own, ok := byID[h.PID]; ok && h.Flag == 0 {
			pp = own
		}
		out = append(out, proto.Message{"id": h.PID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "quality": 0, "power": partnerMessage(pp).Int("power"), "ext_data": []proto.Message{}})
	}
	return out
}

func (e *endSvc) info(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		now := s.Now()
		cur, next, nextTime := endSelect(now)
		old, nw := st.mode(endOld), st.mode(cur)
		mineOld, listOld, err := e.rankOf(role, endOld)
		if err != nil {
			return err
		}
		mineNew, listNew, err := e.rankOf(role, cur)
		if err != nil {
			return err
		}
		hired := []proto.Message{}
		for _, h := range st.Hired {
			hired = append(hired, e.heroMessages(role, []endHero{h})...)
		}
		return s.Send(23900, proto.Message{"type": st.Chosen, "select_type": cur, "next_type": next, "next_time": nextTime,
			"max_round": old.Max, "current_round": old.Start, "day_pass_round": old.DayPass, "my_idx": mineOld, "rank_list": listOld,
			"new_max_round": nw.Max, "new_current_round": nw.Start, "new_day_pass_round": nw.DayPass, "new_my_idx": mineNew, "new_rank_list": listNew,
			"is_employ": b2i(len(st.Hired) > 0), "list": hired, "is_appoint": b2i(len(st.Sent) > 0), "is_reward": b2i(nw.Claimed)})
	})
}

// first (23903): the next first-clear reward of a mode that can be taken or is waited for.
func (e *endSvc) first(s *gateway.Session, req proto.Message) error {
	t := req.Int("type")
	return e.with(s, func(role *store.Role, st *endState) error {
		if t < 1 || t > 5 {
			return nil
		}
		m := st.mode(t)
		rows := endCfg.First[t-1]
		var next, last int64
		status := int64(0)
		taken := []proto.Message{}
		for _, r := range rows {
			if m.Taken[strconv.FormatInt(r.ID, 10)] {
				taken = append(taken, proto.Message{"id": r.ID})
				continue
			}
			if next == 0 {
				next = r.ID
				status = int64(b2i(m.Max >= r.LimitID))
			}
		}
		if len(rows) > 0 {
			last = rows[len(rows)-1].ID
		}
		if next == 0 {
			next = last
		}
		// max_id is the role's highest round: the client compares it with limit_id and counts down "N rounds to go" (capture: 0 for a new role)
		return s.Send(23903, proto.Message{"id": next, "status": status, "max_id": m.Max, "rewarded": taken, "type": t})
	})
}

func (e *endSvc) takeFirst(s *gateway.Session, req proto.Message) error {
	id, t := req.Int("id"), req.Int("type")
	return e.with(s, func(role *store.Role, st *endState) error {
		refuse := func(msg string) error { return s.Send(23904, proto.Message{"code": 0, "msg": msg, "type": t}) }
		if t < 1 || t > 5 {
			return refuse("奖励不存在")
		}
		m := st.mode(t)
		for _, r := range endCfg.First[t-1] {
			if r.ID != id {
				continue
			}
			key := strconv.FormatInt(id, 10)
			switch {
			case m.Taken[key]:
				return refuse("已经领取")
			case m.Max < r.LimitID:
				return refuse("通关数不足")
			}
			m.Taken[key] = true
			if err := e.gain(s, role, r.Items); err != nil {
				return err
			}
			return s.Send(23904, proto.Message{"code": 1, "msg": "领取成功", "type": t})
		}
		return refuse("奖励不存在")
	})
}

func timeFromUnix(n int64) time.Time { return time.Unix(n, 0) }
