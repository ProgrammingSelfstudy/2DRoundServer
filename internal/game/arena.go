package game

import (
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The arena (the loop match), from the client's arena_data.lua and its Lua. A role has a score and
// challenges the three opponents on its list; a won challenge moves score from the opponent to it. All of
// the numbers below that the client names are quoted; the rest are our own and listed in OPEN_QUESTIONS.
const (
	combatArena      = 2    // BattleConst.Fight_Type.Arena
	arenaInitScore   = 1000 // init_score
	arenaMinScore    = 800  // min_score
	arenaFreeCombats = 5    // combat_num: free challenges a day
	arenaRefreshCD   = 3    // ref_time: seconds between two list refreshes
	arenaLimitLev    = 8    // limit_lev
	arenaKeepPermil  = 700  // reset_score: the share of the score kept into the next season
	arenaTicket      = 10103
	arenaTicketGems  = 20 // ticket_price
	arenaLoseRatio   = 600
	arenaWinCoin     = 30000 // ours: the drops 20002 / 20003 are on the server only
	arenaOpponents   = 3
	arenaTopList     = 50
	arenaRobotBase   = 4000000000 // robot rids start here, above any real role
)

// season_num_reward: the chests opened by the number of challenges of the season.
var arenaChests = []struct {
	Num   int64
	Items [][2]int64
}{
	{10, [][2]int64{{10002, 5}}}, {20, [][2]int64{{10403, 1}}}, {35, [][2]int64{{10403, 1}}}, {50, [][2]int64{{37002, 1}}},
	{70, [][2]int64{{37002, 1}}}, {90, [][2]int64{{14001, 1}}}, {120, [][2]int64{{14001, 1}}},
}

var arenaRobotNames = []string{"海军少尉", "草帽路人", "无名剑士", "东海猎手", "偉大航路客", "冒险家乔", "赏金猎人", "神秘旅人", "航海见习生", "港口卫兵"}
var arenaRobotFaces = []int64{10508, 20401, 20402, 20403} // face_list

type arenaHero struct{ Pos, BID, Lev, Star, Break int64 }

// arenaOpp is who an opponent is, as stored on the list. A robot keeps its line-up; a real role's is read
// when it is fought.
type arenaOpp struct {
	RID      int64
	SrvID    string
	Name     string
	Lev      int64
	Sex      int64
	Face     int64
	Power    int64
	Score    int64
	Robot    bool
	FormType int64
	Lineup   []arenaHero
}

// arenaLogEntry is one line of the fight log as it is stored: Type 1 is a challenge by the role, 2 one against it;
// Ret 1 is a win of the role, 2 a loss; Score is the role's change.
type arenaLogEntry struct {
	RID, Lev, Sex, Face, Power, Score, Type, Ret, ReplayID int64
	SrvID, Name                                            string
}

// arenaSeason names the season of a moment by the Monday it began and gives its first and last second.
func arenaSeason(now time.Time) (key, start, end int64) {
	t := now.In(dayZone)
	monday := t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
	start = dayStart(monday).Unix()
	return dayKey(monday), start, start + 7*86400 - 1
}

// arenaGain is the score a win is worth: more against a stronger opponent.
func arenaGain(mine, theirs int64) int64 {
	g := 20 + (theirs-mine)/10
	return min(max(g, 5), 60)
}

// arenaTouch brings a standing up to date: the day's counters and the season's.
func arenaTouch(a *store.ArenaPlayer, now time.Time) {
	if day := dayKey(now); a.Day != day {
		a.Day, a.FreeUsed, a.Bought = day, 0, 0
	}
	if key, _, _ := arenaSeason(now); a.Season != key {
		if a.Season != 0 {
			a.Score = max(arenaInitScore, a.Score*arenaKeepPermil/1000)
		}
		a.Season, a.Battles, a.ContWin = key, 0, 0
	}
}

func (o arenaOpp) heroMessages() []proto.Message {
	out := []proto.Message{}
	for _, h := range o.Lineup {
		out = append(out, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "quality": h.Star, "star": h.Star, "break_lev": h.Break, "use_skin": 0, "resonate_lev": 0})
	}
	return out
}

// arenaHeroes reads who stands where in a real role's campaign line-up.
func arenaHeroes(st *store.Store, rid int64) (slots []store.FormationSlot, formType int64, byID map[int64]store.Partner, err error) {
	form, err := formationOf(st, rid, formDrama)
	if err != nil {
		return nil, 0, nil, err
	}
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return nil, 0, nil, err
	}
	byID = map[int64]store.Partner{}
	for _, h := range heroes {
		byID[h.PartnerID] = h
	}
	slots = append(slots, form.Slots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Pos < slots[j].Pos })
	return slots, form.FormationType, byID, nil
}

// fighters turns an opponent into what a fight needs.
func (o arenaOpp) fighters(st *store.Store) (slots []store.FormationSlot, formType int64, byID map[int64]store.Partner, err error) {
	if !o.Robot {
		return arenaHeroes(st, o.RID)
	}
	byID = map[int64]store.Partner{}
	for i, h := range o.Lineup {
		id := int64(i + 1)
		byID[id] = store.Partner{PartnerID: id, RID: o.RID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star), BreakLev: int(h.Break)}
		slots = append(slots, store.FormationSlot{Pos: h.Pos, PartnerID: id})
	}
	return slots, o.FormType, byID, nil
}

// lineupOf describes a real role's line-up for the lists.
func lineupOf(st *store.Store, rid int64) ([]arenaHero, int64, int64, error) {
	slots, formType, byID, err := arenaHeroes(st, rid)
	if err != nil {
		return nil, 0, 0, err
	}
	var out []arenaHero
	var all []store.Partner
	for _, sl := range slots {
		if h, ok := byID[sl.PartnerID]; ok {
			if _, known := partnerTable[h.BID]; known {
				out = append(out, arenaHero{sl.Pos, h.BID, int64(h.Lev), int64(h.Star), int64(h.BreakLev)})
				all = append(all, h)
			}
		}
	}
	return out, formType, rolePower(all), nil
}

func oppFromRole(st *store.Store, r *store.Role, score int64) (arenaOpp, bool, error) {
	lineup, formType, power, err := lineupOf(st, r.RID)
	if err != nil || len(lineup) == 0 {
		return arenaOpp{}, false, err
	}
	face := int64(r.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	return arenaOpp{RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: int64(r.Lev), Sex: int64(r.Sex), Face: face, Power: power, Score: score, FormType: formType, Lineup: lineup}, true, nil
}

// newOpponents picks the three opponents of a role: real roles whose score is closest, and robots (a copy of
// the role's own team, a little weaker or stronger) to fill up.
func newOpponents(st *store.Store, role *store.Role, score int64, rng *rand.Rand) ([]arenaOpp, error) {
	var out []arenaOpp
	near, err := st.ArenaNear(role.SrvID, role.RID, score, 12)
	if err != nil {
		return nil, err
	}
	for _, n := range near {
		if len(out) == arenaOpponents {
			break
		}
		r, err := st.RoleByID(n.RID)
		if err != nil {
			continue
		}
		o, ok, err := oppFromRole(st, r, n.Score)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, o)
		}
	}
	mine, formType, _, err := lineupOf(st, role.RID)
	if err != nil {
		return nil, err
	}
	for i := len(out); i < arenaOpponents; i++ {
		o := arenaOpp{RID: arenaRobotBase + role.RID%1000000*10 + int64(i), SrvID: role.SrvID, Robot: true, FormType: formType,
			Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))], Sex: int64(rng.Intn(2) + 1), Face: arenaRobotFaces[rng.Intn(len(arenaRobotFaces))],
			Score: max(arenaMinScore, score+int64(i-1)*40+int64(rng.Intn(21)-10))}
		factor := 85 + int64(rng.Intn(21)) + int64(i)*3 // 85-105 per cent of the role's levels, rising down the list
		var all []store.Partner
		for _, h := range mine {
			lev := max(1, h.Lev*factor/100)
			o.Lineup = append(o.Lineup, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
			all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
		}
		o.Power = rolePower(all)
		o.Lev = int64(role.Lev)
		out = append(out, o)
	}
	return out, nil
}

// RegisterArena registers the arena: 20200 my standing, 20201 the list, 20202 an opponent, 20203 challenge,
// 20206 refresh, 20207 buy tickets, 20208/20209 the season chests, 20220-20223 the ranking and the log. The
// result is 20210.
func RegisterArena(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	rngOf := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	standing := func(s *gateway.Session) (*store.ArenaPlayer, error) {
		a, err := st.ArenaPlayerOf(s.Role().RID, arenaInitScore, s.Now().Unix())
		if err != nil {
			return nil, err
		}
		arenaTouch(a, s.Now())
		return a, st.SaveArenaPlayer(a)
	}
	// the opponents on the list, made when there are none
	opponents := func(s *gateway.Session, a *store.ArenaPlayer, force bool) ([]store.ArenaSlot, error) {
		role := s.Role()
		slots, err := st.ArenaSlots(role.RID)
		if err != nil {
			return nil, err
		}
		if len(slots) > 0 && !force {
			return slots, nil
		}
		if _, err := st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
			return nil, err
		}
		opps, err := newOpponents(st, role, a.Score, rngOf())
		if err != nil {
			return nil, err
		}
		slots = nil
		for i, o := range opps {
			raw, _ := json.Marshal(o)
			slots = append(slots, store.ArenaSlot{Idx: int64(i + 1), OppRID: o.RID, Info: string(raw)})
		}
		return slots, st.SetArenaSlots(role.RID, slots)
	}
	slotMessage := func(sl store.ArenaSlot, mine int64) (proto.Message, arenaOpp) {
		var o arenaOpp
		_ = json.Unmarshal([]byte(sl.Info), &o)
		return proto.Message{"idx": sl.Idx, "rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "sex": o.Sex, "face": o.Face, "power": o.Power,
			"score": o.Score, "get_score": arenaGain(mine, o.Score), "status": sl.Status, "p_list": o.heroMessages(), "face_update_time": 0, "face_file": "",
			"gid": 0, "gsrv_id": "", "gname": ""}, o
	}
	sendList := func(s *gateway.Session, a *store.ArenaPlayer, kind int64, only int64) error {
		slots, err := opponents(s, a, false)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, sl := range slots {
			if only == 0 || sl.Idx == only {
				m, _ := slotMessage(sl, a.Score)
				list = append(list, m)
			}
		}
		return s.Send(20201, proto.Message{"f_list": list, "type": kind})
	}
	standingMessage := func(s *gateway.Session, a *store.ArenaPlayer) (proto.Message, error) {
		rank, err := st.ArenaRank(a.RID)
		if err != nil {
			return nil, err
		}
		_, start, end := arenaSeason(s.Now())
		return proto.Message{"rank": rank, "score": a.Score, "can_combat_num": max(0, arenaFreeCombats-a.FreeUsed), "buy_combat_num": a.Bought,
			"ref_time": a.RefTime, "start_time": start, "end_time": end, "cont_win": a.ContWin}, nil
	}
	r.Handle(20200, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		m, err := standingMessage(s, a)
		if err != nil {
			return err
		}
		return s.Send(20200, m)
	})
	r.Handle(20201, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		return sendList(s, a, 0, 0)
	})
	r.Handle(20206, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		if left := a.RefTime - s.Now().Unix(); left > 0 {
			return s.Send(20206, proto.Message{"code": 0, "msg": "刷新太频繁了，请稍后再试"})
		}
		a.RefTime = s.Now().Unix() + arenaRefreshCD
		if err := st.SaveArenaPlayer(a); err != nil {
			return err
		}
		if _, err := opponents(s, a, true); err != nil {
			return err
		}
		if err := s.Send(20206, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return sendList(s, a, 0, 0)
	})
	r.Handle(20202, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		slots, err := opponents(s, a, false)
		if err != nil {
			return err
		}
		for _, sl := range slots {
			if sl.OppRID == req.Int("rid") {
				_, o := slotMessage(sl, a.Score)
				return s.Send(20202, proto.Message{"rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "face": o.Face, "power": o.Power, "score": o.Score,
					"formation_type": o.FormType, "formation_lev": 1, "p_list": o.heroMessages(), "face_update_time": 0, "face_file": "", "gid": 0, "gsrv_id": "", "gname": ""})
			}
		}
		return nil
	})
	r.Handle(20207, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		n := req.Int("num")
		if n <= 0 || n > 99 {
			return s.Send(20207, proto.Message{"code": 0, "msg": "购买数量不对"})
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		err = st.Exchange(role.RID, map[int64]int64{3: arenaTicketGems * n}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{arenaTicket: n})
		if errors.Is(err, store.ErrConflict) {
			return s.Send(20207, proto.Message{"code": 0, "msg": "钻石不足"})
		}
		if err != nil {
			return err
		}
		a.Bought += n
		if err := st.SaveArenaPlayer(a); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(20207, proto.Message{"code": 1, "msg": "购买成功"})
	})
	// the season chests
	r.Handle(20208, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		claimed, err := st.ArenaClaimed(a.RID, a.Season)
		if err != nil {
			return err
		}
		nums := []proto.Message{}
		for _, n := range claimed {
			nums = append(nums, proto.Message{"num": n})
		}
		return s.Send(20208, proto.Message{"had_combat_num": a.Battles, "num_list": nums})
	})
	r.Handle(20209, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		for _, c := range arenaChests {
			if c.Num != req.Int("num") {
				continue
			}
			if a.Battles < c.Num {
				return s.Send(20209, proto.Message{"code": 0, "msg": "挑战次数不足"})
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(c.Items), 1, ga, gi)
			err := st.ClaimArenaChest(role.RID, a.Season, c.Num, ga, gi)
			if errors.Is(err, store.ErrConflict) {
				return s.Send(20209, proto.Message{"code": 0, "msg": "已经领取过了"})
			}
			if err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			return s.Send(20209, proto.Message{"code": 1, "msg": "领取成功"})
		}
		return s.Send(20209, proto.Message{"code": 0, "msg": "参数错误"})
	})
	// the ranking: the first three as statues, the first fifty as a list
	top := func(s *gateway.Session, n int) ([]proto.Message, error) {
		rows, err := st.ArenaTop(s.Role().SrvID, n)
		if err != nil {
			return nil, err
		}
		out := []proto.Message{}
		for i, row := range rows {
			r, err := st.RoleByID(row.RID)
			if err != nil {
				continue
			}
			face := int64(r.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			_, _, power, _ := lineupOf(st, r.RID)
			out = append(out, proto.Message{"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "face": face, "sex": r.Sex, "rank": i + 1, "score": row.Score,
				"power": power, "avatar_id": 0, "lookid": 0, "face_update_time": 0, "face_file": "", "worship": 0, "worship_status": 0})
		}
		return out, nil
	}
	r.Handle(20220, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		list, err := top(s, 3)
		if err != nil {
			return err
		}
		return s.Send(20220, proto.Message{"rank_list": list})
	})
	r.Handle(20221, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		rank, err := st.ArenaRank(a.RID)
		if err != nil {
			return err
		}
		list, err := top(s, arenaTopList)
		if err != nil {
			return err
		}
		return s.Send(20221, proto.Message{"rank": rank, "score": a.Score, "worship": 0, "rank_list": list})
	})
	r.Handle(20222, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		logs, err := st.ArenaLogs(a.RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, l := range logs {
			var e arenaLogEntry
			if json.Unmarshal([]byte(l.Info), &e) == nil {
				out = append(out, proto.Message{"rid": e.RID, "srv_id": e.SrvID, "name": e.Name, "lev": e.Lev, "sex": e.Sex, "face": e.Face, "avatar_id": 0,
					"power": e.Power, "score": e.Score, "type": e.Type, "ret": e.Ret, "replay_id": e.ReplayID, "time": l.Time, "face_update_time": 0, "face_file": ""})
			}
		}
		if a.LogSeen != int64(len(logs)) {
			a.LogSeen = int64(len(logs))
			if err := st.SaveArenaPlayer(a); err != nil {
				return err
			}
		}
		return s.Send(20222, proto.Message{"log_list": out})
	})
	r.Handle(20203, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(20203, proto.Message{"code": 0, "msg": msg}) }
		if fresh, err := st.RoleByID(role.RID); err != nil {
			return err
		} else if fresh.Lev < arenaLimitLev {
			return refuse("等级不足，竞技场需要达到8级")
		}
		a, err := standing(s)
		if err != nil {
			return err
		}
		slots, err := opponents(s, a, false)
		if err != nil {
			return err
		}
		var slot *store.ArenaSlot
		var opp arenaOpp
		for i := range slots {
			if slots[i].OppRID == req.Int("rid") {
				slot = &slots[i]
				_ = json.Unmarshal([]byte(slots[i].Info), &opp)
			}
		}
		switch {
		case slot == nil:
			return refuse("对手不存在，请刷新列表")
		case slot.Status != 0:
			return refuse("已经挑战过该对手")
		}
		team, objects, form, err := fights.teamSide(role)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		oslots, oform, obyID, err := opp.fighters(st)
		if err != nil {
			return err
		}
		foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
		if len(foes) == 0 {
			return refuse("对手已无法应战，请刷新列表")
		}
		// pay: a free challenge first, else a ticket
		if a.FreeUsed < arenaFreeCombats {
			a.FreeUsed++
		} else if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{arenaTicket: 1}, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("挑战次数不足")
		} else if err != nil {
			return err
		}
		if err := st.SaveArenaPlayer(a); err != nil {
			return err
		}
		intro := proto.Message{
			"combat_type": combatArena, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
			"objects": append(objects, foeObjects...), "is_auto": req.Int("is_auto"), "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": opp.Name,
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		rounds, won := buildFight(team, foes, rngOf())
		if err := s.Send(20203, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		idx := slot.Idx
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		_, _, power, _ := lineupOf(st, role.RID)
		replayID := recordReplay(st, combatArena, replaySide{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Power: power, Form: form.FormationType},
			replaySide{RID: opp.RID, SrvID: opp.SrvID, Name: opp.Name, Lev: opp.Lev, Face: opp.Face, Power: opp.Power, Form: oform}, intro, rounds, won, s.Now().Unix())
		return fights.begin(s, role, combatArena, intro, rounds, won, func(s *gateway.Session, won bool) error {
			return settleArena(s, st, idx, opp, won, replayID)
		})
	})
}

// settleArena pays and records a finished arena fight and sends the result (20210).
func settleArena(s *gateway.Session, st *store.Store, idx int64, opp arenaOpp, won bool, replayID int64) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	a, err := st.ArenaPlayerOf(role.RID, arenaInitScore, s.Now().Unix())
	if err != nil {
		return err
	}
	arenaTouch(a, s.Now())
	var gain, lose int64
	coin := int64(arenaWinCoin)
	result := int64(2)
	if won {
		result = 1
		gain = arenaGain(a.Score, opp.Score)
		lose = gain
		a.Score += gain
		a.ContWin++
	} else {
		coin = coin * arenaLoseRatio / 1000
		a.ContWin = 0
	}
	a.Battles++
	lifeCount(st, role.RID, "evt_arena_fight", 1)
	if err := st.SaveArenaPlayer(a); err != nil {
		return err
	}
	if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{1: coin}, map[int64]int64{}); err != nil {
		return err
	}
	if err := st.MarkArenaSlot(role.RID, idx, 1); err != nil {
		return err
	}
	now := s.Now().Unix()
	logOf := func(typ, ret, change int64, name string, lev, sex, face, power int64, rid int64, srv string) string {
		raw, _ := json.Marshal(arenaLogEntry{RID: rid, SrvID: srv, Name: name, Lev: lev, Sex: sex, Face: face, Power: power, Score: change, Type: typ, Ret: ret, ReplayID: replayID})
		return string(raw)
	}
	if err := st.AddArenaLog(role.RID, logOf(1, result, gain, opp.Name, opp.Lev, opp.Sex, opp.Face, opp.Power, opp.RID, opp.SrvID), now); err != nil {
		return err
	}
	if !opp.Robot {
		o, err := st.ArenaPlayerOf(opp.RID, arenaInitScore, now)
		if err != nil {
			return err
		}
		arenaTouch(o, s.Now())
		defRet := int64(1)
		if won {
			o.Score = max(arenaMinScore, o.Score-lose)
			defRet = 2
		}
		if err := st.SaveArenaPlayer(o); err != nil {
			return err
		}
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		_, _, power, _ := lineupOf(st, role.RID)
		if err := st.AddArenaLog(opp.RID, logOf(2, defRet, -lose, role.Name, int64(role.Lev), int64(role.Sex), face, power, role.RID, role.SrvID), now); err != nil {
			return err
		}
	}
	stats, bid, hurt, total, err := damageTable(st, role)
	if err != nil {
		return err
	}
	if err := sendAssets(s, st, role.RID); err != nil {
		return err
	}
	if err := s.Send(20210, proto.Message{"result": result, "score": a.Score, "get_score": gain, "items": []proto.Message{{"bid": 1, "num": coin}},
		"tar_name": opp.Name, "tar_lev": opp.Lev, "tar_face": opp.Face, "face_update_time": 0, "face_file": "", "tar_score": opp.Score - lose, "lose_score": lose,
		"target_role_name": opp.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "replay_id": replayID,
		"partner_bid": bid, "partner_hurt": hurt, "partner_total_hurt": total}); err != nil {
		return err
	}
	slots, err := st.ArenaSlots(role.RID)
	if err != nil {
		return err
	}
	for _, sl := range slots {
		if sl.Idx == idx {
			var o arenaOpp
			_ = json.Unmarshal([]byte(sl.Info), &o)
			return s.Send(20201, proto.Message{"f_list": []proto.Message{{"idx": sl.Idx, "rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "sex": o.Sex, "face": o.Face,
				"power": o.Power, "score": o.Score, "get_score": gain, "status": sl.Status, "p_list": o.heroMessages(), "face_update_time": 0, "face_file": "", "gid": 0, "gsrv_id": "", "gname": ""}}, "type": 1})
		}
	}
	return nil
}
