package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// monopoly.json: the fire festival of the client (tools/extract_monopoly.py).
//
//go:embed data/monopoly.json
var monopolyJSON []byte

const (
	monoModule = "monopoly"
	monoCells  = 50 // every layout is a ring of fifty cells
	monoMaps   = 4
	monoItem   = 80244 // the festival's coin, what the shop takes
	monoTicket = 80245 // the challenge ticket
	monoDice   = 80246 // the ghost-head machine: a random step
	monoSecret = 80247 // the map: a chosen step

	// the event types of a cell (MonopolyConst.Event_Type)
	evNormal, evTrap, evAward, evMorra, evDialog, evRedbag, evBoss = 1, 2, 3, 4, 5, 6, 7
	evAdvance, evBuff, evWish, evMedicine, evEnd, evFlag, evStart  = 8, 9, 10, 11, 12, 13, 14

	// the buff kinds of 27408, in the order of the client's icons
	bfAdvance, bfWish, bfBuff, bfMedicine = 1, 2, 3, 4

	monoAtkUnit = 50  // one point of the guild attack buff
	monoHPUnit  = 500 // one point of the guild hp buff
)

type monoMapCfg struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	MaxDevelop int64  `json:"max_develop"`
	Need       int64  `json:"need"`
}

type monoBossCfg struct {
	BossID  int64     `json:"boss_id"`
	Name    string    `json:"name"`
	Develop int64     `json:"develop"`
	Drop    [][]int64 `json:"drop"`
	Reward  [][]int64 `json:"reward"`
}

type monoConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Maps   []monoMapCfg               `json:"maps"`
	Bosses [][]monoBossCfg            `json:"bosses"`
	Dramas []struct {
		Type   int64               `json:"type"`
		Reward [][]json.RawMessage `json:"reward"`
	} `json:"dramas"`
	Talks []struct {
		ID, Step, Answers int64
	} `json:"talks"`
	Awards []struct {
		Items    [][]int64
		Min, Max int64
	} `json:"awards"`
}

var monoCfg = func() *monoConf {
	c := &monoConf{}
	if err := json.Unmarshal(monopolyJSON, c); err != nil || len(c.Maps) != monoMaps || len(c.Bosses) != monoMaps {
		panic("game: bad monopoly.json")
	}
	return c
}()

func (c *monoConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

func (c *monoConf) pair(name string) (lo, hi int64) {
	var p []int64
	_ = json.Unmarshal(c.Const[name], &p)
	if len(p) < 2 {
		return 1, 1
	}
	return p[0], p[1]
}

// the cells of a lap that are not plain: type -> how many (our layout, the client only draws what it is sent)
var monoLayout = []struct{ Type, Count int }{
	{evTrap, 4}, {evAward, 6}, {evMorra, 3}, {evDialog, 5}, {evRedbag, 2}, {evBoss, 2},
	{evAdvance, 2}, {evBuff, 2}, {evWish, 2}, {evMedicine, 2}, {evFlag, 2},
}

type monoEvt struct {
	Pos  int64 `json:"pos"`
	Type int64 `json:"type"`
}

type monoMapState struct {
	Layout  int64 // the client's map picture of the lap
	Lap     int64
	Pos     int64
	Now     int64 // the event of the cell the role stands on, 0 = settled
	Pct     int64 // the strength of the guard of that cell in per mille of the role's own power
	Events  []monoEvt
	Buffs   [5]int64 // by buff kind
	Flag    int64    // the step chosen on a flag cell
	Develop int64
	Seen    bool // the story talk of the map was shown
}

type monoState struct {
	Day   int64
	Cur   int64 // the map entered last, what the requests without an id are for
	Maps  map[string]*monoMapState
	Drama map[string]bool
}

type monoGuild struct {
	Dev  [monoMaps + 1]int64
	Atk  int64 // points of the attack buff
	HP   int64
	Boss map[string]*monoBoss        // "map:boss"
	Dmg  map[string]map[string]int64 // map -> rid -> damage
}

type monoBoss struct {
	HP     int64
	Killed bool
}

type monoSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterMonopoly registers the fire festival (27400-27410, 27500-27506): four maps of fifty cells walked with the
// ghost-head machine (a random step) or the map (a chosen one), the events of the cells (guards, rewards, a stone-paper-
// scissors, story talks, red bags, buffs, flags), the explore value that belongs to the guild and unlocks its guard
// bosses, the guild's attack and hp buffs and the guild's damage rankings. Our rules, not captured.
func RegisterMonopoly(r *gateway.Router, db *store.Store) {
	m := &monoSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(27400, m.info)
	r.Handle(27401, m.enter)
	r.Handle(27403, m.move)
	r.Handle(27404, m.event)
	r.Handle(27407, m.drama)
	r.Handle(27408, m.mapBuffs)
	r.Handle(27410, m.guards)
	m.registerBoss(r)
}

func monoKey(id int64) string { return strconv.FormatInt(id, 10) }

func (m *monoSvc) with(s *gateway.Session, fn func(role *store.Role, st *monoState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := m.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &monoState{}
	if _, err := m.st.LoadState(role.RID, monoModule, st); err != nil {
		return err
	}
	if st.Maps == nil {
		st.Maps = map[string]*monoMapState{}
	}
	if st.Drama == nil {
		st.Drama = map[string]bool{}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return m.st.SaveState(role.RID, monoModule, st)
}

func (st *monoState) mapOf(id int64) *monoMapState {
	ms := st.Maps[monoKey(id)]
	if ms == nil {
		ms = &monoMapState{Pos: 1}
		ms.relayout(id)
		st.Maps[monoKey(id)] = ms
	}
	return ms
}

// relayout deals a new lap: the picture rotates through the three of the map, the events are drawn again.
func (ms *monoMapState) relayout(id int64) {
	ms.Layout = (id-1)*3 + 1 + ms.Lap%3
	ms.Events = nil
	cells := newRng().Perm(monoCells - 2) // cells 2..49; the first is the start, the last the end
	n := 0
	ms.Events = append(ms.Events, monoEvt{Pos: 1, Type: evStart}, monoEvt{Pos: monoCells, Type: evEnd})
	for _, l := range monoLayout {
		for i := 0; i < l.Count; i++ {
			ms.Events = append(ms.Events, monoEvt{Pos: int64(cells[n] + 2), Type: int64(l.Type)})
			n++
		}
	}
}

func (ms *monoMapState) eventAt(pos int64) int64 {
	for _, e := range ms.Events {
		if e.Pos == pos {
			return e.Type
		}
	}
	return evNormal
}

func (ms *monoMapState) clearAt(pos int64) {
	for i := range ms.Events {
		if ms.Events[i].Pos == pos && ms.Events[i].Type != evStart && ms.Events[i].Type != evEnd {
			ms.Events = append(ms.Events[:i], ms.Events[i+1:]...)
			return
		}
	}
}

// ---- the guild's share ----

func monoGuildKey(gid int64) string { return "monopoly_g" + strconv.FormatInt(gid, 10) }

func (m *monoSvc) guildOf(rid int64) (*store.Guild, error) {
	g, _, err := m.st.GuildOfRole(rid)
	if err == store.ErrNotFound {
		return nil, nil
	}
	return g, err
}

func (m *monoSvc) loadGuild(g *store.Guild) (*monoGuild, error) {
	mg := &monoGuild{}
	if g != nil {
		if _, err := m.st.LoadGlobal(g.SrvID, monoGuildKey(g.GID), mg); err != nil {
			return nil, err
		}
	}
	if mg.Boss == nil {
		mg.Boss = map[string]*monoBoss{}
	}
	if mg.Dmg == nil {
		mg.Dmg = map[string]map[string]int64{}
	}
	return mg, nil
}

func (m *monoSvc) saveGuild(g *store.Guild, mg *monoGuild) error {
	return m.st.SaveGlobal(g.SrvID, monoGuildKey(g.GID), mg)
}

// unlocked is whether the guild may walk a map: the first always, the others once the one before is explored.
func (mg *monoGuild) unlocked(id int64) bool {
	if id < 1 || id > monoMaps {
		return false
	}
	if id == 1 {
		return true
	}
	return mg.Dev[id-1] >= monoCfg.Maps[id-1].Need
}

func (mg *monoGuild) addDevelop(id int64) {
	mg.Dev[id] = min(mg.Dev[id]+1, monoCfg.Maps[id-1].MaxDevelop)
}

// ---- helpers ----

func monoEnd(now time.Time) int64 {
	d := dayStart(now)
	days := (8 - int(d.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	return d.AddDate(0, 0, days).Unix()
}

func (m *monoSvc) open(role *store.Role) string {
	if int64(role.Lev) < monoCfg.num("open_lv_limit") {
		return "角色等级达到" + monoKey(monoCfg.num("open_lv_limit")) + "级开放和之国火祭"
	}
	return ""
}

// allowance tops the machine, the map and the tickets up once a day, so that the festival can be played without the
// activity tasks that hand them out in the original.
func (m *monoSvc) allowance(s *gateway.Session, role *store.Role, st *monoState) error {
	day := dayKey(s.Now())
	if st.Day == day {
		return nil
	}
	st.Day = day
	have, err := m.st.RoleItems(role.RID)
	if err != nil {
		return err
	}
	gain := map[int64]int64{}
	for id, to := range map[int64]int64{monoDice: 10, monoSecret: 3, monoTicket: 3} {
		if have[id] < to {
			gain[id] = to - have[id]
		}
	}
	if len(gain) == 0 {
		return nil
	}
	return m.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, gain)
}

func (m *monoSvc) gain(s *gateway.Session, role *store.Role, items [][2]int64) error {
	if len(items) == 0 {
		return nil
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(items), 1, ga, gi)
	if err := m.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, m.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, m.st)
}

func pairList(items [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, it := range items {
		out = append(out, proto.Message{"base_id": it[0], "num": it[1]})
	}
	return out
}

// ---- 27400 27401 ----

func (m *monoSvc) info(s *gateway.Session, _ proto.Message) error {
	return m.with(s, func(role *store.Role, st *monoState) error {
		if m.open(role) != "" {
			return s.Send(27400, proto.Message{"flag": 0, "end_time": 0, "stage": []proto.Message{}})
		}
		if err := m.allowance(s, role, st); err != nil {
			return err
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		stage := []proto.Message{}
		for _, c := range monoCfg.Maps {
			lock := int64(1)
			if mg.unlocked(c.ID) {
				lock = 0
			}
			stage = append(stage, proto.Message{"id": c.ID, "lock": lock, "develop": st.mapOf(c.ID).Develop, "guild_develop": mg.Dev[c.ID]})
		}
		return s.Send(27400, proto.Message{"flag": 1, "end_time": monoEnd(s.Now()), "stage": stage})
	})
}

func (m *monoSvc) mapMessage(ms *monoMapState, id int64, mg *monoGuild, flag int64, msg string) proto.Message {
	events := []proto.Message{}
	for _, e := range ms.Events {
		events = append(events, proto.Message{"pos": e.Pos, "type": e.Type})
	}
	return proto.Message{"flag": flag, "msg": msg, "id": id, "map_id": ms.Layout, "pos": ms.Pos, "develop": ms.Develop, "events": events,
		"now_type": ms.Now, "guild_develop": mg.Dev[id], "look_id": 0}
}

func (m *monoSvc) enter(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return m.with(s, func(role *store.Role, st *monoState) error {
		refuse := func(msg string) error { return s.Send(27401, proto.Message{"flag": 0, "msg": msg}) }
		if msg := m.open(role); msg != "" {
			return refuse(msg)
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		if !mg.unlocked(id) {
			return refuse("该地图尚未解锁")
		}
		st.Cur = id
		ms := st.mapOf(id)
		if err := s.Send(27401, m.mapMessage(ms, id, mg, 1, "")); err != nil {
			return err
		}
		if !ms.Seen {
			ms.Seen = true
			return s.Send(27406, proto.Message{"type": id})
		}
		return nil
	})
}

// ---- 27403: a step ----

func (m *monoSvc) move(s *gateway.Session, req proto.Message) error {
	kind, num := req.Int("type"), req.Int("num")
	return m.with(s, func(role *store.Role, st *monoState) error {
		refuse := func(msg string) error { return s.Send(27403, proto.Message{"flag": 0, "msg": msg}) }
		id := st.Cur
		if id == 0 {
			return refuse("请先进入地图")
		}
		ms := st.mapOf(id)
		if ms.Now != 0 {
			return refuse("当前有未完成的事件")
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		if !mg.unlocked(id) {
			return refuse("该地图尚未解锁")
		}
		spend := map[int64]int64{}
		lo, hi := monoCfg.pair("monopoly_dice_rand")
		switch kind {
		case 1:
			spend[monoDice] = 1
			num = lo + int64(newRng().Intn(int(hi-lo+1)))
		case 2:
			slo, shi := monoCfg.pair("monopoly_secret_rand")
			if num < slo || num > shi {
				return refuse("步数不对")
			}
			spend[monoSecret] = 1
		default:
			return refuse("参数错误")
		}
		if err := m.st.Exchange(role.RID, map[int64]int64{}, spend, map[int64]int64{}, map[int64]int64{}); err != nil {
			return refuse("道具不足")
		}
		steps := num
		if ms.Buffs[bfAdvance] > 0 { // the jet: the same walk once more
			ms.Buffs[bfAdvance]--
			steps *= 2
		}
		ms.Pos = (ms.Pos-1+steps)%monoCells + 1
		ms.Now = ms.eventAt(ms.Pos)
		if ms.Now == evTrap || ms.Now == evBoss {
			plo, phi := monoCfg.pair("monopoly_event_trap_power")
			if ms.Now == evBoss {
				plo, phi = monoCfg.pair("monopoly_event_boss_power")
			}
			ms.Pct = plo + int64(newRng().Intn(int(phi-plo+1)))
		}
		if g != nil {
			mg.addDevelop(id)
			if err := m.saveGuild(g, mg); err != nil {
				return err
			}
			ms.Develop = min(ms.Develop+1, monoCfg.Maps[id-1].MaxDevelop)
		}
		body := proto.Message{"flag": 1, "msg": "", "num": steps, "pos": ms.Pos, "develop": ms.Develop, "now_type": ms.Now, "id": id, "guild_develop": mg.Dev[id]}
		if err := s.Send(27403, body); err != nil {
			return err
		}
		if ms.Flag != 0 && ms.Flag == num { // the flag was set for this very step
			ms.Flag = 0
			prize := [][2]int64{{monoTicket, 2}, {monoItem, 30}}
			if err := m.gain(s, role, prize); err != nil {
				return err
			}
			return s.Send(27409, proto.Message{"id": id, "buffs": []proto.Message{}, "reward": pairList(prize)})
		}
		ms.Flag = 0
		return nil
	})
}

// ---- 27404: the event of the cell ----

func monoArgs(req proto.Message) map[int64][2]int64 {
	out := map[int64][2]int64{}
	for _, a := range req.Arr("args") {
		out[a.Int("type")] = [2]int64{a.Int("arg1"), a.Int("arg2")}
	}
	return out
}

// morra: 1 beats 2, 2 beats 3, 3 beats 1; the result is 1 for a win, 2 for a loss, 0 for a draw
func monoMorra(mine, theirs int64) int64 {
	switch {
	case mine == theirs:
		return 0
	case (mine == 1 && theirs == 2) || (mine == 2 && theirs == 3) || (mine == 3 && theirs == 1):
		return 1
	}
	return 2
}

// monoBuffOf is the buff a cell hands out.
var monoBuffOf = map[int64]int64{evAdvance: bfAdvance, evBuff: bfBuff, evWish: bfWish, evMedicine: bfMedicine}

// settle ends the event of the cell the role stands on: the prize (doubled by a blessing), the reply of 27404 and the
// push of 27409. The end cell deals the next lap and pays the guild's buffs.
func (m *monoSvc) settle(s *gateway.Session, role *store.Role, ms *monoMapState, id int64, prize [][2]int64, buffs []int64) error {
	if ms.Buffs[bfWish] > 0 && len(prize) > 0 {
		ms.Buffs[bfWish]--
		for i := range prize {
			prize[i][1] *= 2
		}
	}
	shown := []proto.Message{}
	for _, b := range buffs {
		ms.Buffs[b]++
		shown = append(shown, proto.Message{"type": b})
	}
	cell := ms.Now
	if cell != evStart && cell != evEnd {
		cell = evNormal
	}
	pos := ms.Pos
	ms.clearAt(pos)
	if ms.Now == evEnd {
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		if g != nil {
			mg, err := m.loadGuild(g)
			if err != nil {
				return err
			}
			mg.Atk, mg.HP = mg.Atk+3, mg.HP+3
			if err := m.saveGuild(g, mg); err != nil {
				return err
			}
			if err := m.pushGuildBuffs(s, mg); err != nil {
				return err
			}
		}
		ms.Lap++
		ms.relayout(id)
	}
	ms.Now = 0
	if err := m.gain(s, role, prize); err != nil {
		return err
	}
	if err := s.Send(27404, proto.Message{"flag": 1, "msg": "", "pos": pos, "type": cell, "id": id}); err != nil {
		return err
	}
	if len(prize) == 0 && len(shown) == 0 {
		return nil
	}
	return s.Send(27409, proto.Message{"id": id, "buffs": shown, "reward": pairList(prize)})
}

func (m *monoSvc) event(s *gateway.Session, req proto.Message) error {
	return m.with(s, func(role *store.Role, st *monoState) error {
		refuse := func(msg string) error {
			return s.Send(27404, proto.Message{"flag": 0, "msg": msg, "pos": 0, "type": 0, "id": st.Cur})
		}
		id := st.Cur
		if id == 0 {
			return refuse("请先进入地图")
		}
		ms := st.mapOf(id)
		if ms.Now == 0 {
			return refuse("没有待处理的事件")
		}
		args := monoArgs(req)
		var prize [][2]int64
		var buffs []int64
		switch ms.Now {
		case evNormal, evStart:
		case evAward:
			prize = [][2]int64{{monoTicket, 2}, {monoItem, 10}}
		case evRedbag:
			prize = [][2]int64{{monoItem, 20}}
		case evAdvance, evBuff, evWish, evMedicine:
			buffs = []int64{monoBuffOf[ms.Now]}
		case evEnd:
			prize = [][2]int64{{3, 150}, {monoTicket, 1}}
		case evDialog:
			a, ok := args[5]
			if !ok {
				return refuse("请选择")
			}
			_ = a
			prize = [][2]int64{{monoItem, 8}}
		case evFlag:
			a, ok := args[6]
			if !ok || a[0] < 1 || a[0] > 4 {
				return refuse("请选择步数")
			}
			ms.Flag = a[0]
		case evMorra:
			a, ok := args[4]
			if !ok || a[0] < 1 || a[0] > 3 {
				return refuse("请出拳")
			}
			theirs := int64(newRng().Intn(3)) + 1
			ret := monoMorra(a[0], theirs)
			prize = [][2]int64{{monoItem, 3}}
			if ret == 1 {
				prize = [][2]int64{{monoTicket, 2}, {monoItem, 15}}
			}
			if err := s.Send(27405, proto.Message{"id": id, "choice": a[0], "system_choice": theirs, "ret": ret}); err != nil {
				return err
			}
		case evTrap, evBoss:
			return m.guardFight(s, role, st, ms, id, req, args, refuse)
		}
		return m.settle(s, role, ms, id, prize, buffs)
	})
}

// guardPower is the power of the guard of the cell: a share of the role's own best five, the original's range in per mille.
func (m *monoSvc) guardPower(role *store.Role, ms *monoMapState) (power int64, all []store.Partner, err error) {
	if all, err = m.st.PartnersOf(role.RID); err != nil {
		return 0, nil, err
	}
	best := bestHeroes(all, 5)
	return max(1, rolePower(best)*ms.Pct/1000), all, nil
}

func (m *monoSvc) guardFight(s *gateway.Session, role *store.Role, st *monoState, ms *monoMapState, id int64, req proto.Message, args map[int64][2]int64, refuse func(string) error) error {
	var slots []pfSlot
	for _, p := range req.Arr("args") {
		if p.Int("type") == 2 && p.Int("arg1") >= 1 {
			slots = append(slots, pfSlot{Pos: p.Int("arg1"), ID: p.Int("arg2")})
		}
	}
	if len(slots) == 0 {
		return refuse("至少需要上阵一个英雄")
	}
	power, all, err := m.guardPower(role, ms)
	if err != nil {
		return err
	}
	roster := make([]pfHero, 0, len(all))
	for _, h := range all {
		roster = append(roster, pfHero{PID: h.PartnerID, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star), HP: 100})
	}
	switch { // the rice makes the fight a walkover, the millet wine a fivefold blow
	case ms.Buffs[bfMedicine] > 0:
		ms.Buffs[bfMedicine]--
		power = 1
	case ms.Buffs[bfBuff] > 0:
		ms.Buffs[bfBuff]--
		power = max(1, power/5)
	}
	name := "和之国守卫"
	if ms.Now == evBoss {
		name = "大Boss"
	}
	combat := 100 + id
	plan, msg, err := planesFight(m.st, m.fights, role, roster, slots, args[1][0], power, name, []proto.Message{}, combat)
	if err != nil {
		return err
	}
	if msg != "" {
		return refuse(msg)
	}
	boss := ms.Now == evBoss
	if err := m.fights.begin(s, role, combat, plan.intro, plan.rounds, plan.won, func(s *gateway.Session, won bool) error {
		return m.with(s, func(role *store.Role, st *monoState) error {
			ms := st.mapOf(id)
			if ms.Now != evTrap && ms.Now != evBoss {
				return nil
			}
			prize := [][2]int64{{monoItem, 3}}
			switch {
			case won && boss:
				prize = [][2]int64{{monoTicket, 2}, {monoItem, 40}, {monoSecret, 1}}
			case won:
				prize = [][2]int64{{monoTicket, 1}, {monoItem, 15}, {monoDice, 1}}
			case boss:
				prize = [][2]int64{{monoItem, 5}}
			}
			return m.settle(s, role, ms, id, prize, nil)
		})
	}); err != nil {
		return err
	}
	return nil
}

// ---- 27407 27408 27410 ----

// drama (27407): the choice at the end of the story talk of a map pays its reward, once.
func (m *monoSvc) drama(s *gateway.Session, req proto.Message) error {
	t, choice := req.Int("type"), req.Int("choice")
	return m.with(s, func(role *store.Role, st *monoState) error {
		key := monoKey(t)
		if st.Drama[key] {
			return s.Send(27407, proto.Message{"flag": 0, "msg": "已经领取过"})
		}
		var prize [][2]int64
		for _, d := range monoCfg.Dramas {
			if d.Type != t {
				continue
			}
			for _, r := range d.Reward {
				var c int64
				var items [][]int64
				if len(r) == 2 && json.Unmarshal(r[0], &c) == nil && json.Unmarshal(r[1], &items) == nil && (c == choice || len(d.Reward) == 1) {
					prize = pairsOf(items)
				}
			}
		}
		if len(prize) == 0 {
			return s.Send(27407, proto.Message{"flag": 0, "msg": "没有奖励"})
		}
		st.Drama[key] = true
		if err := m.gain(s, role, prize); err != nil {
			return err
		}
		return s.Send(27407, proto.Message{"flag": 1, "msg": "获得奖励"})
	})
}

func (m *monoSvc) mapBuffs(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return m.with(s, func(_ *store.Role, st *monoState) error {
		buffs := []proto.Message{}
		if id >= 1 && id <= monoMaps {
			ms := st.mapOf(id)
			for k := int64(1); k <= 4; k++ {
				if ms.Buffs[k] > 0 {
					buffs = append(buffs, proto.Message{"type": k, "num": ms.Buffs[k]})
				}
			}
		}
		return s.Send(27408, proto.Message{"id": id, "buffs": buffs})
	})
}

// guards (27410): who stands on the cell, a scaled copy of the role's own best heroes as for the planes.
func (m *monoSvc) guards(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return m.with(s, func(role *store.Role, st *monoState) error {
		if id < 1 || id > monoMaps {
			return nil
		}
		ms := st.mapOf(id)
		power, all, err := m.guardPower(role, ms)
		if err != nil {
			return err
		}
		guards := []proto.Message{}
		for i, h := range bestHeroes(all, 5) {
			guards = append(guards, proto.Message{"pos": i + 1, "bid": h.BID, "lev": max(1, int64(h.Lev)*ms.Pct/1000), "star": h.Star, "ext_data": []proto.Message{}})
		}
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		name := "和之国守卫"
		if ms.Now == evBoss {
			name = "大Boss"
		}
		return s.Send(27410, proto.Message{"id": id, "rid": 0, "srv_id": role.SrvID, "name": name, "face": face, "lev": role.Lev, "power": power, "guards": guards})
	})
}
