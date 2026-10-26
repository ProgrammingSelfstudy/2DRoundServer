package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// arenamany.json: the holiday multi-player arena of the client (tools/extract_arenamany.py).
//
//go:embed data/arenamany.json
var arenamanyJSON []byte

const (
	amModule    = "arenamany"
	amFormKind  = 38 // PartnerConst.Fun_Form.ArenaManyPeople
	combatAMP   = 42 // BattleConst.Fight_Type.AreanManyPeople
	amSize      = 3
	amLogKeep   = 30
	amInitScore = 1000
)

type amConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Levels []struct {
		Lev  int64  `json:"lev"`
		Name string `json:"name"`
		Need int64  `json:"need"`
		Base int64  `json:"base"`
	} `json:"levels"`
	Awards []struct {
		Lev   int64      `json:"lev"`
		Award [][2]int64 `json:"award"`
	} `json:"awards"`
	Ranks []struct {
		Min   int64      `json:"min"`
		Max   int64      `json:"max"`
		Items [][2]int64 `json:"items"`
	} `json:"ranks"`
	Pay []struct {
		Count  int64    `json:"count"`
		Expend [2]int64 `json:"expend"`
	} `json:"pay"`
}

var amCfg = func() *amConf {
	c := &amConf{}
	if err := json.Unmarshal(arenamanyJSON, c); err != nil || len(c.Levels) == 0 || len(c.Awards) == 0 || len(c.Pay) == 0 {
		panic("game: bad arenamany.json")
	}
	return c
}()

func (c *amConf) num(key string) int64 {
	var n float64
	_ = json.Unmarshal(c.Const[key], &n)
	return int64(n)
}

// amMinLev is the level a role needs for the arena (a variable for the tests).
var amMinLev = amCfg.num("team_open_lev")

// amLev is the score level (1-12) of a score.
func amLev(score int64) int64 {
	lev := int64(1)
	for _, l := range amCfg.Levels {
		if score >= l.Need {
			lev = l.Lev
		}
	}
	return lev
}

// amLog is one fight of the role: A is the role's team, B the other.
type amLog struct {
	ID, Ret, Wins, Loses, Time int64
	A, B                       atSnap
	AOldRank, ANewRank         int64
	AOldScore, ANewScore       int64
	BRank, BScore              int64
	Duels                      []atDuel
}

// amMatch is the pair of teams matched for the next fight.
type amMatch struct {
	Atk, Def []atMember
	Refs     int64
	End      int64
	Entered  bool
}

// amRole is what a role keeps in the arena.
type amRole struct {
	RID         int64
	SrvID, Name string
	Season      int64
	Score       int64
	Wins, Loses int64
	Do          int64 // fights made in the season; one is needed to be ranked
	Day         int64
	Used, Buy   int64
	Partner     int64 // the invited teammate (a role, or a robot of the recommended list); 0 alone
	Leader      bool
	Order       []int64
	Claimed     []int64
	Red         bool
	Logs        []amLog
	NextLog     int64
	Match       *amMatch
}

type amInv struct{ From, To, Time int64 }

type amBoard struct {
	Roles   map[int64]*amRole
	Invites []amInv
	Settled int64
}

type amSvc struct {
	st  *store.Store
	at  *atSvc // its helpers for lineups, robots and fights
	mu  sync.Mutex
	rng func() *rand.Rand
}

// RegisterArenamany registers the multi-player arena (29000-29035), a holiday event: a role fights with a teammate it
// invited (the third, or both, are robots of ours as the original draws them from the other players of the server),
// against a team of three matched by score, three duels with the first to two wins; the score moves the score level,
// the level boxes are taken at once and the rank rewards are mailed when the weekly season turns. Our rules where the
// client does not say, not captured. hub may be nil (no pushes).
func RegisterArenamany(r *gateway.Router, db *store.Store, hub *gateway.Hub) {
	a := &amSvc{st: db, at: &atSvc{st: db, hub: hub}}
	fights := fightServiceOf(db)
	a.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	for cmd, h := range map[uint16]gateway.Handler{
		29000: a.wrap(29000, false, a.main), 29001: a.wrap(29001, false, a.searchPlayer), 29002: a.wrap(29002, true, a.invite),
		29003: a.wrap(29003, false, a.inviteIn), 29004: a.wrap(29004, true, a.answer), 29005: a.wrap(29005, true, a.clearIn),
		29006: a.wrap(29006, true, a.leave), 29007: a.wrap(29007, true, a.kick), 29008: a.wrap(29008, false, a.recommend),
		29009: a.wrap(29009, false, a.friends), 29010: a.wrap(29010, false, a.inviteOut), 29016: a.wrap(29016, true, a.match),
		29017: a.wrap(29017, true, a.rematch), 29018: a.wrap(29018, false, a.enter), 29019: a.wrap(29019, true, a.fight),
		29021: a.wrap(29021, false, a.mine), 29022: a.wrap(29022, false, a.saveOrder), 29025: a.wrap(29025, false, a.ranking),
		29026: a.wrap(29026, false, a.reports), 29027: a.wrap(29027, false, a.report), 29028: a.wrap(29028, false, a.redPoint),
		29030: a.wrap(29030, true, a.buyCount), 29031: a.wrap(29031, true, a.box), 29035: a.wrap(29035, false, a.nothing),
	} {
		r.Handle(cmd, h)
	}
}

type amCtx struct {
	role *store.Role
	now  time.Time
	b    *amBoard
	me   *amRole // nil when the role is below the level
	low  string
}

// wrap runs a handler on the board under the lock and saves the board afterwards. A command of the code/msg kind is
// refused below the level; the others get nothing.
func (a *amSvc) wrap(cmd uint16, refuse bool, fn func(s *gateway.Session, c *amCtx, req proto.Message) error) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		role, err := a.st.RoleByID(s.Role().RID)
		if err != nil {
			return err
		}
		now := s.Now()
		b, err := a.board(role.SrvID, now)
		if err != nil {
			return err
		}
		c := &amCtx{role: role, now: now, b: b}
		if int64(role.Lev) < amMinLev {
			c.low = itoa64(amMinLev) + "级开启多人竞技场"
			if refuse {
				return s.Send(cmd, proto.Message{"code": 0, "msg": c.low})
			}
			return nil
		}
		if _, err := a.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
			return err
		}
		c.me = b.touch(role, now)
		if err := fn(s, c, req); err != nil {
			return err
		}
		return a.st.SaveGlobal(role.SrvID, amModule, b)
	}
}

func amSeason(now time.Time) (key, end int64) {
	k, start, _ := arenaSeason(now)
	return k, start + 7*86400
}

// board reads the roles; a season that has ended is settled (mails) and the roles start the new one over.
func (a *amSvc) board(srv string, now time.Time) (*amBoard, error) {
	b := &amBoard{}
	if _, err := a.st.LoadGlobal(srv, amModule, b); err != nil {
		return nil, err
	}
	if b.Roles == nil {
		b.Roles = map[int64]*amRole{}
	}
	key, _ := amSeason(now)
	var old []*amRole
	for _, r := range b.Roles {
		if r.Season != key {
			old = append(old, r)
		}
	}
	if len(old) > 0 {
		if err := a.settle(srv, old); err != nil {
			return nil, err
		}
		for _, r := range old {
			r.Season, r.Score, r.Wins, r.Loses, r.Do, r.Claimed, r.Logs, r.Match, r.Red = key, amInitScore, 0, 0, 0, nil, nil, nil, false
		}
		if err := a.st.SaveGlobal(srv, amModule, b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// touch gets the role's standing, new for a role never seen, with the day's counters brought up to date.
func (b *amBoard) touch(role *store.Role, now time.Time) *amRole {
	key, _ := amSeason(now)
	r := b.Roles[role.RID]
	if r == nil {
		r = &amRole{RID: role.RID, Season: key, Score: amInitScore}
		b.Roles[role.RID] = r
	}
	r.SrvID, r.Name = role.SrvID, role.Name
	if day := dayKey(now); r.Day != day {
		r.Day, r.Used, r.Buy = day, 0, 0
	}
	return r
}

// settle mails the rank rewards of the ranked roles and the level boxes they did not take.
func (a *amSvc) settle(srv string, roles []*amRole) error {
	ranked := amRanked(roles)
	for i, r := range ranked {
		rank := int64(i + 1)
		for _, rk := range amCfg.Ranks {
			if rank >= rk.Min && rank <= rk.Max {
				m := store.Mail{SrvID: srv, Subject: "多人竞技场排名奖励", Content: "本赛季多人竞技场你的最终排名是第" + itoa64(rank) + "名，这是排名奖励。",
					FromName: "多人竞技场", CreatedBy: "arenamany", Attachments: attachmentsOf(rk.Items)}
				if _, err := a.st.CreateMail(m, []int64{r.RID}); err != nil {
					return err
				}
			}
		}
	}
	for _, r := range roles {
		var items [][2]int64
		for _, aw := range amCfg.Awards {
			if amLev(r.Score) >= aw.Lev && !has(r.Claimed, aw.Lev) {
				items = append(items, aw.Award...)
			}
		}
		if len(items) > 0 {
			m := store.Mail{SrvID: srv, Subject: "多人竞技场段位奖励", Content: "本赛季多人竞技场你还有段位宝箱没有领取，这是补发的奖励。",
				FromName: "多人竞技场", CreatedBy: "arenamany", Attachments: attachmentsOf(items)}
			if _, err := a.st.CreateMail(m, []int64{r.RID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// amRanked orders the roles that fought, the higher score first.
func amRanked(roles []*amRole) []*amRole {
	var out []*amRole
	for _, r := range roles {
		if r.Do > 0 {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].RID < out[j].RID
	})
	return out
}

func (b *amBoard) ranked() []*amRole {
	var all []*amRole
	for _, r := range b.Roles {
		all = append(all, r)
	}
	return amRanked(all)
}

func (b *amBoard) rankOf(r *amRole) int64 {
	for i, x := range b.ranked() {
		if x.RID == r.RID {
			return int64(i + 1)
		}
	}
	return 0
}

// left is the challenges the role has today.
func (r *amRole) left() int64 {
	return max(0, amCfg.num("team_battle_free")+r.Buy-r.Used)
}

// ---- who is who

// live is a member of a party as shown: a robot partner is made again from the leader's line-up.
func (a *amSvc) live(c *amCtx, rid int64, leader *store.Role) (atLive, bool) {
	if rid >= arenaRobotBase {
		if leader != nil {
			for idx := int64(0); idx < 10; idx++ {
				if m := a.at.robotFor(leader, idx); m.RID == rid {
					return m.asLive(), true
				}
			}
		}
		return atLive{}, false
	}
	who, err := a.st.RoleByID(rid)
	if err != nil {
		return atLive{}, false
	}
	return a.roleLive(who), true
}

func (a *amSvc) roleLive(r *store.Role) atLive {
	l := atLive{atMember: atMember{RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: int64(r.Lev)}}
	l.Face = int64(r.FaceID)
	if l.Face == 0 {
		l.Face = store.DefaultFaceID
	}
	if heroes, form, power, err := lineupOfKind(a.st, r.RID, amFormKind); err == nil {
		l.Heroes, l.Form, l.Power = heroes, form, power
	}
	return l
}

// party is the role and the teammate it invited, the leader first.
func (a *amSvc) party(c *amCtx, r *amRole, role *store.Role) []atLive {
	me := a.roleLive(role)
	if r.Partner == 0 {
		return []atLive{me}
	}
	leader := role
	if !r.Leader {
		if lr, err := a.st.RoleByID(r.Partner); err == nil {
			leader = lr
		}
	}
	mate, ok := a.live(c, r.Partner, leader)
	if !ok {
		return []atLive{me}
	}
	if r.Leader {
		me.Leader, me.Pos, mate.Pos = true, 1, 2
		return []atLive{me, mate}
	}
	mate.Leader, me.Pos, mate.Pos = true, 2, 1
	return []atLive{mate, me}
}

func (a *amSvc) scoreOf(c *amCtx, rid int64) (score, rank int64) {
	if r := c.b.Roles[rid]; r != nil {
		return r.Score, c.b.rankOf(r)
	}
	return amInitScore, 0
}

// shown is a member as the lists and the match show it (the heroes only matter in the match).
func (a *amSvc) shown(c *amCtx, l atLive, score, rank int64) proto.Message {
	infos := []proto.Message{}
	for _, h := range l.Heroes {
		infos = append(infos, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "use_skin": 0, "resonate_lev": 0})
	}
	return proto.Message{"rid": l.RID, "sid": l.SrvID, "name": l.Name, "face_id": l.Face, "power": l.Power, "avatar_bid": 0, "lev": l.Lev,
		"pos": l.Pos, "is_leader": atBool(l.Leader), "rank": rank, "score": score, "score_lev": amLev(score), "face_update_time": 0, "face_file": "",
		"is_online": a.at.online(l.RID, l.Robot), "formation_type": l.Form, "hallows_id": 0, "hallows_look_id": 0, "sprite_lev": 0,
		"sprites": []proto.Message{}, "partner_infos": infos}
}

func (a *amSvc) shownReal(c *amCtx, l atLive) proto.Message {
	score, rank := a.scoreOf(c, l.RID)
	if l.Robot {
		score, rank = amInitScore, 0
	}
	return a.shown(c, l, score, rank)
}

func (a *amSvc) shownAll(c *amCtx, ls []atLive) []proto.Message {
	out := []proto.Message{}
	for _, l := range ls {
		out = append(out, a.shownReal(c, l))
	}
	return out
}

// inParty is whether a role is in a party (it has a teammate).
func (b *amBoard) inParty(rid int64) bool {
	r := b.Roles[rid]
	return r != nil && r.Partner != 0
}

func (b *amBoard) dropInvites(rid int64) {
	keep := b.Invites[:0]
	for _, i := range b.Invites {
		if i.From != rid && i.To != rid {
			keep = append(keep, i)
		}
	}
	b.Invites = keep
}

// ---- main page

func (a *amSvc) mainMessage(c *amCtx, r *amRole, role *store.Role) proto.Message {
	awards := []proto.Message{}
	for _, aw := range amCfg.Awards {
		st := int64(0)
		switch {
		case has(r.Claimed, aw.Lev):
			st = 2
		case amLev(r.Score) >= aw.Lev:
			st = 1
		}
		awards = append(awards, proto.Message{"award_id": aw.Lev, "status": st})
	}
	_, end := amSeason(c.now)
	return proto.Message{"end_time": end, "state": 1, "count": r.left(), "buy_count": r.Buy,
		"team_members": a.shownAll(c, a.party(c, r, role)), "award_list": awards, "team_count": r.left()}
}

// pushMain sends a role the main page again (the party changed).
func (a *amSvc) pushMain(c *amCtx, rid int64) {
	r := c.b.Roles[rid]
	if r == nil || rid >= arenaRobotBase {
		return
	}
	role, err := a.st.RoleByID(rid)
	if err != nil {
		return
	}
	a.at.send(rid, 29000, a.mainMessage(c, r, role))
}

func (a *amSvc) main(s *gateway.Session, c *amCtx, _ proto.Message) error {
	return s.Send(29000, a.mainMessage(c, c.me, c.role))
}

func (a *amSvc) nothing(s *gateway.Session, c *amCtx, _ proto.Message) error { return nil }

func (a *amSvc) redPoint(s *gateway.Session, c *amCtx, _ proto.Message) error {
	return s.Send(29028, proto.Message{"point": atBool(c.me.Red)})
}

func (a *amSvc) buyCount(s *gateway.Session, c *amCtx, _ proto.Message) error {
	reply := func(code int64, msg string) error {
		return s.Send(29030, proto.Message{"code": code, "msg": msg, "count": c.me.left(), "buy_count": c.me.Buy})
	}
	if int(c.me.Buy) >= len(amCfg.Pay) {
		return reply(0, "已到达今日可购买次数上限")
	}
	cost := amCfg.Pay[c.me.Buy].Expend
	if err := a.st.Exchange(c.role.RID, map[int64]int64{cost[0]: cost[1]}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
		return reply(0, "钻石不足")
	}
	c.me.Buy++
	if err := sendAssets(s, a.st, c.role.RID); err != nil {
		return err
	}
	return reply(1, "购买成功")
}

func (a *amSvc) box(s *gateway.Session, c *amCtx, req proto.Message) error {
	id := req.Int("id")
	reply := func(code int64, msg string) error {
		m := a.mainMessage(c, c.me, c.role)
		return s.Send(29031, proto.Message{"code": code, "msg": msg, "id": id, "award_list": m["award_list"]})
	}
	for _, aw := range amCfg.Awards {
		if aw.Lev != id {
			continue
		}
		switch {
		case amLev(c.me.Score) < id:
			return reply(0, "段位不足")
		case has(c.me.Claimed, id):
			return reply(0, "已经领取过了")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(aw.Award), 1, ga, gi)
		if err := a.st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		c.me.Claimed = append(c.me.Claimed, id)
		if err := sendAssets(s, a.st, c.role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, a.st); err != nil {
			return err
		}
		return reply(1, "")
	}
	return reply(0, "没有这个宝箱")
}

// ---- teaming up

func (a *amSvc) candidate(c *amCtx, r store.Role) bool {
	return r.RID != c.role.RID && int64(r.Lev) >= amMinLev && !c.b.inParty(r.RID) && r.RID != c.me.Partner
}

func (a *amSvc) recommend(s *gateway.Session, c *amCtx, _ proto.Message) error {
	out := []proto.Message{}
	if c.me.Partner == 0 {
		rs, err := a.st.RecommendFriends(c.role.SrvID, c.role.RID, 40)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if len(out) == 4 {
				break
			}
			if a.candidate(c, r) {
				out = append(out, a.shownReal(c, a.roleLive(&r)))
			}
		}
		for idx := int64(0); idx < 3; idx++ {
			out = append(out, a.shownReal(c, a.at.robotFor(c.role, idx).asLive()))
		}
	}
	return s.Send(29008, proto.Message{"team_members": out})
}

func (a *amSvc) friends(s *gateway.Session, c *amCtx, _ proto.Message) error {
	out := []proto.Message{}
	if c.me.Partner == 0 {
		rs, err := a.st.Friends(c.role.RID)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if a.candidate(c, r) {
				out = append(out, a.shownReal(c, a.roleLive(&r)))
			}
		}
	}
	return s.Send(29009, proto.Message{"team_members": out})
}

func (a *amSvc) searchPlayer(s *gateway.Session, c *amCtx, req proto.Message) error {
	out := []proto.Message{}
	if q := strings.TrimSpace(req.Str("name")); q != "" {
		rs, err := a.st.SearchRolesByName(c.role.SrvID, q, 10)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if a.candidate(c, r) {
				out = append(out, a.shownReal(c, a.roleLive(&r)))
			}
		}
	}
	return s.Send(29001, proto.Message{"team_members": out})
}

func (a *amSvc) inviteOut(s *gateway.Session, c *amCtx, _ proto.Message) error {
	out := []proto.Message{}
	for _, i := range c.b.Invites {
		if i.From == c.role.RID {
			if who, err := a.st.RoleByID(i.To); err == nil {
				out = append(out, proto.Message{"rid": i.To, "sid": who.SrvID})
			}
		}
	}
	return s.Send(29010, proto.Message{"invite_list": out})
}

func (a *amSvc) inviteIn(s *gateway.Session, c *amCtx, _ proto.Message) error {
	out := []proto.Message{}
	for _, i := range c.b.Invites {
		if i.To != c.role.RID {
			continue
		}
		if from, err := a.st.RoleByID(i.From); err == nil {
			l := a.roleLive(from)
			l.Leader = true
			out = append(out, a.shownReal(c, l))
		}
	}
	return s.Send(29003, proto.Message{"team_members": out})
}

func (a *amSvc) locked(c *amCtx) string {
	if c.me.Match != nil {
		return "队伍匹配或挑战中，不能变动"
	}
	return ""
}

func (a *amSvc) invite(s *gateway.Session, c *amCtx, req proto.Message) error {
	rid := req.Int("rid")
	reply := func(code int64, msg string) error {
		return s.Send(29002, proto.Message{"code": code, "msg": msg, "rid": rid, "srv_id": req.Str("srv_id")})
	}
	switch {
	case c.me.Partner != 0:
		return reply(0, "你已经有队友了")
	case rid == c.role.RID:
		return reply(0, "不能邀请自己")
	}
	if rid >= arenaRobotBase {
		if l, ok := a.live(c, rid, c.role); ok {
			c.me.Partner, c.me.Leader = l.RID, true
			c.b.dropInvites(c.role.RID)
			if err := s.Send(29002, proto.Message{"code": 1, "msg": "邀请成功，对方已加入队伍", "rid": rid, "srv_id": req.Str("srv_id")}); err != nil {
				return err
			}
			return s.Send(29000, a.mainMessage(c, c.me, c.role))
		}
		return reply(0, "该玩家不存在")
	}
	who, err := a.st.RoleByID(rid)
	if err != nil {
		return reply(0, "该玩家不存在")
	}
	switch {
	case int64(who.Lev) < amMinLev:
		return reply(0, "对方等级不足")
	case c.b.inParty(rid):
		return reply(0, "对方已经有队伍了")
	}
	for _, i := range c.b.Invites {
		if i.From == c.role.RID && i.To == rid {
			return reply(0, "已经邀请过了")
		}
	}
	c.b.Invites = append(c.b.Invites, amInv{c.role.RID, rid, c.now.Unix()})
	a.at.send(rid, 29003, proto.Message{"team_members": a.invitesOf(c, rid)})
	return reply(1, "邀请已发送")
}

func (a *amSvc) invitesOf(c *amCtx, rid int64) []proto.Message {
	out := []proto.Message{}
	for _, i := range c.b.Invites {
		if i.To == rid {
			if from, err := a.st.RoleByID(i.From); err == nil {
				l := a.roleLive(from)
				l.Leader = true
				out = append(out, a.shownReal(c, l))
			}
		}
	}
	return out
}

func (a *amSvc) answer(s *gateway.Session, c *amCtx, req proto.Message) error {
	from := req.Int("rid")
	found := false
	for _, i := range c.b.Invites {
		found = found || (i.From == from && i.To == c.role.RID)
	}
	reply := func(code int64, msg string) error { return s.Send(29004, proto.Message{"code": code, "msg": msg}) }
	drop := func() {
		keep := c.b.Invites[:0]
		for _, i := range c.b.Invites {
			if !(i.From == from && i.To == c.role.RID) {
				keep = append(keep, i)
			}
		}
		c.b.Invites = keep
	}
	if !found {
		return reply(0, "邀请已失效")
	}
	if req.Int("type") != 1 {
		drop()
		return reply(1, "已拒绝")
	}
	leader := c.b.Roles[from]
	switch {
	case c.me.Partner != 0:
		drop()
		return reply(0, "你已经有队伍了")
	case leader == nil || leader.Partner != 0:
		drop()
		return reply(0, "对方已经有队友了")
	}
	leader.Partner, leader.Leader = c.role.RID, true
	c.me.Partner, c.me.Leader = from, false
	c.b.dropInvites(from)
	c.b.dropInvites(c.role.RID)
	if err := reply(1, "加入队伍成功"); err != nil {
		return err
	}
	a.pushMain(c, from)
	return s.Send(29000, a.mainMessage(c, c.me, c.role))
}

func (a *amSvc) clearIn(s *gateway.Session, c *amCtx, _ proto.Message) error {
	keep := c.b.Invites[:0]
	for _, i := range c.b.Invites {
		if i.To != c.role.RID {
			keep = append(keep, i)
		}
	}
	c.b.Invites = keep
	return s.Send(29005, proto.Message{"code": 1, "msg": "已清空"})
}

// dissolve ends a party: both sides are alone again.
func (a *amSvc) dissolve(c *amCtx, r *amRole) {
	mate := r.Partner
	r.Partner, r.Leader = 0, false
	if m := c.b.Roles[mate]; m != nil && mate < arenaRobotBase {
		m.Partner, m.Leader = 0, false
		a.pushMain(c, mate)
	}
}

func (a *amSvc) leave(s *gateway.Session, c *amCtx, _ proto.Message) error {
	switch {
	case c.me.Partner == 0:
		return s.Send(29006, proto.Message{"code": 0, "msg": "你还没有队友"})
	case a.locked(c) != "":
		return s.Send(29006, proto.Message{"code": 0, "msg": a.locked(c)})
	}
	a.dissolve(c, c.me)
	if err := s.Send(29006, proto.Message{"code": 1, "msg": "已退出队伍"}); err != nil {
		return err
	}
	return s.Send(29000, a.mainMessage(c, c.me, c.role))
}

func (a *amSvc) kick(s *gateway.Session, c *amCtx, req proto.Message) error {
	switch {
	case c.me.Partner == 0 || c.me.Partner != req.Int("rid"):
		return s.Send(29007, proto.Message{"code": 0, "msg": "他不在你的队伍里"})
	case !c.me.Leader:
		return s.Send(29007, proto.Message{"code": 0, "msg": "只有队长可进行该操作"})
	case a.locked(c) != "":
		return s.Send(29007, proto.Message{"code": 0, "msg": a.locked(c)})
	}
	a.dissolve(c, c.me)
	if err := s.Send(29007, proto.Message{"code": 1, "msg": "已请出队伍"}); err != nil {
		return err
	}
	return s.Send(29000, a.mainMessage(c, c.me, c.role))
}

// ---- ranking, reports

func (a *amSvc) ranking(s *gateway.Session, c *amCtx, req proto.Message) error {
	from, to := max(1, req.Int("start_num")), req.Int("end_num")
	if to == 0 || to > 100 {
		to = 100
	}
	list := []proto.Message{}
	for i, r := range c.b.ranked() {
		if rank := int64(i + 1); rank >= from && rank <= to {
			if who, err := a.st.RoleByID(r.RID); err == nil {
				list = append(list, a.shown(c, a.roleLive(who), r.Score, rank))
			}
		}
	}
	return s.Send(29025, proto.Message{"rank": c.b.rankOf(c.me), "team_members": list})
}

func (a *amSvc) mine(s *gateway.Session, c *amCtx, _ proto.Message) error {
	score, rank := a.scoreOf(c, c.role.RID)
	return s.Send(29021, a.shown(c, a.roleLive(c.role), score, rank))
}

func (a *amSvc) saveOrder(s *gateway.Session, c *amCtx, req proto.Message) error {
	var order []int64
	slots := req.Arr("pos_info")
	sort.Slice(slots, func(i, j int) bool { return slots[i].Int("pos") < slots[j].Int("pos") })
	for _, p := range slots {
		order = append(order, p.Int("rid"))
	}
	c.me.Order = order
	return s.Send(29022, proto.Message{})
}

func (a *amSvc) faces(sn atSnap) []proto.Message {
	out := []proto.Message{}
	for _, m := range sn.Members {
		out = append(out, proto.Message{"pos": m.Pos, "rid": m.RID, "srv_id": m.SrvID, "face_id": m.Face, "avatar_bid": 0, "is_leader": atBool(m.Leader),
			"lev": m.Lev, "face_update_time": 0, "face_file": ""})
	}
	return out
}

func (a *amSvc) reports(s *gateway.Session, c *amCtx, _ proto.Message) error {
	list := []proto.Message{}
	for _, l := range c.me.Logs {
		list = append(list, proto.Message{"id": l.ID, "is_atk": 1, "rid": l.A.Leader, "srv_id": l.A.SrvID, "atk_name": l.A.Name, "atk_face": a.faces(l.A),
			"atk_ole_rank": l.AOldRank, "atk_rank": l.ANewRank, "atk_ole_score": l.AOldScore, "atk_score": l.ANewScore,
			"def_rid": l.B.Leader, "def_srv_id": l.B.SrvID, "def_name": l.B.Name, "def_face": a.faces(l.B), "def_ole_rank": l.BRank, "def_rank": l.BRank,
			"def_ole_score": l.BScore, "def_score": l.BScore, "ret": l.Ret, "win_count": l.Wins, "lose_count": l.Loses, "time": l.Time,
			"atk_power": l.A.Power, "def_power": l.B.Power})
	}
	c.me.Red = false
	return s.Send(29026, proto.Message{"holiday_arena_team_log": list})
}

func (a *amSvc) report(s *gateway.Session, c *amCtx, req proto.Message) error {
	plist := func(hs []arenaHero) []proto.Message {
		out := []proto.Message{}
		for _, h := range hs {
			out = append(out, proto.Message{"pos": h.Pos, "id": 0, "bid": h.BID, "lev": h.Lev, "star": h.Star, "power": 0, "skin_id": 0, "resonate_lev": 0,
				"hurt": 0, "curt": 0, "be_hurt": 0})
		}
		return out
	}
	infos := []proto.Message{}
	for _, l := range c.me.Logs {
		if l.ID != req.Int("id") {
			continue
		}
		for i, d := range l.Duels {
			infos = append(infos, proto.Message{"order": i + 1, "id": d.Replay, "round": 1, "ret": d.Ret, "time": d.Time, "rid": d.ARID, "srv_id": d.ASrv,
				"a_power": d.APower, "a_formation_type": d.AForm, "a_order": d.AOrder, "a_end_hp": 0, "a_sprite_lev": 0, "a_sprites": []proto.Message{},
				"b_rid": d.BRID, "b_srv_id": d.BSrv, "b_power": d.BPower, "b_formation_type": d.BForm, "b_order": d.BOrder, "b_end_hp": 0, "b_sprite_lev": 0,
				"b_sprites": []proto.Message{}, "a_plist": plist(d.AList), "b_plist": plist(d.BList), "a_name": l.A.Name, "b_name": l.B.Name})
		}
	}
	return s.Send(29027, proto.Message{"id": req.Int("id"), "replay_infos": infos})
}

// ---- matching and fighting

// fillTeam fills the team with robots of the leader's strength up to three members.
func (a *amSvc) fillTeam(c *amCtx, mine []atLive) []atLive {
	team := append([]atLive{}, mine...)
	for idx := int64(3); len(team) < amSize && idx < 10; idx++ {
		team = append(team, a.at.robotFor(c.role, idx).asLive())
	}
	for i := range team {
		team[i].Pos = int64(i + 1)
	}
	return team
}

func toMembers(ls []atLive) []atMember {
	out := make([]atMember, len(ls))
	for i, l := range ls {
		out[i] = l.atMember
		out[i].Pos = int64(i + 1)
		out[i].Hide = false
	}
	return out
}

// drawDef is the team to fight: the role of the closest score, within the level the client allows, and robots.
func (a *amSvc) drawDef(c *amCtx, mine []atLive, party map[int64]bool) []atMember {
	rng := a.rng()
	var near []*amRole
	for _, r := range c.b.Roles {
		if party[r.RID] || r.RID == c.role.RID {
			continue
		}
		if d := amLev(r.Score) - amLev(c.me.Score); d < -amCfg.num("team_level_disparity") || d > amCfg.num("team_level_disparity") {
			continue
		}
		near = append(near, r)
	}
	sort.Slice(near, func(i, j int) bool {
		di, dj := abs64(near[i].Score-c.me.Score), abs64(near[j].Score-c.me.Score)
		if di != dj {
			return di < dj
		}
		return near[i].RID < near[j].RID
	})
	var def []atMember
	for _, r := range near[:min(len(near), 3)] {
		if who, err := a.st.RoleByID(r.RID); err == nil {
			if l := a.roleLive(who); len(l.Heroes) > 0 {
				def = append(def, l.atMember)
				break
			}
		}
	}
	rt := a.at.robotTeam(&atCtx{role: c.role, t: &atTeam{}}, mine, c.me.Score, rng)
	for _, m := range rt.Members {
		if len(def) < amSize {
			def = append(def, m)
		}
	}
	for i := range def {
		def[i].Pos = int64(i + 1)
	}
	return def
}

func (a *amSvc) matchLives(c *amCtx, ms []atMember) []atLive {
	out := make([]atLive, len(ms))
	for i, m := range ms {
		out[i] = atLive{atMember: m}
		if !m.Robot {
			if who, err := a.st.RoleByID(m.RID); err == nil {
				out[i] = a.roleLive(who)
				out[i].Pos = m.Pos
			}
		}
		out[i].Leader = i == 0 && len(ms) > 0 && ms[0].RID == c.role.RID && c.me.Leader || (i == 0 && c.me.Partner != 0 && !c.me.Leader && m.RID == c.me.Partner)
	}
	return out
}

func (a *amSvc) matchMessage(c *amCtx, m *amMatch, code int64, msg string) proto.Message {
	return proto.Message{"code": code, "msg": msg, "atk_team_members": a.shownAll(c, a.matchLives(c, m.Atk)),
		"def_team_members": a.shownAll(c, a.matchLives(c, m.Def)), "ref_count": m.Refs, "end_time": m.End, "is_enter": atBool(m.Entered)}
}

func (a *amSvc) newMatch(c *amCtx, refs int64) *amMatch {
	mine := a.fillTeam(c, a.party(c, c.me, c.role))
	party := map[int64]bool{}
	for _, l := range mine {
		party[l.RID] = true
	}
	return &amMatch{Atk: toMembers(mine), Def: a.drawDef(c, mine, party), Refs: refs, End: c.now.Unix() + amCfg.num("team_opponent_time")}
}

func (a *amSvc) match(s *gateway.Session, c *amCtx, _ proto.Message) error {
	if c.me.Match == nil {
		if c.me.left() <= 0 {
			return s.Send(29016, proto.Message{"code": 0, "msg": "挑战次数不足"})
		}
		if heroes, _, _, err := lineupOfKind(a.st, c.role.RID, amFormKind); err != nil || len(heroes) == 0 {
			return s.Send(29016, proto.Message{"code": 0, "msg": "请先布阵"})
		}
		c.me.Match = a.newMatch(c, 0)
	}
	return s.Send(29016, a.matchMessage(c, c.me.Match, 1, ""))
}

func (a *amSvc) rematch(s *gateway.Session, c *amCtx, _ proto.Message) error {
	m := c.me.Match
	switch {
	case m == nil:
		return s.Send(29017, proto.Message{"code": 0, "msg": "请先匹配对手"})
	case m.Refs >= amCfg.num("team_refresh_num"):
		return s.Send(29017, proto.Message{"code": 0, "msg": "刷新次数不足"})
	}
	c.me.Match = a.newMatch(c, m.Refs+1)
	if err := s.Send(29017, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	return s.Send(29016, a.matchMessage(c, c.me.Match, 1, ""))
}

// enter (29018): the role accepts the opponent and goes to the fight page.
func (a *amSvc) enter(s *gateway.Session, c *amCtx, _ proto.Message) error {
	if m := c.me.Match; m != nil {
		m.Entered, m.End = true, c.now.Unix()+amCfg.num("team_opponent_time")
	}
	return s.Send(29018, proto.Message{})
}

// amDelta is the score a fight moves: a win is worth the base of the level and more against a stronger team, a loss costs a
// base and more against a weaker one.
func amDelta(mine, theirs int64, win bool) int64 {
	base := int64(0)
	for _, l := range amCfg.Levels {
		if l.Lev == amLev(mine) {
			base = l.Base
		}
	}
	clamp := func(v float64) float64 { return min(1, max(-1, v)) }
	if win {
		w := clamp(float64(theirs-mine) / float64(amCfg.num("team_score_constant1")))
		return max(1, base+int64(float64(amCfg.num("team_score_constant2"))*w))
	}
	w := clamp(float64(mine-theirs) / float64(amCfg.num("team_deduction_constant1")))
	return amCfg.num("team_deduction_basics") + int64(float64(amCfg.num("team_deduction_constant2"))*max(0, w))
}

type amDuels struct {
	duels        []atDuel
	rounds       [][]fightRound
	intro        proto.Message
	hurts        []proto.Message
	wins, loses  int64
	won          bool
	mine, theirs []atLive
}

// play fights the three duels in order, mine[i] against theirs[i], until one side has two wins.
func (a *amSvc) play(mine, theirs []atLive, rng *rand.Rand) (*amDuels, error) {
	p := &amDuels{mine: mine, theirs: theirs}
	for i := 0; i < len(mine) && i < len(theirs) && p.wins < 2 && p.loses < 2; i++ {
		ms, mf, mb, err := a.at.opp(mine[i]).fighters(a.st)
		if err != nil {
			return nil, err
		}
		ts, tf, tb, err := a.at.opp(theirs[i]).fighters(a.st)
		if err != nil {
			return nil, err
		}
		mu, mo := sideUnits(mine[i].RID, mine[i].SrvID, ms, mf, mb, true)
		tu, to := sideUnits(theirs[i].RID, theirs[i].SrvID, ts, tf, tb, false)
		var rounds []fightRound
		won := len(mu) > 0
		if len(mu) > 0 && len(tu) > 0 {
			rounds, won = buildFight(mu, tu, rng)
		}
		d := atDuel{Ret: 2, ARID: mine[i].RID, ASrv: mine[i].SrvID, BRID: theirs[i].RID, BSrv: theirs[i].SrvID, AOrder: int64(i + 1), BOrder: int64(i + 1),
			APower: mine[i].Power, BPower: theirs[i].Power, AForm: mf, BForm: tf, AList: mine[i].Heroes, BList: theirs[i].Heroes}
		if won {
			d.Ret = 1
			p.wins++
		} else {
			p.loses++
		}
		if len(p.duels) == 0 {
			p.intro = proto.Message{
				"combat_type": combatAMP, "formation": []proto.Message{{"group": 1, "formation_type": mf, "formation_lev": 1}, {"group": 2, "formation_type": tf, "formation_lev": 1}},
				"objects": append(append([]proto.Message{atSpirit}, mo...), to...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
				"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(mu), "actor_role_name": mine[0].Name,
				"target_role_name": theirs[0].Name, "suppress": 0, "halo_list": []proto.Message{},
				"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
			}
		}
		p.hurts = append(p.hurts, proto.Message{"type": len(p.duels) + 1, "a_round": atBool(won), "b_round": atBool(!won),
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": a.at.hurtsOf(mine[i], ms, mb)}}})
		p.duels = append(p.duels, d)
		p.rounds = append(p.rounds, rounds)
	}
	p.won = p.wins > p.loses
	return p, nil
}

func (a *amSvc) snap(c *amCtx, ls []atLive, score, newScore, rank, newRank int64) atSnap {
	var leader int64
	if len(ls) > 0 {
		leader = ls[0].RID
	}
	sn := atSnap{Leader: leader, SrvID: c.role.SrvID, Power: teamPower(ls), Score: score, NewScore: newScore, Rank: rank, NewRank: newRank}
	if len(ls) > 0 {
		sn.Name = ls[0].Name
	}
	for _, l := range ls {
		sn.Members = append(sn.Members, atSnapMember{RID: l.RID, Lev: l.Lev, Power: l.Power, Pos: l.Pos, Face: l.Face, Leader: l.Leader, SrvID: l.SrvID,
			Name: l.Name, Heroes: l.Heroes})
	}
	return sn
}

// fight (29019): the role fixes the order of its team and the fight is made; the first duel is played for it, then the
// result (29020) comes.
func (a *amSvc) fight(s *gateway.Session, c *amCtx, req proto.Message) error {
	refuse := func(msg string) error { return s.Send(29019, proto.Message{"code": 0, "msg": msg}) }
	m := c.me.Match
	if m == nil {
		return refuse("请先匹配对手")
	}
	if c.me.left() <= 0 {
		return refuse("挑战次数不足")
	}
	atk := a.matchLives(c, m.Atk)
	byRID := map[int64]atLive{}
	for _, l := range atk {
		byRID[l.RID] = l
	}
	var mine []atLive
	seen := map[int64]bool{}
	slots := req.Arr("pos_info")
	sort.Slice(slots, func(i, j int) bool { return slots[i].Int("pos") < slots[j].Int("pos") })
	for _, p := range slots {
		if l, ok := byRID[p.Int("rid")]; ok && !seen[l.RID] {
			seen[l.RID] = true
			mine = append(mine, l)
		}
	}
	for _, l := range atk { // members left out keep their place at the end
		if !seen[l.RID] {
			mine = append(mine, l)
		}
	}
	for i := range mine {
		mine[i].Pos = int64(i + 1)
	}
	theirs := a.matchLives(c, m.Def)
	plan, err := a.play(mine, theirs, a.rng())
	if err != nil {
		return err
	}
	if len(plan.duels) == 0 {
		return refuse("队伍没有可以出战的英雄")
	}
	for i := range plan.duels {
		d := &plan.duels[i]
		d.Time = c.now.Unix()
		d.Replay = recordReplay(a.st, combatAMP, replaySide{RID: d.ARID, SrvID: d.ASrv, Name: mine[0].Name, Lev: mine[0].Lev, Face: mine[0].Face, Power: d.APower, Form: d.AForm},
			replaySide{RID: d.BRID, SrvID: d.BSrv, Name: theirs[0].Name, Lev: theirs[0].Lev, Face: theirs[0].Face, Power: d.BPower, Form: d.BForm},
			plan.intro, plan.rounds[i], d.Ret == 1, c.now.Unix())
		plan.hurts[i]["replay_id"], plan.hurts[i]["a_name"], plan.hurts[i]["b_name"], plan.hurts[i]["ret"] = d.Replay, mine[0].Name, theirs[0].Name, d.Ret
	}
	c.me.Used++
	c.me.Match = nil
	result := int64(2)
	if plan.won {
		result = 1
	}
	var foeScore int64
	for _, l := range theirs {
		sc, _ := a.scoreOf(c, l.RID)
		if l.Robot {
			sc = c.me.Score
		}
		foeScore += sc
	}
	foeScore /= int64(len(theirs))
	// the roles of the team that fight for real (not the matched robots) move their score and keep the log
	var my29020 proto.Message
	for _, l := range mine {
		if l.Robot {
			continue
		}
		r := c.b.Roles[l.RID]
		if r == nil {
			continue
		}
		oldScore, oldRank := r.Score, c.b.rankOf(r)
		if plan.won {
			r.Score += amDelta(oldScore, foeScore, true)
			r.Wins++
		} else {
			r.Score = max(amInitScore, r.Score-amDelta(oldScore, foeScore, false))
			r.Loses++
		}
		r.Do++
		newRank := c.b.rankOf(r)
		r.NextLog++
		r.Logs = append([]amLog{{ID: r.NextLog, Ret: result, Wins: plan.wins, Loses: plan.loses, Time: c.now.Unix(),
			A: a.snap(c, mine, oldScore, r.Score, oldRank, newRank), B: a.snap(c, theirs, foeScore, foeScore, 0, 0), AOldRank: oldRank, ANewRank: newRank,
			AOldScore: oldScore, ANewScore: r.Score, BScore: foeScore, Duels: plan.duels}}, r.Logs...)
		r.Logs = r.Logs[:min(len(r.Logs), amLogKeep)]
		if l.RID != c.role.RID {
			r.Red = true
			a.at.send(l.RID, 29028, proto.Message{"point": 1})
			continue
		}
		my29020 = proto.Message{"result": result, "win_count": plan.wins, "lose_count": plan.loses, "score_lev": amLev(oldScore), "new_score_lev": amLev(r.Score),
			"score": oldScore, "new_score": r.Score, "rank": oldRank, "new_rank": newRank, "all_hurt_statistics": plan.hurts}
	}
	if my29020 == nil {
		my29020 = proto.Message{"result": result, "win_count": plan.wins, "lose_count": plan.loses, "all_hurt_statistics": plan.hurts}
	}
	if err := s.Send(29019, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	settle := func(s *gateway.Session, _ bool) error { return s.Send(29020, my29020) }
	if err := a.st.SaveGlobal(c.role.SrvID, amModule, c.b); err != nil {
		return err
	}
	return fightServiceOf(a.st).begin(s, c.role, combatAMP, plan.intro, plan.rounds[0], plan.duels[0].Ret == 1, settle)
}
