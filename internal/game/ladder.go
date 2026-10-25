package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// ladder.json: the sky ladder of the client (tools/extract_ladder.py).
//
//go:embed data/ladder.json
var ladderJSON []byte

const (
	ladderModule     = "ladder"
	combatLadder     = 18 // BattleConst.Fight_Type.LadderWar
	ladderFoes       = 5  // role_pos_1..5 of the main window
	ladderWindow     = 9  // how far above a role its foes are drawn from
	ladderRobotBase  = 4100000000
	ladderLogKeep    = 50
	ladderGodKeep    = 20
	ladderOpenDay    = time.Saturday // 周六00:00 .. 周日23:00
	ladderCloseAfter = 24*3600 + 23*3600
)

type ladderConf struct {
	Const map[string]json.RawMessage `json:"const"`
	Award []struct {
		Min, Max int64
		Reward   [][2]int64
	} `json:"award"`
	Buy []struct{ Cost, Vip int64 } `json:"buy"`
}

var ladderCfg = func() *ladderConf {
	c := &ladderConf{}
	if err := json.Unmarshal(ladderJSON, c); err != nil || len(c.Award) == 0 || len(c.Buy) == 0 {
		panic("game: bad ladder.json")
	}
	return c
}()

func (c *ladderConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *ladderConf) pairs(key string) [][2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

func (c *ladderConf) ids(key string) []int64 {
	var l []int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

// ladderOpen says whether the ladder is open: Saturday 00:00 until Sunday 23:00. LADDER_ALWAYS_OPEN=1 keeps it open
// for tests on a device.
var ladderAlways = os.Getenv("LADDER_ALWAYS_OPEN") == "1"

// ladderWeek is the Saturday that began the ladder week of a moment (the last one that has begun), and open whether
// the ladder is running in it.
func ladderWeek(now time.Time) (start time.Time, open bool) {
	t := dayStart(now.In(dayZone))
	start = t.AddDate(0, 0, -((int(t.Weekday()) - int(ladderOpenDay) + 7) % 7))
	return start, ladderAlways || now.Unix() < start.Unix()+ladderCloseAfter
}

// ladderLog is one line of a role's fight log: Type 1 is a challenge by the role, 2 one against it; Ret 1 is a win of the
// role, 2 a loss; RankType 1 is a climb of the role, 2 a fall, 0 no change; Rank is the role's rank after it.
type ladderLog struct {
	RID, Lev, Sex, Face, Power, Type, Ret, RankType, Rank, ReplayID, Time int64
	SrvID, Name                                                           string
}

// ladderGod is one fight of the best players, for watching.
type ladderGod struct {
	AtkRID, AtkRank, AtkFace, DefRID, DefRank, DefFace, Ret, ReplayID, Time int64
	AtkSrv, AtkName, DefSrv, DefName                                        string
}

// ladderBoard is the ranking of a server: who stands on which rank (a rank nobody stands on belongs to a robot).
type ladderBoard struct {
	Ranks   map[int64]int64 // rank -> rid
	Settled int64           // key of the last week whose rewards were sent
	God     []ladderGod
}

func (b *ladderBoard) rankOf(rid int64) int64 {
	for r, id := range b.Ranks {
		if id == rid {
			return r
		}
	}
	return 0
}

// ladderRole is what a role keeps: its counters of the day, its foes and its log.
type ladderRole struct {
	Day, Used, Bought, RefTime, Best, Seen int64
	Foes                                   []int64 // ranks of the foes on the list
	Fought                                 map[int64]bool
	Logs                                   []ladderLog
}

type ladderSvc struct {
	st  *store.Store
	mu  sync.Mutex
	rng func() *rand.Rand
}

// RegisterLadder registers the sky ladder (24300-24318): the ranks run to 1000, a role of the arena's best hundred
// takes a place at the bottom and fights five foes drawn from the ranks above it; a win exchanges the places. Open from
// Saturday to Sunday evening, when the rank rewards go out by mail. The ranks nobody holds belong to robots that copy
// the challenger's team. Our rules where the client does not say, not captured.
func RegisterLadder(r *gateway.Router, db *store.Store) {
	l := &ladderSvc{st: db}
	fights := fightServiceOf(db)
	l.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.Handle(24300, l.info)
	r.Handle(24301, l.foes)
	r.Handle(24302, l.foe)
	r.Handle(24303, l.challenge)
	r.Handle(24304, l.refresh)
	r.Handle(24305, l.buy)
	r.Handle(24306, l.quick)
	r.Handle(24308, l.topThree)
	r.Handle(24309, l.ranking)
	r.Handle(24310, l.myLog)
	r.Handle(24311, l.godLog)
	r.Handle(24312, l.isOpen)
	r.Handle(24315, l.noCD)
	r.Handle(24316, l.heroCard)
	r.Handle(24318, l.share)
}

func (l *ladderSvc) maxRank() int64 { return ladderCfg.num("max_number") }

// board reads the ranking, sending out the rewards of a week that has ended.
func (l *ladderSvc) board(srv string, now time.Time) (*ladderBoard, error) {
	b := &ladderBoard{}
	if _, err := l.st.LoadGlobal(srv, ladderModule, b); err != nil {
		return nil, err
	}
	if b.Ranks == nil {
		b.Ranks = map[int64]int64{}
	}
	start, open := ladderWeek(now)
	if key := dayKey(start); !open && b.Settled < key {
		b.Settled = key
		ranks := make([]int64, 0, len(b.Ranks))
		for rank := range b.Ranks {
			ranks = append(ranks, rank)
		}
		sort.Slice(ranks, func(i, j int) bool { return ranks[i] < ranks[j] })
		for _, rank := range ranks {
			reward := ladderAward(rank)
			if len(reward) == 0 {
				continue
			}
			m := store.Mail{SrvID: srv, Subject: "天梯争霸奖励", Content: "本周天梯争霸你的最终排名是第" + itoa64(rank) + "名，这是排名奖励。",
				FromName: "天梯争霸", CreatedBy: "ladder", Attachments: attachmentsOf(reward)}
			if _, err := l.st.CreateMail(m, []int64{b.Ranks[rank]}); err != nil {
				return nil, err
			}
		}
		if err := l.st.SaveGlobal(srv, ladderModule, b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func ladderAward(rank int64) [][2]int64 {
	for _, a := range ladderCfg.Award {
		if rank >= a.Min && rank <= a.Max {
			return a.Reward
		}
	}
	return nil
}

func (l *ladderSvc) role(rid int64, now time.Time) (*ladderRole, error) {
	lr := &ladderRole{}
	if _, err := l.st.LoadState(rid, ladderModule, lr); err != nil {
		return nil, err
	}
	if day := dayKey(now); lr.Day != day {
		lr.Day, lr.Used, lr.Bought, lr.Fought = day, 0, 0, nil
	}
	return lr, nil
}

// join puts a role without a place on the lowest free rank, when it is among the arena's best.
func (l *ladderSvc) join(role *store.Role, b *ladderBoard, now time.Time) (int64, error) {
	if rank := b.rankOf(role.RID); rank != 0 {
		return rank, nil
	}
	a, err := l.st.ArenaPlayerOf(role.RID, arenaInitScore, now.Unix())
	if err != nil {
		return 0, err
	}
	if rank, err := l.st.ArenaRank(a.RID); err != nil || rank > ladderCfg.num("arena_rank") {
		return 0, err
	}
	for rank := l.maxRank(); rank >= 1; rank-- {
		if _, taken := b.Ranks[rank]; !taken {
			b.Ranks[rank] = role.RID
			return rank, l.st.SaveGlobal(role.SrvID, ladderModule, b)
		}
	}
	return 0, nil
}

// ladderFoe is who stands on a rank as the lists show it.
type ladderFoe struct {
	arenaOpp
	Rank int64
	Look int64
}

// foeAt is who holds a rank: the role standing on it, else the robot of it, a copy of the challenger's team that grows
// stronger toward the first place.
func (l *ladderSvc) foeAt(b *ladderBoard, rank int64, me *store.Role) (ladderFoe, bool, error) {
	if rid, ok := b.Ranks[rank]; ok {
		r, err := l.st.RoleByID(rid)
		if err != nil {
			return ladderFoe{}, false, err
		}
		o, ok, err := oppFromRole(l.st, r, 0)
		face := int64(r.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		return ladderFoe{arenaOpp: o, Rank: rank, Look: defaultLookID(int(face))}, ok, err
	}
	rng := rand.New(rand.NewSource(rank*7919 + 17))
	mine, formType, _, err := lineupOf(l.st, me.RID)
	if err != nil {
		return ladderFoe{}, false, err
	}
	o := arenaOpp{RID: ladderRobotBase + rank, SrvID: me.SrvID, Robot: true, FormType: formType, Lev: int64(me.Lev), Sex: int64(rng.Intn(2) + 1),
		Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))] + itoa64(rank)}
	faces := ladderCfg.ids("robot_head")
	o.Face = faces[rng.Intn(len(faces))]
	bodies := ladderCfg.ids("robot_body")
	factor := 70 + 55*(l.maxRank()-rank)/l.maxRank() // 70 per cent of the challenger's levels at the bottom, 125 at the top
	var all []store.Partner
	for _, h := range mine {
		lev := max(1, h.Lev*factor/100)
		o.Lineup = append(o.Lineup, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
		all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
	}
	o.Power = rolePower(all)
	return ladderFoe{arenaOpp: o, Rank: rank, Look: bodies[rng.Intn(len(bodies))]}, len(o.Lineup) > 0, nil
}

// foeByRID finds a foe by the id the client sends: a robot's id names its rank.
func (l *ladderSvc) foeByRID(b *ladderBoard, rid int64, me *store.Role) (ladderFoe, bool, error) {
	rank := b.rankOf(rid)
	if rid > ladderRobotBase && rid <= ladderRobotBase+l.maxRank() {
		rank = rid - ladderRobotBase
		if _, held := b.Ranks[rank]; held {
			return ladderFoe{}, false, nil
		}
	}
	if rank == 0 {
		return ladderFoe{}, false, nil
	}
	return l.foeAt(b, rank, me)
}

func (f ladderFoe) message(idx int64) proto.Message {
	return proto.Message{"rid": f.RID, "srv_id": f.SrvID, "idx": idx, "name": f.Name, "lev": f.Lev, "sex": f.Sex, "face": f.Face, "power": f.Power,
		"rank": f.Rank, "look": f.Look, "face_update_time": 0, "face_file": ""}
}

// draw picks the foes of a role on rank: ranks above it, the nearer the likelier; below it when there is no room above.
func (l *ladderSvc) draw(rank int64) []int64 {
	lo, hi := max(1, rank-ladderWindow), min(l.maxRank(), rank+ladderFoes)
	var pool []int64
	for r := lo; r <= hi; r++ {
		if r != rank {
			pool = append(pool, r)
		}
	}
	rng := l.rng()
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	pool = pool[:min(ladderFoes, len(pool))]
	sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
	return pool
}

// view is everything a handler needs: the role as saved, its place and the open state.
type ladderView struct {
	role *store.Role
	b    *ladderBoard
	lr   *ladderRole
	rank int64
	open bool
}

func (l *ladderSvc) view(s *gateway.Session) (*ladderView, error) {
	role, err := l.st.RoleByID(s.Role().RID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	b, err := l.board(role.SrvID, now)
	if err != nil {
		return nil, err
	}
	lr, err := l.role(role.RID, now)
	if err != nil {
		return nil, err
	}
	if _, err := l.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		return nil, err
	}
	_, open := ladderWeek(now)
	v := &ladderView{role: role, b: b, lr: lr, open: open}
	if open {
		if v.rank, err = l.join(role, b, now); err != nil {
			return nil, err
		}
	} else {
		v.rank = b.rankOf(role.RID)
	}
	if v.rank > 0 && (v.rank < lr.Best || lr.Best == 0) {
		lr.Best = v.rank
	}
	if v.rank > 0 && len(lr.Foes) == 0 {
		lr.Foes = l.draw(v.rank)
	}
	return v, l.st.SaveState(role.RID, ladderModule, lr)
}

func (l *ladderSvc) left(v *ladderView) int64 {
	return max(0, ladderCfg.num("join_times")+v.lr.Bought-v.lr.Used)
}

func (l *ladderSvc) infoMessage(v *ladderView) proto.Message {
	return proto.Message{"rank": v.rank, "best_rank": v.lr.Best, "can_combat_num": l.left(v), "buy_combat_num": v.lr.Bought,
		"ref_time": v.lr.RefTime, "combat_time": 0}
}

func (l *ladderSvc) info(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	if err := s.Send(24300, l.infoMessage(v)); err != nil {
		return err
	}
	return s.Send(24317, proto.Message{"code": b2i(v.open && v.rank > 0 && l.left(v) > 0)})
}

func (l *ladderSvc) foeList(v *ladderView) ([]proto.Message, error) {
	out := []proto.Message{}
	if !v.open || v.rank == 0 {
		return out, nil
	}
	for i, rank := range v.lr.Foes {
		f, ok, err := l.foeAt(v.b, rank, v.role)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, f.message(int64(i+1)))
		}
	}
	return out, nil
}

func (l *ladderSvc) foes(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	list, err := l.foeList(v)
	if err != nil {
		return err
	}
	return s.Send(24301, proto.Message{"f_list": list, "type": 0})
}

func (l *ladderSvc) foe(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	f, ok, err := l.foeByRID(v.b, req.Int("rid"), v.role)
	if err != nil || !ok {
		return err
	}
	vip := int64(0)
	if !f.Robot {
		if r, err := l.st.RoleByID(f.RID); err == nil {
			vip = int64(r.VIPLev)
		}
	}
	return s.Send(24302, proto.Message{"rid": f.RID, "srv_id": f.SrvID, "name": f.Name, "lev": f.Lev, "vip_lev": vip, "face": f.Face, "sex": f.Sex,
		"power": f.Power, "rank": f.Rank, "gname": "", "avatar_id": 0, "formation_type": f.FormType, "formation_lev": 1, "p_list": ladderPartners(f),
		"face_update_time": 0, "face_file": ""})
}

func ladderPartners(f ladderFoe) []proto.Message {
	out := []proto.Message{}
	for i, h := range f.Lineup {
		out = append(out, proto.Message{"pos": h.Pos, "id": i + 1, "bid": h.BID, "lev": h.Lev, "quality": h.Star, "star": h.Star, "is_robot": b2i(f.Robot)})
	}
	return out
}

func (l *ladderSvc) refuse(s *gateway.Session, cmd uint16, msg string) error {
	return s.Send(cmd, proto.Message{"code": 0, "msg": msg})
}

// usable checks that the ladder can be fought in and reports why not.
func (l *ladderSvc) usable(v *ladderView) string {
	switch {
	case !v.open:
		return "每周六00:00至周日23:00开启天梯争霸"
	case v.rank == 0:
		return "竞技场排名前" + itoa64(ladderCfg.num("arena_rank")) + "名才能参加天梯争霸"
	}
	return ""
}

func (l *ladderSvc) refresh(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	if msg := l.usable(v); msg != "" {
		return l.refuse(s, 24304, msg)
	}
	if now := s.Now().Unix(); v.lr.RefTime > now {
		return l.refuse(s, 24304, "刷新太频繁了，请稍后再试")
	}
	v.lr.RefTime = s.Now().Unix() + ladderCfg.num("refresh_time")
	v.lr.Foes, v.lr.Fought = l.draw(v.rank), nil
	if err := l.st.SaveState(v.role.RID, ladderModule, v.lr); err != nil {
		return err
	}
	if err := s.Send(24304, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if err := s.Send(24300, l.infoMessage(v)); err != nil {
		return err
	}
	list, err := l.foeList(v)
	if err != nil {
		return err
	}
	return s.Send(24301, proto.Message{"f_list": list, "type": 0})
}

func (l *ladderSvc) buy(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	if v.lr.Bought >= int64(len(ladderCfg.Buy)) {
		return l.refuse(s, 24305, "当前已经购买达到上限")
	}
	price := ladderCfg.Buy[v.lr.Bought]
	if int64(v.role.VIPLev) < price.Vip {
		return l.refuse(s, 24305, "提升VIP等级可增加购买次数")
	}
	err = l.st.Exchange(v.role.RID, map[int64]int64{3: price.Cost}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return l.refuse(s, 24305, "钻石不足")
	}
	if err != nil {
		return err
	}
	v.lr.Bought++
	if err := l.st.SaveState(v.role.RID, ladderModule, v.lr); err != nil {
		return err
	}
	if err := sendAssets(s, l.st, v.role.RID); err != nil {
		return err
	}
	if err := s.Send(24305, proto.Message{"code": 1, "msg": "购买成功"}); err != nil {
		return err
	}
	return s.Send(24300, l.infoMessage(v))
}

// pay turns a reward list into what the role holds.
func (l *ladderSvc) pay(rid int64, reward [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(reward), 1, ga, gi)
	return l.st.Exchange(rid, map[int64]int64{}, map[int64]int64{}, ga, gi)
}

func rewardMessages(reward [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, g := range guildGains(reward) {
		out = append(out, proto.Message{"bid": g.ID, "num": g.Amount})
	}
	return out
}

// quick is the one-key challenge: it takes a challenge and gives the prize of a win without a fight or a move.
func (l *ladderSvc) quick(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	if msg := l.usable(v); msg != "" {
		return l.refuse(s, 24306, msg)
	}
	if l.left(v) == 0 {
		return l.refuse(s, 24306, "挑战次数不足")
	}
	v.lr.Used++
	if err := l.st.SaveState(v.role.RID, ladderModule, v.lr); err != nil {
		return err
	}
	reward := ladderCfg.pairs("swap_times")
	if err := l.pay(v.role.RID, reward); err != nil {
		return err
	}
	if err := sendAssets(s, l.st, v.role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, l.st); err != nil {
		return err
	}
	if err := s.Send(24306, proto.Message{"code": 1, "msg": "一键挑战成功"}); err != nil {
		return err
	}
	return s.Send(24300, l.infoMessage(v))
}

func (l *ladderSvc) challenge(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	v, err := l.view(s)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	role := v.role
	refuse := func(msg string) error { l.mu.Unlock(); return l.refuse(s, 24303, msg) }
	if msg := l.usable(v); msg != "" {
		return refuse(msg)
	}
	foe, ok, err := l.foeByRID(v.b, req.Int("rid"), role)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	listed := false
	for _, r := range v.lr.Foes {
		listed = listed || r == foe.Rank
	}
	switch {
	case !ok || !listed:
		return refuse("对手的排名已经变化，请刷新列表")
	case l.left(v) == 0:
		return refuse("挑战次数不足")
	}
	fights := fightServiceOf(l.st)
	team, objects, form, err := fights.teamSide(role)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	if len(team) == 0 {
		return refuse("没有可以出战的英雄")
	}
	oslots, oform, obyID, err := foe.fighters(l.st)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	foes, foeObjects := sideUnits(foe.RID, foe.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return refuse("对手已无法应战，请刷新列表")
	}
	v.lr.Used++
	err = l.st.SaveState(role.RID, ladderModule, v.lr)
	l.mu.Unlock()
	if err != nil {
		return err
	}
	intro := proto.Message{
		"combat_type": combatLadder, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
		"objects": append(objects, foeObjects...), "is_auto": req.Int("is_auto"), "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": foe.Name,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	rounds, won := buildFight(team, foes, l.rng())
	if err := s.Send(24303, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	_, _, power, _ := lineupOf(l.st, role.RID)
	replayID := recordReplay(l.st, combatLadder, replaySide{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Power: power, Form: form.FormationType},
		replaySide{RID: foe.RID, SrvID: foe.SrvID, Name: foe.Name, Lev: foe.Lev, Face: foe.Face, Power: foe.Power, Form: oform}, intro, rounds, won, s.Now().Unix())
	return fights.begin(s, role, combatLadder, intro, rounds, won, func(s *gateway.Session, won bool) error {
		return l.settle(s, foe, won, replayID, face, power)
	})
}

// settle ends a fight: a win over a foe above exchanges the places, the prize is paid and the result (24307) sent.
func (l *ladderSvc) settle(s *gateway.Session, foe ladderFoe, won bool, replayID, face, power int64) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := s.Now()
	b, err := l.board(role.SrvID, now)
	if err != nil {
		return err
	}
	lr, err := l.role(role.RID, now)
	if err != nil {
		return err
	}
	old := b.rankOf(role.RID)
	rank := old
	// the place is only taken when the foe still holds the rank it held at the start
	held := b.Ranks[foe.Rank] == foe.RID || (foe.Robot && b.Ranks[foe.Rank] == 0)
	if won && old > 0 && foe.Rank < old && held {
		if !foe.Robot {
			b.Ranks[old] = foe.RID
		} else {
			delete(b.Ranks, old)
		}
		b.Ranks[foe.Rank] = role.RID
		rank = foe.Rank
	}
	best := int64(0)
	if lr.Best == 0 || rank < lr.Best {
		lr.Best, best = rank, 1
	}
	result := int64(2)
	reward := ladderCfg.pairs("lose_reward")
	if won {
		result, reward = 1, ladderCfg.pairs("win_reward")
	}
	t := now.Unix()
	rankType := int64(0)
	if rank < old {
		rankType = 1
	}
	lr.Logs = append([]ladderLog{{RID: foe.RID, SrvID: foe.SrvID, Name: foe.Name, Lev: foe.Lev, Sex: foe.Sex, Face: foe.Face, Power: foe.Power, Type: 1,
		Ret: result, RankType: rankType, Rank: rank, ReplayID: replayID, Time: t}}, lr.Logs...)
	lr.Logs = lr.Logs[:min(len(lr.Logs), ladderLogKeep)]
	if err := l.st.SaveState(role.RID, ladderModule, lr); err != nil {
		return err
	}
	if !foe.Robot {
		dl, err := l.role(foe.RID, now)
		if err != nil {
			return err
		}
		defRet, defType := int64(1), int64(0)
		if won {
			defRet = 2
			if rank != old {
				defType = 2
			}
		}
		defRank := b.rankOf(foe.RID)
		dl.Logs = append([]ladderLog{{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Sex: int64(role.Sex), Face: face, Power: power, Type: 2,
			Ret: defRet, RankType: defType, Rank: defRank, ReplayID: replayID, Time: t}}, dl.Logs...)
		dl.Logs = dl.Logs[:min(len(dl.Logs), ladderLogKeep)]
		if err := l.st.SaveState(foe.RID, ladderModule, dl); err != nil {
			return err
		}
	}
	if min(old, foe.Rank) <= ladderCfg.num("watch_rank") {
		b.God = append([]ladderGod{{AtkRID: role.RID, AtkSrv: role.SrvID, AtkName: role.Name, AtkRank: rank, AtkFace: face, DefRID: foe.RID, DefSrv: foe.SrvID,
			DefName: foe.Name, DefRank: b.rankOf(foe.RID), DefFace: foe.Face, Ret: result, ReplayID: replayID, Time: t}}, b.God...)
		b.God = b.God[:min(len(b.God), ladderGodKeep)]
	}
	if err := l.st.SaveGlobal(role.SrvID, ladderModule, b); err != nil {
		return err
	}
	if err := l.pay(role.RID, reward); err != nil {
		return err
	}
	stats, _, _, _, err := damageTable(l.st, role)
	if err != nil {
		return err
	}
	if err := sendAssets(s, l.st, role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, l.st); err != nil {
		return err
	}
	defRank := b.rankOf(foe.RID)
	if foe.Robot {
		defRank = foe.Rank
		if rank == foe.Rank {
			defRank = old
		}
	}
	if err := s.Send(24307, proto.Message{"result": result, "rid": foe.RID, "srv_id": foe.SrvID, "def_name": foe.Name, "def_lev": foe.Lev, "def_face": foe.Face,
		"def_face_update_time": 0, "def_face_file": "", "def_change_rank": foe.Rank - defRank, "def_rank": defRank, "is_change_best_rank": best,
		"atk_change_rank": old - rank, "atk_rank": rank, "reward": rewardMessages(reward), "target_role_name": foe.Name,
		"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "replay_id": replayID}); err != nil {
		return err
	}
	// the new places: the list is drawn again around the new rank
	v := &ladderView{role: role, b: b, lr: lr, rank: rank, open: true}
	if rank != old {
		lr.Foes = l.draw(rank)
		if err := l.st.SaveState(role.RID, ladderModule, lr); err != nil {
			return err
		}
	}
	if err := s.Send(24300, l.infoMessage(v)); err != nil {
		return err
	}
	list, err := l.foeList(v)
	if err != nil {
		return err
	}
	return s.Send(24301, proto.Message{"f_list": list, "type": 0})
}

// rankedRole describes whoever holds a rank for the ranking and the statues.
func (l *ladderSvc) rankedRole(b *ladderBoard, rank int64, me *store.Role) (proto.Message, error) {
	f, ok, err := l.foeAt(b, rank, me)
	if err != nil || !ok {
		return nil, err
	}
	return proto.Message{"rid": f.RID, "srv_id": f.SrvID, "name": f.Name, "gname": "", "lev": f.Lev, "face": f.Face, "sex": f.Sex, "rank": rank, "power": f.Power,
		"avatar_id": 0, "lookid": f.Look, "worship": 0, "worship_status": 0, "face_update_time": 0, "face_file": ""}, nil
}

func (l *ladderSvc) topThree(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	for rank := int64(1); rank <= 3; rank++ {
		m, err := l.rankedRole(v.b, rank, v.role)
		if err != nil {
			return err
		}
		if m != nil {
			out = append(out, m)
		}
	}
	return s.Send(24308, proto.Message{"rank_list": out})
}

func (l *ladderSvc) ranking(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	for rank := int64(1); rank <= ladderCfg.num("watch_rank"); rank++ {
		m, err := l.rankedRole(v.b, rank, v.role)
		if err != nil {
			return err
		}
		if m != nil {
			out = append(out, m)
		}
	}
	return s.Send(24309, proto.Message{"rank": v.rank, "best_rank": v.lr.Best, "rank_list": out})
}

func (l *ladderSvc) myLog(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	for _, e := range v.lr.Logs {
		out = append(out, proto.Message{"rid": e.RID, "srv_id": e.SrvID, "name": e.Name, "lev": e.Lev, "sex": e.Sex, "face": e.Face, "avatar_id": 0, "power": e.Power,
			"type": e.Type, "ret": e.Ret, "rank_type": e.RankType, "rank": e.Rank, "replay_id": e.ReplayID, "time": e.Time, "face_update_time": 0, "face_file": ""})
	}
	return s.Send(24310, proto.Message{"replay_srv_id": v.role.SrvID, "log_list": out})
}

func (l *ladderSvc) godLog(s *gateway.Session, _ proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v, err := l.view(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	for _, e := range v.b.God[:min(len(v.b.God), int(ladderCfg.num("watch_num")))] {
		out = append(out, proto.Message{"atk_rid": e.AtkRID, "atk_srv_id": e.AtkSrv, "atk_name": e.AtkName, "atk_face": e.AtkFace, "atk_rank": e.AtkRank,
			"def_rid": e.DefRID, "def_srv_id": e.DefSrv, "def_name": e.DefName, "def_face": e.DefFace, "def_rank": e.DefRank, "ret": e.Ret, "replay_id": e.ReplayID,
			"time": e.Time, "atk_face_update_time": 0, "atk_face_file": "", "def_face_update_time": 0, "def_face_file": ""})
	}
	return s.Send(24311, proto.Message{"replay_srv_id": v.role.SrvID, "log_list": out})
}

func (l *ladderSvc) isOpen(s *gateway.Session, _ proto.Message) error {
	_, open := ladderWeek(s.Now())
	return s.Send(24312, proto.Message{"code": b2i(open)})
}

// noCD takes away the cool-down after a fight; ours has none (battle_cd is 0).
func (l *ladderSvc) noCD(s *gateway.Session, _ proto.Message) error {
	return s.Send(24315, proto.Message{"code": 1, "msg": ""})
}

// heroCard would show one hero of a foe in detail; the foes' heroes are on the list already.
func (l *ladderSvc) heroCard(s *gateway.Session, _ proto.Message) error {
	return s.Send(24316, proto.Message{"code": 0, "msg": "暂不支持查看英雄详情"})
}

// share is the share of a replay to a chat channel; replays are shared in the replay hall (19905).
func (l *ladderSvc) share(s *gateway.Session, _ proto.Message) error {
	return s.Send(24318, proto.Message{"result": 0, "msg": "请在录像界面分享"})
}
