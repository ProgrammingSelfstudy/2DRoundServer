package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// guildwar.json: the guild war of the client (tools/extract_guildwar.py).
//
//go:embed data/guildwar.json
var guildWarJSON []byte

const (
	guildWarModule = "guildwar"
	combatGuildWar = 16 // BattleConst.Fight_Type.GuildWar
	gwPositions    = 50
	gwStars        = 3 // the stars of a position
)

// The war status the client shows (GuildwarConst.status) and the result of a side (GuildwarConst.result).
const (
	gwClose, gwMatching, gwShowing, gwProcessing, gwSettlement = 1, 2, 3, 4, 5
	gwFighting, gwLose, gwWin, gwDogfall                       = 0, 1, 2, 3
)

var guildWarCfg = func() (c struct {
	Const      map[string]json.RawMessage
	Positions  [][]int64
	Buff       []map[string]int64
	GuardPower []int64 `json:"guard_power"`
	RankAwards []struct {
		Num   int64
		Items [][2]int64
	} `json:"rank_awards"`
}) {
	if err := json.Unmarshal(guildWarJSON, &c); err != nil || len(c.Positions) != gwPositions || len(c.Buff) == 0 {
		panic("game: bad guildwar.json")
	}
	return c
}()

func gwInt(name string) int64 {
	var n int64
	_ = json.Unmarshal(guildWarCfg.Const[name], &n)
	return n
}

func gwPairs(name string) [][2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(guildWarCfg.Const[name], &l)
	return l
}

func gwTimes(name string) []int64 {
	var l []int64
	_ = json.Unmarshal(guildWarCfg.Const[name], &l)
	return l
}

// gwDays are the weekdays (1 = Monday) the wars are held; the matching, the start and the end are at the hours of the constants.
var gwDays = func() []int64 {
	var l [][]int64
	_ = json.Unmarshal(guildWarCfg.Const["matching_time"], &l)
	out := []int64{}
	for _, e := range l {
		out = append(out, e[0])
	}
	return out
}()

// gwTimeline is the moments of the war of a day (China time): the matching, the start, the end and the end of the boxes.
func gwTimeline(day time.Time) (match, start, end, boxEnd int64) {
	d := dayStart(day.In(dayZone))
	at := func(name string) int64 {
		h := gwTimes(name)
		return d.Add(time.Duration(h[0])*time.Hour + time.Duration(h[1])*time.Minute + time.Duration(h[2])*time.Second).Unix()
	}
	m := gwPairsTime()
	return d.Add(time.Duration(m[0])*time.Hour + time.Duration(m[1])*time.Minute).Unix(), at("start_time"), at("end_time"), at("end_time") + gwInt("box_time_limit")
}

func gwPairsTime() [2]int64 {
	var l [][]int64
	_ = json.Unmarshal(guildWarCfg.Const["matching_time"], &l)
	return [2]int64{l[0][1], l[0][2]}
}

// gwHold is a position of a side: the member that holds it (or a neutral guard) with the line-up it had at the matching.
type gwHold struct {
	Pos, RID, Lev, Face, Power, FormType int64
	Name                                 string
	Guard                                bool
	Lineup                               []arenaHero
	Hp                                   int64 // stars left; 0 is a ruin
	Def                                  int64 // attacks it stood off
	Relic                                int64 // attacks on its ruin
}

type gwPerson struct {
	RID, Lev, Face, Power int64
	Name                  string
}

type gwSide struct {
	GID, Rank   int64
	SrvID, Name string
	Holds       []gwHold
	People      []gwPerson
	Buff        int64
	Used        map[int64]int64 // attempts used, by role
	Score       map[int64]int64 // war score, by role
	Stars       map[int64]int64 // stars won, by role
}

func (s *gwSide) hp() int64 {
	var n int64
	for _, h := range s.Holds {
		n += h.Hp
	}
	return n
}

// gwLog is one attack of the war.
type gwLog struct {
	Time               int64
	Side               int // the side that attacked, 0 or 1
	ARID, DRID         int64
	AName, DName       string
	Pos                int64
	Ruin, Win          bool
	Stars, Score, Buff int64
	StarsA, StarsB     int64 // the stars left of the attacker's side and of the other one after it
	Level, Power       int64
	Face, ALev         int64
	Lineup             []arenaHero
	FormType, DefPower int64
}

type gwBox struct {
	Order, RID int64
	Name       string
	Item, Num  int64
}

type gwPair struct {
	Sides   [2]gwSide
	Logs    []gwLog
	Result  [2]int64
	Settled bool
	Boxes   [2][]gwBox
}

type gwWar struct {
	Key   int64 // the day of the war
	Pairs []*gwPair
	Done  bool // the settlement of every pair was made
}

func (w *gwWar) pairOf(gid int64) (*gwPair, int) {
	for _, p := range w.Pairs {
		for i := range p.Sides {
			if p.Sides[i].GID == gid {
				return p, i
			}
		}
	}
	return nil, 0
}

// gwWarDay is the war day whose matching has happened at the moment, the latest one, and whether there is one.
func gwWarDay(now time.Time) (time.Time, bool) {
	for back := 0; back < 8; back++ {
		d := now.In(dayZone).AddDate(0, 0, -back)
		wd := weekday(d)
		if !has(gwDays, wd) {
			continue
		}
		if match, _, _, _ := gwTimeline(d); match <= now.Unix() {
			return d, true
		}
	}
	return time.Time{}, false
}

// gwPhase is the status the client shows for a war day at a moment.
func gwPhase(day time.Time, now int64) (status, countdownTo int64) {
	match, start, end, boxEnd := gwTimeline(day)
	switch {
	case now < match:
		return gwClose, match
	case now < start:
		return gwShowing, start
	case now < end:
		return gwProcessing, end
	case now < boxEnd:
		return gwSettlement, boxEnd
	}
	return gwClose, boxEnd
}

type gwSvc struct {
	st     *store.Store
	fights *FightService
	hub    *gateway.Hub
	rng    func() *rand.Rand
}

// power of a role: the best five heroes, from the line-up it shows.
func (g *gwSvc) person(r store.MemberRow) (gwPerson, gwHold, bool) {
	role, err := g.st.RoleByID(r.RID)
	if err != nil || role == nil {
		return gwPerson{}, gwHold{}, false
	}
	opp, ok, err := oppFromRole(g.st, role, 0)
	if err != nil || !ok {
		return gwPerson{}, gwHold{}, false
	}
	p := gwPerson{RID: r.RID, Lev: int64(r.Lev), Face: opp.Face, Power: opp.Power, Name: r.Name}
	h := gwHold{RID: r.RID, Lev: int64(r.Lev), Face: opp.Face, Power: opp.Power, Name: r.Name, FormType: opp.FormType, Lineup: opp.Lineup, Hp: gwStars}
	return p, h, true
}

// side makes the side of a guild: the members that were on in the last days, the strongest first, each on a position; the
// positions that are left are held by neutral guards.
func (g *gwSvc) side(gd store.Guild, now int64) (*gwSide, bool, error) {
	members, err := g.st.MembersOf(gd.GID)
	if err != nil {
		return nil, false, err
	}
	s := &gwSide{GID: gd.GID, SrvID: gd.SrvID, Name: gd.Name, Used: map[int64]int64{}, Score: map[int64]int64{}, Stars: map[int64]int64{}}
	type cand struct {
		p gwPerson
		h gwHold
	}
	var cs []cand
	for _, m := range members {
		if last := max(m.LastLoginAt, m.LastLogoutAt); now-last > gwInt("active_member_condition")*86400 && g.hub.Online(m.RID) == nil {
			continue
		}
		if p, h, ok := g.person(m); ok {
			cs = append(cs, cand{p, h})
		}
	}
	if int64(len(cs)) < max(1, gwInt("active_member_num")) {
		return nil, false, nil
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].p.Power > cs[j].p.Power })
	var total, lev int64
	for i, c := range cs {
		if i < gwPositions {
			h := c.h
			h.Pos = int64(i + 1)
			s.Holds = append(s.Holds, h)
			s.People = append(s.People, c.p)
		}
		total += c.p.Power
		lev += c.p.Lev
	}
	avgLev := max(1, lev/int64(len(cs)))
	for pos := len(s.Holds) + 1; pos <= gwPositions; pos++ {
		power := guildWarCfg.GuardPower[min(avgLev, int64(len(guildWarCfg.GuardPower)))-1]
		s.Holds = append(s.Holds, gwHold{Pos: int64(pos), Guard: true, Name: "中立守卫", Lev: avgLev, Face: store.DefaultFaceID, Power: power, FormType: 1, Hp: gwStars})
	}
	s.Rank = total // the ordering key until the ranks are numbered
	return s, true, nil
}

// match makes the war of a day: the guilds that can fight are put in pairs by their power; one left over has no opponent.
func (g *gwSvc) match(srv string, day time.Time) (*gwWar, error) {
	war := &gwWar{Key: dayKey(day)}
	guilds, _, err := g.st.GuildsOf(srv, "", 1, 1000)
	if err != nil {
		return nil, err
	}
	var sides []*gwSide
	for _, gr := range guilds {
		if gr.Lev < gwInt("limit_lev") {
			continue
		}
		s, ok, err := g.side(gr.Guild, day.Unix())
		if err != nil {
			return nil, err
		}
		if ok {
			sides = append(sides, s)
		}
	}
	sort.SliceStable(sides, func(i, j int) bool { return sides[i].Rank > sides[j].Rank })
	for i, s := range sides {
		s.Rank = int64(i + 1)
	}
	for i := 0; i+1 < len(sides); i += 2 {
		war.Pairs = append(war.Pairs, &gwPair{Sides: [2]gwSide{*sides[i], *sides[i+1]}})
	}
	return war, nil
}

// ensure brings the server's war up to the moment: the war of the day is made after its matching, the one before is settled
// when it is over. It returns the war (nil before any) and the day it belongs to.
func (g *gwSvc) ensure(srv string, now time.Time) (*gwWar, time.Time, bool, error) {
	war := &gwWar{}
	if _, err := g.st.LoadGlobal(srv, guildWarModule, war); err != nil {
		return nil, time.Time{}, false, err
	}
	day, ok := gwWarDay(now)
	if !ok {
		return war, time.Time{}, false, nil
	}
	changed := false
	if war.Key != dayKey(day) {
		if war.Key != 0 && !war.Done {
			if err := g.settleAll(srv, war); err != nil {
				return nil, day, false, err
			}
		}
		var err error
		if war, err = g.match(srv, day); err != nil {
			return nil, day, false, err
		}
		changed = true
	}
	if _, _, end, _ := gwTimeline(day); now.Unix() >= end && !war.Done {
		if err := g.settleAll(srv, war); err != nil {
			return nil, day, false, err
		}
		changed = true
	}
	if changed {
		if err := g.st.SaveGlobal(srv, guildWarModule, war); err != nil {
			return nil, day, false, err
		}
	}
	return war, day, true, nil
}

// settleAll ends every pair of a war: the result by the stars, the mails to the guilds and the members, the boxes.
func (g *gwSvc) settleAll(srv string, war *gwWar) error {
	for _, p := range war.Pairs {
		if p.Settled {
			continue
		}
		a, b := p.Sides[0].hp(), p.Sides[1].hp()
		switch {
		case a > b:
			p.Result = [2]int64{gwWin, gwLose}
		case a < b:
			p.Result = [2]int64{gwLose, gwWin}
		default:
			p.Result = [2]int64{gwDogfall, gwDogfall}
		}
		for i := range p.Sides {
			s := &p.Sides[i]
			var rids []int64
			for _, per := range s.People {
				rids = append(rids, per.RID)
			}
			reward, label := gwPairs("guild_lose_reward"), "公会战失败奖励"
			if p.Result[i] == gwWin {
				reward, label = gwPairs("guild_win_reward"), "公会战胜利奖励"
			}
			if len(rids) > 0 {
				m := store.Mail{SrvID: srv, Subject: label, Content: "本次公会战已经结束，这是全盟奖励。", FromName: "公会战", CreatedBy: "guildwar", Attachments: attachmentsOf(reward)}
				if _, err := g.st.CreateMail(m, rids); err != nil {
					return err
				}
			}
			// the war score ranks
			type row struct{ rid, score int64 }
			var rows []row
			for rid, sc := range s.Score {
				rows = append(rows, row{rid, sc})
			}
			sort.Slice(rows, func(x, y int) bool {
				if rows[x].score != rows[y].score {
					return rows[x].score > rows[y].score
				}
				return rows[x].rid < rows[y].rid
			})
			for rank, r := range rows {
				for _, aw := range guildWarCfg.RankAwards {
					if int64(rank+1) <= aw.Num {
						m := store.Mail{SrvID: srv, Subject: "公会战战绩奖励", Content: "公会战战绩排在公会第" + itoa64(int64(rank+1)) + "名，这是战绩奖励。",
							FromName: "公会战", CreatedBy: "guildwar", Attachments: attachmentsOf(aw.Items)}
						if _, err := g.st.CreateMail(m, []int64{r.rid}); err != nil {
							return err
						}
						break
					}
				}
			}
			for n := int64(1); n <= int64(len(s.People)); n++ {
				p.Boxes[i] = append(p.Boxes[i], gwBox{Order: n})
			}
		}
		p.Settled = true
	}
	war.Done = true
	return nil
}

// boxReward is what a box holds: the guild contribution (item 10), more from the gold box of the winners; one in ten holds
// five times as much ("a chance of a great deal of contribution", our numbers).
func boxReward(gold bool, rng *rand.Rand) int64 {
	n := int64(50)
	if gold {
		n = 100
	}
	if rng.Intn(10) == 0 {
		n *= 5
	}
	return n
}

// ---- messages ----

func (h gwHold) status() int64 {
	switch {
	case h.Hp <= 0:
		return 2 // fall
	case h.Hp < gwStars:
		return 1 // attacked
	}
	return 0
}

func (h gwHold) defenseMessage() proto.Message {
	return proto.Message{"pos": h.Pos, "rid": h.RID, "srv_id": "", "name": h.Name, "lev": h.Lev, "face": h.Face, "power": h.Power, "hp": h.Hp, "hp_max": gwStars,
		"relic_def_count": h.Relic, "status": h.status(), "face_update_time": 0, "face_file": ""}
}

func (s *gwSide) defense(srv string) []proto.Message {
	out := []proto.Message{}
	for _, h := range s.Holds {
		m := h.defenseMessage()
		m["srv_id"] = srv
		out = append(out, m)
	}
	return out
}

func (s *gwSide) top() []proto.Message {
	type row struct {
		rid, score int64
		name       string
	}
	var rows []row
	for _, p := range s.People {
		if sc := s.Score[p.RID]; sc > 0 {
			rows = append(rows, row{p.RID, sc, p.Name})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].rid < rows[j].rid
	})
	out := []proto.Message{}
	for i, r := range rows {
		if i == 3 {
			break
		}
		out = append(out, proto.Message{"rank": i + 1, "name": r.name})
	}
	return out
}

func (g *gwSvc) heroMessages(l []arenaHero) []proto.Message {
	out := []proto.Message{}
	for i, h := range l {
		out = append(out, proto.Message{"pos": h.Pos, "id": i + 1, "bid": h.BID, "lev": h.Lev, "star": h.Star, "use_skin": 0, "end_time": 0, "resonate_lev": 0,
			"ext_data": []proto.Message{}})
	}
	return out
}

// ---- the handlers ----

// guildWarCtx is what a request needs: the role's side of the war.
type gwCtx struct {
	role    *store.Role
	war     *gwWar
	day     time.Time
	pair    *gwPair
	me, foe int
	status  int64
	to      int64
	now     int64
}

func (g *gwSvc) with(s *gateway.Session, fn func(c *gwCtx) (save bool, err error)) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	gd, _, err := g.st.GuildOfRole(role.RID)
	c := &gwCtx{role: role, now: s.Now().Unix()}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	war, day, ok, err := g.ensure(role.SrvID, s.Now())
	if err != nil {
		return err
	}
	c.war, c.day = war, day
	if ok {
		c.status, c.to = gwPhase(day, c.now)
	} else {
		c.status = gwClose
	}
	if gd != nil && ok {
		if p, i := war.pairOf(gd.GID); p != nil {
			c.pair, c.me, c.foe = p, i, 1-i
		}
	}
	if c.pair == nil && c.status != gwClose && c.status != gwShowing {
		c.status = gwClose
	}
	save, err := fn(c)
	if err != nil || !save {
		return err
	}
	return g.st.SaveGlobal(role.SrvID, guildWarModule, war)
}

func (c *gwCtx) result() int64 {
	if c.pair == nil || !c.pair.Settled {
		return gwFighting
	}
	return c.pair.Result[c.me]
}

func (g *gwSvc) base(c *gwCtx) proto.Message {
	return proto.Message{"result": c.result(), "hp": c.pair.Sides[c.me].hp(), "buff_lev": c.pair.Sides[c.me].Buff, "hp2": c.pair.Sides[c.foe].hp(), "ranks": c.pair.Sides[c.me].top()}
}

func (g *gwSvc) pushSide(p *gwPair, idx int, cmd uint16, m proto.Message) {
	for _, per := range p.Sides[idx].People {
		if sess := g.hub.Online(per.RID); sess != nil {
			_ = sess.Send(cmd, m)
		}
	}
}

// Register is RegisterGuildWar.
func RegisterGuildWar(r *gateway.Router, st *store.Store, hub *gateway.Hub) {
	g := &gwSvc{st: st, fights: fightServiceOf(st), hub: hub, rng: newRng}
	r.Handle(24204, g.status)
	r.Handle(24205, g.battleList)
	r.Handle(24200, g.info)
	r.Handle(24201, g.position)
	r.Handle(24202, g.attack)
	r.Handle(24208, g.mine)
	r.Handle(24209, g.defend)
	r.Handle(24212, g.logs)
	r.Handle(24213, g.ranks)
	r.Handle(24220, g.boxes)
	r.Handle(24221, g.openBox)
}

func (g *gwSvc) status(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		flag, start, end := int64(0), int64(0), int64(0)
		if c.pair != nil {
			flag = 1
		}
		if c.status != gwClose || c.pair != nil {
			match, st, e, boxEnd := gwTimeline(c.day)
			start = match
			switch c.status {
			case gwShowing:
				start, end = match, st
			case gwProcessing:
				start, end = st, e
			case gwSettlement:
				start, end = e, boxEnd
			}
		}
		return false, s.Send(24204, proto.Message{"status": c.status, "flag": flag, "start_time": start, "end_time": end})
	})
}

func (g *gwSvc) battleList(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		list := []proto.Message{}
		if c.war != nil {
			for _, p := range c.war.Pairs {
				a, b := p.Sides[0], p.Sides[1]
				m := proto.Message{"g_id1": a.GID, "g_sid1": a.SrvID, "srv_name1": a.SrvID, "guild_name1": a.Name, "rank1": a.Rank,
					"g_id2": b.GID, "g_sid2": b.SrvID, "srv_name2": b.SrvID, "guild_name2": b.Name, "rank2": b.Rank, "status": 0, "g_id": 0, "g_sid": ""}
				if p.Settled {
					m["status"] = 1
					switch {
					case p.Result[0] == gwWin:
						m["g_id"], m["g_sid"] = a.GID, a.SrvID
					case p.Result[1] == gwWin:
						m["g_id"], m["g_sid"] = b.GID, b.SrvID
					}
				}
				list = append(list, m)
			}
		}
		return false, s.Send(24205, proto.Message{"match_info": list})
	})
}

func (g *gwSvc) info(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		if c.pair == nil {
			return false, s.Send(24200, proto.Message{"count": 0, "result": gwFighting, "ranks": []proto.Message{}, "gname1": "", "hp1": 0, "buff_lev1": 0, "gname2": "",
				"g_id": 0, "g_sid": "", "hp2": 0, "defense": []proto.Message{}})
		}
		me, foe := &c.pair.Sides[c.me], &c.pair.Sides[c.foe]
		return false, s.Send(24200, proto.Message{"count": me.Used[c.role.RID], "result": c.result(), "ranks": me.top(), "gname1": me.Name, "hp1": me.hp(), "buff_lev1": me.Buff,
			"gname2": foe.Name, "g_id": foe.GID, "g_sid": foe.SrvID, "hp2": foe.hp(), "defense": foe.defense(foe.SrvID)})
	})
}

func (g *gwSvc) holdOf(side *gwSide, pos int64) *gwHold {
	if pos < 1 || pos > int64(len(side.Holds)) {
		return nil
	}
	return &side.Holds[pos-1]
}

func (g *gwSvc) position(s *gateway.Session, req proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		if c.pair == nil {
			return false, nil
		}
		foe := &c.pair.Sides[c.foe]
		h := g.holdOf(foe, req.Int("pos"))
		if h == nil {
			return false, nil
		}
		return false, s.Send(24201, proto.Message{"pos": h.Pos, "rid": h.RID, "srv_id": foe.SrvID, "name": h.Name, "lev": h.Lev, "face": h.Face, "power": h.Power,
			"formation_type": h.FormType, "formation_lev": 1, "hp": h.Hp, "hp_max": gwStars, "def_count": h.Def, "relic_def_count": h.Relic,
			"defense": g.heroMessages(h.Lineup), "face_update_time": 0, "face_file": ""})
	})
}

func (g *gwSvc) mine(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		if c.pair == nil {
			return false, s.Send(24208, proto.Message{"hp": 0, "defense": []proto.Message{}, "avg_power": 0})
		}
		me := &c.pair.Sides[c.me]
		var power int64
		for _, p := range me.People {
			power += p.Power
		}
		avg := int64(0)
		if len(me.People) > 0 {
			avg = power / int64(len(me.People))
		}
		return false, s.Send(24208, proto.Message{"hp": me.hp(), "defense": me.defense(me.SrvID), "avg_power": avg})
	})
}

func (g *gwSvc) defend(s *gateway.Session, req proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		empty := proto.Message{"power": 0, "formation_type": 1, "formation_lev": 1, "defense": []proto.Message{}, "guild_war_role_log": []proto.Message{}}
		if c.pair == nil {
			return false, s.Send(24209, empty)
		}
		idx := c.me
		if req.Int("g_id1") == c.pair.Sides[c.foe].GID {
			idx = c.foe
		}
		h := g.holdOf(&c.pair.Sides[idx], req.Int("pos"))
		if h == nil {
			return false, s.Send(24209, empty)
		}
		logs := []proto.Message{}
		for _, l := range c.pair.Logs {
			if l.Side != 1-idx || l.Pos != h.Pos || l.Ruin {
				continue
			}
			res := int64(2)
			if l.Win {
				res = 1
			}
			logs = append(logs, proto.Message{"rid": l.ARID, "srv_id": c.pair.Sides[1-idx].SrvID, "name": l.AName, "lev": l.ALev, "face": l.Face, "power": l.Power, "time": l.Time,
				"repaly_id": 0, "hp": l.Stars, "result": res, "formation_type": l.FormType, "formation_lev": 1, "defense": g.heroMessages(l.Lineup),
				"face_update_time": 0, "face_file": ""})
		}
		return false, s.Send(24209, proto.Message{"power": h.Power, "formation_type": h.FormType, "formation_lev": 1, "defense": g.heroMessages(h.Lineup), "guild_war_role_log": logs})
	})
}

func (g *gwSvc) logs(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		out := []proto.Message{}
		if c.pair != nil {
			foe := &c.pair.Sides[c.foe]
			for i := len(c.pair.Logs) - 1; i >= 0; i-- {
				l := c.pair.Logs[i]
				own := l.Side == c.me
				flag2 := int64(1)
				if l.Ruin {
					flag2 = 2
				}
				m := proto.Message{"flag1": b2i(own), "flag2": flag2, "time": l.Time, "srv_id": foe.SrvID, "gname": foe.Name,
					"int_args": []proto.Message{{"key": 1, "val": l.Stars}, {"key": 2, "val": l.Score}, {"key": 3, "val": c.pair.Sides[c.foe].hp()}, {"key": 4, "val": l.Buff},
						{"key": 5, "val": b2i(l.Win)}, {"key": 6, "val": c.pair.Sides[c.me].hp()}},
					"str_args": []proto.Message{}}
				if own { // the attacker is of ours: name1 is the attacker, name2 the position
					m["rid1"], m["srv_id1"], m["name1"], m["rid2"], m["srv_id2"], m["name2"] = l.ARID, c.pair.Sides[c.me].SrvID, l.AName, l.DRID, foe.SrvID, l.DName
				} else {
					m["rid1"], m["srv_id1"], m["name1"], m["rid2"], m["srv_id2"], m["name2"] = l.DRID, c.pair.Sides[c.me].SrvID, l.DName, l.ARID, foe.SrvID, l.AName
				}
				m["rid"] = l.ARID
				out = append(out, m)
			}
		}
		return false, s.Send(24212, proto.Message{"guild_war_log": out})
	})
}

func (g *gwSvc) ranks(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		out := []proto.Message{}
		if c.pair != nil {
			me := &c.pair.Sides[c.me]
			ps := append([]gwPerson(nil), me.People...)
			sort.SliceStable(ps, func(i, j int) bool {
				if me.Score[ps[i].RID] != me.Score[ps[j].RID] {
					return me.Score[ps[i].RID] > me.Score[ps[j].RID]
				}
				return ps[i].RID < ps[j].RID
			})
			for i, p := range ps {
				out = append(out, proto.Message{"rank": i + 1, "rid": p.RID, "srv_id": me.SrvID, "face": p.Face, "lev": p.Lev, "name": p.Name, "star": me.Stars[p.RID],
					"war_score": me.Score[p.RID], "look_id": 0, "face_update_time": 0, "face_file": ""})
			}
		}
		return false, s.Send(24213, proto.Message{"ranks": out})
	})
}

func boxMessage(b gwBox, srv string) proto.Message {
	return proto.Message{"order": b.Order, "rid": b.RID, "sid": srv, "name": b.Name, "item_id": b.Item, "item_num": b.Num}
}

func (g *gwSvc) boxes(s *gateway.Session, _ proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		list := []proto.Message{}
		result, status, end := int64(0), int64(0), int64(0)
		if c.pair != nil && c.pair.Settled {
			result = int64(b2i(c.pair.Result[c.me] == gwWin)) // the box kind: 1 gold for the winners, 0 bronze
			_, _, _, boxEnd := gwTimeline(c.day)
			end = boxEnd
			if c.now < boxEnd {
				status = 1
			}
			for _, b := range c.pair.Boxes[c.me] { // the boxes that are not opened have no role
				list = append(list, boxMessage(b, c.pair.Sides[c.me].SrvID))
			}
		}
		return false, s.Send(24220, proto.Message{"result": result, "end_time": end, "status": status, "guild_war_box": list})
	})
}

func (g *gwSvc) openBox(s *gateway.Session, req proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		refuse := func(msg string) (bool, error) { return false, s.Send(24221, proto.Message{"result": 0, "msg": msg}) }
		if c.pair == nil || !c.pair.Settled {
			return refuse("没有可以开启的宝箱")
		}
		_, _, _, boxEnd := gwTimeline(c.day)
		if c.now >= boxEnd {
			return refuse("宝箱已经过期")
		}
		me := &c.pair.Sides[c.me]
		isMember := false
		for _, p := range me.People {
			isMember = isMember || p.RID == c.role.RID
		}
		if !isMember {
			return refuse("只有参战的活跃成员才可以开启宝箱")
		}
		boxes := c.pair.Boxes[c.me]
		for _, b := range boxes {
			if b.RID == c.role.RID {
				return refuse("你已经开启过宝箱了")
			}
		}
		order := req.Int("order")
		if order < 1 || order > int64(len(boxes)) || boxes[order-1].RID != 0 {
			return refuse("这个宝箱已经被开启")
		}
		num := boxReward(c.pair.Result[c.me] == gwWin, g.rng())
		if err := g.st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{guildLabel(10): num}, map[int64]int64{}); err != nil {
			return false, err
		}
		boxes[order-1] = gwBox{Order: order, RID: c.role.RID, Name: c.role.Name, Item: 10, Num: num}
		if err := s.Send(24221, proto.Message{"result": 1, "msg": "开启成功"}); err != nil {
			return true, err
		}
		g.pushSide(c.pair, c.me, 24223, boxMessage(boxes[order-1], me.SrvID))
		return true, sendAssets(s, g.st, c.role.RID)
	})
}

// scale multiplies the strength of fighters by permille/1000.
func scaleFoes(foes []fightUnit, objs []proto.Message, permille int64) {
	for i := range foes {
		foes[i].hp = foes[i].hp * permille / 1000
		foes[i].maxHP = foes[i].maxHP * permille / 1000
		foes[i].atk = foes[i].atk * permille / 1000
	}
	for _, o := range objs {
		o["hp"], o["hp_max"] = o.Int("hp")*permille/1000, o.Int("hp_max")*permille/1000
	}
}

var gwDiffName = [4]string{"", "easy", "normal", "hard"}

func (g *gwSvc) attack(s *gateway.Session, req proto.Message) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		refuse := func(msg string) (bool, error) {
			return false, s.Send(24202, proto.Message{"code": 0, "msg": msg, "count": 0})
		}
		switch {
		case c.pair == nil:
			return refuse("你的公会没有参加本次公会战")
		case c.status != gwProcessing:
			return refuse("公会战还没有开始或已经结束")
		}
		me, foe := &c.pair.Sides[c.me], &c.pair.Sides[c.foe]
		isMember := false
		for _, p := range me.People {
			isMember = isMember || p.RID == c.role.RID
		}
		diff, ruin := req.Int("hp"), req.Int("flag") == 1
		h := g.holdOf(foe, req.Int("pos"))
		switch {
		case !isMember:
			return refuse("你没有参加本次公会战")
		case me.Used[c.role.RID] >= gwInt("challange_time_limit"):
			return refuse("挑战次数不足")
		case h == nil:
			return refuse("据点不存在")
		case ruin && h.Hp > 0:
			return refuse("这个据点还没有沦陷")
		case ruin && h.Relic >= gwInt("ruins_challange_limit"):
			return refuse("废墟的挑战次数已满")
		case !ruin && h.Hp <= 0:
			return refuse("这个据点已经沦陷")
		case !ruin && (diff < 1 || diff > 3 || diff > h.Hp):
			return refuse("难度不可选")
		}
		team, objects, form, err := g.fights.teamSide(c.role)
		if err != nil {
			return false, err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		// our guild's buff makes the heroes stronger
		if me.Buff > 0 {
			b := guildWarCfg.Buff[me.Buff-1]
			for i := range team {
				team[i].atk = team[i].atk * (1000 + b["atk_per"]) / 1000
				team[i].hp = team[i].hp * (1000 + b["hp_max_per"]) / 1000
				team[i].maxHP = team[i].maxHP * (1000 + b["hp_max_per"]) / 1000
			}
		}
		// the holder: a member's line-up as it was at the matching, a guard is a copy of our own team a little weaker
		opp := arenaOpp{RID: h.RID, SrvID: foe.SrvID, Name: h.Name, Lev: h.Lev, Face: h.Face, Power: h.Power, Robot: true, FormType: h.FormType, Lineup: h.Lineup}
		factor := int64(1000)
		if h.Guard {
			mine, formType, _, err := lineupOf(g.st, c.role.RID)
			if err != nil {
				return false, err
			}
			opp.FormType, opp.Lineup = formType, nil
			for _, x := range mine {
				opp.Lineup = append(opp.Lineup, arenaHero{x.Pos, x.BID, max(1, x.Lev*80/100), x.Star, x.Break})
			}
			opp.RID = 0
		}
		if !ruin {
			factor = gwInt(gwDiffName[diff] + "_diff_addition")
			if h.Guard {
				factor = factor * 800 / 1000
			}
		} else {
			factor = 800
		}
		oslots, oform, obyID, err := opp.fighters(g.st)
		if err != nil {
			return false, err
		}
		foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
		if len(foes) == 0 {
			return refuse("守卫已无法应战")
		}
		scaleFoes(foes, foeObjects, factor)
		me.Used[c.role.RID]++
		rounds, won, _ := playFightOpts(team, foes, g.rng(), maxFightRounds, true)
		intro := proto.Message{
			"combat_type": combatGuildWar, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
			"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{{"param": h.Pos}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": c.role.Name,
			"target_role_name": h.Name, "suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(24202, proto.Message{"code": 1, "msg": "", "count": me.Used[c.role.RID]}); err != nil {
			return true, err
		}
		pos := h.Pos
		return true, g.fights.begin(s, c.role, combatGuildWar, intro, rounds, won, func(s *gateway.Session, won bool) error {
			return g.settle(s, pos, diff, ruin, won, opp, form.FormationType)
		})
	})
}

// settle applies a finished attack: the stars, the score, the rewards, the log and the news to both guilds.
func (g *gwSvc) settle(s *gateway.Session, pos, diff int64, ruin, won bool, opp arenaOpp, formType int64) error {
	return g.with(s, func(c *gwCtx) (bool, error) {
		if c.pair == nil {
			return false, nil
		}
		me, foe := &c.pair.Sides[c.me], &c.pair.Sides[c.foe]
		h := g.holdOf(foe, pos)
		if h == nil {
			return false, nil
		}
		var stars, score, buff int64
		var reward [][2]int64
		switch {
		case ruin && won:
			score = gwInt("win_ruins_record")
			h.Relic++
			me.Buff = min(me.Buff+1, int64(len(guildWarCfg.Buff)))
			buff = me.Buff
			reward = gwPairs("win_strongholds_reward_easy")
		case ruin:
			score = gwInt("lose_ruins_record")
			h.Relic++
			reward = gwPairs("lose_strongholds_reward_easy")
		case won:
			stars = min(diff, h.Hp)
			h.Hp -= stars
			score = guildWarCfg.Positions[pos-1][diff-1]
			reward = gwPairs("win_strongholds_reward_" + gwDiffName[diff])
		default:
			score = gwInt("lose_guard_record")
			h.Def++
			reward = gwPairs("lose_strongholds_reward_" + gwDiffName[diff])
		}
		me.Score[c.role.RID] += score
		me.Stars[c.role.RID] += stars
		lineup, _, power, err := lineupOf(g.st, c.role.RID)
		if err != nil {
			return false, err
		}
		c.pair.Logs = append(c.pair.Logs, gwLog{Time: c.now, Side: c.me, ARID: c.role.RID, DRID: h.RID, AName: c.role.Name, DName: h.Name, Pos: pos, Ruin: ruin, Win: won,
			Stars: stars, Score: score, Buff: buff, StarsA: me.hp(), StarsB: foe.hp(), ALev: int64(c.role.Lev), Power: power, Face: int64(c.role.FaceID), FormType: formType, Lineup: lineup})
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(reward), 1, ga, gi)
		if err := g.st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return false, err
		}
		items := []proto.Message{}
		for _, r := range reward {
			items = append(items, proto.Message{"bid": r[0], "is_bind": 1, "num": r[1]})
		}
		result := int64(2)
		if won {
			result = 1
		}
		if err := s.Send(24203, proto.Message{"result": result, "timer": 0, "item_rewards": items, "all_dps": 0, "best_partner": 0, "bid": 0, "lev": 0, "star": 0, "use_skin": 0,
			"target_role_name": h.Name, "hurt_statistics": []proto.Message{}, "replay_id": 0}); err != nil {
			return true, err
		}
		if err := sendAssets(s, g.st, c.role.RID); err != nil {
			return true, err
		}
		// the news: the position changed (to the guild that owns it as "mine", to the other as "enemy"), the totals
		g.pushSide(c.pair, c.foe, 24206, proto.Message{"flag": 1, "defense": []proto.Message{foe.defenseOne(h)}})
		g.pushSide(c.pair, c.me, 24206, proto.Message{"flag": 0, "defense": []proto.Message{foe.defenseOne(h)}})
		g.pushSide(c.pair, c.me, 24207, g.base(c))
		o := &gwCtx{pair: c.pair, me: c.foe, foe: c.me}
		g.pushSide(c.pair, c.foe, 24207, proto.Message{"hp": foe.hp(), "buff_lev": foe.Buff, "hp2": me.hp(), "ranks": foe.top(), "result": o.result()})
		return true, nil
	})
}

func (s *gwSide) defenseOne(h *gwHold) proto.Message {
	m := h.defenseMessage()
	m["srv_id"] = s.SrvID
	return m
}
