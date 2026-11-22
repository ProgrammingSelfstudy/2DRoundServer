package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"sync/atomic"
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
	ID, Type, Difficulty, Power int64
	LevLimit                    int64 `json:"lev_limit"`
	PreID                       int64 `json:"pre_id"`
	UnitID                      int64 `json:"unit_id"`
	Name, Title                 string
	First, Show                 [][]int64
	Monsters                    []towerMonster
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

func stoneDungeonTeam(fights *FightService, role *store.Role, heroes []store.Partner, h5 bool) ([]fightUnit, []proto.Message, store.Formation, error) {
	if !h5 {
		return fights.teamSide(role)
	}
	form, err := fights.Store.Formation(role.RID, formDrama)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fallback := defaultFormation(formDrama, heroes)
		form = &fallback
	case err != nil:
		return nil, nil, store.Formation{}, err
	default:
		allowed := make(map[int64]bool, len(heroes))
		for _, h := range heroes {
			allowed[h.PartnerID] = true
		}
		form.Slots = slices.DeleteFunc(form.Slots, func(slot store.FormationSlot) bool {
			return !allowed[slot.PartnerID]
		})
	}
	team, objects, err := fights.teamFor(role, form.FormationType, form.Slots)
	return team, objects, *form, err
}

// RegisterStoneDungeon registers the material dungeons: 13030 the state, 13031 a challenge (the result comes as
// 20006) and 13032 a sweep of a cleared dungeon. A kind has 2 free attempts a day and more for gems and VIP, the
// day turns at 5:00, and H5 applies the configured level and previous-clear requirements for each difficulty.
// The role's power must reach the dungeon's. The monsters are those of the dungeon's unit group, fought
// with the heroes' own attack. The pushes follow the capture original-2026-11-05-materials (13031 and
// 13032 of the first gold dungeon); the rules around them are ours.
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
	check := func(s *gateway.Session, id, day int64, sweep, h5 bool) (d stoneDungeon, role *store.Role, heroes []store.Partner, done int64, msg string, err error) {
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
		cleared, err := st.StoneCleared(role.RID)
		if err != nil {
			return
		}
		heroes, err = st.PartnersOf(role.RID)
		if err != nil {
			return
		}
		heroes = dungeonHeroes(heroes, h5)
		counts, err := st.StoneCounts(role.RID, day)
		if err != nil {
			return
		}
		done = counts[d.Type]
		switch {
		case int64(role.Lev) < stoneCfg.OpenLev[strconv.FormatInt(d.Type, 10)] || (h5 && int64(role.Lev) < d.LevLimit):
			msg = "等级不足"
		case h5 && d.PreID != 0 && !cleared[d.PreID]:
			msg = "通关前置难度后开启"
		case rolePower(heroes) < d.Power:
			msg = "战力不足"
		case sweep && !cleared[d.ID]:
			msg = "通关后才能扫荡"
		case done >= stoneAllowed(d.Type, role.VIPLev):
			// the original names the VIP level the next attempt needs (capture original-2026-11-05-dungeons)
			if list := stoneCfg.Buys[strconv.FormatInt(d.Type, 10)]; int(done) < len(list) {
				msg = fmt.Sprintf("需要vip等级达%d级后可购买", list[done].VIP)
			} else {
				msg = "今日已无次数"
			}
		}
		return
	}
	pay := func(role *store.Role, d stoneDungeon, day, done int64, assets, items map[int64]int64) error {
		return st.TakeStoneAttempt(role.RID, d.Type, day, done, stoneAllowed(d.Type, role.VIPLev), stoneCost(d.Type, done), assets, items)
	}
	r.Handle(13031, func(s *gateway.Session, req proto.Message) error {
		h5 := dungeonH5(s)
		day := summonDay(s.Now())
		d, role, heroes, done, msg, err := check(s, req.Int("id"), day, false, h5)
		if err != nil || role == nil {
			return err
		}
		refuse := func(m string) error { return s.Send(13031, proto.Message{"code": 0, "msg": m}) }
		if msg != "" {
			return refuse(msg)
		}
		team, objects, form, err := stoneDungeonTeam(fights, role, heroes, h5)
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
		if err := pay(role, d, day, done, nil, nil); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足或今日已无次数")
		} else if err != nil {
			return err
		}
		// a paid attempt shows its gems at once: 10302 comes before the quest and the answer (capture
		// original-2026-11-05-dungeons); the sweep carries them in the 10302 of its reward
		if stoneCost(d.Type, done) > 0 {
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
		}
		rounds, won, _ := playFightOpts(team, foes, rngOf(), maxFightRounds, true)
		intro := proto.Message{
			"combat_type": combatStoneDungeon, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": objects, "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1, "extra_args": []proto.Message{{"param": d.ID}}, "string_ext_args": []proto.Message{},
			"a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": d.Name, "suppress": 0, "halo_list": []proto.Message{},
			"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		// the daily quest and the feat count when the fight starts, before the answer (capture
		// original-2026-11-05-materials: 10409 and 16401 come before 13031)
		if err := questEvent(s, st, role.RID, evtStoneFight, 1); err != nil {
			return err
		}
		if err := s.Send(13031, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		var settled atomic.Bool
		return fights.begin(s, role, combatStoneDungeon, intro, rounds, won, func(s *gateway.Session, won bool) error {
			if settled.Swap(true) {
				return nil
			}
			var stats []proto.Message
			var err error
			if h5 {
				stats, _, _, _ = formationDamage(role, form, heroes)
			} else {
				stats, _, _, _, err = damageTable(st, role)
				if err != nil {
					return err
				}
			}
			result := int64(2)
			var gained [][2]int64
			if won {
				result = 1
				before, err := snapshotBags(st, role.RID)
				if err != nil {
					return err
				}
				gained = stoneLoot(d.Show, h5)
				firstLoot := stoneLoot(d.First, h5)
				assets, items := dungeonGains(gained)
				firstAssets, firstItems := dungeonGains(firstLoot)
				first, err := st.ClearStoneDungeon(role.RID, d.ID, assets, items, firstAssets, firstItems)
				if err != nil {
					return err
				}
				if first {
					gained = append(gained, firstLoot...)
				}
				if err := sendStoneBalances(s, st, role.RID, before); err != nil {
					return err
				}
			}
			items := []proto.Message{}
			for _, g := range gained {
				items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
			}
			// the state comes before the result (capture original-2026-11-05-materials: 10302, 10512, 13030, 20006)
			if err := state(s); err != nil {
				return err
			}
			return s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0,
				"room_id": 0, "adventure_end_hp": 1000, "ext_list": []proto.Message{},
				"combat_type": combatStoneDungeon, "target_role_name": d.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}})
		})
	})
	r.Handle(13032, func(s *gateway.Session, req proto.Message) error {
		h5 := dungeonH5(s)
		day := summonDay(s.Now())
		d, role, _, done, msg, err := check(s, req.Int("id"), day, true, h5)
		if err != nil || role == nil {
			return err
		}
		refuse := func(m string) error { return s.Send(13032, proto.Message{"code": 0, "msg": m, "id": req.Int("id")}) }
		if msg != "" {
			return refuse(msg)
		}
		before, err := snapshotBags(st, role.RID)
		if err != nil {
			return err
		}
		assets, items := dungeonGains(stoneLoot(d.Show, h5))
		if err := pay(role, d, day, done, assets, items); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足或今日已无次数")
		} else if err != nil {
			return err
		}
		if err := sendStoneBalances(s, st, role.RID, before); err != nil {
			return err
		}
		gain := make([]sellGain, 0, len(assets)+len(items))
		for _, id := range mergedKeys(assets, items) {
			gain = append(gain, sellGain{id, assets[id] + items[id]})
		}
		// capture original-2026-11-05-materials: 10302, 10512, 12742, the quest (10409) and feat
		// (16401), 13030, then the answer with an empty msg
		if err := sendGainList(s, gain); err != nil {
			return err
		}
		if err := questEvent(s, st, role.RID, evtStoneFight, 1); err != nil {
			return err
		}
		if err := state(s); err != nil {
			return err
		}
		return s.Send(13032, proto.Message{"code": 1, "msg": "", "id": d.ID})
	})
}
