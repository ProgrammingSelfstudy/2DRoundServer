package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// whiteday.json: the goddess trial (女神试炼, 28800~28803) of the client, a boss to hit for damage (tools/extract_whiteday.py).
//
//go:embed data/whiteday.json
var whiteDayJSON []byte

const (
	wdModule  = "whiteday"
	wdForm    = 33         // PartnerConst.Fun_Form.WhiteDay
	combatWD  = 41         // BattleConst.Fight_Type.WhiteDayWar
	wdBossHP  = 1000000000 // the boss is never killed: the reward steps stop at 95 million (the client has no hp)
	wdMaxStep = 95000000
)

var wdCfg = func() (c struct {
	Camps map[string]struct {
		ID     int64
		Boss   int64
		Title  string
		Free   int64
		Buy    int64
		Reward [][2]int64
	}
	Steps  map[string][]struct{ Min, Max, Count int64 }
	Prices [][2]int64
	Units  map[string]monsterDef
}) {
	if err := json.Unmarshal(whiteDayJSON, &c); err != nil || len(c.Camps) == 0 || len(c.Prices) == 0 {
		panic("game: bad whiteday.json")
	}
	return
}()

// wdCamps are the camp_ids an operator can open the trial with, sorted.
var wdCamps = func() []int64 {
	var ids []int64
	for k := range wdCfg.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}()

// wdUnit is the monster a boss fights as; a boss the client has no row for (97204) fights as the first one it has.
func wdUnit(boss int64) monsterDef {
	if m, ok := wdCfg.Units[strconv.FormatInt(boss, 10)]; ok {
		return m
	}
	var keys []string
	for k := range wdCfg.Units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return wdCfg.Units[keys[0]]
}

// wdCount is how many of the guaranteed item the damage gives: the step it reaches, none below one point.
func wdCount(id, damage int64) int64 {
	damage = min(damage, wdMaxStep)
	for _, s := range wdCfg.Steps[strconv.FormatInt(id, 10)] {
		if damage >= s.Min && damage <= s.Max {
			return s.Count
		}
	}
	return 0
}

type wdRole struct {
	Camp  int64
	Start int64
	Day   int64
	Used  int64
	Buys  int64
}

// RegisterWhiteDay registers the goddess trial: an operator opens it with one of the camp_ids in whiteday.json; every
// day 2 free fights and some bought ones, one fight on the boss gives the guaranteed item by the damage steps. The other
// drops the client lists (it says "a chance") are not given.
func RegisterWhiteDay(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	type ctx struct {
		role  *store.Role
		r     *wdRole
		camp  int64
		boss  int64
		free  int64
		maxBy int64
		open  bool
	}
	load := func(s *gateway.Session) (*ctx, error) {
		role := s.Role()
		if role == nil {
			return nil, nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		now := s.Now()
		c := &ctx{role: role, r: &wdRole{}}
		var start int64
		for _, id := range wdCamps {
			if a, _, ok := ActivityOpen(st, role.SrvID, id, now); ok {
				c.camp, start, c.open = id, a, true
				break
			}
		}
		if _, err := st.LoadState(role.RID, wdModule, c.r); err != nil {
			return nil, err
		}
		if c.open {
			def := wdCfg.Camps[strconv.FormatInt(c.camp, 10)]
			c.boss, c.free, c.maxBy = def.ID, def.Free, def.Buy
			if c.r.Camp != c.camp || c.r.Start != start {
				*c.r = wdRole{Camp: c.camp, Start: start}
			}
			if day := dayKey(now); c.r.Day != day {
				c.r.Day, c.r.Used, c.r.Buys = day, 0, 0
			}
		}
		return c, nil
	}
	state := func(c *ctx) proto.Message {
		if !c.open {
			return proto.Message{"id": 0, "time": 0, "buy_time": 0}
		}
		return proto.Message{"id": c.boss, "time": max(0, c.free+c.r.Buys-c.r.Used), "buy_time": max(0, c.maxBy-c.r.Buys)}
	}
	r.Handle(28800, func(s *gateway.Session, _ proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		return s.Send(28800, state(c))
	})
	r.Handle(28803, func(s *gateway.Session, _ proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(28803, proto.Message{"flag": 0, "msg": msg}) }
		switch {
		case !c.open:
			return refuse("活动未开启")
		case c.r.Buys >= c.maxBy:
			return refuse("购买次数已用完")
		}
		price := wdCfg.Prices[min(int(c.r.Buys), len(wdCfg.Prices)-1)]
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{price[0], price[1]}}, 1, spendA, spendI)
		if err := st.Exchange(c.role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("钻石不足")
			}
			return err
		}
		c.r.Buys++
		if err := st.SaveState(c.role.RID, wdModule, c.r); err != nil {
			return err
		}
		if err := s.Send(28803, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		if err := s.Send(28800, state(c)); err != nil {
			return err
		}
		return sendAssets(s, st, c.role.RID)
	})
	r.Handle(28801, func(s *gateway.Session, req proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(28801, proto.Message{"flag": 0, "msg": msg}) }
		switch {
		case !c.open:
			return refuse("活动未开启")
		case req.Int("id") != c.boss:
			return refuse("该关卡当前不可挑战")
		case c.r.Used >= c.free+c.r.Buys:
			return refuse("挑战次数不足")
		}
		def := wdCfg.Camps[strconv.FormatInt(c.camp, 10)]
		if _, err := st.GrantStarterPartners(c.role.RID, starterSeeds(c.role.Sex)); err != nil {
			return err
		}
		formType := req.Int("formation_type")
		var slots []store.FormationSlot
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			if id := p.Int("id"); id != 0 && !seen[id] && p.Int("pos") >= 1 {
				seen[id] = true
				slots = append(slots, store.FormationSlot{Pos: p.Int("pos"), PartnerID: id})
			}
		}
		if len(slots) == 0 {
			form, err := formationOf(st, c.role.RID, wdForm)
			if err != nil {
				return err
			}
			slots, formType = form.Slots, form.FormationType
		}
		formType = max(1, formType)
		team, objects, err := fights.teamFor(c.role, formType, slots)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		unit, obj := monsterOf(wdUnit(def.Boss), 0)
		unit.hp, unit.maxHP = wdBossHP, wdBossHP
		obj["hp"], obj["hp_max"], obj["object_name"] = int64(wdBossHP), int64(wdBossHP), def.Title
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
		damage := max(0, wdBossHP-sim.units[len(team)].hp)
		count := wdCount(c.boss, damage)
		ga, gi := map[int64]int64{}, map[int64]int64{}
		if count > 0 && len(def.Reward) > 0 {
			splitGains([]sellGain{{def.Reward[0][0], count * def.Reward[0][1]}}, 1, ga, gi)
			if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return err
			}
		}
		c.r.Used++
		if err := st.SaveState(c.role.RID, wdModule, c.r); err != nil {
			return err
		}
		hurts, best, err := (&tbSvc{st: st}).hurts(c.role, slots, damage)
		if err != nil {
			return err
		}
		if err := s.Send(28801, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		reward := []proto.Message{}
		if count > 0 && len(def.Reward) > 0 {
			reward = append(reward, proto.Message{"base_id": def.Reward[0][0], "num": count * def.Reward[0][1]})
		}
		end := proto.Message{"all_dps": damage, "best_partner": best, "target_role_name": def.Title, "hurt_statistics": hurts, "reward": reward}
		return fights.begin(s, c.role, combatWD, tbIntro(c.role, combatWD, formType, objects, obj, len(team), def.Title), rounds, false, func(s *gateway.Session, _ bool) error {
			if err := s.Send(28802, end); err != nil {
				return err
			}
			if err := s.Send(28800, state(c)); err != nil {
				return err
			}
			if err := sendAssets(s, st, c.role.RID); err != nil {
				return err
			}
			return sendBackpack(s, st)
		})
	})
}
