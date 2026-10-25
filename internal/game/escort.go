package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// escort.json: the rewards of the escort by quality and level (tools/extract_escort.py).
//
//go:embed data/escort.json
var escortJSON []byte

// The escort (the hired beasts), from escort_data.lua and the Lua of the client: a role hires a beast of a quality
// to carry goods for a quarter of an hour, other roles may plunder it meanwhile, and the role takes revenge or asks
// its guild to repel the plunderer. The numbers named in the config are quoted; the chances and the fight are ours.
const (
	escModule   = "escort"
	escBoard    = "escort_board"
	combatEsc   = 13 // BattleConst.Fight_Type.Escort
	escOpenLev  = 32 // open_lev (the client also asks for the second day of the server)
	escMaxQ     = 4  // orange: quality_protect, a plundered orange beast loses nothing
	escTimeSecs = 900
	escPutLimit = 20 // escortput_limit: beasts listed at once
	escLogLimit = 10 // plunder_log_limit / beplunder_log_limit
	escRaisePct = 40 // ours: the chance that a refresh raises the quality by one

	// the counters of 24006, the client's EscortConst.times_type
	cntEscort, cntPlunder, cntRevenge, cntHelp, cntDoHelp, cntRefresh = 1, 2, 3, 4, 5, 6

	// the day's allowances
	escLimEscort, escLimPlunder, escLimRevenge, escLimHelp, escLimDoHelp, escLimFree = 3, 5, 5, 2, 3, 3

	escMonLimit   = 4    // mon_plunder_limit: times a beast can be plundered
	escSingleLim  = 2    // single_revenge_limit
	escRevengePm  = 1500 // revenge_reward, per mille of the lost reward
	escRequesterP = 500  // help_reward: the share of the one who asked
	escHelperP    = 500  // request_reward: the share of the one who helped
	escHelpCD     = 3600 // request_colddown
)

type escRewards struct {
	Award, Plunder [][2]int64
}

var escRewardTable = func() map[string]escRewards {
	var f struct {
		Rewards map[string]escRewards `json:"rewards"`
	}
	if err := json.Unmarshal(escortJSON, &f); err != nil || len(f.Rewards) != 500 {
		panic("game: bad escort.json")
	}
	return f.Rewards
}()

// the finish costs, by the time still to wait (data_finish)
var escFinishCost = []struct{ From, Gems int64 }{{1200, 40}, {900, 40}, {600, 30}, {300, 20}, {0, 10}}

// the doubled hours (double_time)
var escDouble = [][2][2]int{{{11, 0}, {13, 0}}, {{21, 30}, {22, 30}}}

// the first quality of a day: init_base, (quality, weight)
var escInitBase = [][2]int64{{0, 700}, {1, 200}, {2, 100}}

// escRewardOf is the reward of a beast of a quality for a role of a level.
func escRewardOf(quality, lev int64) escRewards {
	lev = min(max(lev, 1), 100)
	return escRewardTable[strconv.FormatInt(quality, 10)+"_"+strconv.FormatInt(lev, 10)]
}

func escDoubleAt(t time.Time) bool {
	t = t.In(dayZone)
	m := t.Hour()*60 + t.Minute()
	for _, w := range escDouble {
		if m >= w[0][0]*60+w[0][1] && m < w[1][0]*60+w[1][1] {
			return true
		}
	}
	return false
}

// escLog is a line of a role's log. Type 1 is a plunder it suffered (and what it can do about it), 2 one it did.
type escLog struct {
	ID, Type, Quality     int64
	RID                   int64 // the other side
	SrvID, Name, Guild    string
	Lev, Sex, Face, Power int64
	Ret, AtkRet           int64 // Ret 1: the plunder worked; AtkRet 1: revenge worked, 2: it did not
	AtkCount              int64
	Time                  int64
	HelpCount, HelpTime   int64
	Items                 [][2]int64 // what was lost / won
	Base                  [][2]int64 // the plunder reward of the beast, the base of the revenge
}

type escState struct {
	Day     int64
	Cnt     [7]int64
	Picked  bool // a quality was drawn for the next hire
	Status  int64
	Quality int64
	End     int64
	Lev     int64
	Lost    int64 // times the beast was plundered
	By      []int64
	Double  bool
	Logs    []escLog
}

// escGlobal is what the server shares: the beasts out, the next log id and who owns a log.
type escGlobal struct {
	Active map[string]bool
	Next   int64
	Owner  map[string]int64
}

type escSvc struct {
	mu     sync.Mutex
	st     *store.Store
	fights *FightService
}

// RegisterEscort registers the escort: 24000 the beasts out, 24001 refresh, 24002 hire, 24003 finish at once,
// 24004 collect, 24006 my state, 24010/24011 look at and plunder a beast (the result is 24012), 24013 the logs, 24014 /
// 24015 look at and fight a plunderer (revenge or, for a guild mate, a repel), 24017 ask the guild, 24020 whether the
// hours are doubled.
func RegisterEscort(r *gateway.Router, st *store.Store) {
	e := &escSvc{st: st, fights: fightServiceOf(st)}
	r.Handle(24000, e.list)
	r.Handle(24001, e.refresh)
	r.Handle(24002, e.hire)
	r.Handle(24003, e.finish)
	r.Handle(24004, e.collect)
	r.Handle(24006, func(s *gateway.Session, _ proto.Message) error {
		return e.with(s, func(role *store.Role, me *escState, _ *escGlobal) error { return e.sendMine(s, me) })
	})
	r.Handle(24010, e.look)
	r.Handle(24011, e.plunder)
	r.Handle(24013, e.logs)
	r.Handle(24014, e.lookPlunderer)
	r.Handle(24015, e.fightPlunderer)
	r.Handle(24017, e.askHelp)
	r.Handle(24020, func(s *gateway.Session, _ proto.Message) error { return s.Send(24020, doubleMsg(s.Now())) })
}

func doubleMsg(now time.Time) proto.Message {
	if escDoubleAt(now) {
		return proto.Message{"code": 1}
	}
	return proto.Message{"code": 0}
}

func escKey(n int64) string { return strconv.FormatInt(n, 10) }

func (e *escSvc) loadGlobal(srv string) (*escGlobal, error) {
	g := &escGlobal{}
	if _, err := e.st.LoadGlobal(srv, escBoard, g); err != nil {
		return nil, err
	}
	if g.Active == nil {
		g.Active = map[string]bool{}
	}
	if g.Owner == nil {
		g.Owner = map[string]int64{}
	}
	return g, nil
}

func (e *escSvc) load(rid int64, now time.Time) (*escState, error) {
	st := &escState{}
	if _, err := e.st.LoadState(rid, escModule, st); err != nil {
		return nil, err
	}
	if day := dayKey(now); st.Day != day {
		st.Day = day
		st.Cnt = [7]int64{}
	}
	if st.Status == 0 && !st.Picked {
		st.Quality, st.Picked = escDraw(), true
	}
	return st, nil
}

func escDraw() int64 {
	var total int64
	for _, w := range escInitBase {
		total += w[1]
	}
	n := newRng().Int63n(total)
	for _, w := range escInitBase {
		if n -= w[1]; n < 0 {
			return w[0]
		}
	}
	return 0
}

// with runs fn for the role of the session, holding the lock, and saves its state and the shared one after.
func (e *escSvc) with(s *gateway.Session, fn func(role *store.Role, me *escState, g *escGlobal) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	me, err := e.load(role.RID, s.Now())
	if err != nil {
		return err
	}
	g, err := e.loadGlobal(role.SrvID)
	if err != nil {
		return err
	}
	if err := fn(role, me, g); err != nil {
		return err
	}
	if err := e.st.SaveState(role.RID, escModule, me); err != nil {
		return err
	}
	return e.st.SaveGlobal(role.SrvID, escBoard, g)
}

func escClosed(role *store.Role) string {
	if role.Lev < escOpenLev {
		return "开服第2天且角色等级达32级开启"
	}
	return ""
}

func (e *escSvc) sendMine(s *gateway.Session, me *escState) error {
	datas := []proto.Message{}
	for i := 1; i <= 6; i++ {
		datas = append(datas, proto.Message{"id": i, "val": me.Cnt[i]})
	}
	if err := s.Send(24020, doubleMsg(s.Now())); err != nil {
		return err
	}
	return s.Send(24006, proto.Message{"status": me.Status, "quality": me.Quality, "end_time": me.End, "plunder_count": me.Lost, "datas": datas})
}

func (e *escSvc) pay(s *gateway.Session, role *store.Role, cost [][2]int64) error {
	sa, si := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(cost), 1, sa, si)
	return e.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
}

func (e *escSvc) gain(s *gateway.Session, role *store.Role, items [][2]int64) error {
	if len(items) == 0 {
		return nil
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(items), 1, ga, gi)
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, e.st)
}

func escScale(items [][2]int64, num, den int64) [][2]int64 {
	out := make([][2]int64, 0, len(items))
	for _, it := range items {
		if v := it[1] * num / den; v > 0 {
			out = append(out, [2]int64{it[0], v})
		}
	}
	return out
}

func escItems(items [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, it := range items {
		out = append(out, proto.Message{"bid": it[0], "num": it[1]})
	}
	return out
}

func (e *escSvc) reply(s *gateway.Session, cmd uint16, ok bool, msg string) error {
	res := int64(0)
	if ok {
		res = 1
	}
	return s.Send(cmd, proto.Message{"result": res, "msg": msg})
}

func (e *escSvc) refresh(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		if msg := escClosed(role); msg != "" {
			return e.reply(s, 24001, false, msg)
		}
		if me.Status != 0 {
			return e.reply(s, 24001, false, "护送中不能刷新")
		}
		if me.Quality >= escMaxQ {
			return e.reply(s, 24001, false, "已经是最高品质")
		}
		if req.Int("type") == 2 { // all the way to orange, for diamonds
			if err := e.pay(s, role, [][2]int64{{15, 50}}); errors.Is(err, store.ErrConflict) {
				return e.reply(s, 24001, false, "钻石不足")
			} else if err != nil {
				return err
			}
			me.Quality = escMaxQ
		} else {
			if me.Cnt[cntRefresh] >= escLimFree {
				if err := e.pay(s, role, [][2]int64{{1, 1500}}); errors.Is(err, store.ErrConflict) {
					return e.reply(s, 24001, false, "金币不足")
				} else if err != nil {
					return err
				}
			}
			me.Cnt[cntRefresh]++
			if newRng().Intn(100) < escRaisePct {
				me.Quality++
			}
		}
		if err := sendAssets(s, e.st, role.RID); err != nil {
			return err
		}
		if err := e.reply(s, 24001, true, "刷新成功"); err != nil {
			return err
		}
		return e.sendMine(s, me)
	})
}

func (e *escSvc) hire(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		switch {
		case escClosed(role) != "":
			return e.reply(s, 24002, false, escClosed(role))
		case me.Status != 0:
			return e.reply(s, 24002, false, "已经在护送中")
		case me.Cnt[cntEscort] >= escLimEscort:
			return e.reply(s, 24002, false, "今日护送次数已用完")
		}
		me.Cnt[cntEscort]++
		me.Status, me.End, me.Lev, me.Lost, me.By = 1, s.Now().Unix()+escTimeSecs, int64(role.Lev), 0, nil
		me.Double = escDoubleAt(s.Now())
		g.Active[escKey(role.RID)] = true
		if err := e.reply(s, 24002, true, "护送开始"); err != nil {
			return err
		}
		return e.sendMine(s, me)
	})
}

func (e *escSvc) finish(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		now := s.Now().Unix()
		if me.Status != 1 || me.End <= now {
			return e.reply(s, 24003, false, "没有可以快速完成的护送")
		}
		left, gems := me.End-now, int64(10)
		for _, c := range escFinishCost {
			if left >= c.From {
				gems = c.Gems
				break
			}
		}
		if err := e.pay(s, role, [][2]int64{{15, gems}}); errors.Is(err, store.ErrConflict) {
			return e.reply(s, 24003, false, "钻石不足")
		} else if err != nil {
			return err
		}
		me.End = now
		if err := sendAssets(s, e.st, role.RID); err != nil {
			return err
		}
		if err := e.reply(s, 24003, true, "护送完成"); err != nil {
			return err
		}
		return e.sendMine(s, me)
	})
}

// collect pays a finished escort: the award of the quality, less what the plunderers took (an orange beast loses nothing),
// doubled when it was hired in the doubled hours.
func (e *escSvc) collect(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		if me.Status != 1 || me.End > s.Now().Unix() {
			return e.reply(s, 24004, false, "护送还没有完成")
		}
		rw := escRewardOf(me.Quality, me.Lev)
		lost := map[int64]int64{}
		if me.Quality < escMaxQ {
			for _, it := range rw.Plunder {
				lost[it[0]] = it[1] * me.Lost
			}
		}
		times := int64(1)
		if me.Double {
			times = 2
		}
		var pay [][2]int64
		for _, it := range rw.Award {
			if v := (it[1] - lost[it[0]]) * times; v > 0 {
				pay = append(pay, [2]int64{it[0], v})
			}
		}
		me.Status, me.Picked, me.Lost, me.By, me.Double = 0, false, 0, nil, false
		delete(g.Active, escKey(role.RID))
		if err := e.gain(s, role, pay); err != nil {
			return err
		}
		if err := e.reply(s, 24004, true, "领取成功"); err != nil {
			return err
		}
		return e.sendMine(s, me)
	})
}

func (e *escSvc) faceOf(r *store.Role) int64 {
	if r.FaceID == 0 {
		return store.DefaultFaceID
	}
	return int64(r.FaceID)
}

func (e *escSvc) guildName(rid int64) (gid int64, srv, name string) {
	if g, _, err := e.st.GuildOfRole(rid); err == nil && g != nil {
		return g.GID, g.SrvID, g.Name
	}
	return 0, "", ""
}

// targets are the beasts of other roles that are out and can still be plundered.
func (e *escSvc) targets(me *store.Role, g *escGlobal, now time.Time) []proto.Message {
	rids := []int64{}
	for k := range g.Active {
		if rid, err := strconv.ParseInt(k, 10, 64); err == nil && rid != me.RID {
			rids = append(rids, rid)
		}
	}
	sort.Slice(rids, func(i, j int) bool { return rids[i] < rids[j] })
	out := []proto.Message{}
	for _, rid := range rids {
		if len(out) >= escPutLimit {
			break
		}
		st := &escState{}
		if _, err := e.st.LoadState(rid, escModule, st); err != nil || st.Status != 1 || st.End <= now.Unix() {
			continue
		}
		r, err := e.st.RoleByID(rid)
		if err != nil {
			continue
		}
		gid, gsrv, gname := e.guildName(rid)
		by := []proto.Message{}
		for _, b := range st.By {
			by = append(by, proto.Message{"rid": b, "srv_id": me.SrvID})
		}
		out = append(out, proto.Message{"rid": rid, "srv_id": r.SrvID, "gid": gid, "gsrv_id": gsrv, "name": r.Name, "gname": gname,
			"quality": st.Quality, "end_time": st.End, "plunder_lists": by})
	}
	return out
}

func (e *escSvc) list(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		if err := e.sendMine(s, me); err != nil {
			return err
		}
		return s.Send(24000, proto.Message{"plunders": e.targets(role, g, s.Now())})
	})
}

// lineupMsg describes the line-up of a role for the windows of the escort.
func (e *escSvc) lineupMsg(r *store.Role) (proto.Message, error) {
	lineup, formType, power, err := lineupOf(e.st, r.RID)
	if err != nil {
		return nil, err
	}
	heroes := []proto.Message{}
	for _, h := range lineup {
		heroes = append(heroes, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "quality": h.Star, "star": h.Star, "break_lev": h.Break})
	}
	_, _, gname := e.guildName(r.RID)
	return proto.Message{"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "face": e.faceOf(r), "avatar_id": 0, "power": power,
		"guild_name": gname, "formation_type": formType, "formation_lev": 1, "p_list": heroes}, nil
}

func (e *escSvc) look(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		foe, err := e.st.RoleByID(req.Int("rid"))
		if err != nil || foe.RID == role.RID {
			return nil
		}
		st := &escState{}
		if _, err := e.st.LoadState(foe.RID, escModule, st); err != nil {
			return err
		}
		msg, err := e.lineupMsg(foe)
		if err != nil {
			return err
		}
		gid, gsrv, _ := e.guildName(foe.RID)
		msg["quality"], msg["gid"], msg["gsrv_id"], msg["lev"] = st.Quality, gid, gsrv, st.Lev
		return s.Send(24010, msg)
	})
}

// duel plays the role against another role's line-up and starts the fight; settle gets the outcome.
func (e *escSvc) duel(s *gateway.Session, role, foe *store.Role, req proto.Message, settle func(s *gateway.Session, won bool) error) (refused string, err error) {
	opp, ok, err := oppFromRole(e.st, foe, 0)
	if err != nil {
		return "", err
	}
	team, objects, form, err := e.fights.teamSide(role)
	if err != nil {
		return "", err
	}
	if len(team) == 0 {
		return "没有可以出战的英雄", nil
	}
	if !ok {
		return "对手已无法应战", nil
	}
	oslots, oform, obyID, err := opp.fighters(e.st)
	if err != nil {
		return "", err
	}
	foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return "对手已无法应战", nil
	}
	rng := newRng()
	if e.fights.Rand != nil {
		rng = e.fights.Rand()
	}
	intro := proto.Message{
		"combat_type": combatEsc, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
		"objects": append(objects, foeObjects...), "is_auto": req.Int("is_auto"), "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": foe.Name,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	rounds, won := buildFight(team, foes, rng)
	return "", e.fights.begin(s, role, combatEsc, intro, rounds, won, settle)
}

func (e *escSvc) plunder(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		refuse := func(msg string) error { return e.reply(s, 24011, false, msg) }
		if msg := escClosed(role); msg != "" {
			return refuse(msg)
		}
		foe, err := e.st.RoleByID(req.Int("rid"))
		if err != nil || foe.RID == role.RID || foe.SrvID != role.SrvID {
			return refuse("目标不存在")
		}
		st := &escState{}
		if _, err := e.st.LoadState(foe.RID, escModule, st); err != nil {
			return err
		}
		switch {
		case st.Status != 1 || st.End <= s.Now().Unix():
			return refuse("该雇佣兽已经完成护送")
		case me.Cnt[cntPlunder] >= escLimPlunder:
			return refuse("今日掠夺次数已用完")
		case st.Lost >= escMonLimit:
			return refuse("该雇佣兽被掠夺次数已达上限")
		}
		for _, b := range st.By {
			if b == role.RID {
				return refuse("已经掠夺过该雇佣兽")
			}
		}
		foeRID := foe.RID
		refused, err := e.duel(s, role, foe, req, func(s *gateway.Session, won bool) error {
			return e.settlePlunder(s, foeRID, won)
		})
		if err != nil {
			return err
		}
		if refused != "" {
			return refuse(refused)
		}
		me.Cnt[cntPlunder]++
		return e.reply(s, 24011, true, "")
	})
}

func (e *escSvc) addLog(g *escGlobal, st *escState, rid int64, l escLog) escLog {
	g.Next++
	l.ID = g.Next
	g.Owner[escKey(l.ID)] = rid
	st.Logs = append([]escLog{l}, st.Logs...)
	for len(st.Logs) > escLogLimit {
		delete(g.Owner, escKey(st.Logs[len(st.Logs)-1].ID))
		st.Logs = st.Logs[:len(st.Logs)-1]
	}
	return l
}

func (e *escSvc) logAbout(typ int64, r *store.Role, gname string, power int64) escLog {
	return escLog{Type: typ, RID: r.RID, SrvID: r.SrvID, Name: r.Name, Guild: gname, Lev: int64(r.Lev), Sex: int64(r.Sex), Face: e.faceOf(r), Power: power}
}

// settlePlunder pays the plunderer when it won and the beast is still out, and logs the fight on both sides.
func (e *escSvc) settlePlunder(s *gateway.Session, foeRID int64, won bool) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	foe, err := e.st.RoleByID(foeRID)
	if err != nil {
		return err
	}
	me, err := e.load(role.RID, s.Now())
	if err != nil {
		return err
	}
	g, err := e.loadGlobal(role.SrvID)
	if err != nil {
		return err
	}
	st, err := e.load(foeRID, s.Now())
	if err != nil {
		return err
	}
	now := s.Now().Unix()
	var got, lost [][2]int64
	result := int64(2)
	if won {
		result = 1
		if st.Status == 1 && st.End > now && st.Lost < escMonLimit {
			rw := escRewardOf(st.Quality, st.Lev)
			times := int64(1)
			if escDoubleAt(s.Now()) {
				times = 2
			}
			got = escScale(rw.Plunder, times, 1)
			st.Lost++
			st.By = append(st.By, role.RID)
			if st.Quality < escMaxQ {
				lost = rw.Plunder
			}
			_, _, mpower := e.powerOf(role)
			_, _, gname := e.guildName(role.RID)
			dl := e.logAbout(1, role, gname, mpower)
			dl.Quality, dl.Ret, dl.Time, dl.Items, dl.Base = st.Quality, 1, now, lost, rw.Plunder
			e.addLog(g, st, foeRID, dl)
		}
	}
	_, _, fpower := e.powerOf(foe)
	_, _, fname := e.guildName(foeRID)
	al := e.logAbout(2, foe, fname, fpower)
	al.Quality, al.Ret, al.Time, al.Items = st.Quality, result, now, got
	e.addLog(g, me, role.RID, al)
	if err := e.st.SaveState(foeRID, escModule, st); err != nil {
		return err
	}
	if err := e.st.SaveState(role.RID, escModule, me); err != nil {
		return err
	}
	if err := e.st.SaveGlobal(role.SrvID, escBoard, g); err != nil {
		return err
	}
	if err := e.gain(s, role, got); err != nil {
		return err
	}
	if err := s.Send(24012, proto.Message{"result": result, "item_rewards": escItems(got)}); err != nil {
		return err
	}
	return e.sendMine(s, me)
}

func (e *escSvc) powerOf(r *store.Role) (lineup []arenaHero, formType, power int64) {
	lineup, formType, power, _ = lineupOf(e.st, r.RID)
	return
}

func (e *escSvc) logMsg(l escLog) proto.Message {
	return proto.Message{"id": l.ID, "quality": l.Quality, "rid": l.RID, "srv_id": l.SrvID, "name": l.Name, "guild_name": l.Guild, "lev": l.Lev, "sex": l.Sex,
		"face": l.Face, "power": l.Power, "avatar_id": 0, "ret": l.Ret, "atk_ret": l.AtkRet, "atk_count": l.AtkCount, "time": l.Time,
		"help_count": l.HelpCount, "help_time": l.HelpTime, "items": escItems(l.Items), "replay_id": 0}
}

func (e *escSvc) logs(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		typ := req.Int("type")
		out := []proto.Message{}
		for _, l := range me.Logs {
			if l.Type == typ {
				out = append(out, e.logMsg(l))
			}
		}
		return s.Send(24013, proto.Message{"type": typ, "logs": out})
	})
}

// logOf finds a log by its id, on whoever owns it.
func (e *escSvc) logOf(g *escGlobal, id int64) (owner *escState, ownerRID int64, idx int, ok bool, err error) {
	ownerRID, found := g.Owner[escKey(id)]
	if !found {
		return nil, 0, 0, false, nil
	}
	owner = &escState{}
	if _, err = e.st.LoadState(ownerRID, escModule, owner); err != nil {
		return nil, 0, 0, false, err
	}
	for i, l := range owner.Logs {
		if l.ID == id && l.Type == 1 {
			return owner, ownerRID, i, true, nil
		}
	}
	return nil, 0, 0, false, nil
}

func (e *escSvc) lookPlunderer(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		owner, _, idx, ok, err := e.logOf(g, req.Int("id"))
		if err != nil || !ok {
			return err
		}
		l := owner.Logs[idx]
		foe, err := e.st.RoleByID(l.RID)
		if err != nil {
			return nil
		}
		msg, err := e.lineupMsg(foe)
		if err != nil {
			return err
		}
		msg["quality"], msg["id"], msg["type"] = l.Quality, l.ID, req.Int("type")
		return s.Send(24014, msg)
	})
}

// fightPlunderer fights the one who plundered a log: the owner takes revenge, a mate of its guild repels at its call.
func (e *escSvc) fightPlunderer(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		refuse := func(msg string) error { return e.reply(s, 24015, false, msg) }
		owner, ownerRID, idx, ok, err := e.logOf(g, req.Int("id"))
		if err != nil {
			return err
		}
		if !ok {
			return refuse("掠夺记录不存在")
		}
		l := owner.Logs[idx]
		revenge := ownerRID == role.RID
		switch {
		case l.AtkRet != 0:
			return refuse("已经处理过这次掠夺")
		case revenge && (l.HelpCount != 0 || l.AtkCount >= escSingleLim):
			return refuse("无法复仇")
		case revenge && me.Cnt[cntRevenge] >= escLimRevenge:
			return refuse("今日复仇次数已用完")
		case !revenge && l.HelpCount == 0:
			return refuse("对方没有求助")
		case !revenge && me.Cnt[cntDoHelp] >= escLimDoHelp:
			return refuse("今日帮助次数已用完")
		}
		if !revenge {
			a, _, _ := e.st.GuildOfRole(role.RID)
			b, _, _ := e.st.GuildOfRole(ownerRID)
			if a == nil || b == nil || a.GID != b.GID {
				return refuse("只能帮助同公会的会友")
			}
		}
		foe, err := e.st.RoleByID(l.RID)
		if err != nil {
			return refuse("对手不存在")
		}
		logID := l.ID
		refused, err := e.duel(s, role, foe, req, func(s *gateway.Session, won bool) error {
			return e.settleRevenge(s, ownerRID, logID, revenge, won)
		})
		if err != nil {
			return err
		}
		if refused != "" {
			return refuse(refused)
		}
		if revenge {
			me.Cnt[cntRevenge]++
		} else {
			me.Cnt[cntDoHelp]++
		}
		return e.reply(s, 24015, true, "")
	})
}

func (e *escSvc) settleRevenge(s *gateway.Session, ownerRID, logID int64, revenge, won bool) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	g, err := e.loadGlobal(role.SrvID)
	if err != nil {
		return err
	}
	owner, _, idx, ok, err := e.logOf(g, logID)
	if err != nil {
		return err
	}
	result := int64(2)
	var mine [][2]int64
	if ok {
		l := &owner.Logs[idx]
		if won && l.AtkRet == 0 {
			result = 1
			l.AtkRet = 1
			times := int64(1)
			if escDoubleAt(s.Now()) {
				times = 2
			}
			if revenge {
				mine = escScale(l.Base, escRevengePm*times, 1000)
			} else {
				mine = escScale(l.Base, escHelperP*times, 1000)
			}
		} else if !won && revenge {
			if l.AtkCount++; l.AtkCount >= escSingleLim {
				l.AtkRet = 2
			}
		}
		if err := e.st.SaveState(ownerRID, escModule, owner); err != nil {
			return err
		}
		if result == 1 && !revenge { // the one who asked gets its share too
			if or, err := e.st.RoleByID(ownerRID); err == nil {
				share := escScale(l.Base, escRequesterP, 1000)
				ga, gi := map[int64]int64{}, map[int64]int64{}
				splitGains(guildGains(share), 1, ga, gi)
				if err := e.st.Exchange(or.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
					return err
				}
			}
		}
	}
	if err := e.gain(s, role, mine); err != nil {
		return err
	}
	if err := s.Send(24012, proto.Message{"result": result, "item_rewards": escItems(mine)}); err != nil {
		return err
	}
	if ok && ownerRID == role.RID {
		return s.Send(24018, e.logMsg(owner.Logs[idx]))
	}
	return nil
}

func (e *escSvc) askHelp(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, me *escState, g *escGlobal) error {
		refuse := func(msg string) error { return s.Send(24017, proto.Message{"code": 0, "msg": msg}) }
		for i := range me.Logs {
			l := &me.Logs[i]
			if l.ID != req.Int("id") || l.Type != 1 {
				continue
			}
			switch {
			case l.AtkRet != 0 || l.HelpCount != 0:
				return refuse("这次掠夺已经处理或求助过")
			case me.Cnt[cntHelp] >= escLimHelp:
				return refuse("今日求助次数已用完")
			}
			if gd, _, err := e.st.GuildOfRole(role.RID); err != nil || gd == nil {
				return refuse("加入公会后才能求助")
			}
			me.Cnt[cntHelp]++
			l.HelpCount, l.HelpTime = 1, s.Now().Unix()+escHelpCD
			if err := s.Send(24017, proto.Message{"code": 1, "msg": "求助成功"}); err != nil {
				return err
			}
			if err := s.Send(24018, e.logMsg(*l)); err != nil {
				return err
			}
			return e.sendMine(s, me)
		}
		return refuse("掠夺记录不存在")
	})
}
