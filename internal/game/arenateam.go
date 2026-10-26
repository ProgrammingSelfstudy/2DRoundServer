package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// arenateam.json: the team arena of the client (tools/extract_arenateam.py).
//
//go:embed data/arenateam.json
var arenateamJSON []byte

const (
	atModule     = "arenateam"
	atRoleModule = "arenateamrole"
	combatTeam   = 35 // BattleConst.Fight_Type.Arean_Team
	atTeamSize   = 3
	atLogKeep    = 30
	atRivals     = 3
	atRobotTeam  = 3000000000 // tids of robot teams start here, above any real team
	atRobotRID   = arenaRobotBase + 5000000
	atRivalRID   = arenaRobotBase + 20000000
)

// atAlways keeps the team arena in its challenge phase for tests on a device.
var atAlways = os.Getenv("ARENATEAM_ALWAYS_OPEN") == "1"

type atConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Faces  []int64                    `json:"faces"`
	Levels []int64                    `json:"levels"`
	Powers []int64                    `json:"powers"`
	Boxes  []struct {
		ID    int64      `json:"id"`
		Count int64      `json:"count"`
		Award [][2]int64 `json:"award"`
	} `json:"boxes"`
	Ranks []struct {
		Min   int64      `json:"min"`
		Max   int64      `json:"max"`
		Items [][2]int64 `json:"items"`
	} `json:"ranks"`
}

var atCfg = func() *atConf {
	c := &atConf{}
	if err := json.Unmarshal(arenateamJSON, c); err != nil || len(c.Boxes) == 0 || len(c.Ranks) == 0 || len(c.Faces) == 0 {
		panic("game: bad arenateam.json")
	}
	return c
}()

func (c *atConf) num(key string) int64 {
	var n float64
	_ = json.Unmarshal(c.Const[key], &n)
	return int64(n)
}

func (c *atConf) list(key string) []int64 {
	var l []int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

// atMinLev is the level a role needs for the team arena (a variable for the tests).
var atMinLev = atCfg.num("open_lev")

// atMember is a member of a team. A real member's name, level and line-up are read from the role when shown; a robot
// keeps its own.
type atMember struct {
	RID    int64
	SrvID  string
	Name   string
	Lev    int64
	Face   int64
	Pos    int64
	Hide   bool
	Robot  bool
	Form   int64
	Power  int64
	Heroes []arenaHero
}

type atRival struct {
	TID   int64
	Robot bool
	Team  *atTeam // a robot team is kept whole, a real one is read again
}

// atSnap is a team as a fight log keeps it.
type atSnap struct {
	TID                                   int64
	Leader                                int64
	SrvID, Name                           string
	Power, Score, NewScore, Rank, NewRank int64
	Members                               []atSnapMember
}

type atSnapMember struct {
	RID, Lev, Power, Pos, Face int64
	Hide, Leader               bool
	SrvID, Name                string
	Heroes                     []arenaHero
}

// atDuel is one fight of a challenge.
type atDuel struct {
	Replay, Ret, Time, AOrder, BOrder, APower, BPower, AForm, BForm int64
	ARID, BRID                                                      int64
	ASrv, BSrv                                                      string
	AList, BList                                                    []arenaHero
}

// atLog is one challenge: A attacked B; Ret 1 is a win of A.
type atLog struct {
	ID, Ret, Wins, Loses, Time int64
	A, B                       atSnap
	Duels                      []atDuel
}

type atTeam struct {
	ID, Season, Leader, Created int64
	SrvID, Name                 string
	LimitLev, LimitPower, Check int64
	Members                     []atMember
	Signed                      bool
	Score, Wins, Loses          int64
	Count, AddTime, Do          int64 // the challenges left, when the next one comes, the ones made
	Rivals                      []atRival
	RivalAt                     int64
	Logs                        []atLog
	NextLog                     int64
}

type atReq struct{ TID, RID, Time int64 }

type atBoard struct {
	Next    int64
	Teams   map[int64]*atTeam
	Applies []atReq // a role asks a team
	Invites []atReq // a team asks a role
	Settled int64   // season whose rewards were sent
}

// atRole is what a role keeps: the boxes taken of the season and the news of fights it has not read.
type atRole struct {
	Season  int64
	Claimed []int64
	Red     bool
}

type atSvc struct {
	st  *store.Store
	hub *gateway.Hub
	mu  sync.Mutex
	rng func() *rand.Rand
}

// RegisterArenateam registers the team arena (27200-27256): teams of three roles of level 70, formed from Friday noon,
// signed up by the leader, fighting from Saturday to Sunday 23:00 against other teams in a wheel of duels, ranked by
// score; the rewards go out by mail at 23:40 on Sunday. A robot can stand in for a teammate (invite one from the
// recommended list) and for the teams on the challenge list, as there is only one server and few players. Our rules
// where the client does not say, not captured. hub may be nil (no pushes).
func RegisterArenateam(r *gateway.Router, db *store.Store, hub *gateway.Hub) {
	a := &atSvc{st: db, hub: hub}
	fights := fightServiceOf(db)
	a.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	for cmd, h := range map[uint16]gateway.Handler{
		27200: a.hall, 27201: a.create, 27202: a.apply, 27203: a.applyList, 27204: a.answerApply, 27205: a.invite,
		27206: a.inviteList, 27207: a.answerInvite, 27208: a.clearInvites, 27210: a.searchTeam, 27211: a.leave,
		27212: a.kick, 27213: a.transfer, 27215: a.points, 27216: a.keyApply, 27220: a.main, 27221: a.details,
		27223: a.ranking, 27224: a.box, 27225: a.setLimits, 27226: a.rename, 27228: a.recommend, 27229: a.searchPlayer,
		27240: a.sign, 27241: a.unsign, 27242: a.saveOrder, 27243: a.members, 27250: a.refresh, 27251: a.rivalList,
		27252: a.challenge, 27255: a.reports, 27256: a.report,
	} {
		r.Handle(cmd, h)
	}
}

// atSeason gives the season of a moment (the Friday noon that began it), the state of the arena in it and the second
// the state ends: 0 closed, 1 forming teams, 2 challenging, 4 settling.
func atSeason(now time.Time) (key, state, end int64) {
	off := func(name string) int64 {
		v := atCfg.list(name)
		return (v[0]-1)*86400 + v[1]*3600 + v[2]*60
	}
	t := dayStart(now.In(dayZone))
	monday := t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
	start := monday.Unix() + off("start_team_time")
	if now.Unix() < start {
		start -= 7 * 86400
	}
	base := start - off("start_team_time")
	challenge, closeAt, settle := base+off("start_match_time"), base+off("end_time"), base+off("end_settlement_time")
	switch n := now.Unix(); {
	case atAlways:
		return start, 2, start + 7*86400
	case n < challenge:
		return start, 1, challenge
	case n < closeAt:
		return start, 2, closeAt
	case n < settle:
		return start, 4, settle
	}
	return start, 0, start + 7*86400
}

// atSettleAt is the second the rewards of a season go out.
func atSettleAt(season int64) int64 {
	v := atCfg.list("end_settlement_time")
	s := atCfg.list("start_team_time")
	return season + ((v[0]-s[0])*86400 + (v[1]-s[1])*3600 + (v[2]-s[2])*60)
}

func (a *atSvc) rngSeeded(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

// board reads the teams, sending out the rewards of a season that has ended and starting the teams over for a new one.
func (a *atSvc) board(srv string, now time.Time) (*atBoard, error) {
	b := &atBoard{}
	if _, err := a.st.LoadGlobal(srv, atModule, b); err != nil {
		return nil, err
	}
	if b.Teams == nil {
		b.Teams = map[int64]*atTeam{}
	}
	key, _, _ := atSeason(now)
	changed := false
	if !atAlways {
		for _, season := range []int64{key - 7*86400, key} {
			if b.Settled < season && now.Unix() >= atSettleAt(season) {
				if err := a.settleSeason(b, srv, season); err != nil {
					return nil, err
				}
				b.Settled, changed = season, true
			}
		}
	}
	for _, t := range b.Teams {
		if t.Season != key {
			t.Season, t.Signed, t.Score, t.Wins, t.Loses, t.Count, t.AddTime, t.Do = key, false, 0, 0, 0, 0, 0, 0
			t.Rivals, t.RivalAt, t.Logs = nil, 0, nil
			changed = true
		}
		if a.recover(t, now) {
			changed = true
		}
	}
	if changed {
		if err := a.st.SaveGlobal(srv, atModule, b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (a *atSvc) save(srv string, b *atBoard) error { return a.st.SaveGlobal(srv, atModule, b) }

// recover gives a team the challenges that came back with time.
func (a *atSvc) recover(t *atTeam, now time.Time) bool {
	if !t.Signed {
		return false
	}
	_, state, _ := atSeason(now)
	if state != 2 {
		return false
	}
	limit, every := atCfg.num("limit_number"), atCfg.num("recovery_time")
	changed := false
	if t.Count < limit && t.AddTime == 0 {
		t.AddTime, changed = now.Unix()+every, true
	}
	for t.Count < limit && t.AddTime != 0 && t.AddTime <= now.Unix() {
		t.Count++
		changed = true
		if t.Count >= limit {
			t.AddTime = 0
		} else {
			t.AddTime += every
		}
	}
	return changed
}

// settleSeason mails the rank rewards and the boxes not taken to the members of the signed teams of a season.
func (a *atSvc) settleSeason(b *atBoard, srv string, season int64) error {
	var teams []*atTeam
	for _, t := range b.Teams {
		if t.Season == season && t.Signed {
			teams = append(teams, t)
		}
	}
	sort.Slice(teams, func(i, j int) bool {
		if teams[i].Score != teams[j].Score {
			return teams[i].Score > teams[j].Score
		}
		return teams[i].ID < teams[j].ID
	})
	for i, t := range teams {
		rank := int64(i + 1)
		var rids []int64
		for _, m := range t.Members {
			if !m.Robot {
				rids = append(rids, m.RID)
			}
		}
		if len(rids) == 0 {
			continue
		}
		if items := atRankReward(rank); len(items) > 0 {
			m := store.Mail{SrvID: srv, Subject: "组队竞技场排名奖励", Content: "本赛季组队竞技场你的队伍最终排名是第" + itoa64(rank) + "名，这是排名奖励。",
				FromName: "组队竞技场", CreatedBy: "arenateam", Attachments: attachmentsOf(items)}
			if _, err := a.st.CreateMail(m, rids); err != nil {
				return err
			}
		}
		for _, rid := range rids {
			ar, err := a.roleState(rid, season)
			if err != nil {
				return err
			}
			var items [][2]int64
			for _, bx := range atCfg.Boxes {
				if t.Do >= bx.Count && !has(ar.Claimed, bx.ID) {
					items = append(items, bx.Award...)
				}
			}
			if len(items) > 0 {
				m := store.Mail{SrvID: srv, Subject: "组队竞技场挑战次数奖励", Content: "本赛季组队竞技场你还有挑战次数宝箱没有领取，这是补发的奖励。",
					FromName: "组队竞技场", CreatedBy: "arenateam", Attachments: attachmentsOf(items)}
				if _, err := a.st.CreateMail(m, []int64{rid}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func has(l []int64, v int64) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func atRankReward(rank int64) [][2]int64 {
	for _, r := range atCfg.Ranks {
		if rank >= r.Min && rank <= r.Max {
			return r.Items
		}
	}
	return nil
}

func (a *atSvc) roleState(rid, season int64) (*atRole, error) {
	ar := &atRole{}
	if _, err := a.st.LoadState(rid, atRoleModule, ar); err != nil {
		return nil, err
	}
	if ar.Season != season {
		ar.Season, ar.Claimed, ar.Red = season, nil, false
	}
	return ar, nil
}

// teamOf finds the team a role is in.
func (b *atBoard) teamOf(rid int64) *atTeam {
	for _, t := range b.Teams {
		for _, m := range t.Members {
			if m.RID == rid && !m.Robot {
				return t
			}
		}
	}
	return nil
}

func (t *atTeam) member(rid int64) (int, bool) {
	for i, m := range t.Members {
		if m.RID == rid {
			return i, true
		}
	}
	return 0, false
}

func (t *atTeam) sorted() []atMember {
	out := append([]atMember(nil), t.Members...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out
}

// renumber puts the members on the places 1..n in their order.
func (t *atTeam) renumber() {
	sort.SliceStable(t.Members, func(i, j int) bool { return t.Members[i].Pos < t.Members[j].Pos })
	for i := range t.Members {
		t.Members[i].Pos = int64(i + 1)
	}
}

func (a *atSvc) send(rid int64, cmd uint16, msg proto.Message) {
	if a.hub == nil {
		return
	}
	if sess := a.hub.Online(rid); sess != nil {
		_ = sess.Send(cmd, msg)
	}
}

// notify is the 27222 push: 1 joined a team, 2 the team changed, 3 a new request, 4 a new invitation, 5 left the team,
// 6 a fight was made.
func (a *atSvc) notify(rid, typ int64) { a.send(rid, 27222, proto.Message{"type": typ}) }

func (a *atSvc) notifyTeam(t *atTeam, typ int64, except int64) {
	for _, m := range t.Members {
		if !m.Robot && m.RID != except {
			a.notify(m.RID, typ)
		}
	}
}

func (a *atSvc) refuse(s *gateway.Session, cmd uint16, msg string) error {
	return s.Send(cmd, proto.Message{"code": 0, "msg": msg})
}

func (a *atSvc) online(rid int64, robot bool) int64 {
	if robot || (a.hub != nil && a.hub.Online(rid) != nil) {
		return 1
	}
	return 0
}

// live is a member as it is now: a real one's name, level and line-up are read again.
type atLive struct {
	atMember
	Leader bool
}

func (a *atSvc) live(t *atTeam, m atMember) atLive {
	l := atLive{atMember: m, Leader: m.RID == t.Leader && !m.Robot}
	if m.Robot {
		return l
	}
	if r, err := a.st.RoleByID(m.RID); err == nil {
		l.Name, l.Lev = r.Name, int64(r.Lev)
		l.Face = int64(r.FaceID)
		if l.Face == 0 {
			l.Face = store.DefaultFaceID
		}
	}
	if heroes, form, power, err := lineupOf(a.st, m.RID); err == nil {
		l.Heroes, l.Form, l.Power = heroes, form, power
	}
	return l
}

func (a *atSvc) lives(t *atTeam) []atLive {
	var out []atLive
	for _, m := range t.sorted() {
		out = append(out, a.live(t, m))
	}
	return out
}

func teamPower(ls []atLive) int64 {
	var p int64
	for _, l := range ls {
		p += l.Power
	}
	return p
}

func atExt(l atLive, a *atSvc) []proto.Message {
	e := []proto.Message{{"extra_key": 6, "extra_val": a.online(l.RID, l.Robot)}}
	if l.Leader {
		e = append(e, proto.Message{"extra_key": 1, "extra_val": 1})
	}
	return e
}

// brief is a member as the lists show it.
func (a *atSvc) brief(l atLive, withName bool) proto.Message {
	m := proto.Message{"rid": l.RID, "sid": l.SrvID, "face_id": l.Face, "avatar_bid": 0, "lev": l.Lev, "pos": l.Pos, "ext": atExt(l, a),
		"face_update_time": 0, "face_file": ""}
	if withName {
		m["name"] = l.Name
	}
	return m
}

// full is a member with the heroes he fights with.
func (a *atSvc) full(l atLive) proto.Message {
	ps := []proto.Message{}
	for _, h := range l.Heroes {
		ps = append(ps, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "break_lev": h.Break, "skin_id": 0, "ext": []proto.Message{}})
	}
	hide := int64(0)
	if l.Hide {
		hide = 1
	}
	return proto.Message{"rid": l.RID, "sid": l.SrvID, "name": l.Name, "lev": l.Lev, "power": l.Power, "pos": l.Pos, "is_hide": hide, "face_id": l.Face,
		"avatar_bid": 0, "team_partner": ps, "ext": atExt(l, a), "face_update_time": 0, "face_file": ""}
}

func (a *atSvc) fulls(ls []atLive) []proto.Message {
	out := []proto.Message{}
	for _, l := range ls {
		out = append(out, a.full(l))
	}
	return out
}

func (a *atSvc) briefs(ls []atLive, withName bool) []proto.Message {
	out := []proto.Message{}
	for _, l := range ls {
		out = append(out, a.brief(l, withName))
	}
	return out
}

func atBool(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// teamBrief is a team as the hall lists it.
func (a *atSvc) teamBrief(t *atTeam) proto.Message {
	ls := a.lives(t)
	return proto.Message{"tid": t.ID, "srv_id": t.SrvID, "team_name": t.Name, "team_power": teamPower(ls), "team_limit_power": t.LimitPower,
		"team_limit_lev": t.LimitLev, "team_is_check": t.Check, "team_members": a.briefs(ls, false)}
}

// rankOf is the place of a score among the signed teams of the season (the teams before it have a higher score).
func (b *atBoard) ranked() []*atTeam {
	var out []*atTeam
	for _, t := range b.Teams {
		if t.Signed {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (b *atBoard) rankOf(t *atTeam) int64 {
	for i, x := range b.ranked() {
		if x.ID == t.ID {
			return int64(i + 1)
		}
	}
	return 0
}

// rankForScore is where a robot team with a score would stand among the real ones.
func (b *atBoard) rankForScore(score int64) int64 {
	rank := int64(1)
	for _, t := range b.ranked() {
		if t.Score > score {
			rank++
		}
	}
	return rank
}

// ctx is what most handlers start from: the role, the board and the team of the role, under the lock.
type atCtx struct {
	role *store.Role
	now  time.Time
	b    *atBoard
	t    *atTeam
}

// begin locks the service; the caller unlocks. It answers with msg set when the role cannot take part.
func (a *atSvc) begin(s *gateway.Session) (*atCtx, string, error) {
	if s.Role() == nil {
		return nil, "", errors.New("no role")
	}
	role, err := a.st.RoleByID(s.Role().RID)
	if err != nil {
		return nil, "", err
	}
	now := s.Now()
	b, err := a.board(role.SrvID, now)
	if err != nil {
		return nil, "", err
	}
	if _, err := a.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		return nil, "", err
	}
	c := &atCtx{role: role, now: now, b: b, t: b.teamOf(role.RID)}
	if int64(role.Lev) < atMinLev {
		return c, itoa64(atMinLev) + "级开启组队竞技场", nil
	}
	return c, "", nil
}

// forming is the reason a team cannot be changed now, empty when it can: the arena is forming teams or challenging, and
// a team that signed up stays as it is.
func (a *atSvc) forming(c *atCtx, needTeam, leader bool) string {
	if _, state, _ := atSeason(c.now); state != 1 && state != 2 {
		return "组队竞技场未开启"
	}
	if needTeam && c.t == nil {
		return "你还没有队伍"
	}
	if leader && c.t != nil && c.t.Leader != c.role.RID {
		return "只有队长可进行该操作"
	}
	if c.t != nil && c.t.Signed {
		return "队伍已经报名，不能再变动"
	}
	return ""
}

func (a *atSvc) newMember(role *store.Role, pos int64) atMember {
	return atMember{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Pos: pos}
}

func (a *atSvc) join(c *atCtx, t *atTeam, role *store.Role) {
	t.Members = append(t.Members, a.newMember(role, int64(len(t.Members)+1)))
	// asks and invitations of the role to other teams are void now
	keep := c.b.Applies[:0]
	for _, r := range c.b.Applies {
		if r.RID != role.RID {
			keep = append(keep, r)
		}
	}
	c.b.Applies = keep
	keepI := c.b.Invites[:0]
	for _, r := range c.b.Invites {
		if r.RID != role.RID {
			keepI = append(keepI, r)
		}
	}
	c.b.Invites = keepI
}

func nameOK(name string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	if n == 0 {
		return "请输入队伍名称"
	}
	if n > 7 {
		return "队伍名称最多7个字"
	}
	return ""
}

func (b *atBoard) nameTaken(name string, except int64) bool {
	for _, t := range b.Teams {
		if t.Name == name && t.ID != except {
			return true
		}
	}
	return false
}

// main (27220) is the front page: my team, the state of the arena, the challenges and the boxes.
func (a *atSvc) main(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	return s.Send(27220, a.mainMessage(c))
}

func (a *atSvc) mainMessage(c *atCtx) proto.Message {
	key, state, end := atSeason(c.now)
	m := proto.Message{"tid": 0, "srv_id": c.role.SrvID, "team_name": "", "team_power": 0, "score": 0, "team_members": []proto.Message{},
		"state": state, "end_time": end, "rank": 0, "count": 0, "do_count": 0, "add_time": 0, "is_sign": 0}
	ar, _ := a.roleState(c.role.RID, key)
	var do int64
	if c.t != nil {
		ls := a.lives(c.t)
		do = c.t.Do
		m["tid"], m["srv_id"], m["team_name"], m["team_power"] = c.t.ID, c.t.SrvID, c.t.Name, teamPower(ls)
		m["team_members"] = a.briefs(ls, false)
		m["is_sign"] = atBool(c.t.Signed)
		if c.t.Signed {
			m["score"], m["rank"] = c.t.Score, c.b.rankOf(c.t)
			m["count"], m["do_count"], m["add_time"] = c.t.Count, c.t.Do, c.t.AddTime
		}
	}
	awards := []proto.Message{}
	for _, bx := range atCfg.Boxes {
		status := int64(0)
		if c.t != nil && c.t.Signed && do >= bx.Count {
			status = 1
		}
		if ar != nil && has(ar.Claimed, bx.ID) {
			status = 2
		}
		awards = append(awards, proto.Message{"award_id": bx.ID, "status": status})
	}
	m["award_list"] = awards
	return m
}

// hall (27200) lists the teams that can still take someone, and the teams I asked.
func (a *atSvc) hall(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	list := []proto.Message{}
	ids := make([]int64, 0, len(c.b.Teams))
	for id := range c.b.Teams {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if t := c.b.Teams[id]; !t.Signed && len(t.Members) < atTeamSize && (c.t == nil || c.t.ID != id) {
			list = append(list, a.teamBrief(t))
		}
	}
	joined := []proto.Message{}
	for _, r := range c.b.Applies {
		if r.RID == c.role.RID {
			if t := c.b.Teams[r.TID]; t != nil {
				joined = append(joined, proto.Message{"tid": t.ID, "srv_id": t.SrvID})
			}
		}
	}
	return s.Send(27200, proto.Message{"team_list": list, "do_join_list": joined})
}

func (a *atSvc) create(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Str("name"))
	switch {
	case msg != "":
		return a.refuse(s, 27201, msg)
	case c.t != nil:
		return a.refuse(s, 27201, "你已经有队伍了")
	case a.forming(c, false, false) != "":
		return a.refuse(s, 27201, a.forming(c, false, false))
	case nameOK(name) != "":
		return a.refuse(s, 27201, nameOK(name))
	case c.b.nameTaken(name, 0):
		return a.refuse(s, 27201, "队伍名称已被使用")
	}
	key, _, _ := atSeason(c.now)
	c.b.Next++
	t := &atTeam{ID: c.b.Next, Season: key, Leader: c.role.RID, Created: c.now.Unix(), SrvID: c.role.SrvID, Name: name,
		LimitLev: req.Int("limit_lev"), LimitPower: req.Int("limit_power"), Check: req.Int("is_check")}
	c.b.Teams[t.ID] = t
	a.join(c, t, c.role)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	return s.Send(27201, proto.Message{"code": 1, "msg": "创建成功"})
}

// canJoin says why a role cannot join a team, empty when it can.
func (a *atSvc) canJoin(c *atCtx, t *atTeam, who *store.Role) string {
	switch {
	case t.Signed:
		return "该队伍已经报名"
	case len(t.Members) >= atTeamSize:
		return "队伍人数已满"
	case int64(who.Lev) < atMinLev:
		return "角色等级不足" + itoa64(atMinLev) + "级"
	case c.b.teamOf(who.RID) != nil:
		return "已经有队伍了"
	}
	return ""
}

func (a *atSvc) powerOf(rid int64) int64 {
	_, _, p, _ := lineupOf(a.st, rid)
	return p
}

func (a *atSvc) apply(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	t := c.b.Teams[req.Int("tid")]
	switch {
	case msg != "":
		return a.refuse(s, 27202, msg)
	case c.t != nil:
		return a.refuse(s, 27202, "你已经有队伍了")
	case a.forming(c, false, false) != "":
		return a.refuse(s, 27202, a.forming(c, false, false))
	case t == nil:
		return a.refuse(s, 27202, "队伍不存在")
	}
	if m := a.canJoin(c, t, c.role); m != "" {
		return a.refuse(s, 27202, m)
	}
	if int64(c.role.Lev) < t.LimitLev || a.powerOf(c.role.RID) < t.LimitPower {
		return a.refuse(s, 27202, "不满足队伍的入队条件")
	}
	if t.Check == 0 {
		a.join(c, t, c.role)
		if err := a.save(c.role.SrvID, c.b); err != nil {
			return err
		}
		a.notifyTeam(t, 2, c.role.RID)
		if err := s.Send(27202, proto.Message{"code": 1, "msg": "加入队伍成功", "tid": t.ID, "srv_id": t.SrvID}); err != nil {
			return err
		}
		return s.Send(27222, proto.Message{"type": 1})
	}
	for _, r := range c.b.Applies {
		if r.RID == c.role.RID && r.TID == t.ID {
			return a.refuse(s, 27202, "已经申请过了")
		}
	}
	c.b.Applies = append(c.b.Applies, atReq{t.ID, c.role.RID, c.now.Unix()})
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notify(t.Leader, 3)
	return s.Send(27202, proto.Message{"code": 1, "msg": "申请成功，请等待队长审核", "tid": t.ID, "srv_id": t.SrvID})
}

// keyApply (27216) asks several teams at once.
func (a *atSvc) keyApply(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	switch {
	case msg != "":
		return s.Send(27216, proto.Message{"code": 0, "msg": msg, "do_join_list": []proto.Message{}})
	case c.t != nil:
		return s.Send(27216, proto.Message{"code": 0, "msg": "你已经有队伍了", "do_join_list": []proto.Message{}})
	case a.forming(c, false, false) != "":
		return s.Send(27216, proto.Message{"code": 0, "msg": a.forming(c, false, false), "do_join_list": []proto.Message{}})
	}
	done := []proto.Message{}
	power := a.powerOf(c.role.RID)
	for _, item := range req.Arr("do_join_list") {
		t := c.b.Teams[item.Int("tid")]
		if t == nil || a.canJoin(c, t, c.role) != "" || int64(c.role.Lev) < t.LimitLev || power < t.LimitPower {
			continue
		}
		if c.t != nil {
			break
		}
		if t.Check == 0 {
			a.join(c, t, c.role)
			c.t = t
			a.notifyTeam(t, 2, c.role.RID)
			done = append(done, proto.Message{"tid": t.ID, "srv_id": t.SrvID})
			continue
		}
		dup := false
		for _, r := range c.b.Applies {
			dup = dup || (r.RID == c.role.RID && r.TID == t.ID)
		}
		if !dup {
			c.b.Applies = append(c.b.Applies, atReq{t.ID, c.role.RID, c.now.Unix()})
			a.notify(t.Leader, 3)
		}
		done = append(done, proto.Message{"tid": t.ID, "srv_id": t.SrvID})
	}
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	if len(done) == 0 {
		return s.Send(27216, proto.Message{"code": 0, "msg": "没有可以申请的队伍", "do_join_list": done})
	}
	if err := s.Send(27216, proto.Message{"code": 1, "msg": "申请成功", "do_join_list": done}); err != nil {
		return err
	}
	if c.t != nil {
		return s.Send(27222, proto.Message{"type": 1})
	}
	return nil
}

// roleLive is a role off the street as the candidate lists show it.
func (a *atSvc) roleLive(r store.Role) atLive {
	l := atLive{atMember: atMember{RID: r.RID, SrvID: r.SrvID, Name: r.Name, Lev: int64(r.Lev), Pos: 0}}
	l.Face = int64(r.FaceID)
	if l.Face == 0 {
		l.Face = store.DefaultFaceID
	}
	if heroes, form, power, err := lineupOf(a.st, r.RID); err == nil {
		l.Heroes, l.Form, l.Power = heroes, form, power
	}
	return l
}

func (a *atSvc) applyList(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	if c.t != nil {
		for _, r := range c.b.Applies {
			if r.TID != c.t.ID {
				continue
			}
			if who, err := a.st.RoleByID(r.RID); err == nil {
				out = append(out, a.full(a.roleLive(*who)))
			}
		}
	}
	return s.Send(27203, proto.Message{"arena_team_member": out})
}

func (b *atBoard) dropApply(tid, rid int64) {
	keep := b.Applies[:0]
	for _, r := range b.Applies {
		if !(r.TID == tid && r.RID == rid) {
			keep = append(keep, r)
		}
	}
	b.Applies = keep
}

func (b *atBoard) dropInvite(tid, rid int64) {
	keep := b.Invites[:0]
	for _, r := range b.Invites {
		if !(r.TID == tid && r.RID == rid) {
			keep = append(keep, r)
		}
	}
	b.Invites = keep
}

func (a *atSvc) answerApply(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	rid := req.Int("rid")
	reply := func(code int64, m string) error {
		return s.Send(27204, proto.Message{"code": code, "msg": m, "rid": rid, "srv_id": req.Str("srv_id")})
	}
	if msg != "" {
		return reply(0, msg)
	}
	if m := a.forming(c, true, true); m != "" {
		return reply(0, m)
	}
	asked := false
	for _, r := range c.b.Applies {
		asked = asked || (r.TID == c.t.ID && r.RID == rid)
	}
	if !asked {
		return reply(0, "该玩家已取消申请")
	}
	if req.Int("type") != 1 {
		c.b.dropApply(c.t.ID, rid)
		if err := a.save(c.role.SrvID, c.b); err != nil {
			return err
		}
		return reply(1, "已拒绝")
	}
	who, err := a.st.RoleByID(rid)
	if err != nil {
		c.b.dropApply(c.t.ID, rid)
		return reply(0, "该玩家不存在")
	}
	if m := a.canJoin(c, c.t, who); m != "" {
		c.b.dropApply(c.t.ID, rid)
		_ = a.save(c.role.SrvID, c.b)
		return reply(0, m)
	}
	a.join(c, c.t, who)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notify(rid, 1)
	a.notifyTeam(c.t, 2, rid)
	return reply(1, "已同意")
}

// robotFor makes the robot teammate of a leader that stands on the recommended list at place idx: a copy of the
// leader's team a little weaker or stronger; the same leader always gets the same robots.
func (a *atSvc) robotFor(leader *store.Role, idx int64) atMember {
	rid := atRobotRID + leader.RID%1000000*10 + idx
	rng := a.rngSeeded(rid)
	mine, form, _, _ := lineupOf(a.st, leader.RID)
	m := atMember{RID: rid, SrvID: leader.SrvID, Robot: true, Form: form, Lev: max(atMinLev, int64(leader.Lev)-int64(rng.Intn(5))),
		Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))], Face: atCfg.Faces[rng.Intn(len(atCfg.Faces))]}
	factor := 85 + int64(rng.Intn(21))
	var all []store.Partner
	for _, h := range mine {
		lev := max(1, h.Lev*factor/100)
		m.Heroes = append(m.Heroes, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
		all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
	}
	m.Power = rolePower(all)
	return m
}

func (m atMember) asLive() atLive { return atLive{atMember: m} }

func (a *atSvc) invite(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	rid := req.Int("rid")
	reply := func(code int64, m string) error {
		return s.Send(27205, proto.Message{"code": code, "msg": m, "rid": rid, "srv_id": req.Str("srv_id")})
	}
	if msg != "" {
		return reply(0, msg)
	}
	if m := a.forming(c, true, true); m != "" {
		return reply(0, m)
	}
	if len(c.t.Members) >= atTeamSize {
		return reply(0, "队伍人数已满")
	}
	if rid >= atRobotRID && rid < atRobotRID+10000000 {
		for idx := int64(0); idx < 10; idx++ {
			if r := a.robotFor(c.role, idx); r.RID == rid {
				if _, in := c.t.member(rid); in {
					return reply(0, "已经在队伍里了")
				}
				r.Pos = int64(len(c.t.Members) + 1)
				c.t.Members = append(c.t.Members, r)
				if err := a.save(c.role.SrvID, c.b); err != nil {
					return err
				}
				return reply(1, "邀请成功，对方已加入队伍")
			}
		}
		return reply(0, "该玩家不存在")
	}
	who, err := a.st.RoleByID(rid)
	if err != nil {
		return reply(0, "该玩家不存在")
	}
	if m := a.canJoin(c, c.t, who); m != "" {
		return reply(0, m)
	}
	for _, r := range c.b.Invites {
		if r.TID == c.t.ID && r.RID == rid {
			return reply(0, "已经邀请过了")
		}
	}
	c.b.Invites = append(c.b.Invites, atReq{c.t.ID, rid, c.now.Unix()})
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notify(rid, 4)
	return reply(1, "邀请已发送")
}

func (a *atSvc) inviteList(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	for _, r := range c.b.Invites {
		if t := c.b.Teams[r.TID]; r.RID == c.role.RID && t != nil {
			out = append(out, a.teamBrief(t))
		}
	}
	return s.Send(27206, proto.Message{"team_list": out})
}

func (a *atSvc) answerInvite(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	tid := req.Int("tid")
	if msg != "" {
		return a.refuse(s, 27207, msg)
	}
	invited := false
	for _, r := range c.b.Invites {
		invited = invited || (r.TID == tid && r.RID == c.role.RID)
	}
	t := c.b.Teams[tid]
	if !invited || t == nil {
		c.b.dropInvite(tid, c.role.RID)
		_ = a.save(c.role.SrvID, c.b)
		return a.refuse(s, 27207, "邀请已失效")
	}
	if req.Int("type") != 1 {
		c.b.dropInvite(tid, c.role.RID)
		if err := a.save(c.role.SrvID, c.b); err != nil {
			return err
		}
		return s.Send(27207, proto.Message{"code": 1, "msg": "已拒绝"})
	}
	if m := a.forming(c, false, false); m != "" {
		return a.refuse(s, 27207, m)
	}
	if m := a.canJoin(c, t, c.role); m != "" {
		c.b.dropInvite(tid, c.role.RID)
		_ = a.save(c.role.SrvID, c.b)
		return a.refuse(s, 27207, m)
	}
	a.join(c, t, c.role)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notifyTeam(t, 2, c.role.RID)
	if err := s.Send(27207, proto.Message{"code": 1, "msg": "加入队伍成功"}); err != nil {
		return err
	}
	return s.Send(27222, proto.Message{"type": 1})
}

func (a *atSvc) clearInvites(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	keep := c.b.Invites[:0]
	for _, r := range c.b.Invites {
		if r.RID != c.role.RID {
			keep = append(keep, r)
		}
	}
	c.b.Invites = keep
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	return s.Send(27208, proto.Message{"code": 1, "msg": "已清空"})
}

func (a *atSvc) searchTeam(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	q := strings.TrimSpace(req.Str("name"))
	for _, t := range c.b.Teams {
		if q != "" && strings.Contains(t.Name, q) && !t.Signed {
			out = append(out, a.teamBrief(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Int("tid") < out[j].Int("tid") })
	if len(out) == 0 {
		return s.Send(27210, proto.Message{"code": 0, "msg": "没有找到这个队伍", "team_list": out})
	}
	return s.Send(27210, proto.Message{"code": 1, "msg": "", "team_list": out})
}

// remove takes a member out; a leader going hands the team to the member with the highest power, the last one
// going ends the team.
func (a *atSvc) remove(c *atCtx, t *atTeam, rid int64) {
	i, ok := t.member(rid)
	if !ok {
		return
	}
	t.Members = append(t.Members[:i], t.Members[i+1:]...)
	var humans []atMember
	for _, m := range t.Members {
		if !m.Robot {
			humans = append(humans, m)
		}
	}
	if len(humans) == 0 {
		delete(c.b.Teams, t.ID)
		keep := c.b.Applies[:0]
		for _, r := range c.b.Applies {
			if r.TID != t.ID {
				keep = append(keep, r)
			}
		}
		c.b.Applies = keep
		keepI := c.b.Invites[:0]
		for _, r := range c.b.Invites {
			if r.TID != t.ID {
				keepI = append(keepI, r)
			}
		}
		c.b.Invites = keepI
		return
	}
	if t.Leader == rid {
		best := humans[0]
		for _, m := range humans[1:] {
			if a.powerOf(m.RID) > a.powerOf(best.RID) {
				best = m
			}
		}
		t.Leader = best.RID
	}
	t.renumber()
}

func (a *atSvc) leave(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	if m := a.forming(c, true, false); m != "" {
		return a.refuse(s, 27211, m)
	}
	t := c.t
	a.remove(c, t, c.role.RID)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	if c.b.Teams[t.ID] != nil {
		a.notifyTeam(t, 2, 0)
	}
	return s.Send(27211, proto.Message{"code": 1, "msg": "已退出队伍"})
}

func (a *atSvc) kick(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	rid := req.Int("rid")
	if m := a.forming(c, true, true); m != "" {
		return a.refuse(s, 27212, m)
	}
	if _, in := c.t.member(rid); !in || rid == c.role.RID {
		return a.refuse(s, 27212, "队伍里没有这个玩家")
	}
	a.remove(c, c.t, rid)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notify(rid, 5)
	a.notifyTeam(c.t, 2, c.role.RID)
	return s.Send(27212, proto.Message{"code": 1, "msg": "已将该玩家移出队伍"})
}

func (a *atSvc) transfer(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	rid := req.Int("rid")
	if m := a.forming(c, true, true); m != "" {
		return a.refuse(s, 27213, m)
	}
	i, in := c.t.member(rid)
	if !in || c.t.Members[i].Robot || rid == c.role.RID {
		return a.refuse(s, 27213, "不能移交给这个玩家")
	}
	c.t.Leader = rid
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notifyTeam(c.t, 2, c.role.RID)
	return s.Send(27213, proto.Message{"code": 1, "msg": "已移交队长"})
}

// points (27215) are the red dots at login: 1 a fight to read, 2 a request for the leader, 3 an invitation.
func (a *atSvc) points(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	key, _, _ := atSeason(c.now)
	ar, err := a.roleState(c.role.RID, key)
	if err != nil {
		return err
	}
	apply, invite := false, false
	for _, r := range c.b.Applies {
		apply = apply || (c.t != nil && c.t.Leader == c.role.RID && r.TID == c.t.ID)
	}
	for _, r := range c.b.Invites {
		invite = invite || r.RID == c.role.RID
	}
	return s.Send(27215, proto.Message{"point": []proto.Message{{"type": 1, "state": atBool(ar.Red)}, {"type": 2, "state": atBool(apply)},
		{"type": 3, "state": atBool(invite)}, {"type": 4, "state": 0}}})
}

// details (27221) is my team with every member's heroes.
func (a *atSvc) details(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	m := proto.Message{"tid": 0, "srv_id": c.role.SrvID, "rid": 0, "sid": "", "team_name": "", "team_power": 0, "team_limit_power": 0, "team_limit_lev": 0,
		"team_is_check": 0, "arena_team_member": []proto.Message{}, "is_sign": 0}
	if c.t != nil {
		ls := a.lives(c.t)
		m["tid"], m["srv_id"], m["rid"], m["sid"] = c.t.ID, c.t.SrvID, c.t.Leader, c.t.SrvID
		m["team_name"], m["team_power"], m["team_limit_power"], m["team_limit_lev"], m["team_is_check"] = c.t.Name, teamPower(ls), c.t.LimitPower, c.t.LimitLev, c.t.Check
		m["arena_team_member"], m["is_sign"] = a.fulls(ls), atBool(c.t.Signed)
	}
	return s.Send(27221, m)
}

func (a *atSvc) members(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	if c.t != nil {
		out = a.fulls(a.lives(c.t))
	}
	return s.Send(27243, proto.Message{"arena_team_member": out})
}

func (a *atSvc) setLimits(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	if m := a.forming(c, true, true); m != "" {
		return a.refuse(s, 27225, m)
	}
	c.t.LimitLev, c.t.LimitPower, c.t.Check = req.Int("limit_lev"), req.Int("limit_power"), req.Int("is_check")
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	return s.Send(27225, proto.Message{"code": 1, "msg": "设置成功"})
}

func (a *atSvc) rename(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Str("name"))
	switch {
	case c.t == nil:
		return a.refuse(s, 27226, "你还没有队伍")
	case c.t.Leader != c.role.RID:
		return a.refuse(s, 27226, "只有队长可进行该操作")
	case nameOK(name) != "":
		return a.refuse(s, 27226, nameOK(name))
	case c.b.nameTaken(name, c.t.ID):
		return a.refuse(s, 27226, "队伍名称已被使用")
	}
	c.t.Name = name
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notifyTeam(c.t, 2, c.role.RID)
	return s.Send(27226, proto.Message{"code": 1, "msg": ""})
}

// recommend (27228) lists roles a team could ask: real ones of the level without a team, then robots.
func (a *atSvc) recommend(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	if c.t != nil {
		rs, err := a.st.RecommendFriends(c.role.SrvID, c.role.RID, 40)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if len(out) == 4 {
				break
			}
			if int64(r.Lev) >= atMinLev && c.b.teamOf(r.RID) == nil {
				out = append(out, a.full(a.roleLive(r)))
			}
		}
		for idx := int64(0); idx < 3; idx++ {
			if r := a.robotFor(c.role, idx); func() bool { _, in := c.t.member(r.RID); return !in }() {
				out = append(out, a.full(r.asLive()))
			}
		}
	}
	return s.Send(27228, proto.Message{"arena_team_member": out})
}

func (a *atSvc) searchPlayer(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	if q := strings.TrimSpace(req.Str("name")); q != "" {
		rs, err := a.st.SearchRolesByName(c.role.SrvID, q, 10)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if r.RID != c.role.RID && int64(r.Lev) >= atMinLev && c.b.teamOf(r.RID) == nil {
				out = append(out, a.full(a.roleLive(r)))
			}
		}
	}
	return s.Send(27229, proto.Message{"arena_team_member": out})
}

// sign (27240) enters the team in the season; from then on it is not changed.
func (a *atSvc) sign(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, msg, err := a.begin(s)
	if err != nil {
		return err
	}
	if msg != "" {
		return a.refuse(s, 27240, msg)
	}
	if m := a.forming(c, true, true); m != "" {
		return a.refuse(s, 27240, m)
	}
	if len(c.t.Members) < atTeamSize {
		return a.refuse(s, 27240, "队伍人数不足")
	}
	c.t.Signed, c.t.Score = true, atCfg.num("init_score")
	c.t.Count, c.t.AddTime, c.t.Do = atCfg.num("initial_number"), 0, 0
	a.recover(c.t, c.now)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notifyTeam(c.t, 2, c.role.RID)
	return s.Send(27240, proto.Message{"code": 1, "msg": "报名成功"})
}

func (a *atSvc) unsign(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	_, state, _ := atSeason(c.now)
	switch {
	case c.t == nil || !c.t.Signed:
		return a.refuse(s, 27241, "队伍还没有报名")
	case c.t.Leader != c.role.RID:
		return a.refuse(s, 27241, "只有队长可进行该操作")
	case state != 1:
		return a.refuse(s, 27241, "比赛已经开始，不能取消报名")
	}
	c.t.Signed, c.t.Score, c.t.Count, c.t.AddTime = false, 0, 0, 0
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	a.notifyTeam(c.t, 2, c.role.RID)
	return s.Send(27241, proto.Message{"code": 1, "msg": "已取消报名"})
}

// saveOrder (27242) sets who fights first and who is hidden; the leader only, also after the sign-up.
func (a *atSvc) saveOrder(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	if c.t == nil || c.t.Leader != c.role.RID {
		return a.refuse(s, 27242, "只有队长可进行该操作")
	}
	infos := req.Arr("pos_info")
	if len(infos) != len(c.t.Members) {
		return a.refuse(s, 27242, "出战顺序有误")
	}
	seen := map[int64]bool{}
	hidden := 0
	for _, p := range infos {
		if _, in := c.t.member(p.Int("rid")); !in || p.Int("pos") < 1 || p.Int("pos") > int64(len(infos)) || seen[p.Int("pos")] {
			return a.refuse(s, 27242, "出战顺序有误")
		}
		seen[p.Int("pos")] = true
		if p.Int("is_hide") == 1 {
			hidden++
		}
	}
	if hidden > 1 {
		return a.refuse(s, 27242, "最多隐藏一名队员")
	}
	for _, p := range infos {
		i, _ := c.t.member(p.Int("rid"))
		c.t.Members[i].Pos, c.t.Members[i].Hide = p.Int("pos"), p.Int("is_hide") == 1
	}
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	return s.Send(27242, proto.Message{"code": 1, "msg": ""})
}

func (a *atSvc) ranking(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	ranked := c.b.ranked()
	from, to := max(1, req.Int("start_rank")), req.Int("end_rank")
	if to == 0 || to > 100 {
		to = 100
	}
	list := []proto.Message{}
	for i, t := range ranked {
		if rank := int64(i + 1); rank >= from && rank <= to {
			list = append(list, proto.Message{"team_name": t.Name, "team_power": teamPower(a.lives(t)), "score": t.Score, "rank": rank,
				"team_members": a.briefs(a.lives(t), true)})
		}
	}
	my, rank := int64(0), int64(0)
	if c.t != nil && c.t.Signed {
		my, rank = c.t.Score, c.b.rankOf(c.t)
	}
	return s.Send(27223, proto.Message{"my_score": my, "my_rank": rank, "ranks": list})
}

// box (27224) takes a box of the challenges made; every member takes his own.
func (a *atSvc) box(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	id := req.Int("id")
	reply := func(code int64, m string) error {
		return s.Send(27224, proto.Message{"code": code, "msg": m, "id": id})
	}
	key, _, _ := atSeason(c.now)
	ar, err := a.roleState(c.role.RID, key)
	if err != nil {
		return err
	}
	for _, bx := range atCfg.Boxes {
		if bx.ID != id {
			continue
		}
		switch {
		case c.t == nil || !c.t.Signed || c.t.Do < bx.Count:
			return reply(0, "挑战次数不足")
		case has(ar.Claimed, id):
			return reply(0, "已经领取过了")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(bx.Award), 1, ga, gi)
		if err := a.st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		ar.Claimed = append(ar.Claimed, id)
		if err := a.st.SaveState(c.role.RID, atRoleModule, ar); err != nil {
			return err
		}
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

// robotTeam makes a team of three robots that copies the line-ups of mine, a little weaker or stronger.
func (a *atSvc) robotTeam(c *atCtx, mine []atLive, score int64, rng *rand.Rand) *atTeam {
	id := atRobotTeam + rng.Int63n(900000000)
	t := &atTeam{ID: id, SrvID: c.role.SrvID, Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))] + "队", Signed: true, Score: score, Season: c.t.Season}
	factor := 85 + int64(rng.Intn(31))
	for i := int64(0); i < atTeamSize; i++ {
		rid := atRivalRID + (id%1000000)*10 + i
		m := atMember{RID: rid, SrvID: c.role.SrvID, Robot: true, Pos: i + 1, Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))],
			Face: atCfg.Faces[rng.Intn(len(atCfg.Faces))]}
		src := mine[int(i)%len(mine)]
		m.Lev, m.Form = src.Lev, src.Form
		var all []store.Partner
		for _, h := range src.Heroes {
			lev := max(1, h.Lev*factor/100)
			m.Heroes = append(m.Heroes, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
			all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
		}
		m.Power = rolePower(all)
		t.Members = append(t.Members, m)
	}
	t.Leader = t.Members[0].RID
	return t
}

// drawRivals picks the teams to fight: the real signed teams whose score is closest, robot teams to fill up.
func (a *atSvc) drawRivals(c *atCtx) {
	rng := a.rng()
	var real []*atTeam
	for _, t := range c.b.Teams {
		if t.Signed && t.ID != c.t.ID && len(t.Members) == atTeamSize {
			real = append(real, t)
		}
	}
	sort.Slice(real, func(i, j int) bool {
		di, dj := abs64(real[i].Score-c.t.Score), abs64(real[j].Score-c.t.Score)
		if di != dj {
			return di < dj
		}
		return real[i].ID < real[j].ID
	})
	c.t.Rivals = nil
	for _, t := range real {
		if len(c.t.Rivals) == atRivals {
			break
		}
		c.t.Rivals = append(c.t.Rivals, atRival{TID: t.ID})
	}
	mine := a.lives(c.t)
	for len(c.t.Rivals) < atRivals {
		pct := atCfg.num("integral_higher")
		if c.t.Score >= atCfg.num("guard_score") {
			pct = atCfg.num("integral_low")
		}
		score := max(atCfg.num("init_score"), c.t.Score*(100+rng.Int63n(2*pct+1)-pct)/100)
		rt := a.robotTeam(c, mine, score, rng)
		c.t.Rivals = append(c.t.Rivals, atRival{TID: rt.ID, Robot: true, Team: rt})
	}
	c.t.RivalAt = c.now.Unix()
}

func (a *atSvc) rivalTeam(c *atCtx, r atRival) *atTeam {
	if r.Robot {
		return r.Team
	}
	return c.b.Teams[r.TID]
}

func (a *atSvc) challengeGate(c *atCtx) string {
	if _, state, _ := atSeason(c.now); state != 2 {
		return "不在挑战时间内"
	}
	switch {
	case c.t == nil:
		return "未组队不能参赛"
	case !c.t.Signed:
		return "队伍还没有报名"
	case c.t.Leader != c.role.RID:
		return "只有队长可进行该操作"
	}
	return ""
}

func (a *atSvc) refresh(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	if m := a.challengeGate(c); m != "" {
		return a.refuse(s, 27250, m)
	}
	a.drawRivals(c)
	if err := a.save(c.role.SrvID, c.b); err != nil {
		return err
	}
	return s.Send(27250, proto.Message{"code": 1, "msg": ""})
}

func (a *atSvc) rivalList(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	out := []proto.Message{}
	if a.challengeGate(c) == "" {
		if len(c.t.Rivals) == 0 || c.now.Unix() > c.t.RivalAt+atCfg.num("list_validity_time") {
			a.drawRivals(c)
			if err := a.save(c.role.SrvID, c.b); err != nil {
				return err
			}
		}
		for _, r := range c.t.Rivals {
			t := a.rivalTeam(c, r)
			if t == nil {
				continue
			}
			ls := a.lives(t)
			rank := c.b.rankOf(t)
			if r.Robot {
				rank = c.b.rankForScore(t.Score)
			}
			out = append(out, proto.Message{"tid": t.ID, "srv_id": t.SrvID, "rid": t.Leader, "sid": t.SrvID, "team_name": t.Name, "team_power": teamPower(ls),
				"team_rank": rank, "team_score": t.Score, "team_members": a.fulls(ls)})
		}
	}
	return s.Send(27251, proto.Message{"rival_list": out})
}

// atGain is what a win adds to the score; a loss changes nothing (reduce_score is 0 in the table), and the team beaten
// loses nothing. The scale grows with the score the beaten team has over ours.
func atGain(mine, theirs int64) int64 {
	w := 0.5 + float64(theirs-mine)/float64(atCfg.num("coefficient1"))
	w = min(1, max(0, w))
	return atCfg.num("add_score") + int64(float64(atCfg.num("add_coefficient1"))*w)
}

func (a *atSvc) snapOf(t *atTeam, ls []atLive, score, newScore, rank, newRank int64) atSnap {
	sn := atSnap{TID: t.ID, Leader: t.Leader, SrvID: t.SrvID, Name: t.Name, Power: teamPower(ls), Score: score, NewScore: newScore, Rank: rank, NewRank: newRank}
	for _, l := range ls {
		sn.Members = append(sn.Members, atSnapMember{RID: l.RID, Lev: l.Lev, Power: l.Power, Pos: l.Pos, Face: l.Face, Hide: l.Hide,
			Leader: l.Leader, SrvID: l.SrvID, Name: l.Name, Heroes: l.Heroes})
	}
	return sn
}

func (sn atSnap) members(a *atSvc) []proto.Message {
	out := []proto.Message{}
	for _, m := range sn.Members {
		l := atLive{atMember: atMember{RID: m.RID, SrvID: m.SrvID, Name: m.Name, Lev: m.Lev, Face: m.Face, Pos: m.Pos, Hide: m.Hide, Power: m.Power, Heroes: m.Heroes}, Leader: m.Leader}
		l.Robot = m.RID >= arenaRobotBase
		out = append(out, a.full(l))
	}
	return out
}

// plan is a challenge worked out before it is played: the duels and who wins.
type atPlan struct {
	rival        atRival
	mine, theirs []atLive
	duels        []atDuel
	rounds       [][]fightRound
	won          bool
	wins, loses  int64
	intro        proto.Message
	hurts        []proto.Message
	form         int64
}

var atSpirit = proto.Message{
	"camp_type": 0, "career": 0, "extra_data": []proto.Message{}, "face_id": 0, "group": 1, "hp": 34856, "hp_max": 34856, "lev": 1,
	"object_bid": 1, "object_id": 1, "object_name": "精灵", "object_type": 5, "owner_id": 0, "owner_srv_id": "", "pos": spiritPos,
	"sex": 0, "skills": []proto.Message{}, "sprites": []proto.Message{{"item_bid": 0, "pos": 1}}, "star": 0,
}

func (a *atSvc) opp(l atLive) arenaOpp {
	return arenaOpp{RID: l.RID, SrvID: l.SrvID, Name: l.Name, Lev: l.Lev, Face: l.Face, Power: l.Power, Robot: l.Robot, FormType: l.Form, Lineup: l.Heroes}
}

// wheel plays the duels: the first of each side fights; the loser is replaced by the next of his team, the winner stays
// (with full hp again: ours do not carry the hp over), until a side has no one left.
func (a *atSvc) wheel(mine, theirs []atLive, rng *rand.Rand) (*atPlan, error) {
	p := &atPlan{mine: mine, theirs: theirs}
	i, j := 0, 0
	for i < len(mine) && j < len(theirs) {
		ms, mf, mb, err := a.opp(mine[i]).fighters(a.st)
		if err != nil {
			return nil, err
		}
		ts, tf, tb, err := a.opp(theirs[j]).fighters(a.st)
		if err != nil {
			return nil, err
		}
		mu, mo := sideUnits(mine[i].RID, mine[i].SrvID, ms, mf, mb, true)
		tu, to := sideUnits(theirs[j].RID, theirs[j].SrvID, ts, tf, tb, false)
		if len(mu) == 0 {
			i++
			p.loses++
			continue
		}
		if len(tu) == 0 {
			j++
			p.wins++
			continue
		}
		rounds, won := buildFight(mu, tu, rng)
		d := atDuel{Ret: 2, ARID: mine[i].RID, ASrv: mine[i].SrvID, BRID: theirs[j].RID, BSrv: theirs[j].SrvID, AOrder: int64(i + 1), BOrder: int64(j + 1),
			APower: mine[i].Power, BPower: theirs[j].Power, AForm: mf, BForm: tf, AList: mine[i].Heroes, BList: theirs[j].Heroes}
		if won {
			d.Ret = 1
			j++
			p.wins++
		} else {
			i++
			p.loses++
		}
		if len(p.duels) == 0 {
			p.form = mf
			p.intro = proto.Message{
				"combat_type": combatTeam, "formation": []proto.Message{{"group": 1, "formation_type": mf, "formation_lev": 1}, {"group": 2, "formation_type": tf, "formation_lev": 1}},
				"objects": append(append([]proto.Message{atSpirit}, mo...), to...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
				"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(mu), "actor_role_name": mine[0].Name,
				"target_role_name": theirs[0].Name, "suppress": 0, "halo_list": []proto.Message{},
				"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
			}
		}
		p.hurts = append(p.hurts, proto.Message{"type": len(p.duels) + 1, "a_round": atBool(won), "b_round": atBool(!won),
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": a.hurtsOf(mine[i-b2iInt(!won)], ms, mb)}}})
		p.duels = append(p.duels, d)
		p.rounds = append(p.rounds, rounds)
	}
	p.won = j >= len(theirs)
	return p, nil
}

func b2iInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// hurtsOf is the damage table of one member's heroes; there is no per-hero damage in our fights, so a hero's power
// stands for it.
func (a *atSvc) hurtsOf(l atLive, slots []store.FormationSlot, byID map[int64]store.Partner) []proto.Message {
	out := []proto.Message{}
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		info := partnerTable[h.BID]
		out = append(out, proto.Message{"rid": l.RID, "srvid": l.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": info.Camp,
			"dps": partnerMessage(h).Int("power"), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
	}
	return out
}

// challenge (27252): the leader fights a team of the list. The duels are worked out at once; the first is played for the
// leader unless he skips, then the score moves and the result (27253) comes.
func (a *atSvc) challenge(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	a.mu.Lock()
	c, msg, err := a.begin(s)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	refuse := func(m string) error { a.mu.Unlock(); return a.refuse(s, 27252, m) }
	if msg != "" {
		return refuse(msg)
	}
	if m := a.challengeGate(c); m != "" {
		return refuse(m)
	}
	if len(c.t.Members) < atTeamSize {
		return refuse("队伍人数不足")
	}
	if c.t.Count <= 0 {
		return refuse("挑战次数不足")
	}
	var rival *atRival
	for i := range c.t.Rivals {
		if c.t.Rivals[i].TID == req.Int("tid") {
			rival = &c.t.Rivals[i]
		}
	}
	if rival == nil {
		return refuse("挑战列表已过期，请刷新")
	}
	foe := a.rivalTeam(c, *rival)
	if foe == nil || (!rival.Robot && !foe.Signed) {
		return refuse("该队伍已不能挑战")
	}
	if _, err := a.st.GrantStarterPartners(c.role.RID, starterSeeds(c.role.Sex)); err != nil {
		a.mu.Unlock()
		return err
	}
	plan, err := a.wheel(a.lives(c.t), a.lives(foe), a.rng())
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if len(plan.duels) == 0 {
		return refuse("队伍没有可以出战的英雄")
	}
	plan.rival = *rival
	for i := range plan.duels {
		d := &plan.duels[i]
		d.Time = c.now.Unix()
		d.Replay = recordReplay(a.st, combatTeam, replaySide{RID: d.ARID, SrvID: d.ASrv, Name: plan.mine[0].Name, Lev: plan.mine[0].Lev, Face: plan.mine[0].Face, Power: d.APower, Form: d.AForm},
			replaySide{RID: d.BRID, SrvID: d.BSrv, Name: plan.theirs[0].Name, Lev: plan.theirs[0].Lev, Face: plan.theirs[0].Face, Power: d.BPower, Form: d.BForm},
			plan.intro, plan.rounds[i], d.Ret == 1, c.now.Unix())
	}
	if c.t.Count >= atCfg.num("limit_number") {
		c.t.AddTime = c.now.Unix() + atCfg.num("recovery_time")
	}
	c.t.Count--
	c.t.Do++
	c.t.Rivals = nil
	err = a.save(c.role.SrvID, c.b)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if err := s.Send(27252, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	settle := func(s *gateway.Session, _ bool) error { return a.settle(s, c.t.ID, foe, rival.Robot, plan) }
	if req.Int("is_auto") == 1 {
		return settle(s, plan.won)
	}
	fights := fightServiceOf(a.st)
	return fights.begin(s, c.role, combatTeam, plan.intro, plan.rounds[0], plan.duels[0].Ret == 1, settle)
}

// settle moves the scores, writes the logs and sends the result.
func (a *atSvc) settle(s *gateway.Session, tid int64, foe *atTeam, robot bool, plan *atPlan) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := s.Now()
	b, err := a.board(role.SrvID, now)
	if err != nil {
		return err
	}
	t := b.teamOf(role.RID)
	if t == nil || t.ID != tid {
		return nil
	}
	if !robot {
		if real := b.Teams[foe.ID]; real != nil {
			foe = real
		}
	}
	oldScore, oldRank := t.Score, b.rankOf(t)
	foeScore := foe.Score
	foeRank := b.rankOf(foe)
	if robot {
		foeRank = b.rankForScore(foeScore)
	}
	result := int64(2)
	if plan.won {
		result = 1
		t.Score += atGain(oldScore, foeScore)
		t.Wins++
	} else {
		t.Score = max(atCfg.num("init_score"), t.Score-atCfg.num("reduce_score"))
		t.Loses++
	}
	newRank := b.rankOf(t)
	mineSnap := a.snapOf(t, plan.mine, oldScore, t.Score, oldRank, newRank)
	foeSnap := a.snapOf(foe, plan.theirs, foeScore, foeScore, foeRank, foeRank)
	t.NextLog++
	entry := atLog{ID: t.NextLog, Ret: result, Wins: plan.wins, Loses: plan.loses, Time: now.Unix(), A: mineSnap, B: foeSnap, Duels: plan.duels}
	t.Logs = append([]atLog{entry}, t.Logs...)
	t.Logs = t.Logs[:min(len(t.Logs), atLogKeep)]
	if !robot && b.Teams[foe.ID] != nil {
		foe.NextLog++
		e2 := entry
		e2.ID = foe.NextLog
		foe.Logs = append([]atLog{e2}, foe.Logs...)
		foe.Logs = foe.Logs[:min(len(foe.Logs), atLogKeep)]
	}
	if err := a.save(role.SrvID, b); err != nil {
		return err
	}
	for _, tm := range []*atTeam{t, foe} {
		if tm == foe && robot {
			continue
		}
		for _, m := range tm.Members {
			if m.Robot {
				continue
			}
			ar, err := a.roleState(m.RID, t.Season)
			if err != nil {
				return err
			}
			ar.Red = true
			if err := a.st.SaveState(m.RID, atRoleModule, ar); err != nil {
				return err
			}
			if m.RID != role.RID {
				a.notify(m.RID, 6)
			}
		}
	}
	return s.Send(27253, proto.Message{"result": result, "win_count": t.Wins, "lose_count": t.Loses, "a_score": oldScore, "a_new_score": t.Score,
		"a_rank": oldRank, "a_new_rank": newRank, "a_team_name": t.Name, "a_team_members": mineSnap.members(a), "b_score": foeScore, "b_new_score": foeScore,
		"b_rank": foeRank, "b_new_rank": foeRank, "b_team_name": foe.Name, "b_team_members": foeSnap.members(a), "all_hurt_statistics": plan.hurts})
}

// reports (27255): the fights of my team, newest first.
func (a *atSvc) reports(s *gateway.Session, _ proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	key, _, _ := atSeason(c.now)
	ar, err := a.roleState(c.role.RID, key)
	if err != nil {
		return err
	}
	list := []proto.Message{}
	if c.t != nil {
		for _, l := range c.t.Logs {
			list = append(list, proto.Message{"id": l.ID, "a_rid": l.A.Leader, "a_srv_id": l.A.SrvID, "atk_name": l.A.Name, "a_score": l.A.Score,
				"a_new_score": l.A.NewScore, "a_rank": l.A.Rank, "a_new_rank": l.A.NewRank, "a_team_power": l.A.Power, "a_team_members": l.A.members(a),
				"b_rid": l.B.Leader, "b_srv_id": l.B.SrvID, "b_score": l.B.Score, "b_new_score": l.B.NewScore, "b_rank": l.B.Rank, "b_new_rank": l.B.NewRank,
				"b_team_name": l.B.Name, "b_team_power": l.B.Power, "b_team_members": l.B.members(a), "ret": l.Ret, "win_count": l.Wins, "lose_count": l.Loses,
				"time": l.Time})
		}
	}
	ar.Red = false
	if err := a.st.SaveState(c.role.RID, atRoleModule, ar); err != nil {
		return err
	}
	return s.Send(27255, proto.Message{"arena_team_log": list})
}

// report (27256): the duels of one fight.
func (a *atSvc) report(s *gateway.Session, req proto.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.begin(s)
	if err != nil {
		return err
	}
	infos := []proto.Message{}
	if c.t != nil {
		for _, l := range c.t.Logs {
			if l.ID != req.Int("id") {
				continue
			}
			plist := func(hs []arenaHero) []proto.Message {
				out := []proto.Message{}
				for _, h := range hs {
					out = append(out, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "skin_id": 0, "ext": []proto.Message{}})
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
	}
	return s.Send(27256, proto.Message{"id": req.Int("id"), "arena_replay_infos": infos})
}
