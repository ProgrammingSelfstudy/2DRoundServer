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

// expedition.json: the hero expedition of the client (tools/extract_expedition.py).
//
//go:embed data/expedition.json
var expeditionJSON []byte

const (
	expModule      = "heroexpedit"
	combatExpedit  = 21 // BattleConst.Fight_Type.ExpeditFight
	expFloors      = 15
	expLastPoint   = 20 // the map has 20 points: 15 floors and a box after every third
	expRewardShare = 20 // a floor and a box each pay a twentieth of the difficulty's total
)

type expPoint struct {
	ID    int64 `json:"id"`
	Floor int64 `json:"floor"`
	Type  int64 `json:"type"` // 1 = a floor, 2 = a box
}

type expMode struct {
	ID    int64     `json:"id"`
	Power int64     `json:"power"`
	Items [][]int64 `json:"items"`
}

type expConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Points []expPoint                 `json:"points"`
	Modes  []expMode                  `json:"modes"`
}

// Drop is what the owner of a hired hero gets.
func (c *expConf) Drop() [][2]int64 {
	var raw [][]int64
	_ = json.Unmarshal(c.Const["drop"], &raw)
	var out [][2]int64
	for _, p := range raw {
		if len(p) > 1 {
			out = append(out, [2]int64{p[0], p[1]})
		}
	}
	return out
}

func (c *expConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var expCfg = func() *expConf {
	c := &expConf{}
	if err := json.Unmarshal(expeditionJSON, c); err != nil || len(c.Points) != expLastPoint || len(c.Modes) != 3 {
		panic("game: bad expedition.json")
	}
	return c
}()

// expHire is a friend's hero hired for the day.
type expHire struct {
	RID, PID, BID, Lev, Star, Per int64
	SrvID                         string
	Used                          bool // won a fight already, cannot fight again
}

type expPrev struct{ Diff, Cleared int64 }

type expState struct {
	Day      int64
	Diff     int64            // the difficulty chosen today (0 = none yet)
	Max      int64            // the highest difficulty cleared
	Guard    int64            // the next point to fight
	Taken    []int64          // boxes taken today
	Got      map[string]int64 // today's rewards by item
	Hurt     map[string]int64 // own heroes' hp in per cent, by hero id (full when missing)
	Hired    []expHire
	Sent     int64 // the hero dispatched to the friends
	Borrowed int64 // times the dispatched hero was hired today
	Prev     expPrev
}

type expSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterExpedition registers the hero expedition (24400-24415): fifteen guard floors a day on a difficulty chosen
// for the day, a box after every third, hp that carries over from fight to fight, friends' heroes lent and hired, and
// the automatic sweep of what the same difficulty cleared the day before. The guards are scaled copies of the role's own
// best heroes (as in the planes), a floor stronger than the last; the rewards are the client's totals shared
// out evenly. Our rules, not captured.
func RegisterExpedition(r *gateway.Router, db *store.Store) {
	e := &expSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(24400, e.info)
	r.Handle(24401, e.floor)
	r.Handle(24402, e.takeBox)
	r.Handle(24403, e.fight)
	r.Handle(24405, e.mySent)
	r.Handle(24406, e.friendHeroes)
	r.Handle(24407, e.dispatch)
	r.Handle(24408, e.hire)
	r.Handle(24409, e.hp)
	r.Handle(24410, func(s *gateway.Session, _ proto.Message) error { return s.Send(24410, proto.Message{"is_show": 0}) })
	r.Handle(24411, func(s *gateway.Session, _ proto.Message) error { return s.Send(24411, proto.Message{"is_show": 0}) })
	r.Handle(24412, e.choose)
	r.Handle(24415, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(24415, proto.Message{"replay_infos": []proto.Message{}})
	})
}

func (st *expState) guard() int64 { return max(1, st.Guard) }

// cleared is the number of floors passed.
func (st *expState) cleared() int64 {
	var n int64
	for _, p := range expCfg.Points {
		if p.Type == 1 && p.ID < st.guard() {
			n++
		}
	}
	return n
}

// roll starts a new day: what the difficulty cleared is kept for the sweep, the rest clears.
func (st *expState) roll(now time.Time) {
	day := dayKey(now)
	if st.Day == day {
		return
	}
	st.Prev = expPrev{}
	if st.Day == dayKey(now.AddDate(0, 0, -1)) {
		st.Prev = expPrev{Diff: st.Diff, Cleared: st.cleared()}
	}
	st.Day, st.Diff, st.Guard, st.Taken, st.Got, st.Hurt, st.Hired, st.Borrowed = day, 0, 1, nil, nil, nil, nil, 0
}

func (e *expSvc) with(s *gateway.Session, fn func(role *store.Role, st *expState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &expState{}
	if _, err := e.st.LoadState(role.RID, expModule, st); err != nil {
		return err
	}
	st.roll(s.Now())
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, expModule, st)
}

// pointID is the map point of a floor: a box sits after every third.
func expPointID(floor int64) int64 { return floor + (floor-1)/3 }

// next is the point after a cleared one, boxes skipped (they are taken, not fought).
func expNext(id int64) int64 {
	id++
	if id <= expLastPoint && expCfg.Points[id-1].Type == 2 {
		id++
	}
	return id
}

// share is what a floor or a box pays on a difficulty.
func expShare(diff int64) [][2]int64 {
	var out [][2]int64
	for _, p := range expCfg.Modes[diff-1].Items {
		if len(p) > 1 {
			out = append(out, [2]int64{p[0], max(1, p[1]/expRewardShare)})
		}
	}
	return out
}

func (e *expSvc) gain(s *gateway.Session, role *store.Role, items [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(items), 1, ga, gi)
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

// pay gives the rewards and writes them on the day's account.
func (e *expSvc) pay(s *gateway.Session, role *store.Role, st *expState, items [][2]int64, times int64) ([]proto.Message, error) {
	var total [][2]int64
	out := []proto.Message{}
	if st.Got == nil {
		st.Got = map[string]int64{}
	}
	for _, it := range items {
		n := it[1] * times
		total = append(total, [2]int64{it[0], n})
		st.Got[strconv.FormatInt(it[0], 10)] += n
		out = append(out, proto.Message{"bid": it[0], "num": n})
	}
	return out, e.gain(s, role, total)
}

func (e *expSvc) todayRewards(st *expState) []proto.Message {
	out := []proto.Message{}
	if st.Diff == 0 {
		return out
	}
	for _, it := range expCfg.Modes[st.Diff-1].Items {
		if len(it) > 1 {
			out = append(out, proto.Message{"bid": it[0], "num": st.Got[strconv.FormatInt(it[0], 10)]})
		}
	}
	return out
}

func (e *expSvc) sendInfo(s *gateway.Session, st *expState) error {
	taken := []proto.Message{}
	for _, id := range st.Taken {
		taken = append(taken, proto.Message{"reward_id": id})
	}
	return s.Send(24400, proto.Message{"guard_id": st.guard(), "difficulty": st.Diff, "max_difficulty": st.Max, "reward": taken, "rewards": e.todayRewards(st)})
}

func (e *expSvc) info(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *expState) error { return e.sendInfo(s, st) })
}

// ---- the guards

// basePower is what the floors are measured against: the smaller of the role's power and the difficulty's mapping power.
func expBasePower(own, diff int64) int64 {
	cap := []string{"easy_mapping_power", "difficult_mapping_power", "hell_mapping_power"}[diff-1]
	return min(own, expCfg.num(cap))
}

// guardPower is the power of the guards on a floor: half of the base at the first floor, a little stronger each.
func expGuardPower(own, diff, floor int64) int64 {
	return expBasePower(own, diff) * (500 + 35*(floor-1)) / 1000
}

// guards are scaled copies of the role's best heroes, as the fight makes them.
func expGuards(best []store.Partner, power int64) []proto.Message {
	factor := power * 1000 / max(1, rolePower(best))
	out := []proto.Message{}
	for i, h := range best {
		out = append(out, proto.Message{"pos": i + 1, "bid": h.BID, "lev": max(1, int64(h.Lev)*factor/1000), "star": h.Star, "hp_per": 100, "ext_data": []proto.Message{}})
	}
	return out
}

func (e *expSvc) floor(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *expState) error {
		if id < 1 || id > expLastPoint || st.Diff == 0 {
			return nil
		}
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		best := bestHeroes(all, 5)
		own := rolePower(best)
		p := expCfg.Points[id-1]
		status := int64(0)
		if id < st.guard() {
			status = 2
		}
		body := proto.Message{"id": id, "rid": role.RID, "srv_id": role.SrvID, "name": role.Name, "face": role.FaceID, "lev": role.Lev, "power": own,
			"guards": []proto.Message{}, "is_holiday": 0, "status": status}
		rewards := []proto.Message{}
		for _, it := range expShare(st.Diff) {
			rewards = append(rewards, proto.Message{"bid": it[0], "num": it[1]})
		}
		body["rewards"] = rewards
		if p.Type == 1 {
			body["guards"] = expGuards(best, expGuardPower(own, st.Diff, p.Floor))
		}
		return s.Send(24401, body)
	})
}

func (e *expSvc) takeBox(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *expState) error {
		refuse := func(msg string) error { return s.Send(24402, proto.Message{"code": 0, "msg": msg, "id": id}) }
		switch {
		case id < 1 || id > expLastPoint || expCfg.Points[id-1].Type != 2:
			return refuse("宝箱不存在")
		case st.Diff == 0 || id >= st.guard():
			return refuse("通关前面的关卡后才能领取")
		}
		for _, t := range st.Taken {
			if t == id {
				return refuse("已经领取")
			}
		}
		st.Taken = append(st.Taken, id)
		if _, err := e.pay(s, role, st, expShare(st.Diff), 1); err != nil {
			return err
		}
		return s.Send(24402, proto.Message{"code": 1, "msg": "领取成功", "id": id})
	})
}

// ---- the fights

func (st *expState) hurt(pid int64) int64 {
	if v, ok := st.Hurt[strconv.FormatInt(pid, 10)]; ok {
		return v
	}
	return 100
}

// fight (24403) takes the formation and plays the guard of the current point.
func (e *expSvc) fight(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, st *expState) error {
		refuse := func(msg string) error { return s.Send(24403, proto.Message{"code": 0, "msg": msg}) }
		switch {
		case st.Diff == 0:
			return refuse("请先选择难度")
		case st.guard() > expLastPoint:
			return refuse("今日远征已全部通关")
		}
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		var roster []pfHero
		var slots []pfSlot
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			id, owner := p.Int("id"), p.Int("owner_id")
			if seen[id] || len(slots) == formMaxHeroes {
				continue
			}
			if owner == 0 || owner == role.RID {
				if h, ok := byID[id]; ok {
					seen[id] = true
					roster = append(roster, pfHero{Flag: 0, PID: id, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star), HP: st.hurt(id)})
					slots = append(slots, pfSlot{Pos: p.Int("pos"), ID: id, Flag: 0})
				}
				continue
			}
			for _, h := range st.Hired {
				if h.PID == id && h.RID == owner && !h.Used {
					seen[id] = true
					roster = append(roster, pfHero{Flag: 1, PID: id, BID: h.BID, Lev: h.Lev, Star: h.Star, HP: h.Per})
					slots = append(slots, pfSlot{Pos: p.Int("pos"), ID: id, Flag: 1})
				}
			}
		}
		if len(slots) == 0 {
			return refuse("请选择出战英雄")
		}
		point := st.guard()
		floor := expCfg.Points[point-1].Floor
		pf, msg, err := planesFight(e.st, e.fights, role, roster, slots, req.Int("formation_type"),
			expGuardPower(rolePower(bestHeroes(all, 5)), st.Diff, floor), "远征守卫", []proto.Message{{"param": floor}}, combatExpedit)
		if err != nil {
			return err
		}
		if msg != "" {
			return refuse(msg)
		}
		if err := s.Send(24403, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		diff, fighters, ft, left := st.Diff, pf.fighters, pf.formType, pf.left
		return e.fights.begin(s, role, combatExpedit, pf.intro, pf.rounds, pf.won, func(s *gateway.Session, won bool) error {
			return e.settle(s, point, diff, fighters, ft, left, won)
		})
	})
}

func (e *expSvc) settle(s *gateway.Session, point, diff int64, fighters []pfSlot, formType int64, left map[int64]int64, won bool) error {
	return e.with(s, func(role *store.Role, st *expState) error {
		if st.Diff != diff || st.guard() != point { // the day turned meanwhile
			return nil
		}
		if st.Hurt == nil {
			st.Hurt = map[string]int64{}
		}
		for _, sl := range fighters {
			per := left[battleCell(formType, sl.Pos)]
			if sl.Flag == 0 {
				st.Hurt[strconv.FormatInt(sl.ID, 10)] = per
				continue
			}
			for i := range st.Hired {
				if st.Hired[i].PID == sl.ID {
					st.Hired[i].Per = per
					st.Hired[i].Used = st.Hired[i].Used || won
				}
			}
		}
		items := []proto.Message{}
		result := int64(2)
		if won {
			result = 1
			var err error
			if items, err = e.pay(s, role, st, expShare(diff), 1); err != nil {
				return err
			}
			for i := range items {
				items[i]["is_bind"], items[i]["id"] = 1, 0
			}
			st.Guard = expNext(point)
			if st.Guard > expLastPoint {
				st.Max = max(st.Max, diff)
			}
		}
		if err := s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0, "combat_type": combatExpedit,
			"target_role_name": "远征守卫", "hurt_statistics": []proto.Message{}, "adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}}); err != nil {
			return err
		}
		if !won {
			return nil
		}
		rewards := []proto.Message{}
		for _, it := range items {
			rewards = append(rewards, proto.Message{"bid": it.Int("bid"), "num": it.Int("num")})
		}
		if err := s.Send(24414, proto.Message{"guard_id": point, "floor_id": expCfg.Points[point-1].Floor, "rewards": rewards}); err != nil {
			return err
		}
		return e.sendInfo(s, st)
	})
}

// ---- the difficulty and the sweep

// open says why a difficulty is closed to the role, or "".
func (e *expSvc) open(role *store.Role, st *expState, diff, own int64) string {
	switch {
	case diff < 1 || diff > 3:
		return "难度不存在"
	case st.Diff != 0:
		return "今天已经选择了难度"
	case diff > 1 && st.Max < diff-1:
		return "全部通关上一难度后开启"
	case diff > 1 && own < expCfg.Modes[diff-1].Power:
		return "战力达到要求后开启"
	}
	return ""
}

func (e *expSvc) choose(s *gateway.Session, req proto.Message) error {
	diff := req.Int("difficulty")
	return e.with(s, func(role *store.Role, st *expState) error {
		refuse := func(msg string) error { return s.Send(24412, proto.Message{"code": 0, "msg": msg}) }
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		if msg := e.open(role, st, diff, rolePower(bestHeroes(all, 5))); msg != "" {
			return refuse(msg)
		}
		st.Diff, st.Guard, st.Taken, st.Got = diff, 1, nil, nil
		if err := s.Send(24412, proto.Message{"code": 1, "msg": "选择成功"}); err != nil {
			return err
		}
		// the same difficulty as yesterday: what it cleared beyond the backoff is swept at once
		sweep := st.Prev.Cleared - expCfg.num("regression_level")
		if st.Prev.Diff != diff || sweep <= 0 {
			return e.sendInfo(s, st)
		}
		sweep = min(sweep, expFloors)
		rewards, err := e.pay(s, role, st, expShare(diff), sweep+sweep/3)
		if err != nil {
			return err
		}
		st.Guard = expNext(expPointID(sweep))
		for _, p := range expCfg.Points { // the boxes passed were paid with the floors
			if p.Type == 2 && p.ID < st.Guard {
				st.Taken = append(st.Taken, p.ID)
			}
		}
		if st.Guard > expLastPoint {
			st.Max = max(st.Max, diff)
		}
		if err := s.Send(24414, proto.Message{"guard_id": expPointID(sweep), "floor_id": sweep, "rewards": rewards}); err != nil {
			return err
		}
		return e.sendInfo(s, st)
	})
}
