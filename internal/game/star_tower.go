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

// star_tower.json: the star tower ("海底监狱") of the client (tools/extract_star_tower.py).
//
//go:embed data/star_tower.json
var starTowerJSON []byte

const combatStarTower = 7 // BattleConst.Fight_Type.StarTower

// towerMonster is a monster of a floor, stored as [bid, name, career, camp, lev, head].
type towerMonster struct {
	Bid, Career, Camp, Lev int64
	Name, Head             string
}

func (m *towerMonster) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil || len(a) < 6 {
		return errors.New("bad tower monster")
	}
	for i, p := range []any{&m.Bid, &m.Name, &m.Career, &m.Camp, &m.Lev, &m.Head} {
		if err := json.Unmarshal(a[i], p); err != nil {
			return err
		}
	}
	return nil
}

type towerFloor struct {
	Lev       int64
	Name      string
	Recommend int64
	First     [][]int64
	First2    [][]int64
	Award     [][]int64
	Monsters  []towerMonster
}

type towerConf struct {
	Free       int64
	OpenLev    int64 `json:"open_lev"`
	Floors     []towerFloor
	Milestones []struct {
		ID, Tower int64
		Award     [][]int64
	}
	Buys []struct {
		ID     int64
		Expend [][]int64
	}
	VipBuys []int64 `json:"vip_buys"`
}

var towerCfg = func() *towerConf {
	c := &towerConf{}
	if err := json.Unmarshal(starTowerJSON, c); err != nil || len(c.Floors) == 0 || len(c.Milestones) == 0 || len(c.Buys) == 0 {
		panic("game: bad star_tower.json")
	}
	return c
}()

// headSkills are the active skills of the hero template a monster is made of (its lowest star).
func headSkills(head string) []int64 {
	bid, err := strconv.ParseInt(head, 10, 64)
	if err != nil {
		return nil
	}
	info, ok := partnerTable[bid]
	if !ok {
		return nil
	}
	star := lowestStar(bid)
	var out []int64
	for _, sk := range info.Stars[strconv.Itoa(star)].Skills {
		if r, ok := skillTable[sk[1]]; ok && r.Type == "active_skill" {
			out = append(out, sk[1])
		}
	}
	return out
}

func (m towerMonster) def() monsterDef {
	return monsterDef{Bid: m.Bid, Name: m.Name, Career: m.Career, Camp: m.Camp, Lev: m.Lev, Head: m.Head, Skills: headSkills(m.Head)}
}

// towerVipBuys is how many challenges a VIP level may buy a day.
func towerVipBuys(vip int) int64 {
	v := towerCfg.VipBuys
	return v[min(max(vip, 0), len(v)-1)]
}

// towerCount brings a role's day up to date (it turns at 5:00) and says how many challenges are left.
func towerCount(t *store.StarTower, now time.Time) int64 {
	if day := summonDay(now); t.Day != day {
		t.Day, t.Used, t.Bought = day, 0, 0
	}
	return max(0, towerCfg.Free+t.Bought-t.Used)
}

func towerGains(lists ...[][]int64) [][2]int64 {
	var out [][2]int64
	for _, l := range lists {
		out = append(out, pairsOf(l)...)
	}
	return out
}

func awardMessages(list [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, e := range list {
		out = append(out, proto.Message{"item_id": e[0], "num": e[1]})
	}
	return out
}

// RegisterStarTower registers the star tower: 11320 the state, 11321 buy a challenge, 11322 climb a floor (the
// result comes as 11323), 11324 sweep a cleared floor, 11325 the floor's best replays (none), 11327 the top
// three, 11328 take a milestone reward. A floor is fought like a campaign dungeon, but with the heroes' own
// attack; the first win of a floor is free, every other fight or sweep takes one of the day's challenges
// (2 free, 3 or more bought with gems). Our rules, not captured.
func RegisterStarTower(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	rngOf := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	milestoneList := func(rid int64, t *store.StarTower) ([]proto.Message, error) {
		claimed, err := st.TowerClaimed(rid)
		if err != nil {
			return nil, err
		}
		out := make([]proto.Message, 0, len(towerCfg.Milestones))
		for _, m := range towerCfg.Milestones {
			status := int64(0)
			switch {
			case claimed[m.ID]:
				status = 2
			case t.MaxTower >= m.Tower:
				status = 1
			}
			out = append(out, proto.Message{"id": m.ID, "status": status})
		}
		return out, nil
	}
	// open: the level the tower is for
	open := func(s *gateway.Session) (*store.Role, *store.StarTower, bool, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, false, nil
		}
		fresh, err := st.RoleByID(role.RID)
		if err != nil {
			return nil, nil, false, err
		}
		t, err := st.StarTowerOf(role.RID)
		if err != nil {
			return nil, nil, false, err
		}
		towerCount(t, s.Now())
		return fresh, t, int64(fresh.Lev) >= towerCfg.OpenLev, nil
	}
	r.Handle(11320, func(s *gateway.Session, _ proto.Message) error {
		role, t, ok, err := open(s)
		if role == nil || err != nil {
			return err
		}
		if !ok {
			return s.Send(11320, proto.Message{"result": 0, "msg": "角色" + strconv.FormatInt(towerCfg.OpenLev, 10) + "级开启试练塔", "max_tower": 0, "count": 0, "buy_count": 0, "award_list": []proto.Message{}})
		}
		list, err := milestoneList(role.RID, t)
		if err != nil {
			return err
		}
		return s.Send(11320, proto.Message{"result": 1, "msg": "", "max_tower": t.MaxTower, "count": towerCount(t, s.Now()), "buy_count": t.Bought, "award_list": list})
	})
	r.Handle(11321, func(s *gateway.Session, _ proto.Message) error {
		role, t, ok, err := open(s)
		if role == nil || err != nil {
			return err
		}
		refuse := func(msg string) error {
			return s.Send(11321, proto.Message{"result": 0, "msg": msg, "count": towerCount(t, s.Now()), "buy_count": t.Bought})
		}
		switch {
		case !ok:
			return refuse("等级不足")
		case int(t.Bought) >= len(towerCfg.Buys) || t.Bought >= towerVipBuys(role.VIPLev):
			return refuse("今日购买次数已达上限")
		}
		price := towerCfg.Buys[t.Bought]
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(pairsOf(price.Expend)), 1, spendA, spendI)
		if err := st.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足")
		} else if err != nil {
			return err
		}
		t.Bought++
		if err := st.SaveStarTower(t); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return s.Send(11321, proto.Message{"result": 1, "msg": "购买成功", "count": towerCount(t, s.Now()), "buy_count": t.Bought})
	})
	// pay what a won floor gives and tell the client (11323)
	finish := func(s *gateway.Session, role *store.Role, floor towerFloor, first, skip bool, t *store.StarTower) error {
		gains := towerGains(floor.Award)
		var firstGains [][2]int64
		if first {
			firstGains = towerGains(floor.First, floor.First2)
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(append(append([][2]int64{}, gains...), firstGains...)), 1, ga, gi)
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		stats, _, _, _, err := damageTable(st, role)
		if err != nil {
			return err
		}
		return s.Send(11323, proto.Message{"result": 1, "max_tower": t.MaxTower, "tower": floor.Lev, "timer": 0, "count": towerCount(t, s.Now()),
			"first_award": awardMessages(firstGains), "award": awardMessages(gains), "target_role_name": floor.Name,
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "replay_id": 0, "is_skip": b2i(skip)})
	}
	r.Handle(11322, func(s *gateway.Session, req proto.Message) error {
		role, t, ok, err := open(s)
		if role == nil || err != nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(11322, proto.Message{"result": 0, "msg": msg}) }
		n := req.Int("tower")
		switch {
		case !ok:
			return refuse("等级不足")
		case n < 1 || n > int64(len(towerCfg.Floors)) || n > t.MaxTower+1:
			return refuse("请先通关前面的层数")
		}
		floor := towerCfg.Floors[n-1]
		first := n > t.MaxTower
		if !first {
			if towerCount(t, s.Now()) <= 0 {
				return refuse("挑战次数不足")
			}
			t.Used++
		}
		team, objects, form, err := fights.teamSide(role)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		var foes []fightUnit
		for i, m := range floor.Monsters {
			if i == len(monsterPos) {
				break
			}
			unit, obj := monsterOf(m.def(), i)
			foes = append(foes, unit)
			objects = append(objects, obj)
		}
		rounds, won, _ := playFightOpts(team, foes, rngOf(), maxFightRounds, true)
		if !first {
			if err := st.SaveStarTower(t); err != nil {
				return err
			}
		}
		intro := proto.Message{
			"combat_type": combatStarTower, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": objects, "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1, "extra_args": []proto.Message{{"param": n}}, "string_ext_args": []proto.Message{},
			"a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": floor.Name, "suppress": 0, "halo_list": []proto.Message{},
			"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := questEvent(s, st, role.RID, evtStarTower, 1); err != nil {
			return err
		}
		if err := s.Send(11322, proto.Message{"result": 1, "msg": ""}); err != nil {
			return err
		}
		return fights.begin(s, role, combatStarTower, intro, rounds, won, func(s *gateway.Session, won bool) error {
			if !won {
				stats, _, _, _, err := damageTable(st, role)
				if err != nil {
					return err
				}
				return s.Send(11323, proto.Message{"result": 2, "max_tower": t.MaxTower, "tower": n, "timer": 0, "count": towerCount(t, s.Now()), "first_award": []proto.Message{},
					"award": []proto.Message{}, "target_role_name": floor.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "replay_id": 0, "is_skip": 0})
			}
			fresh, err := st.StarTowerOf(role.RID)
			if err != nil {
				return err
			}
			towerCount(fresh, s.Now())
			wasFirst := n > fresh.MaxTower
			if wasFirst {
				fresh.MaxTower, fresh.ClearedAt = n, s.Now().Unix()
				if err := st.SaveStarTower(fresh); err != nil {
					return err
				}
			}
			return finish(s, role, floor, wasFirst, false, fresh)
		})
	})
	r.Handle(11324, func(s *gateway.Session, req proto.Message) error {
		role, t, ok, err := open(s)
		if role == nil || err != nil {
			return err
		}
		refuse := func(msg string) error {
			return s.Send(11324, proto.Message{"result": 0, "msg": msg, "count": towerCount(t, s.Now()), "buy_count": t.Bought})
		}
		n := req.Int("tower")
		switch {
		case !ok:
			return refuse("等级不足")
		case n < 1 || n > t.MaxTower:
			return refuse("通关后才能扫荡")
		case towerCount(t, s.Now()) <= 0:
			return refuse("挑战次数不足")
		}
		t.Used++
		if err := st.SaveStarTower(t); err != nil {
			return err
		}
		if err := s.Send(11324, proto.Message{"result": 1, "msg": "", "count": towerCount(t, s.Now()), "buy_count": t.Bought}); err != nil {
			return err
		}
		return finish(s, role, towerCfg.Floors[n-1], false, true, t)
	})
	r.Handle(11325, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(11325, proto.Message{"tower_replay_data": []proto.Message{}, "m_replay_id": 0, "my_time": 0})
	})
	r.Handle(11327, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		top, err := st.TowerTop(role.SrvID, 3)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for i, row := range top {
			who, err := st.RoleByID(row.RID)
			if err != nil {
				continue
			}
			out = append(out, proto.Message{"rid": who.RID, "srv_id": who.SrvID, "name": who.Name, "rank": i + 1, "tower": row.Tower})
		}
		return s.Send(11327, proto.Message{"rank_lists": out})
	})
	r.Handle(11328, func(s *gateway.Session, req proto.Message) error {
		role, t, ok, err := open(s)
		if role == nil || err != nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(11328, proto.Message{"result": 0, "msg": msg}) }
		if !ok {
			return refuse("等级不足")
		}
		for _, m := range towerCfg.Milestones {
			if m.ID != req.Int("id") {
				continue
			}
			if t.MaxTower < m.Tower {
				return refuse("还没有通关第" + strconv.FormatInt(m.Tower, 10) + "层")
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(pairsOf(m.Award)), 1, ga, gi)
			if err := st.ClaimTowerMilestone(role.RID, m.ID, ga, gi); errors.Is(err, store.ErrConflict) {
				return refuse("已经领取过了")
			} else if err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			if err := s.Send(11328, proto.Message{"result": 1, "msg": "领取成功"}); err != nil {
				return err
			}
			return s.Send(11329, proto.Message{"award_list": []proto.Message{{"id": m.ID, "status": 2}}})
		}
		return refuse("奖励不存在")
	})
}
