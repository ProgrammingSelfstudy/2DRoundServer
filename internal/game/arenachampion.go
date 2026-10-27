package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The arena champion match (冠军赛, 20250-20282) of one server. On the third and the sixth day of the arena season (Wednesday
// and Saturday) the best 128 of the arena fight a selection of six rounds, the best 32 of it fight out four groups of eight
// (three rounds) and the four group winners the semifinals and the final (two rounds). A round takes 300 seconds: 30 to get
// ready, 90 to bet, 180 to fight. The numbers the client names (tools/extract_arenachampion.py) are quoted; the rest is ours
// and is listed in OPEN_QUESTIONS. Everything is worked out when somebody asks (or the ticker looks), from the clock.

//go:embed data/arenachampion.json
var champJSON []byte

const (
	champModule    = "arenachamp"
	combatChampion = 10 // BattleConst.Fight_Type.Champion
	champKind      = 6  // PartnerConst.Fun_Form.ArenaChampion: the formation the match uses
	champRobotBase = 4200000000
	champSize      = 128
	champGroups    = 4
	champStepScore = 1
	champStep32    = 32
	champStep4     = 4
	champScoreRnds = 6
	champRounds    = 11 // 6 selection, 3 of the groups, 2 of the last four
	champWinPoints = 100
	champStartHour = 21
	champStartMin  = 5
	champGrace     = 2 * 3600 // an event found this long after its end is not played any more
	champOddsMin   = 1200
	champOddsMax   = 5000
	champOddsSeed  = 600 // virtual chips on each side of a bet pool, so that an empty one pays 2.0
)

type champConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Awards []struct {
		Min, Max int64
		Items    [][2]int64
	} `json:"awards"`
}

var champCfg = func() *champConf {
	c := &champConf{}
	if err := json.Unmarshal(champJSON, c); err != nil || len(c.Awards) == 0 || len(c.Const) == 0 {
		panic("game: bad arenachampion.json")
	}
	return c
}()

func (c *champConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *champConf) ids(key string) []int64 {
	var l []int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

// champOffsets are the start of every round in seconds after the start of the event, and the end of the last.
var champOffsets = func() []int64 {
	ready, guess, fight := champCfg.num("time_champion_ready"), champCfg.num("time_champion_guess"), champCfg.num("time_champion_fight")
	sReady, sGuess, sFight := champCfg.num("time_score_ready"), champCfg.num("time_score_guess"), champCfg.num("time_score_fight")
	var out []int64
	var at int64
	for i := 0; i < champRounds; i++ {
		out = append(out, at)
		if i < champScoreRnds {
			at += sReady + sGuess + sFight
		} else {
			at += ready + guess + fight
		}
	}
	return append(out, at)
}()

// champTimes are the phases of round i of an event that started at t0.
func champTimes(t0 int64, i int) (start, guess, fight, end int64) {
	ready, gl := champCfg.num("time_score_ready"), champCfg.num("time_score_guess")
	if i >= champScoreRnds {
		ready, gl = champCfg.num("time_champion_ready"), champCfg.num("time_champion_guess")
	}
	start = t0 + champOffsets[i]
	return start, start + ready, start + ready + gl, t0 + champOffsets[i+1]
}

// champEntrant is one of the 128: a role of the arena's best, or a robot copying one of them.
type champEntrant struct {
	arenaOpp
	Points int64 // selection points: a win is worth champWinPoints
	Wins   int64
	Fights int64
	Seed   int64 // place in the selection of the best 32, 0 for the rest
	Group  int64 // 1-4 in the groups
	Rank   int64 // place as it stands once out; alive ones are ranked by seed among those alive
	Out    bool
	OutAt  int // the round (counted from 1) it was knocked out in
}

// total is the selection points plus a share of the arena score (battel_score, per mille).
func (e *champEntrant) total() int64 { return e.Points + e.Score*champCfg.num("battel_score")/1000 }

// champMatch is one fight: A and B are entrants' indexes. Ret is 0 undecided, 1 A won, 2 B won.
type champMatch struct {
	Step, Round, Group, Pos int64
	A, B                    int
	Ret                     int64
	Replay                  int64
	Locked                  bool
	Time                    int64
	ALine, BLine            []arenaHero
	APow, BPow              int64
	AForm, BForm            int64
}

// champBet is a bet on the match of a round: Side 1 is A, 2 is B; Ratio is what it pays per thousand.
type champBet struct {
	Round, Match, Side, Amount, Ratio int64
	Get                               int64
	Paid                              bool
}

type champChips struct {
	Have    int64
	Granted int // rounds whose chips were handed out (the first round the role came to is the first one counted)
	Start   int
	Touched bool
}

type champTop struct {
	RID, Lev, Sex, Face, Rank int64
	SrvID, Name               string
}

type champEvent struct {
	Key     int64
	T0      int64
	Skip    bool // nobody of the server qualified: no event
	Done    bool
	Entries []champEntrant
	Rounds  [][]champMatch
	Paired  [champRounds]bool
	Locked  [champRounds]bool
	Applied [champRounds]bool
	Guess   [champRounds]int
	Groups  [champGroups][14]int // slots 1-14 of a group, -1 empty
	Top4    [7]int               // slots 1-7 of the last four, -1 empty
	Bets    map[int64][]champBet
	Chips   map[int64]*champChips
	Seen    map[int64]bool // roles that were shown their result
}

type champState struct {
	Ev   *champEvent
	Last []champTop
}

type champRole struct{ Best int64 }

type champSvc struct {
	st     *store.Store
	hub    *gateway.Hub
	mu     sync.Mutex
	rng    func() *rand.Rand
	now    func() time.Time
	pushed map[int64]string  // 20250 state last sent to a role
	cast   map[string]string // 20250 state last broadcast, by server
}

// RegisterArenaChampion registers the champion match: 20250 is only pushed (when the state changes), 20251-20263 and
// 20280-20281 are the windows. Run keeps the clients told while the match goes on.
func RegisterArenaChampion(r *gateway.Router, db *store.Store, hub *gateway.Hub) *champSvc {
	c := &champSvc{st: db, hub: hub, pushed: map[int64]string{}, cast: map[string]string{}, now: time.Now}
	fights := fightServiceOf(db)
	c.rng = func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.Handle(20250, func(s *gateway.Session, _ proto.Message) error { // the client asks at login; the tick would tell it in a few seconds
		role := s.Role()
		if role == nil {
			return nil
		}
		c.mu.Lock()
		stt, _, err := c.load(role.SrvID, c.now())
		if err != nil {
			c.mu.Unlock()
			return err
		}
		base := c.baseInfo(stt, c.now(), 0)
		c.mu.Unlock()
		c.pushBase(s, base, champSnapshot(base))
		return nil
	})
	r.Handle(20251, c.roleInfo)
	r.Handle(20252, c.myMatch)
	r.Handle(20253, c.guessMatch)
	r.Handle(20254, c.bet)
	r.Handle(20255, c.myBets)
	r.Handle(20256, c.result)
	r.Handle(20258, c.myLog)
	r.Handle(20260, c.group32)
	r.Handle(20261, c.top4)
	r.Handle(20262, c.guessAt)
	r.Handle(20263, c.matchAt)
	r.Handle(20280, c.statues)
	r.Handle(20281, c.ranking)
	return c
}

// Run looks at the clock every few seconds and tells the online roles when the match moves on, until stop is closed.
func (c *champSvc) Run(stop <-chan struct{}) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c.tick()
		}
	}
}

// tick tells the online roles of every server what changed since the last look.
func (c *champSvc) tick() {
	if c.hub == nil {
		return
	}
	bySrv := map[string][]*gateway.Session{}
	c.hub.Each(func(rid int64, s *gateway.Session) {
		if role := s.Role(); role != nil {
			bySrv[role.SrvID] = append(bySrv[role.SrvID], s)
		}
	})
	now := c.now()
	for srv, sessions := range bySrv {
		c.mu.Lock()
		stt, finished, err := c.load(srv, now)
		if err != nil {
			c.mu.Unlock()
			continue
		}
		base := c.baseInfo(stt, now, 0)
		key := champSnapshot(base)
		changed := c.cast[srv] != key
		c.cast[srv] = key
		if changed { // the clients refetch their windows on a flag, and play the replay of a fight that begins
			base["flag"] = 1
			if base.Int("round_status") == 3 {
				base["flag"] = 2
			}
		}
		var top proto.Message
		if finished {
			top = c.topMessage(stt, now)
		}
		c.mu.Unlock()
		for _, s := range sessions {
			if role := s.Role(); role != nil {
				c.mu.Lock()
				told := c.pushed[role.RID] == key
				c.mu.Unlock()
				if changed || !told { // a role that came in meanwhile is told too
					m := base
					if !changed {
						m = proto.Message{}
						for k, v := range base {
							m[k] = v
						}
						m["flag"] = 0
					}
					c.pushBase(s, m, key)
				}
			}
			if top != nil {
				_ = s.Send(20282, top)
			}
		}
	}
}

func (c *champSvc) pushBase(s *gateway.Session, base proto.Message, key string) {
	if role := s.Role(); role != nil {
		c.mu.Lock()
		c.pushed[role.RID] = key
		c.mu.Unlock()
	}
	_ = s.Send(20250, base)
}

func champSnapshot(base proto.Message) string {
	return fmt.Sprintf("%d/%d/%d/%d/%d", base.Int("step"), base.Int("step_status"), base.Int("round"), base.Int("round_status"), base.Int("start_time"))
}

// ---- the calendar

// champEventAt gives the start of the last event that began at or before now (0 if none lately) and of the next one.
func champEventAt(now time.Time) (last, next int64) {
	today := dayStart(now.In(dayZone))
	for d := -9; d <= 9; d++ {
		day := today.AddDate(0, 0, d)
		if w := day.Weekday(); w != time.Wednesday && w != time.Saturday {
			continue
		}
		t0 := day.Add(time.Duration(champStartHour)*time.Hour + time.Duration(champStartMin)*time.Minute).Unix()
		if t0 <= now.Unix() {
			last = t0
		} else if next == 0 {
			next = t0
		}
	}
	return last, next
}

func champLength() int64 { return champOffsets[champRounds] }

// champEndShown is when the "match is over" shows give way to "not open": the end of the day after the match.
func champEndShown(t0 int64) int64 {
	day := dayStart(time.Unix(t0, 0).In(dayZone))
	return day.AddDate(0, 0, 2).Unix() - 1
}

// ---- the state

// load reads the state of a server and brings it up to date. finished is true when the event ended on this call.
func (c *champSvc) load(srv string, now time.Time) (*champState, bool, error) {
	stt := &champState{}
	if _, err := c.st.LoadGlobal(srv, champModule, stt); err != nil {
		return nil, false, err
	}
	dirty, finished := false, false
	last, _ := champEventAt(now)
	if last != 0 && now.Unix() <= last+champLength()+champGrace && (stt.Ev == nil || stt.Ev.T0 != last) {
		if stt.Ev != nil && !stt.Ev.Done && !stt.Ev.Skip {
			if _, err := c.advance(srv, stt, time.Unix(1<<40, 0)); err != nil {
				return nil, false, err
			}
		}
		ev, err := c.newEvent(srv, last)
		if err != nil {
			return nil, false, err
		}
		stt.Ev, dirty = ev, true
	}
	if ev := stt.Ev; ev != nil && !ev.Skip && !ev.Done {
		was := ev.Done
		d, err := c.advance(srv, stt, now)
		if err != nil {
			return nil, false, err
		}
		dirty = dirty || d
		finished = !was && ev.Done
	}
	if dirty {
		if err := c.st.SaveGlobal(srv, champModule, stt); err != nil {
			return nil, false, err
		}
	}
	return stt, finished, nil
}

// newEvent picks the 128: the best of the arena, then robots copying them.
func (c *champSvc) newEvent(srv string, t0 int64) (*champEvent, error) {
	ev := &champEvent{Key: dayKey(time.Unix(t0, 0)), T0: t0, Bets: map[int64][]champBet{}, Chips: map[int64]*champChips{}, Seen: map[int64]bool{}}
	rows, err := c.st.ArenaTop(srv, champSize)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		role, err := c.st.RoleByID(row.RID)
		if err != nil || role.Status != 1 {
			continue
		}
		o, ok, err := oppFromRoleKind(c.st, role, row.Score, champKind)
		if err != nil {
			return nil, err
		}
		if ok {
			ev.Entries = append(ev.Entries, champEntrant{arenaOpp: o})
		}
	}
	real := len(ev.Entries)
	if real == 0 {
		ev.Skip = true
		return ev, nil
	}
	rng := rand.New(rand.NewSource(t0))
	faces := champCfg.ids("face_list")
	down := champCfg.num("combat_down")
	for i := real; i < champSize; i++ {
		src := ev.Entries[i%real].arenaOpp
		o := arenaOpp{RID: champRobotBase + int64(i), SrvID: srv, Robot: true, FormType: src.FormType, Lev: src.Lev,
			Name: arenaRobotNames[rng.Intn(len(arenaRobotNames))], Sex: int64(rng.Intn(2) + 1), Face: faces[rng.Intn(len(faces))],
			Score: champCfg.num("base_score") + int64(rng.Intn(20))*champCfg.num("score_arg")}
		var all []store.Partner
		for _, h := range src.Lineup {
			lev := max(1, h.Lev*down/1000)
			o.Lineup = append(o.Lineup, arenaHero{h.Pos, h.BID, lev, h.Star, h.Break})
			all = append(all, store.Partner{BID: h.BID, Lev: int(lev), Star: int(h.Star), BreakLev: int(h.Break)})
		}
		o.Power = rolePower(all)
		ev.Entries = append(ev.Entries, champEntrant{arenaOpp: o})
	}
	for g := range ev.Groups {
		for s := range ev.Groups[g] {
			ev.Groups[g][s] = -1
		}
	}
	for s := range ev.Top4 {
		ev.Top4[s] = -1
	}
	ev.Rounds = make([][]champMatch, champRounds)
	return ev, nil
}

// advance does everything that is due at now: pairs a round when it begins, fights it when the time to get ready is up,
// and settles it when it ends. It reports whether anything changed.
func (c *champSvc) advance(srv string, stt *champState, now time.Time) (bool, error) {
	ev := stt.Ev
	changed := false
	for i := 0; i < champRounds; i++ {
		start, _, _, end := champTimes(ev.T0, i)
		if now.Unix() < start {
			break
		}
		if !ev.Paired[i] {
			c.pair(ev, i)
			ev.Paired[i], changed = true, true
		}
		lock := start + c.readyOf(i)
		if now.Unix() >= lock && !ev.Locked[i] {
			c.duels(ev, i, min(now.Unix(), end))
			ev.Locked[i], changed = true, true
		}
		if now.Unix() >= end && !ev.Applied[i] {
			c.apply(ev, i)
			ev.Applied[i], changed = true, true
		}
	}
	if ev.Applied[champRounds-1] && !ev.Done {
		if err := c.finish(srv, stt); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func (c *champSvc) readyOf(i int) int64 {
	if i < champScoreRnds {
		return champCfg.num("time_score_ready")
	}
	return champCfg.num("time_champion_ready")
}

// ---- pairing

func (c *champSvc) met(ev *champEvent, a, b int) bool {
	for i := 0; i < champScoreRnds; i++ {
		for _, m := range ev.Rounds[i] {
			if (m.A == a && m.B == b) || (m.A == b && m.B == a) {
				return true
			}
		}
	}
	return false
}

// ordered lists the entrants best first by the selection standing.
func (ev *champEvent) ordered() []int {
	idx := make([]int, len(ev.Entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		a, b := &ev.Entries[idx[x]], &ev.Entries[idx[y]]
		if a.total() != b.total() {
			return a.total() > b.total()
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return idx[x] < idx[y]
	})
	return idx
}

func (c *champSvc) pair(ev *champEvent, i int) {
	rnd := &ev.Rounds[i]
	switch {
	case i < champScoreRnds:
		order := ev.ordered()
		if i == 0 { // the first round meets neighbours of the arena's order
			for j := range order {
				order[j] = j
			}
		}
		used := map[int]bool{}
		for x := 0; x < len(order); x++ {
			a := order[x]
			if used[a] {
				continue
			}
			used[a] = true
			pick := -1
			for y := x + 1; y < len(order); y++ {
				if !used[order[y]] && !c.met(ev, a, order[y]) {
					pick = order[y]
					break
				}
			}
			if pick < 0 {
				for y := x + 1; y < len(order); y++ {
					if !used[order[y]] {
						pick = order[y]
						break
					}
				}
			}
			if pick < 0 {
				break
			}
			used[pick] = true
			*rnd = append(*rnd, champMatch{Step: champStepScore, Round: int64(i + 1), A: a, B: pick})
		}
	case i == champScoreRnds:
		c.seed(ev)
		for g := 0; g < champGroups; g++ {
			for pos := 1; pos <= 7; pos += 2 {
				*rnd = append(*rnd, champMatch{Step: champStep32, Round: 1, Group: int64(g + 1), Pos: int64(pos), A: ev.Groups[g][pos-1], B: ev.Groups[g][pos]})
			}
		}
	case i == champScoreRnds+1:
		for g := 0; g < champGroups; g++ {
			for pos := 9; pos <= 11; pos += 2 {
				*rnd = append(*rnd, champMatch{Step: champStep32, Round: 2, Group: int64(g + 1), Pos: int64(pos), A: ev.Groups[g][pos-1], B: ev.Groups[g][pos]})
			}
		}
	case i == champScoreRnds+2:
		for g := 0; g < champGroups; g++ {
			*rnd = append(*rnd, champMatch{Step: champStep32, Round: 3, Group: int64(g + 1), Pos: 13, A: ev.Groups[g][12], B: ev.Groups[g][13]})
		}
	case i == champScoreRnds+3:
		*rnd = append(*rnd, champMatch{Step: champStep4, Round: 1, Pos: 1, A: ev.Top4[0], B: ev.Top4[1]}, champMatch{Step: champStep4, Round: 1, Pos: 3, A: ev.Top4[2], B: ev.Top4[3]})
	default:
		*rnd = append(*rnd, champMatch{Step: champStep4, Round: 2, Pos: 5, A: ev.Top4[4], B: ev.Top4[5]})
	}
	ev.Guess[i] = c.chooseGuess(ev, i)
}

// seed takes the best 32 of the selection into four groups, snake fashion, and sets them against each other 1-8, 4-5,
// 3-6, 2-7 inside a group.
func (c *champSvc) seed(ev *champEvent) {
	order := ev.ordered()
	local := make([][]int, champGroups)
	for pos, idx := range order {
		e := &ev.Entries[idx]
		e.Rank = int64(pos + 1)
		if pos >= 32 {
			e.Out = true
			continue
		}
		e.Seed = int64(pos + 1)
		row, col := pos/champGroups, pos%champGroups
		g := col
		if row%2 == 1 {
			g = champGroups - 1 - col
		}
		e.Group = int64(g + 1)
		local[g] = append(local[g], idx)
	}
	slotOrder := []int{0, 7, 3, 4, 2, 5, 1, 6}
	for g := range local {
		for s, k := range slotOrder {
			ev.Groups[g][s] = local[g][k]
		}
	}
}

// chooseGuess picks the match of a round that can be bet on: one whose sides are about equally strong, widening the
// tolerance in the steps the client names (battel_guess).
func (c *champSvc) chooseGuess(ev *champEvent, i int) int {
	var bands [][2]int64
	_ = json.Unmarshal(champCfg.Const["battel_guess"], &bands)
	rng := rand.New(rand.NewSource(ev.T0 + int64(i)))
	for _, band := range bands {
		var ok []int
		for j, m := range ev.Rounds[i] {
			pa, pb := ev.Entries[m.A].Power, ev.Entries[m.B].Power
			hi, lo := max(pa, pb), min(pa, pb)
			diff := int64(0)
			if hi > 0 {
				diff = (hi - lo) * 100 / hi
			}
			if diff >= band[0] && diff <= band[1] {
				ok = append(ok, j)
			}
		}
		if len(ok) > 0 {
			return ok[rng.Intn(len(ok))]
		}
	}
	return rng.Intn(len(ev.Rounds[i]))
}

// ---- fighting

func (c *champSvc) refresh(e *champEntrant) {
	if e.Robot {
		return
	}
	role, err := c.st.RoleByID(e.RID)
	if err != nil {
		return
	}
	if o, ok, err := oppFromRoleKind(c.st, role, e.Score, champKind); err == nil && ok {
		e.arenaOpp = o
	}
}

// duels settles every match of a round: a match with a real role is fought for real and kept as a replay, the others
// are decided by the powers.
func (c *champSvc) duels(ev *champEvent, i int, now int64) {
	for j := range ev.Rounds[i] {
		m := &ev.Rounds[i][j]
		rng := rand.New(rand.NewSource(ev.T0*131 + int64(i)*997 + int64(j)))
		a, b := &ev.Entries[m.A], &ev.Entries[m.B]
		c.refresh(a)
		c.refresh(b)
		m.ALine, m.BLine, m.APow, m.BPow, m.AForm, m.BForm = a.Lineup, b.Lineup, a.Power, b.Power, a.FormType, b.FormType
		m.Time, m.Locked = now, true
		switch {
		case len(a.Lineup) == 0 && len(b.Lineup) == 0:
			m.Ret = 1
			if b.Seed != 0 && (a.Seed == 0 || b.Seed < a.Seed) {
				m.Ret = 2
			}
		case len(a.Lineup) == 0:
			m.Ret = 2
		case len(b.Lineup) == 0:
			m.Ret = 1
		case !a.Robot || !b.Robot:
			if !c.fight(m, a, b, rng, now) {
				m.Ret = c.roll(a, b, rng)
			}
		default:
			m.Ret = c.roll(a, b, rng)
		}
	}
}

// roll decides a fight of robots (or whatever could not be played) by the powers: the stronger wins more often.
func (c *champSvc) roll(a, b *champEntrant, rng *rand.Rand) int64 {
	pa, pb := max(a.Power, 1), max(b.Power, 1)
	if rng.Int63n(pa*pa+pb*pb) < pa*pa {
		return 1
	}
	return 2
}

// fight plays a match with the fight service and keeps the replay.
func (c *champSvc) fight(m *champMatch, a, b *champEntrant, rng *rand.Rand, now int64) bool {
	as, af, aby, err := a.fighters(c.st)
	if err != nil {
		return false
	}
	bs, bf, bby, err := b.fighters(c.st)
	if err != nil {
		return false
	}
	au, ao := sideUnits(a.RID, a.SrvID, as, af, aby, true)
	bu, bo := sideUnits(b.RID, b.SrvID, bs, bf, bby, false)
	if len(au) == 0 || len(bu) == 0 {
		return false
	}
	rounds, won := buildFight(au, bu, rng)
	intro := proto.Message{
		"combat_type": combatChampion, "formation": []proto.Message{{"group": 1, "formation_type": af, "formation_lev": 1}, {"group": 2, "formation_type": bf, "formation_lev": 1}},
		"objects": append(ao, bo...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(au), "actor_role_name": a.Name,
		"target_role_name": b.Name, "suppress": 0, "halo_list": []proto.Message{},
		"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	m.Ret = 2
	if won {
		m.Ret = 1
	}
	m.Replay = recordReplay(c.st, combatChampion, replaySide{RID: a.RID, SrvID: a.SrvID, Name: a.Name, Lev: a.Lev, Face: a.Face, Power: a.Power, Form: af},
		replaySide{RID: b.RID, SrvID: b.SrvID, Name: b.Name, Lev: b.Lev, Face: b.Face, Power: b.Power, Form: bf}, intro, rounds, won, now)
	return true
}

// apply puts a round that has ended into the standings: points, the slots of the next round, the ones that are out, the
// bets that were won.
func (c *champSvc) apply(ev *champEvent, i int) {
	for j := range ev.Rounds[i] {
		m := &ev.Rounds[i][j]
		w, l := m.A, m.B
		if m.Ret == 2 {
			w, l = m.B, m.A
		}
		ev.Entries[m.A].Fights++
		ev.Entries[m.B].Fights++
		ev.Entries[w].Wins++
		if m.Step == champStepScore {
			ev.Entries[w].Points += champWinPoints
			continue
		}
		ev.Entries[l].Out, ev.Entries[l].OutAt = true, i+1
		switch {
		case m.Step == champStep32 && m.Round == 1:
			ev.Groups[m.Group-1][8+(m.Pos+1)/2-1] = w
		case m.Step == champStep32 && m.Round == 2:
			ev.Groups[m.Group-1][12+(m.Pos-9)/2] = w
		case m.Step == champStep32 && m.Round == 3:
			ev.Top4[[]int{0, 2, 3, 1}[m.Group-1]] = w // the semifinals are G1-G4 and G2-G3
		case m.Step == champStep4 && m.Round == 1:
			ev.Top4[4+(m.Pos-1)/2] = w
		default:
			ev.Top4[6] = w
		}
	}
	if i >= champScoreRnds {
		c.rerank(ev, i)
	}
	for rid, bets := range ev.Bets {
		for k := range bets {
			b := &bets[k]
			if b.Paid || b.Round != int64(i) {
				continue
			}
			b.Paid = true
			if m := ev.Rounds[i][b.Match]; m.Ret == b.Side {
				b.Get = b.Amount * b.Ratio / 1000
				ev.chipsOf(rid).Have += b.Get
			}
		}
	}
}

// rerank gives the ones just knocked out the places after the ones still alive, best seed first, and numbers the ones
// alive 1.. by seed.
func (c *champSvc) rerank(ev *champEvent, round int) {
	var alive, fresh []int
	for i := range ev.Entries {
		e := &ev.Entries[i]
		if e.Seed == 0 {
			continue
		}
		if !e.Out {
			alive = append(alive, i)
		} else if e.OutAt == round+1 {
			fresh = append(fresh, i)
		}
	}
	bySeed := func(l []int) {
		sort.Slice(l, func(x, y int) bool { return ev.Entries[l[x]].Seed < ev.Entries[l[y]].Seed })
	}
	bySeed(alive)
	bySeed(fresh)
	n := len(alive) + len(fresh)
	for k, i := range fresh {
		ev.Entries[i].Rank = int64(n - len(fresh) + k + 1)
	}
	for k, i := range alive {
		ev.Entries[i].Rank = int64(k + 1)
	}
}

func (ev *champEvent) chipsOf(rid int64) *champChips {
	ch := ev.Chips[rid]
	if ch == nil {
		ch = &champChips{}
		ev.Chips[rid] = ch
	}
	return ch
}

// finish ends the event: final places, the statues, and the rewards by mail.
func (c *champSvc) finish(srv string, stt *champState) error {
	ev := stt.Ev
	// the winner of the final is first and the loser second; rerank left the winner at 1 and the loser out at 2
	order := make([]int, len(ev.Entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return ev.Entries[order[x]].Rank < ev.Entries[order[y]].Rank })
	stt.Last = nil
	for _, idx := range order {
		e := ev.Entries[idx]
		if e.Rank >= 1 && e.Rank <= 3 {
			stt.Last = append(stt.Last, champTop{RID: e.RID, SrvID: e.SrvID, Name: e.Name, Lev: e.Lev, Sex: e.Sex, Face: e.Face, Rank: e.Rank})
		}
	}
	for _, e := range ev.Entries {
		if e.Robot {
			continue
		}
		var items [][2]int64
		for _, a := range champCfg.Awards {
			if e.Rank >= a.Min && e.Rank <= a.Max {
				items = append(items, a.Items...)
			}
		}
		if ch := ev.Chips[e.RID]; ch != nil && ch.Have > 0 {
			items = append(items, [2]int64{8, ch.Have})
			ch.Have = 0
		}
		m := store.Mail{SrvID: srv, Subject: "冠军赛奖励", Content: "本次冠军赛你的最终名次是第" + itoa64(e.Rank) + "名，这是名次奖励和竞猜币换成的竞技声望。",
			FromName: "冠军赛", CreatedBy: "arenachamp", Attachments: attachmentsOf(items)}
		if len(m.Attachments) > 0 {
			if _, err := c.st.CreateMail(m, []int64{e.RID}); err != nil {
				return err
			}
		}
		role := &champRole{}
		if _, err := c.st.LoadState(e.RID, champModule, role); err != nil {
			return err
		}
		if role.Best == 0 || e.Rank < role.Best {
			role.Best = e.Rank
			if err := c.st.SaveState(e.RID, champModule, role); err != nil {
				return err
			}
		}
	}
	for rid, ch := range ev.Chips { // the ones who only watched and bet
		if ch.Have <= 0 {
			continue
		}
		m := store.Mail{SrvID: srv, Subject: "冠军赛竞猜奖励", Content: "冠军赛结束了，你的竞猜币已换成竞技声望。", FromName: "冠军赛", CreatedBy: "arenachamp",
			Attachments: attachmentsOf([][2]int64{{8, ch.Have}})}
		if _, err := c.st.CreateMail(m, []int64{rid}); err != nil {
			return err
		}
		ch.Have = 0
	}
	ev.Done = true
	return nil
}
