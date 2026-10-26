package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// elitematch.json: the elite match of the client (tools/extract_elitematch.py).
//
//go:embed data/elitematch.json
var elitematchJSON []byte

const (
	eliteModule  = "elitematch"
	combatElite  = 23 // BattleConst.Fight_Type.EliteMatchWar
	eliteLogKeep = 30
	eliteCycle   = 21 // days of a season: 9 + 9 regular, 3 king
)

type eliteLevel struct {
	ID       int64      `json:"id"`
	NextID   int64      `json:"next_id"`
	NeedExp  int64      `json:"need_exp"`
	InitExp  int64      `json:"init_exp"`
	Name     string     `json:"name"`
	Award    [][2]int64 `json:"award"`
	LevAward [][2]int64 `json:"lev_award"`
}

type eliteConf struct {
	Const map[string]json.RawMessage `json:"const"`
	Level []eliteLevel               `json:"level"`
	Buy   []struct {
		Cost [][2]int64 `json:"cost"`
		Vip  int64      `json:"vip"`
	} `json:"buy"`
}

var eliteCfg = func() *eliteConf {
	c := &eliteConf{}
	if err := json.Unmarshal(elitematchJSON, c); err != nil || len(c.Level) == 0 || len(c.Buy) == 0 {
		panic("game: bad elitematch.json")
	}
	return c
}()

func (c *eliteConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *eliteConf) list(key string) []float64 {
	var l []float64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

func (c *eliteConf) level(id int64) eliteLevel {
	for _, l := range c.Level {
		if l.ID == id {
			return l
		}
	}
	return c.Level[0]
}

// eliteMinLev is the level a role needs for the elite match (a variable for the tests).
var eliteMinLev = eliteCfg.num("open_person_lev_limit")

// eliteSlot / eliteForm are a saved line-up of the match as the client sends it (24921).
type eliteSlot struct{ Pos, ID int64 }
type eliteForm struct {
	Order, FormationType, HallowsID int64
	Slots                           []eliteSlot
}

// eliteLog is one line of the fight log of a role: Type 1 is a fight of the role, Ret 1 a win of the role.
type eliteLog struct {
	ID, RID, Lev, Face, Rank, Ret, Time int64
	SrvID, Name                         string
	Power                               int64
	Detail                              *eliteDetail // what the report of the fight (24931) shows
}

// eliteDetail is a fight as the report shows it: the replay, both sides' power, formation and heroes.
type eliteDetail struct {
	Replay, APower, AForm, BForm int64
	AList, BList                 []arenaHero
}

// sideHeroes are the heroes the intro of a fight puts on a side (group 1 ours, 2 theirs); the spirit is not one.
func sideHeroes(intro proto.Message, group int64) []arenaHero {
	var out []arenaHero
	for _, o := range intro.Arr("objects") {
		if o.Int("group") == group && o.Int("object_type") != 5 {
			out = append(out, arenaHero{Pos: o.Int("pos"), BID: o.Int("object_bid"), Lev: o.Int("lev"), Star: o.Int("star")})
		}
	}
	return out
}

// eliteRole is what a role keeps in the match.
type eliteRole struct {
	Day, Used, Bought         int64
	Lev, Score, Period        int64
	Wins, Loses, Streak, Best int64
	Claimed                   []int64 // levels whose reward was taken
	Forms                     map[int64][]eliteForm
	Manifesto                 map[int64]int64
	Logs                      []eliteLog
	Next                      int64 // id of the next log line
	Pending                   *elitePending
}

// elitePending is the opponent matched and waiting for the fight.
type elitePending struct {
	Opp      arenaOpp
	FightAt  int64
	Skip     int64
	Matching int64
}

type eliteSvc struct {
	st  *store.Store
	mu  sync.Mutex
	rng func() *rand.Rand
}

// RegisterElitematch registers the elite match (24900-24950): a daily ranked match of roles from level 100, three free
// fights a day, more for diamonds. A match draws one opponent (a robot that copies the challenger's team; there is
// only one server), the fight moves the score of the rank, which promotes at the score of the level. The zones, the
// season rewards and the pass (24952-24955) are not done. Our rules where the client does not say, not captured.
func RegisterElitematch(r *gateway.Router, db *store.Store) {
	e := &eliteSvc{st: db}
	fights := fightServiceOf(db)
	e.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.Handle(24900, e.info)
	r.Handle(24901, e.enemy)
	r.Handle(24902, e.match)
	r.Handle(24903, e.fight)
	r.Handle(24904, e.buy)
	r.Handle(24905, e.openTime)
	r.Handle(24910, e.zones)
	r.Handle(24911, e.history)
	r.Handle(24915, e.reward)
	r.Handle(24920, e.formation)
	r.Handle(24921, e.saveFormation)
	r.Handle(24930, e.logs)
	r.Handle(24931, e.report)
	r.Handle(24940, e.personal)
	r.Handle(24941, e.share)
	r.Handle(24942, e.shared)
	r.Handle(24945, e.manifesto)
	r.Handle(24946, e.saveManifesto)
}

// eliteOpen says whether the match runs at a moment: every day from 00:01 for 84600 seconds.
func eliteOpen(now time.Time) (open bool, start, end int64) {
	hms := eliteCfg.list("open_time")
	t := dayStart(now.In(dayZone))
	start = t.Unix() + int64(hms[0])*3600 + int64(hms[1])*60 + int64(hms[2])
	end = start + eliteCfg.num("open_times")
	return now.Unix() >= start && now.Unix() < end, start, end
}

func elitePeriod(now time.Time) int64 { return now.Unix()/86400/eliteCycle - 900 }

func (e *eliteSvc) role(rid int64, now time.Time) (*eliteRole, error) {
	er := &eliteRole{}
	if _, err := e.st.LoadState(rid, eliteModule, er); err != nil {
		return nil, err
	}
	if er.Lev == 0 {
		er.Lev = 1
		er.Score = eliteCfg.level(1).InitExp
	}
	if day := dayKey(now); er.Day != day {
		er.Day, er.Used, er.Bought = day, 0, 0
	}
	return er, nil
}

func (e *eliteSvc) left(er *eliteRole) int64 {
	return max(0, eliteCfg.num("match_count")+er.Bought-er.Used)
}

// gate is the reason a role cannot play, empty when it can.
func (e *eliteSvc) gate(role *store.Role, now time.Time) string {
	if int64(role.Lev) < eliteMinLev {
		return "角色等级达到" + itoa64(eliteMinLev) + "级后开启"
	}
	if open, _, _ := eliteOpen(now); !open {
		return "精英段位赛未开启"
	}
	return ""
}

func (e *eliteSvc) refuse(s *gateway.Session, cmd uint16, msg string) error {
	return s.Send(cmd, proto.Message{"code": 0, "msg": msg})
}

// rewardList is the level rewards as the client shows them: flag 0 not reached, 1 to take, 2 taken.
func (e *eliteSvc) rewardList(er *eliteRole) []proto.Message {
	out := []proto.Message{}
	for _, l := range eliteCfg.Level {
		if len(l.LevAward) == 0 {
			continue
		}
		flag := int64(0)
		if er.Lev >= l.ID {
			flag = 1
		}
		for _, c := range er.Claimed {
			if c == l.ID {
				flag = 2
			}
		}
		out = append(out, proto.Message{"lev": l.ID, "flag": flag})
	}
	return out
}

func (e *eliteSvc) promotedInfo(er *eliteRole) []proto.Message {
	return []proto.Message{}
}

func (e *eliteSvc) infoMessage(role *store.Role, er *eliteRole, now time.Time) proto.Message {
	open, start, end := eliteOpen(now)
	state := int64(0)
	if open {
		state = 1
	}
	stateEnd := end
	if !open {
		stateEnd = start
		if now.Unix() >= end {
			stateEnd = start + 86400
		}
	}
	return proto.Message{"lev": er.Lev, "score": er.Score, "day_combat_count": e.left(er), "day_buy_count": er.Bought, "match_cd_time": 0, "rank": 0,
		"is_king": 0, "promoted_info": e.promotedInfo(er), "state": state, "period": elitePeriod(now), "start_time": start, "end_time": end,
		"state_time": stateEnd, "lev_reward": e.rewardList(er), "zone_id": 1, "day_max_buy_count": len(eliteCfg.Buy), "is_skip": 0, "rmb_status": 0}
}

func (e *eliteSvc) info(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	return s.Send(24900, e.infoMessage(role, er, s.Now()))
}

func (e *eliteSvc) openTime(s *gateway.Session, _ proto.Message) error {
	_, start, end := eliteOpen(s.Now())
	open, _, _ := eliteOpen(s.Now())
	state, until := int64(0), start
	if open {
		state, until = 1, end
	}
	return s.Send(24905, proto.Message{"state": state, "end_time": until})
}

func (e *eliteSvc) pendingMessage(er *eliteRole, now time.Time) proto.Message {
	m := proto.Message{"day_combat_count": e.left(er), "match_cd_time": 0, "to_combat_time": 0, "is_match": 0, "rand_list": []proto.Message{}}
	p := er.Pending
	if p == nil {
		return m
	}
	o := p.Opp
	infos := []proto.Message{}
	for _, h := range o.Lineup {
		infos = append(infos, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "power": 0, "extra": []proto.Message{}})
	}
	m["is_match"], m["to_combat_time"] = 1, p.FightAt
	m["rand_list"] = []proto.Message{{"end_time": p.FightAt, "rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "gname": "", "power": o.Power, "lev": o.Lev,
		"sex": o.Sex, "face": o.Face, "look_id": defaultLookID(int(o.Face)), "score": o.Score, "elite_lev": er.Lev, "face_update_time": 0, "face_file": "",
		"defense": []proto.Message{{"type": 1, "defense_info": []proto.Message{{"order": 1, "formation_type": o.FormType, "partner_infos": infos}}}}}}
	return m
}

func (e *eliteSvc) enemy(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	return s.Send(24901, e.pendingMessage(er, s.Now()))
}

// match (24902) draws the opponent: a robot of the challenger's own team, as strong as the rank, ready after a short wait.
func (e *eliteSvc) match(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	role, err := e.st.RoleByID(s.Role().RID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := s.Now()
	er, err := e.role(role.RID, now)
	if err != nil {
		return err
	}
	if e.gate(role, now) != "" || e.left(er) == 0 {
		return s.Send(24901, e.pendingMessage(er, now))
	}
	if er.Pending == nil {
		if _, err := e.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
			return err
		}
		opps, err := newOpponents(e.st, role, 0, e.rng())
		if err != nil {
			return err
		}
		rng := e.rng()
		o := opps[0]
		o.Robot = true
		// a robot copies the team and grows with the rank: 70 per cent at the bottom, 125 at the top
		mine, formType, _, err := lineupOf(e.st, role.RID)
		if err != nil {
			return err
		}
		o.Lineup, o.FormType = nil, formType
		factor := 70 + 55*(er.Lev-1)/int64(len(eliteCfg.Level)-1) + int64(rng.Intn(11)) - 5
		var all []store.Partner
		for _, h := range mine {
			lev := max(1, h.Lev*factor/100)
			o.Lineup = append(o.Lineup, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
			all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
		}
		o.Power, o.Lev = rolePower(all), int64(role.Lev)
		o.Score = eliteCfg.level(er.Lev).InitExp
		wait := eliteCfg.num("no_skip_after_time")
		if req.Int("type") == 1 {
			wait = eliteCfg.num("skip_after_time")
		}
		er.Pending = &elitePending{Opp: o, FightAt: now.Unix() + wait, Skip: req.Int("type")}
		if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
			return err
		}
	}
	return s.Send(24901, e.pendingMessage(er, now))
}

func (e *eliteSvc) fight(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	role, err := e.st.RoleByID(s.Role().RID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	now := s.Now()
	er, err := e.role(role.RID, now)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	refuse := func(msg string) error { e.mu.Unlock(); return e.refuse(s, 24903, msg) }
	if msg := e.gate(role, now); msg != "" {
		return refuse(msg)
	}
	if er.Pending == nil {
		return refuse("还没有匹配到对手")
	}
	if e.left(er) == 0 {
		return refuse("挑战次数不足")
	}
	opp := er.Pending.Opp
	fights := fightServiceOf(e.st)
	team, objects, form, err := fights.teamSide(role)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if len(team) == 0 {
		return refuse("没有可以出战的英雄")
	}
	oslots, oform, obyID, err := opp.fighters(e.st)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return refuse("对手已无法应战")
	}
	er.Used++
	er.Pending = nil
	err = e.st.SaveState(role.RID, eliteModule, er)
	e.mu.Unlock()
	if err != nil {
		return err
	}
	intro := proto.Message{
		"combat_type": combatElite, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
		"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": opp.Name,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	rounds, won := buildFight(team, foes, e.rng())
	if err := s.Send(24903, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	_, _, power, _ := lineupOf(e.st, role.RID)
	replayID := recordReplay(e.st, combatElite, replaySide{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Power: power, Form: form.FormationType},
		replaySide{RID: opp.RID, SrvID: opp.SrvID, Name: opp.Name, Lev: opp.Lev, Face: opp.Face, Power: opp.Power, Form: oform}, intro, rounds, won, now.Unix())
	return fights.begin(s, role, combatElite, intro, rounds, won, func(s *gateway.Session, won bool) error {
		return e.settle(s, opp, won, replayID, face, power, &eliteDetail{Replay: replayID, APower: power, AForm: form.FormationType, BForm: oform,
			AList: sideHeroes(intro, 1), BList: sideHeroes(intro, 2)})
	})
}

// settle moves the score: a win adds the base of the match (30), a loss takes 15 off; the score reaching the need of the
// level promotes to the next level, which starts at its initial score. The score never falls below the level's own.
func (e *eliteSvc) settle(s *gateway.Session, opp arenaOpp, won bool, replayID, face, power int64, detail *eliteDetail) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := s.Now()
	er, err := e.role(role.RID, now)
	if err != nil {
		return err
	}
	cur := eliteCfg.level(er.Lev)
	old, oldScore := er.Lev, er.Score
	add := int64(0)
	var reward [][2]int64
	result := int64(2)
	if won {
		result = 1
		add = int64(eliteCfg.list("normal_win_base_exp_add")[0])
		er.Wins++
		er.Streak++
	} else {
		add = -int64(eliteCfg.list("normal_lose_base_exp_down")[0])
		er.Loses++
		er.Streak = 0
	}
	er.Score = max(0, er.Score+add)
	if cur.NextID != 0 && er.Score >= cur.NeedExp {
		next := eliteCfg.level(cur.NextID)
		er.Lev, er.Score = next.ID, next.InitExp
		reward = next.Award
	}
	if er.Lev > er.Best {
		er.Best = er.Lev
	}
	er.Next++
	er.Logs = append([]eliteLog{{ID: er.Next, RID: opp.RID, SrvID: opp.SrvID, Name: opp.Name, Lev: opp.Lev, Face: opp.Face, Power: opp.Power, Rank: er.Lev, Ret: result, Time: now.Unix(), Detail: detail}}, er.Logs...)
	er.Logs = er.Logs[:min(len(er.Logs), eliteLogKeep)]
	if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
		return err
	}
	if len(reward) > 0 {
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(reward), 1, ga, gi)
		if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := sendAssets(s, e.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, e.st); err != nil {
			return err
		}
	}
	stats, _, _, _, err := damageTable(e.st, role)
	if err != nil {
		return err
	}
	if err := s.Send(24906, proto.Message{"result": result, "win_count": er.Wins, "lose_count": er.Loses, "combat_type": 1, "promoted_info": e.promotedInfo(er),
		"elite_lev": old, "new_elite_lev": er.Lev, "big_score": oldScore, "score": oldScore, "end_score": er.Score, "add_score": add, "awards": eliteItems(reward),
		"all_hurt_statistics": []proto.Message{{"type": 1, "a_round": 1, "b_round": 0, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "replay_id": replayID}},
		"target_role_name":    opp.Name}); err != nil {
		return err
	}
	if err := s.Send(24900, e.infoMessage(role, er, now)); err != nil {
		return err
	}
	return s.Send(24901, e.pendingMessage(er, now))
}

func eliteItems(reward [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, g := range guildGains(reward) {
		out = append(out, proto.Message{"item_id": g.ID, "item_num": g.Amount})
	}
	return out
}

func (e *eliteSvc) buy(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, err := e.st.RoleByID(role.RID)
	if err != nil {
		return err
	}
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	if er.Bought >= int64(len(eliteCfg.Buy)) {
		return e.refuse(s, 24904, "当前已经购买达到上限")
	}
	price := eliteCfg.Buy[er.Bought]
	if int64(cur.VIPLev) < price.Vip {
		return e.refuse(s, 24904, "提升VIP等级可增加购买次数")
	}
	err = e.st.Exchange(role.RID, map[int64]int64{3: price.Cost[0][1]}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return e.refuse(s, 24904, "钻石不足")
	}
	if err != nil {
		return err
	}
	er.Bought++
	if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
		return err
	}
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	return s.Send(24904, proto.Message{"code": 1, "msg": "购买成功", "day_combat_count": e.left(er), "day_buy_count": er.Bought})
}

func (e *eliteSvc) zones(s *gateway.Session, _ proto.Message) error {
	return s.Send(24910, proto.Message{"zone_info": []proto.Message{{"period": elitePeriod(s.Now()), "max_zone_id": 1}}})
}

func (e *eliteSvc) history(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	return s.Send(24911, proto.Message{"rank": 0, "elite_lev": er.Lev, "score": er.Score, "period": req.Int("period"), "arena_elite_rank": []proto.Message{},
		"zone_id": req.Int("zone_id"), "max_zone_id": 1})
}

func (e *eliteSvc) reward(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	lev := req.Int("lev")
	l := eliteCfg.level(lev)
	switch {
	case l.ID != lev || len(l.LevAward) == 0:
		return e.refuse(s, 24915, "没有这个段位奖励")
	case er.Lev < lev:
		return e.refuse(s, 24915, "段位未达到")
	}
	for _, c := range er.Claimed {
		if c == lev {
			return e.refuse(s, 24915, "已经领取过了")
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l.LevAward), 1, ga, gi)
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	er.Claimed = append(er.Claimed, lev)
	if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
		return err
	}
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, e.st); err != nil {
		return err
	}
	return s.Send(24915, proto.Message{"code": 1, "msg": "", "lev_reward": e.rewardList(er)})
}

// formation (24920) gives the line-ups saved for a kind of match, the campaign line-up when there is none.
func (e *eliteSvc) formation(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	typ := req.Int("type")
	forms := er.Forms[typ]
	if len(forms) == 0 {
		base, err := formationOf(e.st, role.RID, formDrama)
		if err != nil {
			return err
		}
		f := eliteForm{Order: 1, FormationType: base.FormationType, HallowsID: base.HallowsID}
		for _, sl := range base.Slots {
			f.Slots = append(f.Slots, eliteSlot{sl.Pos, sl.PartnerID})
		}
		forms = []eliteForm{f}
	}
	out := []proto.Message{}
	for _, f := range forms {
		pos := []proto.Message{}
		for _, sl := range f.Slots {
			pos = append(pos, proto.Message{"pos": sl.Pos, "id": sl.ID})
		}
		out = append(out, proto.Message{"order": f.Order, "formation_type": f.FormationType, "pos_info": pos, "hallows_id": f.HallowsID})
	}
	return s.Send(24920, proto.Message{"type": typ, "formations": out})
}

func (e *eliteSvc) saveFormation(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	var forms []eliteForm
	for _, f := range req.Arr("formations") {
		ef := eliteForm{Order: f.Int("order"), FormationType: f.Int("formation_type"), HallowsID: f.Int("hallows_id")}
		for _, p := range f.Arr("pos_info") {
			ef.Slots = append(ef.Slots, eliteSlot{p.Int("pos"), p.Int("id")})
		}
		forms = append(forms, ef)
	}
	if len(forms) == 0 {
		return e.refuse(s, 24921, "阵容不能为空")
	}
	if er.Forms == nil {
		er.Forms = map[int64][]eliteForm{}
	}
	er.Forms[req.Int("type")] = forms
	if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
		return err
	}
	return s.Send(24921, proto.Message{"code": 1, "msg": ""})
}

func (e *eliteSvc) logs(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	list := []proto.Message{}
	for _, l := range er.Logs {
		list = append(list, proto.Message{"id": l.ID, "rid": role.RID, "srv_id": role.SrvID, "atk_name": role.Name, "atk_face": face, "atk_lev": role.Lev,
			"atk_rank": 0, "atk_elite_lev": er.Lev, "def_rid": l.RID, "def_srv_id": l.SrvID, "def_name": l.Name, "def_face": l.Face, "def_lev": l.Lev,
			"def_rank": 0, "def_elite_lev": l.Rank, "ret": l.Ret, "win_count": 0, "lose_count": 0, "combat_type": 1, "time": l.Time,
			"atk_face_update_time": 0, "atk_face_file": "", "def_face_update_time": 0, "def_face_file": ""})
	}
	return s.Send(24930, proto.Message{"type": req.Int("type"), "arena_elite_log": list})
}

func (e *eliteSvc) personal(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	return e.sendPersonal(s, 24940, role, req.Int("period"))
}

// sendPersonal sends the personal page of a role under the given command (24940 own, 24942 a shared one).
func (e *eliteSvc) sendPersonal(s *gateway.Session, cmd uint16, role *store.Role, period int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := s.Now()
	er, err := e.role(role.RID, now)
	if err != nil {
		return err
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	_, _, power, _ := lineupOf(e.st, role.RID)
	if period == 0 {
		period = elitePeriod(now)
	}
	return s.Send(cmd, proto.Message{"best_mvp": 0, "my_score": er.Score, "my_elite_lev": er.Lev, "period": period, "max_lev": er.Best, "max_score": er.Score,
		"combat_win_count1": er.Wins, "combat_all_count1": er.Wins + er.Loses, "combat_win_count2": 0, "combat_all_count2": 0, "max_dps": 0,
		"winning_streak": er.Streak, "rid": role.RID, "srv_id": role.SrvID, "name": role.Name, "lev": role.Lev, "sex": role.Sex, "g_rid": 0, "g_srv_id": "",
		"gname": "", "power": []proto.Message{{"order": 1, "power": power}}, "face": face, "score": er.Score, "elite_lev": er.Lev,
		"avatar_id": eliteCfg.level(er.Lev).ID * 0, "face_update_time": 0, "face_file": "", "log_zone_id": 1, "log_rank": 0, "use_skin": 0})
}

// shared (24942): the personal page of a role somebody shared (id is its rid).
func (e *eliteSvc) shared(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	target, err := e.st.RoleByID(req.Int("id"))
	if err != nil || (req.Str("share_srv_id") != "" && target.SrvID != req.Str("share_srv_id")) {
		return s.Send(24942, proto.Message{"rid": req.Int("id"), "srv_id": req.Str("share_srv_id"), "period": req.Int("period")})
	}
	return e.sendPersonal(s, 24942, target, req.Int("period"))
}

// share (24941): the page goes to a chat channel; the channels get no announcement (our simplification, as the replays).
func (e *eliteSvc) share(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	return s.Send(24941, proto.Message{"msg": "分享成功"})
}

// report (24931): the fight of one log entry, with its replay.
func (e *eliteSvc) report(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	infos := []proto.Message{}
	for _, l := range er.Logs {
		if l.ID != req.Int("id") || l.Detail == nil {
			continue
		}
		d := l.Detail
		plist := func(hs []arenaHero) []proto.Message {
			out := []proto.Message{}
			for _, h := range hs {
				out = append(out, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "skin_id": 0, "ext": []proto.Message{}})
			}
			return out
		}
		infos = append(infos, proto.Message{"order": 1, "id": d.Replay, "round": 1, "ret": l.Ret, "time": l.Time, "rid": role.RID, "srv_id": role.SrvID,
			"a_power": d.APower, "a_formation_type": d.AForm, "a_order": 1, "a_end_hp": 0, "b_rid": l.RID, "b_srv_id": l.SrvID, "b_power": l.Power,
			"b_formation_type": d.BForm, "b_order": 1, "b_end_hp": 0, "a_plist": plist(d.AList), "b_plist": plist(d.BList), "hurt_statistics": []proto.Message{}})
	}
	return s.Send(24931, proto.Message{"type": req.Int("type"), "id": req.Int("id"), "arena_replay_infos": infos})
}

func (e *eliteSvc) manifesto(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	list := []proto.Message{}
	var defaults [][]float64
	_ = json.Unmarshal(eliteCfg.Const["default_declaration"], &defaults)
	for i, d := range defaults {
		order := int64(d[0])
		id := int64(d[1])
		if v, ok := er.Manifesto[order]; ok {
			id = v
		}
		_ = i
		list = append(list, proto.Message{"order": order, "manifesto_id": id})
	}
	return s.Send(24945, proto.Message{"manifesto": list})
}

func (e *eliteSvc) saveManifesto(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	er, err := e.role(role.RID, s.Now())
	if err != nil {
		return err
	}
	if er.Manifesto == nil {
		er.Manifesto = map[int64]int64{}
	}
	for _, m := range req.Arr("manifesto") {
		er.Manifesto[m.Int("order")] = m.Int("manifesto_id")
	}
	if err := e.st.SaveState(role.RID, eliteModule, er); err != nil {
		return err
	}
	return s.Send(24946, proto.Message{"code": 1, "msg": "设置成功"})
}
