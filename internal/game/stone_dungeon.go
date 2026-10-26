package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// stone_dungeon.json: the material dungeons of the client (tools/extract_stone_dungeon.py).
//
//go:embed data/stone_dungeon.json
var stoneDungeonJSON []byte

const combatStoneDungeon = 14 // BattleConst.Fight_Type.DungeonStone

type stoneDungeon struct {
	ID, Type, Difficulty, LevLimit, PreID, Power, UnitID int64
	Name, Title                                          string
	First, Show                                          [][]int64
	Monsters                                             []towerMonster
}

type stoneConf struct {
	Free     int64
	HookCost int64 `json:"hook_cost"`
	Types    []int64
	Dungeons []stoneDungeon
	Buys     map[string][]struct{ Cost, VIP int64 }
	OpenLev  map[string]int64 `json:"open_lev"`

	byID map[int64]stoneDungeon
}

var stoneCfg = func() *stoneConf {
	c := &stoneConf{}
	if err := json.Unmarshal(stoneDungeonJSON, c); err != nil || len(c.Dungeons) == 0 || len(c.Types) == 0 {
		panic("game: bad stone_dungeon.json")
	}
	c.byID = map[int64]stoneDungeon{}
	for _, d := range c.Dungeons {
		c.byID[d.ID] = d
	}
	return c
}()

// stoneAllowed is how many attempts a kind has a day for a VIP level: the free ones and every extra one whose VIP
// requirement is met (the client's buy table lists the attempts in order, each with the VIP it needs).
func stoneAllowed(typ int64, vip int) int64 {
	var n int64
	for i, b := range stoneCfg.Buys[strconv.FormatInt(typ, 10)] {
		if b.VIP <= int64(vip) {
			n = int64(i + 1)
		}
	}
	return n
}

// stoneCost is the gems the next attempt of a kind costs after `done` attempts today.
func stoneCost(typ, done int64) int64 {
	list := stoneCfg.Buys[strconv.FormatInt(typ, 10)]
	if done < 0 || int(done) >= len(list) {
		return 0
	}
	return list[done].Cost
}

// RegisterStoneDungeon registers the material dungeons: 13030 the state, 13031 a challenge (the result comes as
// 20006) and 13032 a sweep of a cleared dungeon. A kind has 2 free attempts a day and more for gems and VIP, the
// day turns at 5:00 and a dungeon opens when the one before it is cleared; the client also asks for the role's
// power (what 10306 says) to reach the dungeon's. The monsters are those of the dungeon's unit group, fought
// with the heroes' own attack. Our rules, not captured.
func RegisterStoneDungeon(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	rngOf := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	state := func(s *gateway.Session) error {
		role := s.Role()
		counts, err := st.StoneCounts(role.RID, summonDay(s.Now()))
		if err != nil {
			return err
		}
		cleared, err := st.StoneCleared(role.RID)
		if err != nil {
			return err
		}
		list, pass := []proto.Message{}, []proto.Message{}
		for _, t := range stoneCfg.Types {
			list = append(list, proto.Message{"type": t, "day_num": counts[t]})
		}
		for _, d := range stoneCfg.Dungeons {
			if cleared[d.ID] {
				pass = append(pass, proto.Message{"id": d.ID})
			}
		}
		return s.Send(13030, proto.Message{"list": list, "pass_list": pass})
	}
	r.Handle(13030, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return state(s)
	})
	// check says whether the role may enter a dungeon now, and with which attempt it would be paid
	check := func(s *gateway.Session, id int64, sweep bool) (d stoneDungeon, role *store.Role, cleared map[int64]bool, done int64, msg string, err error) {
		me := s.Role()
		if me == nil {
			return d, nil, nil, 0, "", errors.New("no role")
		}
		role, err = st.RoleByID(me.RID)
		if err != nil {
			return
		}
		d, ok := stoneCfg.byID[id]
		if !ok {
			return d, role, nil, 0, "副本不存在", nil
		}
		if cleared, err = st.StoneCleared(role.RID); err != nil {
			return
		}
		heroes, err2 := st.PartnersOf(role.RID)
		if err2 != nil {
			return d, role, cleared, 0, "", err2
		}
		counts, err2 := st.StoneCounts(role.RID, summonDay(s.Now()))
		if err2 != nil {
			return d, role, cleared, 0, "", err2
		}
		done = counts[d.Type]
		switch {
		case int64(role.Lev) < stoneCfg.OpenLev[strconv.FormatInt(d.Type, 10)] || int64(role.Lev) < d.LevLimit:
			msg = "等级不足"
		case d.PreID != 0 && !cleared[d.PreID]:
			msg = "请先通关前一个难度"
		case rolePower(heroes) < d.Power:
			msg = "战力不足"
		case sweep && !cleared[d.ID]:
			msg = "通关后才能扫荡"
		case done >= stoneAllowed(d.Type, role.VIPLev):
			msg = "今日已无次数"
		}
		return
	}
	pay := func(role *store.Role, s *gateway.Session, d stoneDungeon, done int64) error {
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		if cost := stoneCost(d.Type, done); cost > 0 {
			spendA[3] = cost
		}
		return st.StoneAttempt(role.RID, d.Type, summonDay(s.Now()), stoneAllowed(d.Type, role.VIPLev), spendA, spendI)
	}
	rewards := func(d stoneDungeon, first bool) [][2]int64 {
		out := pairsOf(d.Show)
		if first {
			out = append(out, pairsOf(d.First)...)
		}
		return out
	}
	give := func(s *gateway.Session, role *store.Role, list [][2]int64) error {
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(list), 1, ga, gi)
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}
	r.Handle(13031, func(s *gateway.Session, req proto.Message) error {
		d, role, _, done, msg, err := check(s, req.Int("id"), false)
		if err != nil || role == nil {
			return err
		}
		refuse := func(m string) error { return s.Send(13031, proto.Message{"code": 0, "msg": m}) }
		if msg != "" {
			return refuse(msg)
		}
		team, objects, form, err := fights.teamSide(role)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		var foes []fightUnit
		for i, m := range d.Monsters {
			if i == len(monsterPos) {
				break
			}
			unit, obj := monsterOf(m.def(), i)
			foes = append(foes, unit)
			objects = append(objects, obj)
		}
		if err := pay(role, s, d, done); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足或今日已无次数")
		} else if err != nil {
			return err
		}
		rounds, won, _ := playFightOpts(team, foes, rngOf(), maxFightRounds, true)
		intro := proto.Message{
			"combat_type": combatStoneDungeon, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": objects, "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1, "extra_args": []proto.Message{{"param": d.ID}}, "string_ext_args": []proto.Message{},
			"a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": d.Name, "suppress": 0, "halo_list": []proto.Message{},
			"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(13031, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return fights.begin(s, role, combatStoneDungeon, intro, rounds, won, func(s *gateway.Session, won bool) error {
			stats, _, _, _, err := damageTable(st, role)
			if err != nil {
				return err
			}
			result := int64(2)
			var gained [][2]int64
			if won {
				result = 1
				first, err := st.MarkStoneCleared(role.RID, d.ID)
				if err != nil {
					return err
				}
				gained = rewards(d, first)
				if err := give(s, role, gained); err != nil {
					return err
				}
				if err := questEvent(s, st, role.RID, evtStoneFight, 1); err != nil {
					return err
				}
			}
			items := []proto.Message{}
			for _, g := range gained {
				items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
			}
			if err := s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 0, "current_time": 0, "best_time": 0,
				"combat_type": combatStoneDungeon, "target_role_name": d.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}}); err != nil {
				return err
			}
			return state(s)
		})
	})
	r.Handle(13032, func(s *gateway.Session, req proto.Message) error {
		d, role, _, done, msg, err := check(s, req.Int("id"), true)
		if err != nil || role == nil {
			return err
		}
		refuse := func(m string) error { return s.Send(13032, proto.Message{"code": 0, "msg": m, "id": req.Int("id")}) }
		if msg != "" {
			return refuse(msg)
		}
		if err := pay(role, s, d, done); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足或今日已无次数")
		} else if err != nil {
			return err
		}
		if err := give(s, role, rewards(d, false)); err != nil {
			return err
		}
		if err := s.Send(13032, proto.Message{"code": 1, "msg": "扫荡成功", "id": d.ID}); err != nil {
			return err
		}
		return state(s)
	})
}
