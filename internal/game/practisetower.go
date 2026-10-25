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

// practise_tower.json: the practise tower ("兔丼监狱") of the client (tools/extract_practise_tower.py).
//
//go:embed data/practise_tower.json
var practiseTowerJSON []byte

const (
	ptModule     = "practisetower"
	ptRankModule = "practisetower_rank"
	combatPT     = 43 // BattleConst.Fight_Type.PractiseTower
	ptHPPerPower = 3  // a floor's boss has this many hp per point of the floor's recommended power: a team of that power (about 0.9 of its power in damage a fight) needs three fights
	ptRankShown  = 200
	ptEventDays  = 7 // the tower is always open; the end time sent is this far ahead
)

type ptFloor struct {
	ID     int64     `json:"id"`
	Power  int64     `json:"power"`
	Reward [][]int64 `json:"reward"`
}

type ptRankReward struct {
	Min   int64     `json:"min"`
	Max   int64     `json:"max"`
	Items [][]int64 `json:"items"`
}

type ptConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Floors []ptFloor                  `json:"floors"`
	Ranks  []ptRankReward             `json:"ranks"`
}

func (c *ptConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var ptCfg = func() *ptConf {
	c := &ptConf{}
	if err := json.Unmarshal(practiseTowerJSON, c); err != nil || len(c.Floors) != 40 || len(c.Ranks) == 0 {
		panic("game: bad practise_tower.json")
	}
	return c
}()

func ptPairs(list [][]int64) [][2]int64 {
	var out [][2]int64
	for _, p := range list {
		if len(p) > 1 {
			out = append(out, [2]int64{p[0], p[1]})
		}
	}
	return out
}

// ptSnap is the state before the last fight, which "fight again" goes back to.
type ptSnap struct {
	Done, Left, Time int64
}

type ptState struct {
	Day    int64
	Done   int64 // floors cleared (the highest)
	At     int64 // when the highest floor was cleared
	Left   int64 // hp left of the boss of floor Done+1 (0 = untouched, full)
	Time   int64 // attempts left today
	Bought int64 // attempts bought today
	Redo   int64 // "fight again" used today
	Power  int64 // the role's power when it last cleared a floor
	Paid   []int64
	Snap   *ptSnap
}

type ptRankRow struct {
	RID   int64
	Name  string
	SrvID string
	Lev   int64
	Face  int64
	Val   int64
	At    int64
	Power int64
}

type ptRanks struct{ Rows []ptRankRow }

type ptSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterPracticeTower registers the practise tower (29100-29107): forty floors, each with a boss whose hp is carried
// over from fight to fight until it is killed, a few attempts a day (some of them bought), a "fight again" that undoes the
// last fight without costing an attempt, and a ranking by floors cleared. The boss's hp is its floor's recommended power
// times a constant, the tower is open all the time and the rank rewards are never mailed (there is no end to the event).
// Our rules, not captured.
func RegisterPracticeTower(r *gateway.Router, db *store.Store) {
	p := &ptSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(29100, p.info)
	r.Handle(29101, p.fight)
	r.Handle(29103, p.redo)
	r.Handle(29104, p.buy)
	r.Handle(29105, p.rank)
	r.Handle(29106, p.myRank)
	r.Handle(29107, func(*gateway.Session, proto.Message) error { return nil })
}

func (st *ptState) roll(now time.Time) {
	day := dayKey(now)
	if st.Day == day {
		return
	}
	st.Day, st.Time, st.Bought, st.Redo, st.Snap = day, ptCfg.num("holiday_practise_tower_free_time"), 0, 0, nil
}

func (st *ptState) paid(floor int64) bool {
	for _, f := range st.Paid {
		if f == floor {
			return true
		}
	}
	return false
}

func (p *ptSvc) with(s *gateway.Session, fn func(role *store.Role, st *ptState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := p.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &ptState{}
	if _, err := p.st.LoadState(role.RID, ptModule, st); err != nil {
		return err
	}
	st.roll(s.Now())
	if err := fn(role, st); err != nil {
		return err
	}
	return p.st.SaveState(role.RID, ptModule, st)
}

func ptBossHP(floor int64) int64 { return ptCfg.Floors[floor-1].Power * ptHPPerPower }

// radio is the boss's hp in per cent on the next floor.
func (st *ptState) radio() int64 {
	if st.Done >= int64(len(ptCfg.Floors)) {
		return 0
	}
	if st.Left <= 0 {
		return 100
	}
	return max(1, st.Left*100/ptBossHP(st.Done+1))
}

func (p *ptSvc) rankOf(srv string, rid int64) (rank int64, rows []ptRankRow, err error) {
	r := &ptRanks{}
	if _, err = p.st.LoadGlobal(srv, ptRankModule, r); err != nil {
		return
	}
	rows = r.Rows
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Val != rows[j].Val {
			return rows[i].Val > rows[j].Val
		}
		return rows[i].At < rows[j].At
	})
	for i, row := range rows {
		if row.RID == rid {
			rank = int64(i + 1)
		}
	}
	return
}

func (p *ptSvc) note(role *store.Role, st *ptState) error {
	r := &ptRanks{}
	if _, err := p.st.LoadGlobal(role.SrvID, ptRankModule, r); err != nil {
		return err
	}
	row := ptRankRow{RID: role.RID, Name: role.Name, SrvID: role.SrvID, Lev: int64(role.Lev), Face: int64(role.FaceID), Val: st.Done, At: st.At, Power: st.Power}
	for i := range r.Rows {
		if r.Rows[i].RID == role.RID {
			r.Rows[i] = row
			return p.st.SaveGlobal(role.SrvID, ptRankModule, r)
		}
	}
	r.Rows = append(r.Rows, row)
	return p.st.SaveGlobal(role.SrvID, ptRankModule, r)
}

func ptRowMessage(i int, row ptRankRow) proto.Message {
	return proto.Message{"rid": row.RID, "srv_id": row.SrvID, "lev": row.Lev, "face_id": row.Face, "avatar_bid": 0, "name": row.Name, "val": row.Val,
		"unixtime": row.At, "power": row.Power, "rank": i + 1, "video_id": 0, "face_update_time": 0, "face_file": ""}
}

func (p *ptSvc) sendInfo(s *gateway.Session, role *store.Role, st *ptState) error {
	rank, rows, err := p.rankOf(role.SrvID, role.RID)
	if err != nil {
		return err
	}
	top := []proto.Message{}
	for i, row := range rows {
		if i < 3 {
			top = append(top, ptRowMessage(i, row))
		}
	}
	recombat := int64(0)
	if st.Snap != nil && st.Redo < ptCfg.num("holiday_practise_tower_reset_time") {
		recombat = 1
	}
	return s.Send(29100, proto.Message{"flag": 1, "id": st.Done, "time": st.Time, "last_buy_time": max(0, ptCfg.num("holiday_practise_tower_buy_time")-st.Bought),
		"radio": st.radio(), "last_unixtime": s.Now().Add(ptEventDays * 24 * time.Hour).Unix(), "practise_role_rank": top, "role_rank": rank, "is_recombat": recombat})
}

func (p *ptSvc) info(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *ptState) error { return p.sendInfo(s, role, st) })
}

// buy (29104): one more attempt for diamonds.
func (p *ptSvc) buy(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *ptState) error {
		refuse := func(msg string) error { return s.Send(29104, proto.Message{"flag": 0, "msg": msg}) }
		if st.Bought >= ptCfg.num("holiday_practise_tower_buy_time") || st.Time >= ptCfg.num("holiday_practise_tower_max_buy_time") {
			return refuse("购买次数已达上限")
		}
		var cost struct{ Loss [][]int64 }
		_ = json.Unmarshal(ptCfg.Const["holiday_practise_tower_buy_loss"], &cost.Loss)
		spend := map[int64]int64{}
		for _, c := range ptPairs(cost.Loss) {
			spend[c[0]] += c[1]
		}
		if err := p.st.Exchange(role.RID, spend, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			return refuse("钻石不足")
		}
		st.Bought++
		st.Time++
		if err := sendAssets(s, p.st, role.RID); err != nil {
			return err
		}
		if err := s.Send(29104, proto.Message{"flag": 1, "msg": "购买成功"}); err != nil {
			return err
		}
		return p.sendInfo(s, role, st)
	})
}

// redo (29103): back to before the last fight; the attempt is given back.
func (p *ptSvc) redo(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *ptState) error {
		refuse := func(msg string) error { return s.Send(29103, proto.Message{"flag": 0, "msg": msg}) }
		switch {
		case st.Snap == nil:
			return refuse("没有可以重新挑战的战斗")
		case st.Redo >= ptCfg.num("holiday_practise_tower_reset_time"):
			return refuse("今日重新挑战次数已用完")
		}
		st.Done, st.Left, st.Time = st.Snap.Done, st.Snap.Left, st.Snap.Time
		st.Snap = nil
		st.Redo++
		if err := p.note(role, st); err != nil {
			return err
		}
		if err := s.Send(29103, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return p.sendInfo(s, role, st)
	})
}

// fight (29101) plays the boss of the next floor with the formation sent.
func (p *ptSvc) fight(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *ptState) error {
		refuse := func(msg string) error { return s.Send(29101, proto.Message{"flag": 0, "msg": msg}) }
		floor := st.Done + 1
		switch {
		case floor > int64(len(ptCfg.Floors)):
			return refuse("已通关全部楼层")
		case req.Int("id") != 0 && req.Int("id") != floor:
			return refuse("楼层不对")
		case st.Time <= 0:
			return refuse("挑战次数不足")
		}
		var slots []store.FormationSlot
		seen := map[int64]bool{}
		for _, q := range req.Arr("pos_info") {
			if id := q.Int("id"); id != 0 && !seen[id] && q.Int("pos") >= 1 && len(slots) < formMaxHeroes {
				seen[id] = true
				slots = append(slots, store.FormationSlot{Pos: q.Int("pos"), PartnerID: id})
			}
		}
		if len(slots) == 0 {
			return refuse("至少需要上阵一个英雄")
		}
		formType := max(1, req.Int("formation_type"))
		team, objects, err := p.fights.teamFor(role, formType, slots)
		if err != nil {
			return err
		}
		if len(team) == 0 || guildBossCfg.Bosses[0].Monster == nil {
			return refuse("没有可以出战的英雄")
		}
		hp := ptBossHP(floor)
		if st.Left > 0 {
			hp = st.Left
		}
		unit, obj := monsterOf(*guildBossCfg.Bosses[0].Monster, 0)
		unit.hp, unit.maxHP = hp, ptBossHP(floor)
		obj["hp"], obj["hp_max"], obj["object_name"] = hp, ptBossHP(floor), "兔丼狱卒"
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
		damage := max(0, hp-sim.units[len(team)].hp)
		intro := proto.Message{
			"combat_type": combatPT, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{{"param": floor}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": "兔丼狱卒",
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(29101, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		st.Time--
		st.Snap = &ptSnap{Done: st.Done, Left: st.Left, Time: st.Time + 1}
		return p.fights.begin(s, role, combatPT, intro, rounds, damage >= hp, func(s *gateway.Session, _ bool) error {
			return p.settle(s, floor, damage, slots)
		})
	})
}

func (p *ptSvc) settle(s *gateway.Session, floor, damage int64, slots []store.FormationSlot) error {
	return p.with(s, func(role *store.Role, st *ptState) error {
		if st.Done+1 != floor { // the state moved on meanwhile
			return nil
		}
		hp := ptBossHP(floor)
		if st.Left > 0 {
			hp = st.Left
		}
		all, err := p.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		won := damage >= hp
		prize := [][2]int64{}
		if won {
			st.Done, st.Left, st.At = floor, 0, s.Now().Unix()
			st.Power = rolePower(bestHeroes(all, 5))
			if !st.paid(floor) {
				st.Paid = append(st.Paid, floor)
				prize = ptPairs(ptCfg.Floors[floor-1].Reward)
			}
		} else {
			st.Left = hp - damage
		}
		if len(prize) > 0 {
			ga, gi := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(prize), 1, ga, gi)
			if err := p.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return err
			}
			if err := sendAssets(s, p.st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, p.st); err != nil {
				return err
			}
		}
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		var hurts []proto.Message
		var best, bestPower int64
		var bestH store.Partner
		for _, sl := range slots {
			h, ok := byID[sl.PartnerID]
			if !ok {
				continue
			}
			pw := partnerMessage(h).Int("power")
			hurts = append(hurts, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": partnerTable[h.BID].Camp,
				"dps": damage / int64(len(slots)), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
			if pw > bestPower {
				best, bestPower, bestH = h.BID, pw, h
			}
		}
		flag := int64(2)
		if won {
			flag = 1
		}
		if err := s.Send(29102, proto.Message{"flag": flag, "all_dps": damage, "best_partner": best, "bid": best, "lev": bestH.Lev, "star": bestH.Star, "use_skin": 0, "target_role_name": "兔丼狱卒",
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": hurts}}, "reward": awardRewards(prize), "last_anew_times": max(0, ptCfg.num("holiday_practise_tower_reset_time")-st.Redo), "number": floor}); err != nil {
			return err
		}
		if won {
			if err := p.note(role, st); err != nil {
				return err
			}
			if rank, _, err := p.rankOf(role.SrvID, role.RID); err == nil {
				if err := s.Send(29106, proto.Message{"role_rank": rank}); err != nil {
					return err
				}
			}
		}
		return p.sendInfo(s, role, st)
	})
}

func awardRewards(items [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, it := range items {
		out = append(out, proto.Message{"base_id": it[0], "num": it[1]})
	}
	return out
}

// rank (29105): the list and the role's own place.
func (p *ptSvc) rank(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	rank, rows, err := p.rankOf(role.SrvID, role.RID)
	if err != nil {
		return err
	}
	list := []proto.Message{}
	var val, at int64
	for i, row := range rows {
		if i < ptRankShown {
			list = append(list, ptRowMessage(i, row))
		}
		if row.RID == role.RID {
			val, at = row.Val, row.At
		}
	}
	return s.Send(29105, proto.Message{"flag": 1, "practise_role_rank": list, "role_rank": rank, "role_val": val, "role_unixtime": at, "role_video_id": 0})
}

func (p *ptSvc) myRank(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	rank, _, err := p.rankOf(role.SrvID, role.RID)
	if err != nil {
		return err
	}
	return s.Send(29106, proto.Message{"role_rank": rank})
}
