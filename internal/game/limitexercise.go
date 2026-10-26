package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// limit_exercise.json: the limit exercise ("试炼之境") of the client (tools/extract_limitexercise.py).
//
//go:embed data/limit_exercise.json
var limitExerciseJSON []byte

const (
	leModule   = "limitexercise"
	combatLE   = 29 // BattleConst.Fight_Type.LimitExercise
	formLE     = 21 // PartnerConst.Fun_Form.LimitExercise
	leLevels   = 15 // levels of a week, five to an area (down, keep, up)
	leHPPerPow = 3  // a level's boss has this many hp per point of its recommended power (as the practise tower)
	leAreaSize = 5
)

type leBoss struct {
	Order int64    `json:"order"`
	Name  string   `json:"name"`
	Power int64    `json:"power"`
	Head  int64    `json:"head"`
	Desc  []string `json:"desc"`
}

type leReward struct {
	Order  int64     `json:"order"`
	Reward [][]int64 `json:"reward"`
}

type leConf struct {
	Const   map[string]json.RawMessage `json:"const"`
	Rounds  [][][][]leBoss             `json:"rounds"` // [round][difficulty][area][level]
	Rewards [][]leReward               `json:"rewards"`
}

func (c *leConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var leCfg = func() *leConf {
	c := &leConf{}
	if err := json.Unmarshal(limitExerciseJSON, c); err != nil || len(c.Rounds) != 4 || len(c.Rewards) == 0 {
		panic("game: bad limit_exercise.json")
	}
	return c
}()

// boss is the boss of a level (1-15) of a round and difficulty (both from 1).
func (c *leConf) boss(round, diff, order int64) leBoss {
	areas := c.Rounds[(round-1)%int64(len(c.Rounds))][min(diff, int64(len(c.Rewards)))-1]
	return areas[(order-1)/leAreaSize][(order-1)%leAreaSize]
}

// tier is the settle reward (1-3) that clearing up to Order-1 levels has earned, 0 for none.
func leTier(cleared int64) int64 { return min(3, cleared/leAreaSize) }

type leState struct {
	Week    int64
	Day     int64
	Diff    int64
	Order   int64 // the level to fight next (leFullClear when all are cleared)
	Left    int64 // hp left of that level's boss (0 = untouched)
	Count   int64 // attempts left today
	Buy     int64 // attempts bought today
	Fights  int64 // fights this week
	Bosses  int64 // bosses beaten this week
	Claimed bool  // the box of the week taken
	Used    map[int64]int64
	Engaged bool // the first difficulty is picked
	Round   int64
}

type leSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterLimitExercise registers the limit exercise (25410-25414): fifteen levels a week in three areas, each level a boss
// whose hp is carried over from fight to fight until it is killed, two free attempts a day (and two bought), a box
// when every level is cleared, and a limit on how often a hero can be used a week. The week turns on Monday: the
// round changes, the difficulty goes up when the up area was cleared and down when the down area was not, and a box
// not taken is mailed. Our rules, not captured.
func RegisterLimitExercise(r *gateway.Router, db *store.Store) {
	l := &leSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(25410, l.info)
	r.Handle(25411, l.buy)
	r.Handle(25412, l.box)
	r.Handle(25413, l.fight)
	r.Handle(25414, l.used)
}

func leRound(week int64) int64 { return (week/7)%int64(len(leCfg.Rounds)) + 1 }

func leWeekEnd(now time.Time) int64 {
	return dayStart(now.In(dayZone)).AddDate(0, 0, 7-(int(now.In(dayZone).Weekday())+6)%7).Unix()
}

// roll moves the state to today and this week, mailing the box of a week that is over.
func (l *leSvc) roll(role *store.Role, st *leState, now time.Time) error {
	week := homeWeek(now)
	if st.Week != week {
		if st.Engaged {
			cleared := st.Order - 1
			if tier := leTier(cleared); tier > 0 && !st.Claimed {
				reward := leCfg.Rewards[min(st.Diff, int64(len(leCfg.Rewards)))-1][tier-1].Reward
				m := store.Mail{SrvID: role.SrvID, Subject: "试炼之境结算奖励", Content: "上一期试炼之境你通关了" + itoa64(cleared) + "关，这是结算奖励。",
					FromName: "试炼之境", CreatedBy: "limitexercise", Attachments: attachmentsOf(ptPairs(reward))}
				if _, err := l.st.CreateMail(m, []int64{role.RID}); err != nil {
					return err
				}
			}
			switch {
			case cleared >= leLevels:
				st.Diff = min(st.Diff+1, int64(len(leCfg.Rewards)))
			case cleared < leAreaSize:
				st.Diff = max(st.Diff-1, 1)
			}
		}
		st.Week, st.Order, st.Left, st.Fights, st.Bosses, st.Claimed, st.Used = week, 1, 0, 0, 0, false, nil
		if !st.Engaged {
			all, err := l.st.PartnersOf(role.RID)
			if err != nil {
				return err
			}
			st.Diff, st.Engaged = leMatch(rolePower(bestHeroes(all, 5))), true
		}
	}
	st.Round = leRound(week)
	if day := dayKey(now); st.Day != day {
		st.Day, st.Count, st.Buy = day, leCfg.num("start_fight_count"), 0
	}
	return nil
}

// leMatch is the first difficulty of a role: the highest whose first level is not stronger than the role.
func leMatch(power int64) int64 {
	diff := int64(1)
	for d := int64(1); d <= int64(len(leCfg.Rewards)); d++ {
		if leCfg.boss(1, d, 1).Power <= power {
			diff = d
		}
	}
	return diff
}

func (l *leSvc) with(s *gateway.Session, fn func(role *store.Role, st *leState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := l.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &leState{}
	if _, err := l.st.LoadState(role.RID, leModule, st); err != nil {
		return err
	}
	if err := l.roll(role, st, s.Now()); err != nil {
		return err
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return l.st.SaveState(role.RID, leModule, st)
}

func leHP(st *leState) int64 {
	return leCfg.boss(st.Round, st.Diff, min(st.Order, leLevels)).Power * leHPPerPow
}

func (st *leState) hpPer() int64 {
	if st.Order > leLevels {
		return 0
	}
	if st.Left <= 0 {
		return 100
	}
	return max(1, st.Left*100/leHP(st))
}

func (st *leState) boxStatus() int64 {
	switch {
	case st.Claimed:
		return 2
	case st.Order > leLevels:
		return 1
	}
	return 0
}

func (l *leSvc) send(s *gateway.Session, st *leState) error {
	order := min(st.Order, leLevels)
	return s.Send(25410, proto.Message{"round": st.Round, "difficulty": st.Diff, "order": order, "order_type": (order-1)/leAreaSize + 1,
		"status": st.boxStatus(), "count": st.Count, "buy_count": st.Buy, "endtime": leWeekEnd(s.Now()), "hp_per": st.hpPer(),
		"round_combat": st.Fights, "round_boss": st.Bosses})
}

func (l *leSvc) info(s *gateway.Session, _ proto.Message) error {
	return l.with(s, func(_ *store.Role, st *leState) error { return l.send(s, st) })
}

// buy (25411): one more attempt for diamonds.
func (l *leSvc) buy(s *gateway.Session, _ proto.Message) error {
	return l.with(s, func(role *store.Role, st *leState) error {
		refuse := func(msg string) error {
			return s.Send(25411, proto.Message{"code": 0, "msg": msg, "count": st.Count, "buy_count": st.Buy})
		}
		if st.Buy >= leCfg.num("fight_buy_max_count") {
			return refuse("已达今日购买最大值")
		}
		if err := l.st.Exchange(role.RID, map[int64]int64{3: leCfg.num("action_num_espensive")}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			return refuse("钻石不足")
		}
		st.Buy++
		st.Count++
		if err := sendAssets(s, l.st, role.RID); err != nil {
			return err
		}
		return s.Send(25411, proto.Message{"code": 1, "msg": "购买成功", "count": st.Count, "buy_count": st.Buy})
	})
}

// box (25412): the settle reward of the week, when every level is cleared.
func (l *leSvc) box(s *gateway.Session, _ proto.Message) error {
	return l.with(s, func(role *store.Role, st *leState) error {
		refuse := func(msg string) error { return s.Send(25412, proto.Message{"code": 0, "msg": msg}) }
		switch st.boxStatus() {
		case 2:
			return refuse("奖励已领取")
		case 0:
			return refuse("还没有通关全部关卡")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(ptPairs(leCfg.Rewards[min(st.Diff, int64(len(leCfg.Rewards)))-1][leTier(leLevels)-1].Reward)), 1, ga, gi)
		if err := l.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		st.Claimed = true
		if err := sendAssets(s, l.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, l.st); err != nil {
			return err
		}
		return s.Send(25412, proto.Message{"code": 1, "msg": "领取成功"})
	})
}

// used (25414): how often each hero was used this week.
func (l *leSvc) used(s *gateway.Session, _ proto.Message) error {
	return l.with(s, func(_ *store.Role, st *leState) error {
		ids := make([]int64, 0, len(st.Used))
		for id := range st.Used {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"id": id, "count": st.Used[id]})
		}
		return s.Send(25414, proto.Message{"p_list": list})
	})
}

// fight (25413) plays the next level's boss with the team saved for the limit exercise (11212 of kind 21).
func (l *leSvc) fight(s *gateway.Session, _ proto.Message) error {
	return l.with(s, func(role *store.Role, st *leState) error {
		refuse := func(msg string) error { return s.Send(25413, proto.Message{"code": 0, "msg": msg}) }
		switch {
		case st.Order > leLevels:
			return refuse("本期关卡已全部通关")
		case st.Count <= 0:
			return refuse("今日次数已用完~~")
		}
		form, err := formationOf(l.st, role.RID, formLE)
		if err != nil {
			return err
		}
		limit := leCfg.num("partner_num")
		for _, sl := range form.Slots {
			if st.Used[sl.PartnerID] >= limit {
				return refuse("英雄使用次数已满")
			}
		}
		team, objects, err := l.fights.teamFor(role, max(1, form.FormationType), form.Slots)
		if err != nil {
			return err
		}
		if len(team) == 0 || guildBossCfg.Bosses[0].Monster == nil {
			return refuse("没有可以出战的英雄")
		}
		order, boss := st.Order, leCfg.boss(st.Round, st.Diff, st.Order)
		maxHP := leHP(st)
		hp := maxHP
		if st.Left > 0 {
			hp = st.Left
		}
		unit, obj := monsterOf(*guildBossCfg.Bosses[0].Monster, 0)
		unit.hp, unit.maxHP = hp, maxHP
		obj["hp"], obj["hp_max"], obj["object_name"] = hp, maxHP, boss.Name
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
		damage := max(0, hp-sim.units[len(team)].hp)
		formType := max(1, form.FormationType)
		intro := proto.Message{
			"combat_type": combatLE, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			// the client reads the level's text from list[round][difficulty][area][level] by these four
			"extra_args":      []proto.Message{{"param": st.Round}, {"param": min(st.Diff, int64(len(leCfg.Rewards)))}, {"param": (order-1)/leAreaSize + 1}, {"param": (order-1)%leAreaSize + 1}},
			"string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": boss.Name,
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(25413, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		st.Count--
		st.Fights++
		return l.fights.begin(s, role, combatLE, intro, rounds, damage >= hp, func(s *gateway.Session, _ bool) error {
			return l.settle(s, order, damage, form.Slots)
		})
	})
}

func (l *leSvc) settle(s *gateway.Session, order, damage int64, slots []store.FormationSlot) error {
	return l.with(s, func(role *store.Role, st *leState) error {
		if st.Order != order { // the state moved on meanwhile (a new week)
			return nil
		}
		hp := leHP(st)
		if st.Left > 0 {
			hp = st.Left
		}
		won := damage >= hp
		if won {
			st.Order, st.Left = order+1, 0
			st.Bosses++
			if st.Used == nil {
				st.Used = map[int64]int64{}
			}
			for _, sl := range slots {
				st.Used[sl.PartnerID]++
			}
		} else {
			st.Left = hp - damage
		}
		result := int64(2)
		if won {
			result = 1
		}
		if err := s.Send(20006, proto.Message{"result": result, "item_rewards": []proto.Message{}, "show_panel_type": 1, "current_time": 0, "best_time": 0, "combat_type": combatLE,
			"target_role_name": leCfg.boss(st.Round, st.Diff, min(order, leLevels)).Name, "hurt_statistics": []proto.Message{}, "adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}}); err != nil {
			return err
		}
		return l.send(s, st)
	})
}
