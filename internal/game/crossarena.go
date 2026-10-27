package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The cross-server arena (跨服竞技场, 25600-25618) of one server: the roles of the server that are level 115 or more fight each
// other (and robots of ours when there are not enough of them) with three teams against three teams, the first to two wins,
// for a score. The play days are Tuesday, Thursday and Saturday of a weekly season; at the season's turn the first
// three are honoured and the ranks are paid by mail. The numbers the client names (tools/extract_crossarena.py) are
// quoted; the rest (the score of a win, the cards, how the opponents are drawn) is ours and is listed in OPEN_QUESTIONS.

//go:embed data/crossarena.json
var crossArenaJSON []byte

const (
	caModule   = "crossarena"
	caCombat   = 28 // BattleConst.Fight_Type.CrossArenaWar
	caOpps     = 3  // opponents on the list
	caTeams    = 3  // teams a side fights with
	caLogKeep  = 20
	caRobotRID = 4250000000 // below 2^32, clear of the other robots (arena 4.0e9, ladder 4.1e9, champion 4.2e9)
	caHonour   = 3          // the first three of a season are honoured
	// WorshipType.crossarena, the kind the honour list worships with (10316)
	worshipCrossArena = 4
)

type caConf struct {
	Const map[string]json.RawMessage `json:"const"`
	Daily []struct {
		Num   int64      `json:"num"`
		Items [][2]int64 `json:"items"`
	} `json:"daily"`
	Ranks []struct {
		Min   int64      `json:"min"`
		Max   int64      `json:"max"`
		Items [][2]int64 `json:"items"`
	} `json:"ranks"`
}

var caCfg = func() *caConf {
	c := &caConf{}
	if err := json.Unmarshal(crossArenaJSON, c); err != nil || len(c.Daily) == 0 || len(c.Ranks) == 0 || len(c.Const) == 0 {
		panic("game: bad crossarena.json")
	}
	return c
}()

func (c *caConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *caConf) pairs(key string) [][2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

func (c *caConf) pair(key string) [2]int64 {
	var p [2]int64
	_ = json.Unmarshal(c.Const[key], &p)
	return p
}

func (c *caConf) days() []int64 {
	var l []int64
	_ = json.Unmarshal(c.Const["season_time"], &l)
	return l
}

var (
	caMinLev    = caCfg.num("lev_limt")
	caInitScore = caCfg.num("init_score")
	caMinScore  = caCfg.num("min_score")
	caRankMin   = caCfg.num("in_rank_min_score")
	caRankMax   = caCfg.num("rank_number")
)

// caForm is one team of a saved line-up: the heroes by partner id.
type caForm struct {
	Order, FormationType, HallowsID int64
	Hidden                          bool
	Slots                           []eliteSlot
}

// caTeam is a team of a robot: its line-up is fixed.
type caTeam struct {
	Order, FormType int64
	Lineup          []arenaHero
}

// caOpp is one opponent on the list: a role of the server (its defence teams are read when it is looked at or fought) or a robot.
type caOpp struct {
	Idx, RID                     int64
	SrvID, Name                  string
	Lev, Sex, Face, Power, Score int64
	Robot                        bool
	Teams                        []caTeam // a robot's teams
}

type caSide struct {
	RID, Lev, Face, Rank, Score int64
	SrvID, Name                 string
}

type caLog struct {
	ID, Type, Ret, Wins, Loses, Time int64 // Type 1 the role attacked, 2 it was attacked; Ret 1 a win of the role
	Mine, Foe                        caSide
	Duels                            []atDuel
}

type caCard struct {
	Reward [2]int64
	Status int64 // 0 face down, 1 shown (can be bought), 2 taken
}

type caRole struct {
	RID         int64
	SrvID, Name string
	Lev, Face   int64
	Season      int64
	Score       int64
	Fought      bool // fought in the season: only then it is ranked
	Wins, Loses int64
	Best        int64 // best rank of the season
	Day, Used   int64
	Taken       []int64 // daily boxes taken today
	Atk, Def    []caForm
	Opps        []caOpp
	RefAt       int64
	Auto        bool
	Fights      int64 // challenges of the season
	Cards       []caCard
	Logs        []caLog
	NextLog     int64
}

type caHonoured struct {
	RID, Rank   int64
	SrvID, Name string
}

type caBoard struct {
	Roles   map[int64]*caRole
	Honour  []caHonoured
	Settled int64
}

type caSvc struct {
	st  *store.Store
	mu  sync.Mutex
	rng func() *rand.Rand
}

type caCtx struct {
	role *store.Role
	now  time.Time
	b    *caBoard
	me   *caRole
	low  bool // the role is below the level: nothing it does is kept
}

// RegisterCrossArena registers the cross-server arena (25600-25618).
func RegisterCrossArena(r *gateway.Router, db *store.Store) {
	c := &caSvc{st: db}
	fights := fightServiceOf(db)
	c.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	for cmd, h := range map[uint16]func(*gateway.Session, *caCtx, proto.Message) error{
		25600: c.main, 25601: c.list, 25602: c.look, 25603: c.noHero, 25604: c.saveForms, 25605: c.forms, 25606: c.challenge,
		25607: c.boxes, 25608: c.takeBox, 25609: c.refresh, 25610: c.status, 25612: c.cards, 25613: c.flip, 25614: c.honour,
		25615: c.ranking, 25616: c.logs, 25617: c.report, 25618: c.reds,
	} {
		r.Handle(cmd, c.wrap(cmd, h))
	}
}

// wrap runs a handler on the board under the lock and saves the board afterwards. A role below the level is
// answered as one the arena is closed to (the client asks at login): it gets a standing of its own that is not kept, and
// the commands that change something are refused.
func (c *caSvc) wrap(cmd uint16, fn func(*gateway.Session, *caCtx, proto.Message) error) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		role, err := c.st.RoleByID(s.Role().RID)
		if err != nil {
			return err
		}
		now := s.Now()
		b, err := c.board(role.SrvID, now)
		if err != nil {
			return err
		}
		if int64(role.Lev) < caMinLev {
			cx := &caCtx{role: role, now: now, b: b, me: &caRole{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Score: caInitScore}, low: true}
			return fn(s, cx, req)
		}
		if _, err := c.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
			return err
		}
		cx := &caCtx{role: role, now: now, b: b, me: b.touch(role, now)}
		if err := fn(s, cx, req); err != nil {
			return err
		}
		return c.st.SaveGlobal(role.SrvID, caModule, b)
	}
}

// ---- the season

// caSeason is the season of a moment: the key (the Monday it began), the first play day's start and the end (Saturday 23:00).
func caSeason(now time.Time) (key, start, end int64) {
	key, begin, _ := arenaSeason(now)
	return key, begin, begin + 5*86400 + 23*3600
}

// caOpen says whether the arena is open at a moment: a play day, before the season's end.
func caOpen(now time.Time) bool {
	_, _, end := caSeason(now)
	if now.Unix() >= end {
		return false
	}
	wd := int64(now.In(dayZone).Weekday())
	if wd == 0 {
		wd = 7
	}
	for _, d := range caCfg.days() {
		if d == wd {
			return true
		}
	}
	return false
}

// caNextOpen is when the arena opens next: the start of the next play day.
func caNextOpen(now time.Time) int64 {
	day := dayStart(now.In(dayZone))
	for i := 1; i <= 8; i++ {
		if t := day.AddDate(0, 0, i); caOpen(t) {
			return t.Unix()
		}
	}
	return 0
}

func (c *caSvc) board(srv string, now time.Time) (*caBoard, error) {
	b := &caBoard{}
	if _, err := c.st.LoadGlobal(srv, caModule, b); err != nil {
		return nil, err
	}
	if b.Roles == nil {
		b.Roles = map[int64]*caRole{}
	}
	key, _, _ := caSeason(now)
	var old []*caRole
	for _, r := range b.Roles {
		if r.Season != key {
			old = append(old, r)
		}
	}
	if len(old) > 0 {
		if err := c.settle(srv, b, old); err != nil {
			return nil, err
		}
		for _, r := range old {
			r.Season, r.Score, r.Fought, r.Wins, r.Loses, r.Best, r.Fights = key, caInitScore, false, 0, 0, 0, 0
			r.Opps, r.Cards = nil, nil
		}
		b.Settled = key
		if err := c.st.SaveGlobal(srv, caModule, b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// settle mails the rank rewards of the season that is over to the ranked roles and keeps the first three for the honour list.
func (c *caSvc) settle(srv string, b *caBoard, roles []*caRole) error {
	ranked := caRanked(roles)
	b.Honour = nil
	for i, r := range ranked {
		rank := int64(i + 1)
		if rank <= caHonour {
			b.Honour = append(b.Honour, caHonoured{RID: r.RID, Rank: rank, SrvID: r.SrvID, Name: r.Name})
		}
		for _, rk := range caCfg.Ranks {
			if rank >= rk.Min && rank <= rk.Max {
				m := store.Mail{SrvID: srv, Subject: "跨服竞技场排名奖励", Content: "本赛季跨服竞技场你的最终排名是第" + itoa64(rank) + "名，这是排名奖励。",
					FromName: "跨服竞技场", CreatedBy: "crossarena", Attachments: attachmentsOf(rk.Items)}
				if _, err := c.st.CreateMail(m, []int64{r.RID}); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// caRanked orders the roles that fought and have the score to be listed, the higher score first, at most the board's size.
func caRanked(roles []*caRole) []*caRole {
	var out []*caRole
	for _, r := range roles {
		if r.Fought && r.Score >= caRankMin {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].RID < out[j].RID
	})
	if int64(len(out)) > caRankMax {
		out = out[:caRankMax]
	}
	return out
}

func (b *caBoard) ranked() []*caRole {
	var all []*caRole
	for _, r := range b.Roles {
		all = append(all, r)
	}
	return caRanked(all)
}

func (b *caBoard) rankOf(rid int64) int64 {
	for i, r := range b.ranked() {
		if r.RID == rid {
			return int64(i + 1)
		}
	}
	return 0
}

// touch gets the role's standing, new for a role never seen, with the day's counters brought up to date.
func (b *caBoard) touch(role *store.Role, now time.Time) *caRole {
	key, _, _ := caSeason(now)
	r := b.Roles[role.RID]
	if r == nil {
		r = &caRole{RID: role.RID, Season: key, Score: caInitScore}
		b.Roles[role.RID] = r
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	r.SrvID, r.Name, r.Lev, r.Face = role.SrvID, role.Name, int64(role.Lev), face
	if day := dayKey(now); r.Day != day {
		r.Day, r.Used, r.Taken = day, 0, nil
	}
	return r
}

// ---- teams

// caTeamData is a team ready to be shown or to fight.
type caTeamData struct {
	Order, FormType int64
	Hidden          bool
	Slots           []store.FormationSlot
	ByID            map[int64]store.Partner
	Heroes          []arenaHero
	IDs             map[int64]int64 // partner id by position
	Power           int64
}

func teamFromSlots(slots []store.FormationSlot, byID map[int64]store.Partner, order, formType int64, hidden bool) caTeamData {
	t := caTeamData{Order: order, FormType: formType, Hidden: hidden, ByID: byID, IDs: map[int64]int64{}}
	sorted := append([]store.FormationSlot(nil), slots...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Pos < sorted[j].Pos })
	var all []store.Partner
	for _, sl := range sorted {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		if _, known := partnerTable[h.BID]; !known {
			continue
		}
		t.Slots = append(t.Slots, sl)
		t.Heroes = append(t.Heroes, arenaHero{sl.Pos, h.BID, int64(h.Lev), int64(h.Star), int64(h.BreakLev)})
		t.IDs[sl.Pos] = sl.PartnerID
		all = append(all, h)
	}
	t.Power = rolePower(all)
	return t
}

// teamsOf reads a real role's teams of a kind: the saved forms, or the campaign team as the first when nothing was saved.
func (c *caSvc) teamsOf(rid int64, forms []caForm) ([]caTeamData, error) {
	heroes, err := c.st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	byID := map[int64]store.Partner{}
	for _, h := range heroes {
		byID[h.PartnerID] = h
	}
	out := make([]caTeamData, caTeams)
	for i := range out {
		out[i] = caTeamData{Order: int64(i + 1), FormType: 1, ByID: byID, IDs: map[int64]int64{}}
	}
	if len(forms) == 0 {
		base, err := formationOf(c.st, rid, formDrama)
		if err != nil {
			return nil, err
		}
		out[0] = teamFromSlots(base.Slots, byID, 1, base.FormationType, false)
		return out, nil
	}
	for _, f := range forms {
		if f.Order < 1 || f.Order > caTeams {
			continue
		}
		var slots []store.FormationSlot
		for _, sl := range f.Slots {
			slots = append(slots, store.FormationSlot{Pos: sl.Pos, PartnerID: sl.ID})
		}
		out[f.Order-1] = teamFromSlots(slots, byID, f.Order, max(1, f.FormationType), f.Hidden)
	}
	return out, nil
}

// oppTeams are the teams of an opponent as they would fight.
func (c *caSvc) oppTeams(b *caBoard, o caOpp) ([]caTeamData, error) {
	if !o.Robot {
		var def []caForm
		if r := b.Roles[o.RID]; r != nil {
			def = r.Def
		}
		return c.teamsOf(o.RID, def)
	}
	out := make([]caTeamData, caTeams)
	for i := range out {
		out[i] = caTeamData{Order: int64(i + 1), FormType: 1, ByID: map[int64]store.Partner{}, IDs: map[int64]int64{}}
	}
	for _, t := range o.Teams {
		if t.Order < 1 || t.Order > caTeams {
			continue
		}
		opp := arenaOpp{RID: o.RID, SrvID: o.SrvID, Robot: true, FormType: t.FormType, Lineup: t.Lineup}
		slots, ft, byID, err := opp.fighters(c.st)
		if err != nil {
			return nil, err
		}
		out[t.Order-1] = teamFromSlots(slots, byID, t.Order, ft, false)
	}
	return out, nil
}

func totalPower(ts []caTeamData) int64 {
	var n int64
	for _, t := range ts {
		n += t.Power
	}
	return n
}

// ---- opponents

// draw picks the opponents of a role: roles of the server whose score is closest, and robots (a copy of the role's own
// teams, a little weaker or stronger) to fill up.
func (c *caSvc) draw(cx *caCtx, rng *rand.Rand) ([]caOpp, error) {
	var near []*caRole
	for _, r := range cx.b.Roles {
		if r.RID != cx.role.RID && r.SrvID == cx.role.SrvID {
			near = append(near, r)
		}
	}
	abs := func(n int64) int64 {
		if n < 0 {
			return -n
		}
		return n
	}
	sort.Slice(near, func(i, j int) bool {
		di, dj := abs(near[i].Score-cx.me.Score), abs(near[j].Score-cx.me.Score)
		if di != dj {
			return di < dj
		}
		return near[i].RID < near[j].RID
	})
	var out []caOpp
	for _, r := range near {
		if len(out) == caOpps {
			break
		}
		ts, err := c.teamsOf(r.RID, r.Def)
		if err != nil {
			return nil, err
		}
		if totalPower(ts) == 0 {
			continue
		}
		out = append(out, caOpp{Idx: int64(len(out) + 1), RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: r.Lev, Face: r.Face, Power: totalPower(ts), Score: r.Score})
	}
	mine, err := c.teamsOf(cx.role.RID, cx.me.Def)
	if err != nil {
		return nil, err
	}
	for len(out) < caOpps {
		i := int64(len(out) + 1)
		o := caOpp{Idx: i, RID: caRobotRID + cx.role.RID%1000000*10 + i, SrvID: cx.role.SrvID, Robot: true,
			Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))], Sex: int64(rng.Intn(2) + 1), Face: arenaRobotFaces[rng.Intn(len(arenaRobotFaces))],
			Lev: int64(cx.role.Lev), Score: max(caMinScore, cx.me.Score+(i-2)*40+int64(rng.Intn(21)-10))}
		factor := 85 + int64(rng.Intn(21)) + i*3
		for _, t := range mine {
			if len(t.Heroes) == 0 {
				continue
			}
			rt := caTeam{Order: t.Order, FormType: t.FormType}
			var all []store.Partner
			for _, h := range t.Heroes {
				lev := max(1, h.Lev*factor/100)
				rt.Lineup = append(rt.Lineup, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
				all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
			}
			o.Power += rolePower(all)
			o.Teams = append(o.Teams, rt)
		}
		out = append(out, o)
	}
	return out, nil
}

func (c *caSvc) ensureOpps(cx *caCtx) error {
	if len(cx.me.Opps) == caOpps {
		return nil
	}
	opps, err := c.draw(cx, c.rng())
	if err != nil {
		return err
	}
	cx.me.Opps = opps
	return nil
}

func (cx *caCtx) oppByRID(rid int64) *caOpp {
	for i := range cx.me.Opps {
		if cx.me.Opps[i].RID == rid {
			return &cx.me.Opps[i]
		}
	}
	return nil
}

// ---- handlers

func (c *caSvc) tickets(rid int64) (int64, error) {
	items, err := c.st.RoleItems(rid)
	if err != nil {
		return 0, err
	}
	return items[caCfg.num("arena_ticket")], nil
}

func (c *caSvc) main(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	return s.Send(25600, c.mainMessage(cx))
}

func (c *caSvc) mainMessage(cx *caCtx) proto.Message {
	_, start, end := caSeason(cx.now)
	startAt := start
	if !caOpen(cx.now) {
		startAt = caNextOpen(cx.now)
	}
	rank := cx.b.rankOf(cx.role.RID)
	return proto.Message{"rank": rank, "max_rank": cx.me.Best, "score": cx.me.Score, "can_combat_num": 0, "buy_combat_num": 0,
		"ref_time": cx.me.RefAt, "start_time": startAt, "end_time": end, "is_auto": b2i(cx.me.Auto), "season_combat_num": cx.me.Fights}
}

func oppMessage(o caOpp) proto.Message {
	return proto.Message{"idx": o.Idx, "rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "sex": o.Sex, "face": o.Face, "look": 0,
		"power": o.Power, "score": o.Score, "face_update_time": 0, "face_file": ""}
}

func (c *caSvc) list(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	if err := c.ensureOpps(cx); err != nil {
		return err
	}
	list := []proto.Message{}
	for _, o := range cx.me.Opps {
		list = append(list, oppMessage(o))
	}
	return s.Send(25601, proto.Message{"f_list": list, "type": 0})
}

func (c *caSvc) look(s *gateway.Session, cx *caCtx, req proto.Message) error {
	o := cx.oppByRID(req.Int("rid"))
	if o == nil {
		return nil
	}
	ts, err := c.oppTeams(cx.b, *o)
	if err != nil {
		return err
	}
	orders := []proto.Message{}
	for _, t := range ts {
		plist := []proto.Message{}
		for _, h := range t.Heroes {
			if t.Hidden {
				break
			}
			plist = append(plist, proto.Message{"pos": h.Pos, "id": t.IDs[h.Pos], "bid": h.BID, "lev": h.Lev, "star": h.Star, "use_skin": 0, "resonate_lev": 0})
		}
		orders = append(orders, proto.Message{"order": t.Order, "power": t.Power, "is_hidden": b2i(t.Hidden), "p_list": plist})
	}
	return s.Send(25602, proto.Message{"rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "face": o.Face, "face_update_time": 0, "face_file": "",
		"sex": o.Sex, "power": o.Power, "score": o.Score, "avatar_id": 0, "order_list": orders})
}

// noHero (25603) shows one hero of an opponent; the hero tips need data we do not have for a hero of a line-up.
func (c *caSvc) noHero(s *gateway.Session, _ *caCtx, _ proto.Message) error {
	return s.Send(25603, proto.Message{"code": 0, "msg": "暂时无法查看该英雄"})
}

func formsMessage(ts []caTeamData) []proto.Message {
	out := []proto.Message{}
	for _, t := range ts {
		pos := []proto.Message{}
		for _, sl := range t.Slots {
			pos = append(pos, proto.Message{"pos": sl.Pos, "id": sl.PartnerID})
		}
		out = append(out, proto.Message{"order": t.Order, "formation_type": t.FormType, "pos_info": pos, "hallows_id": 0, "is_hidden": b2i(t.Hidden)})
	}
	return out
}

// forms (25605): the line-up of a kind, 1 the attacking one and 2 the defending one.
func (c *caSvc) forms(s *gateway.Session, cx *caCtx, req proto.Message) error {
	typ := req.Int("type")
	saved := cx.me.Atk
	if typ == 2 {
		saved = cx.me.Def
	}
	ts, err := c.teamsOf(cx.role.RID, saved)
	if err != nil {
		return err
	}
	return s.Send(25605, proto.Message{"type": typ, "formations": formsMessage(ts)})
}

func (c *caSvc) saveForms(s *gateway.Session, cx *caCtx, req proto.Message) error {
	typ := req.Int("type")
	refuse := func(msg string) error { return s.Send(25604, proto.Message{"code": 0, "msg": msg}) }
	if cx.low {
		return refuse("等级不足")
	}
	heroes, err := c.st.PartnersOf(cx.role.RID)
	if err != nil {
		return err
	}
	owned := map[int64]bool{}
	for _, h := range heroes {
		owned[h.PartnerID] = true
	}
	var forms []caForm
	used := map[int64]bool{}
	filled, hidden := 0, 0
	for _, f := range req.Arr("formations") {
		cf := caForm{Order: f.Int("order"), FormationType: max(1, f.Int("formation_type")), HallowsID: f.Int("hallows_id"), Hidden: f.Int("is_hidden") == 1}
		if cf.Order < 1 || cf.Order > caTeams {
			return refuse("队伍不对")
		}
		for _, p := range f.Arr("pos_info") {
			id := p.Int("id")
			if id == 0 {
				continue
			}
			if !owned[id] || used[id] {
				return refuse("英雄不能上阵")
			}
			used[id] = true
			cf.Slots = append(cf.Slots, eliteSlot{p.Int("pos"), id})
		}
		if len(cf.Slots) > 0 {
			filled++
		}
		if cf.Hidden {
			hidden++
		}
		forms = append(forms, cf)
	}
	if filled < 2 {
		return refuse("至少需2个队伍有英雄上阵")
	}
	if typ == 2 {
		if rank := cx.b.rankOf(cx.role.RID); rank > 0 && rank <= caCfg.num("second_hide_rank") && hidden != 2 {
			return refuse("排名靠前的玩家需要隐藏2队")
		}
		cx.me.Def = forms
	} else {
		for i := range forms {
			forms[i].Hidden = false
		}
		cx.me.Atk = forms
	}
	return s.Send(25604, proto.Message{"code": 1, "msg": ""})
}

func (c *caSvc) status(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	return s.Send(25610, proto.Message{"code": b2i(!cx.low && caOpen(cx.now))})
}

func (c *caSvc) refresh(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	reply := func(code int, msg string) error {
		return s.Send(25609, proto.Message{"code": code, "msg": msg, "ref_time": cx.me.RefAt})
	}
	if cx.low || !caOpen(cx.now) {
		return reply(0, "玩法未开启")
	}
	if cx.me.RefAt > cx.now.Unix() {
		return reply(0, "刷新太频繁")
	}
	opps, err := c.draw(cx, c.rng())
	if err != nil {
		return err
	}
	cx.me.Opps = opps
	cx.me.RefAt = cx.now.Unix() + caCfg.num("ref_time")
	if err := reply(1, ""); err != nil {
		return err
	}
	return c.list(s, cx, nil)
}

func (c *caSvc) boxes(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	nums := []proto.Message{}
	for _, n := range cx.me.Taken {
		nums = append(nums, proto.Message{"num": n})
	}
	return s.Send(25607, proto.Message{"had_combat_num": cx.me.Used, "num_list": nums})
}

func (c *caSvc) takeBox(s *gateway.Session, cx *caCtx, req proto.Message) error {
	num := req.Int("num")
	refuse := func(msg string) error { return s.Send(25608, proto.Message{"code": 0, "msg": msg}) }
	if cx.low {
		return refuse("等级不足")
	}
	var items [][2]int64
	for _, d := range caCfg.Daily {
		if d.Num == num {
			items = d.Items
		}
	}
	switch {
	case items == nil:
		return refuse("没有这个宝箱")
	case cx.me.Used < num:
		return refuse("挑战次数不足")
	case has(cx.me.Taken, num):
		return refuse("已经领取过了")
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	for _, it := range items {
		splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
	}
	if err := c.st.Exchange(cx.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	cx.me.Taken = append(cx.me.Taken, num)
	if err := s.Send(25608, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if err := c.boxes(s, cx, nil); err != nil {
		return err
	}
	if err := sendAssets(s, c.st, cx.role.RID); err != nil {
		return err
	}
	return sendBackpack(s, c.st)
}

func (c *caSvc) honour(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	list := []proto.Message{}
	for _, h := range cx.b.Honour {
		got, err := c.st.Counter(h.RID, worshipGot)
		if err != nil {
			return err
		}
		done, err := c.st.Counter(cx.role.RID, worshipKey(dayKey(cx.now), worshipCrossArena, h.RID))
		if err != nil {
			return err
		}
		list = append(list, proto.Message{"rid": h.RID, "srv_id": h.SrvID, "name": h.Name, "gname": "", "rank": h.Rank, "lookid": 0,
			"worship": got, "worship_status": b2i(done > 0)})
	}
	return s.Send(25614, proto.Message{"rank_list": list})
}

func (c *caSvc) ranking(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	list := []proto.Message{}
	for i, r := range cx.b.ranked() {
		power := int64(0)
		if ts, err := c.teamsOf(r.RID, r.Def); err == nil {
			power = totalPower(ts)
		}
		list = append(list, proto.Message{"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "gname": "", "lev": r.Lev, "face": r.Face, "sex": 0,
			"rank": i + 1, "power": power, "avatar_id": 0, "score": r.Score, "face_update_time": 0, "face_file": ""})
	}
	return s.Send(25615, proto.Message{"rank": cx.b.rankOf(cx.role.RID), "best_rank": cx.me.Best, "arena_cluster_role": list})
}

func (c *caSvc) logs(s *gateway.Session, cx *caCtx, req proto.Message) error {
	list := []proto.Message{}
	for _, l := range cx.me.Logs {
		atk, def := l.Mine, l.Foe
		if l.Type == 2 {
			atk, def = l.Foe, l.Mine
		}
		list = append(list, proto.Message{"id": l.ID, "rid": atk.RID, "srv_id": atk.SrvID, "atk_name": atk.Name, "atk_face": atk.Face, "atk_lev": atk.Lev,
			"atk_rank": atk.Rank, "atk_score": atk.Score, "atk_face_update_time": 0, "atk_face_file": "", "def_rid": def.RID, "def_srv_id": def.SrvID,
			"def_name": def.Name, "def_face": def.Face, "def_lev": def.Lev, "def_rank": def.Rank, "def_score": def.Score, "def_face_update_time": 0,
			"def_face_file": "", "ret": l.Ret, "win_count": l.Wins, "lose_count": l.Loses, "combat_type": l.Type, "time": l.Time})
	}
	return s.Send(25616, proto.Message{"type": req.Int("type"), "arena_cluster_log": list})
}

func (c *caSvc) report(s *gateway.Session, cx *caCtx, req proto.Message) error {
	infos := []proto.Message{}
	for _, l := range cx.me.Logs {
		if l.ID != req.Int("id") {
			continue
		}
		plist := func(hs []arenaHero) []proto.Message {
			out := []proto.Message{}
			for _, h := range hs {
				out = append(out, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "ext": []proto.Message{}})
			}
			return out
		}
		for i, d := range l.Duels {
			infos = append(infos, proto.Message{"order": i + 1, "id": d.Replay, "round": 1, "ret": d.Ret, "time": d.Time, "rid": d.ARID, "srv_id": d.ASrv,
				"a_power": d.APower, "a_formation_type": d.AForm, "a_order": d.AOrder, "a_end_hp": 0, "b_rid": d.BRID, "b_srv_id": d.BSrv, "b_power": d.BPower,
				"b_formation_type": d.BForm, "b_order": d.BOrder, "b_end_hp": 0, "a_plist": plist(d.AList), "b_plist": plist(d.BList),
				"hurt_statistics": []proto.Message{}})
		}
	}
	return s.Send(25617, proto.Message{"type": req.Int("type"), "id": req.Int("id"), "arena_replay_infos": infos})
}

func (c *caSvc) reds(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	worship := 0
	for _, h := range cx.b.Honour {
		if done, err := c.st.Counter(cx.role.RID, worshipKey(dayKey(cx.now), worshipCrossArena, h.RID)); err == nil && done == 0 {
			worship = 1
		}
	}
	reward := 0
	for _, d := range caCfg.Daily {
		if cx.me.Used >= d.Num && !has(cx.me.Taken, d.Num) {
			reward = 1
		}
	}
	return s.Send(25618, proto.Message{"worship_code": worship, "reward_code": reward, "combat_code": b2i(!cx.low && caOpen(cx.now) && cx.me.Used == 0)})
}

// ---- the cards of a win

func cardsMessage(cards []caCard, price func(taken int) [2]int64) []proto.Message {
	taken := 0
	for _, cd := range cards {
		if cd.Status == 2 {
			taken++
		}
	}
	out := []proto.Message{}
	for i, cd := range cards {
		reward := []proto.Message{}
		val := []proto.Message{}
		if cd.Status > 0 {
			reward = append(reward, proto.Message{"item_id": cd.Reward[0], "num": cd.Reward[1]})
		}
		if cd.Status == 1 {
			p := price(taken)
			val = append(val, proto.Message{"item_id": p[0], "num": p[1]})
		}
		out = append(out, proto.Message{"pos": i + 1, "status": cd.Status, "reward": reward, "val": val})
	}
	return out
}

func (c *caSvc) cardPrice(taken int) [2]int64 {
	if taken >= 2 {
		return caCfg.pair("card_third_cost")
	}
	return caCfg.pair("card_second_cost")
}

// caCardRewards are the three cards of a win (ours: the client names no table): marks of the brave, a little, some and many.
var caCardRewards = [][2]int64{{30, 50}, {30, 100}, {30, 200}}

func (c *caSvc) cards(s *gateway.Session, cx *caCtx, _ proto.Message) error {
	return s.Send(25612, proto.Message{"card_info": cardsMessage(cx.me.Cards, c.cardPrice)})
}

// flip (25613): the first card is free; once one is taken the others are shown and bought at the prices of the second
// and the third.
func (c *caSvc) flip(s *gateway.Session, cx *caCtx, req proto.Message) error {
	refuse := func(msg string) error { return s.Send(25613, proto.Message{"code": 0, "msg": msg}) }
	if cx.low {
		return refuse("等级不足")
	}
	pos := int(req.Int("pos"))
	if len(cx.me.Cards) == 0 {
		return refuse("没有可翻的牌")
	}
	if pos < 1 || pos > len(cx.me.Cards) {
		return refuse("没有这张牌")
	}
	card := &cx.me.Cards[pos-1]
	if card.Status == 2 {
		return refuse("已经领取过了")
	}
	taken := 0
	for _, cd := range cx.me.Cards {
		if cd.Status == 2 {
			taken++
		}
	}
	spendA, spendI := map[int64]int64{}, map[int64]int64{}
	if taken > 0 {
		p := c.cardPrice(taken)
		splitGains([]sellGain{{p[0], p[1]}}, 1, spendA, spendI)
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains([]sellGain{{card.Reward[0], card.Reward[1]}}, 1, ga, gi)
	if err := c.st.Exchange(cx.role.RID, spendA, spendI, ga, gi); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足")
		}
		return err
	}
	card.Status = 2
	for i := range cx.me.Cards {
		if cx.me.Cards[i].Status == 0 {
			cx.me.Cards[i].Status = 1
		}
	}
	if taken+1 == len(cx.me.Cards) {
		cx.me.Cards = nil
	}
	if err := s.Send(25613, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if len(cx.me.Cards) > 0 {
		if err := c.cards(s, cx, nil); err != nil {
			return err
		}
	}
	if err := sendAssets(s, c.st, cx.role.RID); err != nil {
		return err
	}
	return sendBackpack(s, c.st)
}

// ---- the fight

func caIntro(mf, tf int64, objects []proto.Message, mine int, myName, foeName string) proto.Message {
	return proto.Message{
		"combat_type": caCombat, "formation": []proto.Message{{"group": 1, "formation_type": mf, "formation_lev": 1}, {"group": 2, "formation_type": tf, "formation_lev": 1}},
		"objects": append([]proto.Message{atSpirit}, objects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": mine, "actor_role_name": myName,
		"target_role_name": foeName, "suppress": 0, "halo_list": []proto.Message{},
		"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
}

type caDuels struct {
	duels  []atDuel
	rounds [][]fightRound
	intros []proto.Message
	hurts  []proto.Message
	wins   int64
	loses  int64
	won    bool
}

// play fights the teams in order, team i against team i, until one side has two wins. A team without heroes loses to
// one with heroes; when neither has any it is a draw, which is no win for anybody.
func (c *caSvc) play(cx *caCtx, mine, theirs []caTeamData, foe caOpp, rng *rand.Rand) (*caDuels, error) {
	p := &caDuels{}
	face := cx.me.Face
	for i := 0; i < caTeams && p.wins < 2 && p.loses < 2; i++ {
		mu, mo := sideUnits(cx.role.RID, cx.role.SrvID, mine[i].Slots, mine[i].FormType, mine[i].ByID, true)
		tu, to := sideUnits(foe.RID, foe.SrvID, theirs[i].Slots, theirs[i].FormType, theirs[i].ByID, false)
		var rounds []fightRound
		var ret int64
		switch {
		case len(mu) == 0 && len(tu) == 0:
			ret = 0
		case len(tu) == 0:
			ret = 1
		case len(mu) == 0:
			ret = 2
		default:
			var won bool
			rounds, won = buildFight(mu, tu, rng)
			ret = 2
			if won {
				ret = 1
			}
		}
		switch ret {
		case 1:
			p.wins++
		case 2:
			p.loses++
		}
		d := atDuel{Ret: ret, ARID: cx.role.RID, ASrv: cx.role.SrvID, BRID: foe.RID, BSrv: foe.SrvID, AOrder: int64(i + 1), BOrder: int64(i + 1),
			APower: mine[i].Power, BPower: theirs[i].Power, AForm: mine[i].FormType, BForm: theirs[i].FormType, AList: mine[i].Heroes, BList: theirs[i].Heroes, Time: cx.now.Unix()}
		intro := caIntro(mine[i].FormType, theirs[i].FormType, append(append([]proto.Message{}, mo...), to...), len(mu), cx.role.Name, foe.Name)
		if rounds != nil {
			d.Replay = recordReplay(c.st, caCombat, replaySide{RID: cx.role.RID, SrvID: cx.role.SrvID, Name: cx.role.Name, Lev: int64(cx.role.Lev), Face: face, Power: d.APower, Form: d.AForm},
				replaySide{RID: foe.RID, SrvID: foe.SrvID, Name: foe.Name, Lev: foe.Lev, Face: foe.Face, Power: d.BPower, Form: d.BForm}, intro, rounds, ret == 1, cx.now.Unix())
		}
		p.hurts = append(p.hurts, proto.Message{"round": i + 1, "hurt_statistics": []proto.Message{}})
		p.duels = append(p.duels, d)
		p.rounds = append(p.rounds, rounds)
		p.intros = append(p.intros, intro)
	}
	p.won = p.wins >= 2
	return p, nil
}

func countTeams(ts []caTeamData) int {
	n := 0
	for _, t := range ts {
		if len(t.Heroes) > 0 {
			n++
		}
	}
	return n
}

func (c *caSvc) challenge(s *gateway.Session, cx *caCtx, req proto.Message) error {
	refuse := func(msg string) error { return s.Send(25606, proto.Message{"code": 0, "msg": msg}) }
	if cx.low || !caOpen(cx.now) {
		return refuse("玩法未开启")
	}
	foe := cx.oppByRID(req.Int("rid"))
	if foe == nil {
		return refuse("请先刷新对手")
	}
	mine, err := c.teamsOf(cx.role.RID, cx.me.Atk)
	if err != nil {
		return err
	}
	if len(cx.me.Atk) == 0 { // the defence line-up is the attacking one until an attacking one is saved
		if mine, err = c.teamsOf(cx.role.RID, cx.me.Def); err != nil {
			return err
		}
	}
	if countTeams(mine) < 2 {
		return refuse("至少需2个队伍有英雄上阵")
	}
	theirs, err := c.oppTeams(cx.b, *foe)
	if err != nil {
		return err
	}
	cost := caCfg.pairs("arena_ticketcost")
	spendA, spendI := map[int64]int64{}, map[int64]int64{}
	for _, it := range cost {
		splitGains([]sellGain{{it[0], it[1]}}, 1, spendA, spendI)
	}
	if err := c.st.Exchange(cx.role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return refuse("挑战券不足")
		}
		return err
	}
	rng := c.rng()
	plan, err := c.play(cx, mine, theirs, *foe, rng)
	if err != nil {
		return err
	}
	opp := *foe
	// the score
	me := cx.me
	oldRank := cx.b.rankOf(cx.role.RID)
	var get, foeLose int64
	if plan.won {
		get = arenaGain(me.Score, opp.Score)
		me.Score += get
		me.Wins++
	} else {
		me.Loses++
	}
	me.Fought = true
	me.Used++
	me.Fights++
	if !opp.Robot {
		if fr := cx.b.Roles[opp.RID]; fr != nil && plan.won {
			foeLose = max(1, get/2)
			foeLose = min(foeLose, max(0, fr.Score-caMinScore))
			fr.Score -= foeLose
			c.addLog(cx, fr, 2, 2, plan, opp)
		}
	}
	newRank := cx.b.rankOf(cx.role.RID)
	if newRank > 0 && (me.Best == 0 || newRank < me.Best) {
		me.Best = newRank
	}
	result := int64(2)
	if plan.won {
		result = 1
	}
	c.addLog(cx, me, 1, result, plan, opp)
	me.Logs[0].Mine.Rank = oldRank
	me.Cards = nil
	if plan.won {
		order := rng.Perm(len(caCardRewards))
		for _, i := range order {
			me.Cards = append(me.Cards, caCard{Reward: caCardRewards[i]})
		}
	}
	result25611 := proto.Message{"result": result, "score": me.Score, "get_score": get, "tar_name": opp.Name, "tar_lev": opp.Lev, "tar_face": opp.Face,
		"face_update_time": 0, "face_file": "", "tar_score": opp.Score, "lose_score": foeLose, "all_hurt_statistics": plan.hurts}
	if err := s.Send(25606, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if err := c.st.SaveGlobal(cx.role.SrvID, caModule, cx.b); err != nil {
		return err
	}
	settle := func(s *gateway.Session, _ bool) error {
		if err := s.Send(25611, result25611); err != nil {
			return err
		}
		return nil
	}
	// the first team that really fought is played for the role; a fight settled without one is told at once
	for i := range plan.rounds {
		if plan.rounds[i] != nil && !(me.Auto && me.Fights > caCfg.num("arena_skip_count")) {
			return fightServiceOf(c.st).begin(s, cx.role, caCombat, plan.intros[i], plan.rounds[i], plan.duels[i].Ret == 1, settle)
		}
	}
	return settle(s, plan.won)
}

// addLog keeps a fight in the log of a role: the role is who looks at it, Type 1 it attacked, 2 it was attacked.
func (c *caSvc) addLog(cx *caCtx, r *caRole, typ, ret int64, plan *caDuels, foe caOpp) {
	r.NextLog++
	l := caLog{ID: r.NextLog, Type: typ, Ret: ret, Wins: plan.wins, Loses: plan.loses, Time: cx.now.Unix(), Duels: plan.duels}
	if typ == 1 {
		l.Mine = caSide{RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: r.Lev, Face: r.Face, Score: r.Score}
		l.Foe = caSide{RID: foe.RID, SrvID: foe.SrvID, Name: foe.Name, Lev: foe.Lev, Face: foe.Face, Score: foe.Score}
	} else {
		a := cx.me
		l.Mine = caSide{RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: r.Lev, Face: r.Face, Score: r.Score}
		l.Foe = caSide{RID: a.RID, SrvID: a.SrvID, Name: a.Name, Lev: a.Lev, Face: a.Face, Score: a.Score}
	}
	r.Logs = append([]caLog{l}, r.Logs...)
	r.Logs = r.Logs[:min(len(r.Logs), caLogKeep)]
}
