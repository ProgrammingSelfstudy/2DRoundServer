package game

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const advModule = "adventure"

// Room states (AdventureConst.status of the client).
const (
	roomLock    = 0
	roomCanOpen = 1
	roomOpen    = 2
	roomOver    = 3
)

type advRoom struct {
	ID     int64
	Status int64
	Evt    int64 // what the room holds, 0 for an empty room; the client sees it once the room is revealed
	Res    int64 // the tile picture (res_id)
	HPPer  int64 // permille of the guard's hp that is left
	Qs     []int64
	Step   int64
	Right  int64
	Goods  []advGood
}

type advHero struct {
	PID int64
	Per int64 // permille of its hp that is left
}

type advSkillHold struct{ Num, Used int64 }

type advBuff struct{ Bid int64 }

type advGood struct {
	ID, Pay, Price, Bid, Num, Discount int64
	Bought                             bool
}

type advState struct {
	Floor, PassID, PassNum int64
	Rooms                  []advRoom
	Boss                   bool
	Start, End             int64
	Heroes                 []advHero
	Selected               int64
	Skills                 map[int64]advSkillHold
	Buffs                  []int64
	Kills                  int64
	BoxTaken               map[int64]bool
	LastNum                int64 // the buff of the last round (99: none)
	Shop                   []advGood
	Merchant               []advGood
	Combats                int64
	Explored               int64           // rooms revealed, ever (seven-day ranking tasks)
	LastRoom               int64           // 20600 room: the room revealed last (13 on a new floor)
	Gains                  map[int64]int64 // what the rooms of this floor paid, shown by 20606 when the floor is left
}

// advCatalog is what the merchants and the island shop sell. The client has no list (the server makes it),
// so this one is ours.
var advCatalog = []advGood{
	{ID: 1, Pay: 1, Price: 20000, Bid: advKeyItem, Num: 1},
	{ID: 2, Pay: 1, Price: 60000, Bid: 29905, Num: 2},
	{ID: 3, Pay: 1, Price: 60000, Bid: 29105, Num: 2},
	{ID: 4, Pay: 1, Price: 60000, Bid: 29205, Num: 2},
	{ID: 5, Pay: 1, Price: 60000, Bid: 29305, Num: 2},
	{ID: 6, Pay: 3, Price: 150, Bid: 10403, Num: 1},
}

func (g advGood) message() proto.Message {
	return proto.Message{"id": g.ID, "pay_type": g.Pay, "pay_val": g.Price, "bid": g.Bid, "num": g.Num, "is_buy": b2i(g.Bought), "discount": g.Discount}
}

func goodMessages(list []advGood) []proto.Message {
	out := []proto.Message{}
	for _, g := range list {
		out = append(out, g.message())
	}
	return out
}

func advMapID(floor int64) int64 { return min((floor-1)/25+1, int64(len(advCfg.Map))) }

func (r advRoom) cfg() advEventCfg {
	e, _ := advCfg.event(r.Evt)
	return e
}

func (st *advState) room(id int64) *advRoom {
	for i := range st.Rooms {
		if st.Rooms[i].ID == id {
			return &st.Rooms[i]
		}
	}
	return nil
}

// buildFloor makes the 25 rooms of a floor (see advGuardRooms): the guards are open, room 13 shows the mysterious
// man, the rest wait to be revealed.
func (st *advState) buildFloor(floor int64) {
	rng := newRng()
	st.Floor, st.Boss, st.PassNum, st.LastRoom = floor, false, 0, advDoorRoom
	st.Gains = map[int64]int64{}
	st.Rooms = st.Rooms[:0]
	bag, tiles := advRoomBag(rng), advTerrain(floor)
	for id := int64(1); id <= advGridSide*advGridSide; id++ {
		r := advRoom{ID: id, HPPer: 1000, Res: tiles[rng.Intn(len(tiles))]}
		if off, guard := advGuardRooms[id]; guard {
			r.Evt, r.Status = advEventID(floor, off), roomOpen
		} else if id == advDoorRoom {
			r.Evt = advEventID(floor, advMystery)
		} else {
			if bag[0] != 0 {
				r.Evt = advEventID(floor, bag[0])
			}
			bag = bag[1:]
		}
		st.Rooms = append(st.Rooms, r)
	}
}

// blocked: a room next to a guard that still stands cannot be explored ("可探索无守卫区域" once it falls).
func (st *advState) blocked(id int64) bool {
	for _, n := range advNeighbours(id) {
		if _, guard := advGuardRooms[n]; guard {
			if q := st.room(n); q != nil && q.Status != roomOver {
				return true
			}
		}
	}
	return false
}

// reveal opens the way from a room that is over, as the original does: the hidden rooms around it that no standing
// guard touches become visible (one 20603 with the room itself), then each shows what it holds (one 20603 each): an
// event to deal with, the boss in place of the mysterious man, or an empty room that is over at once and reveals the
// rooms around it in turn. It returns the 20603 room lists in the order they are sent.
func (st *advState) reveal(from int64) [][]proto.Message {
	var batches [][]proto.Message
	queue := []int64{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		var seen []int64
		for _, n := range advNeighbours(at) {
			r := st.room(n)
			if _, guard := advGuardRooms[n]; guard || r == nil || r.Status != roomLock || st.blocked(n) {
				continue
			}
			r.Status = roomCanOpen
			seen = append(seen, n)
		}
		if len(seen) == 0 {
			continue
		}
		first := []proto.Message{}
		for _, id := range append([]int64{at}, seen...) {
			first = append(first, st.roomMessage(*st.room(id)))
		}
		batches = append(batches, first)
		for _, n := range seen {
			r := st.room(n)
			st.PassNum++
			st.Explored++ // the seven-day ranking's "探索N个格子" counts the rooms revealed
			st.LastRoom = n
			switch {
			case n == advDoorRoom:
				if p := r.cfg().pairs(); r.cfg().EvtType == evtMysterious && len(p) > 0 {
					r.Evt = p[0][0]
				}
				r.Status = roomOpen
			case r.Evt == 0:
				r.Status = roomOver
				queue = append(queue, n)
			default:
				r.Status = roomOpen
			}
			batches = append(batches, []proto.Message{st.roomMessage(*r)})
		}
	}
	return batches
}

func (st *advState) allOver() bool {
	for _, r := range st.Rooms {
		if r.Status != roomOver {
			return false
		}
	}
	return len(st.Rooms) > 0
}

func (st *advState) heroOf(pid int64) *advHero {
	for i := range st.Heroes {
		if st.Heroes[i].PID == pid {
			return &st.Heroes[i]
		}
	}
	return nil
}

func (st *advState) skill(id int64) advSkillHold { return st.Skills[id] }

// newRound starts a round: a round lasts two days (advRoundStart), the island goes back to its first floor, the heroes are
// whole again, and a round whose boxes were not all taken leaves a buff for the next one.
func (st *advState) newRound(now time.Time) {
	taken := int64(len(st.BoxTaken))
	last := int64(99)
	if st.Start != 0 && taken < int64(len(advCfg.RoundRewards)) {
		last = min(taken, 4)
	}
	start := advRoundStart(now)
	st.Start, st.End = start.Unix(), start.Add(advRoundDays*24*time.Hour).Unix()
	st.PassNum, st.Kills, st.BoxTaken, st.LastNum = 0, 0, map[int64]bool{}, last
	st.Skills, st.Combats, st.Merchant = map[int64]advSkillHold{}, 0, nil
	st.Buffs = nil
	if b, ok := advCfg.Buffs[strconv.FormatInt(last, 10)]; ok && last != 99 {
		for _, p := range b.Buffs {
			st.Buffs = append(st.Buffs, p[0])
		}
	}
	for i := range st.Heroes {
		st.Heroes[i].Per = 1000
	}
	st.Shop = append([]advGood(nil), advCatalog...)
	st.buildFloor(1)
}

func (st *advState) roomMessage(r advRoom) proto.Message {
	evt := r.Evt
	if r.Status < roomOpen && r.ID != advDoorRoom { // hidden or just seen: the client learns what it holds when it is revealed
		evt = 0
	}
	res := r.Res
	if res == 0 {
		res = 1
	}
	return proto.Message{"id": r.ID, "status": r.Status, "lock": 0, "evt_id": evt, "res_id": res}
}

type adv struct {
	st     *store.Store
	fights *FightService
}

// RegisterAdventure registers the sky island adventure: 20600 / 20601 the island and its buffs, 20602 the rooms,
// 20604 / 20605 / 20610 the heroes, 20607 / 20609 / 20636 the potions, 20608 open a room, 20620 act in a room,
// 20611 the guards, 20632 / 20633 the island shop, 20634 / 20635 the round boxes, and (adventure_mine.go) the
// crystal mine 20640-20660. The client has no room layout and no quiz answers (the server makes them), so
// the layout, the rewards of the rooms and the shop are ours: not captured.
func RegisterAdventure(r *gateway.Router, db *store.Store) {
	a := &adv{st: db, fights: fightServiceOf(db)}
	r.Handle(20600, a.base)
	r.Handle(20601, a.buffs)
	r.Handle(20602, func(s *gateway.Session, _ proto.Message) error {
		return a.with(s, func(role *store.Role, st *advState) error { return a.sendRooms(s, st, 20602, nil) })
	})
	r.Handle(20604, func(s *gateway.Session, _ proto.Message) error {
		return a.with(s, func(role *store.Role, st *advState) error { return a.sendHeroes(s, role, st) })
	})
	r.Handle(20605, a.setForm)
	r.Handle(20610, a.selectHero)
	r.Handle(20620, a.act)
	r.Handle(20609, a.skills)
	r.Handle(20607, a.useSkill)
	r.Handle(20611, a.guards)
	r.Handle(20636, a.buyPotions)
	r.Handle(20632, a.shopList)
	r.Handle(20633, a.shopBuy)
	r.Handle(20634, a.boxes)
	r.Handle(20635, a.takeBox)
	registerAdventureMine(r, a)
}

var errNoRole = errors.New("no role")

// with loads the role's adventure, brings the round up to date, runs fn and saves the result.
func (a *adv) with(s *gateway.Session, fn func(role *store.Role, st *advState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	st := &advState{}
	ok, err := a.st.LoadState(role.RID, advModule, st)
	if err != nil {
		return err
	}
	roundEnd := advRoundStart(s.Now()).Add(advRoundDays * 24 * time.Hour).Unix()
	if !ok || s.Now().Unix() >= st.End || st.End > roundEnd { // a round saved by the seven-day version ends too late
		st.newRound(s.Now())
	} else if len(st.Rooms) != advGridSide*advGridSide { // a floor saved by the 13-room version
		st.buildFloor(max(st.Floor, 1))
	}
	if st.Skills == nil {
		st.Skills = map[int64]advSkillHold{}
	}
	if st.BoxTaken == nil {
		st.BoxTaken = map[int64]bool{}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return a.st.SaveState(role.RID, advModule, st)
}

func (a *adv) base(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error { return sendBase(s, st) })
}

func sendBase(s *gateway.Session, st *advState) error {
	return s.Send(20600, (&adv{}).baseMessage(st))
}

func (a *adv) buffs(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error { return a.sendBuffs(s, st) })
}

func (a *adv) sendBuffs(s *gateway.Session, st *advState) error {
	list := []proto.Message{}
	for _, b := range st.Buffs {
		list = append(list, proto.Message{"bid": b, "time": st.End})
	}
	return s.Send(20601, proto.Message{"buff_list": list, "holiday_buff_list": []proto.Message{}})
}

// sendRooms sends the rooms (all, or the ones listed) as cmd 20602 (all) or 20603 (an update).
func (a *adv) sendRooms(s *gateway.Session, st *advState, cmd uint16, only []int64) error {
	list := []proto.Message{}
	for _, r := range st.Rooms {
		if only != nil {
			found := false
			for _, id := range only {
				found = found || id == r.ID
			}
			if !found {
				continue
			}
		}
		list = append(list, st.roomMessage(r))
	}
	return s.Send(cmd, proto.Message{"room_list": list})
}

func (a *adv) sendHeroes(s *gateway.Session, role *store.Role, st *advState) error {
	byID, err := a.partners(role)
	if err != nil {
		return err
	}
	list := []proto.Message{}
	for _, h := range st.Heroes {
		p, ok := byID[h.PID]
		if !ok {
			continue
		}
		m := partnerMessage(p)
		m["now_hp"] = h.Per * m.Int("hp") / 1000
		list = append(list, m)
	}
	return s.Send(20604, proto.Message{"id": st.Selected, "combat_num": st.Combats, "partners": list})
}

func (a *adv) partners(role *store.Role) (map[int64]store.Partner, error) {
	all, err := a.st.PartnersOf(role.RID)
	if err != nil {
		return nil, err
	}
	out := map[int64]store.Partner{}
	for _, p := range all {
		out[p.PartnerID] = p
	}
	return out, nil
}

// setForm (20605) chooses the heroes that go to the island: at most five (data_explain "可选择5个英雄进行冒险"), the first one fights.
func (a *adv) setForm(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		byID, err := a.partners(role)
		if err != nil {
			return err
		}
		var heroes []advHero
		seen := map[int64]bool{}
		for _, p := range req.Arr("plist") {
			id := p.Int("id")
			if _, ok := byID[id]; !ok || seen[id] || len(heroes) == advMaxHeroes {
				continue
			}
			seen[id] = true
			per := int64(1000)
			if old := st.heroOf(id); old != nil {
				per = old.Per
			}
			heroes = append(heroes, advHero{PID: id, Per: per})
		}
		if len(heroes) == 0 {
			return s.Send(20605, proto.Message{"code": 0, "msg": "请选择出战英雄", "last_num": st.LastNum})
		}
		st.Heroes = heroes
		if st.heroOf(st.Selected) == nil {
			st.Selected = heroes[0].PID
		}
		if err := s.Send(20605, proto.Message{"code": 1, "msg": "", "last_num": st.LastNum}); err != nil {
			return err
		}
		return a.sendHeroes(s, role, st)
	})
}

func (a *adv) selectHero(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id := req.Int("id")
		h := st.heroOf(id)
		if h == nil || h.Per == 0 {
			return s.Send(20610, proto.Message{"code": 0, "msg": "该英雄无法出战", "id": id})
		}
		st.Selected = id
		return s.Send(20610, proto.Message{"code": 1, "msg": "", "id": id})
	})
}

// finish marks a room over, reveals what that opens and tells the client (20603 in the original's order, then 20600).
func (a *adv) finish(s *gateway.Session, st *advState, r *advRoom) error {
	r.Status = roomOver
	batches := st.reveal(r.ID)
	if len(batches) == 0 || batches[0][0].Int("id") != r.ID {
		batches = append([][]proto.Message{{st.roomMessage(*r)}}, batches...)
	}
	for _, list := range batches {
		if err := s.Send(20603, proto.Message{"room_list": list}); err != nil {
			return err
		}
	}
	return s.Send(20600, a.baseMessage(st))
}

// roomGain pays what a room gives and adds it to the floor's list (20606).
func (a *adv) roomGain(s *gateway.Session, role *store.Role, st *advState, list [][2]int64) error {
	if len(list) == 0 {
		return nil
	}
	if st.Gains == nil {
		st.Gains = map[int64]int64{}
	}
	for _, g := range list {
		st.Gains[g[0]] += g[1]
	}
	return a.gain(s, role, list)
}

// gainTip is the original's 12741 line for what a room paid: 获得<assets=1/>32000, or an item's name and amount.
func gainTip(list [][2]int64) string {
	text := "获得"
	for _, g := range list {
		if name, ok := ItemName(g[0]); ok && g[0] >= 1000 {
			text += fmt.Sprintf("<div fontcolor=#d63cfc>%s</div><div fontcolor=#289b14>x</div><div fontcolor=#289b14>%d</div>", name, g[1])
		} else {
			text += fmt.Sprintf("<assets=%d/><div fontcolor=#289b14>%d</div>", g[0], g[1])
		}
	}
	return text
}

func (a *adv) gain(s *gateway.Session, role *store.Role, list [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(list), 1, ga, gi)
	if err := a.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, a.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, a.st)
}

// pay takes a price; false when the role cannot afford it.
func (a *adv) pay(s *gateway.Session, role *store.Role, list [][2]int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(list), 1, sa, si)
	err := a.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, sendAssets(s, a.st, role.RID)
}

func itemMessages(list [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, e := range list {
		out = append(out, proto.Message{"bid": e[0], "num": e[1], "move_to": 0})
	}
	return out
}

func weighted(rng *rand.Rand, pairs [][]int64) int {
	var total int64
	for _, p := range pairs {
		if len(p) > 1 {
			total += p[1]
		}
	}
	if total <= 0 {
		return -1
	}
	n := rng.Int63n(total)
	for i, p := range pairs {
		if len(p) > 1 {
			if n < p[1] {
				return i
			}
			n -= p[1]
		}
	}
	return -1
}

func newRng() *rand.Rand { return rand.New(rand.NewSource(time.Now().UnixNano())) }

// extOf reads the ext_list of a 20620 into type -> value.
func extOf(req proto.Message) map[int64]int64 {
	out := map[int64]int64{}
	for _, e := range req.Arr("ext_list") {
		out[e.Int("type")] = e.Int("val")
	}
	return out
}

func (a *adv) act(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id, action := req.Int("room_id"), req.Int("action")
		reply := func(code int64, msg string) error {
			return s.Send(20620, proto.Message{"code": code, "msg": msg, "room_id": id, "action": action})
		}
		r := st.room(id)
		if r == nil {
			return reply(0, "该区域不存在")
		}
		e := r.cfg()
		if e.EvtType == evtNext {
			return a.nextFloor(s, role, st, r, reply)
		}
		if r.Status != roomOpen && !(e.EvtType == evtShop && r.Status == roomOver) {
			return reply(0, "该区域还没有开启")
		}
		ext := extOf(req)
		switch e.EvtType {
		case evtBuff:
			return a.doBuff(s, st, r, e, reply)
		case evtCollect:
			return a.doCollect(s, role, st, r, e, reply)
		case evtBoss, evtMon, evtMon1, evtMon2, evtMon3:
			return a.doGuard(s, role, st, r, e, action, reply)
		case evtGuess:
			return a.doGuess(s, role, st, r, e, action, ext, reply)
		case evtAnswer:
			return a.doAnswer(s, role, st, r, e, action, ext, reply)
		case evtNpc, evtNpcTalk:
			return a.doNpc(s, role, st, r, e, action, ext, reply)
		case evtShop:
			return a.doShop(s, role, st, r, action, ext, reply)
		case evtSkill:
			return a.doSkill(s, st, r, e, reply)
		case evtBox:
			return a.doBox(s, role, st, r, e, reply)
		}
		return reply(0, "该事件暂不支持")
	})
}

func (a *adv) doBuff(s *gateway.Session, st *advState, r *advRoom, e advEventCfg, reply func(int64, string) error) error {
	i := weighted(newRng(), e.pairs())
	if i < 0 {
		return reply(0, "没有可用的增益")
	}
	st.Buffs = append(st.Buffs, e.pairs()[i][0])
	if err := s.Send(12741, proto.Message{"msg": "获取BUFF"}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	if err := a.sendBuffs(s, st); err != nil {
		return err
	}
	return reply(1, "")
}

func (a *adv) doCollect(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, reply func(int64, string) error) error {
	prize := pairsOf(e.BaseItems)
	if err := a.roomGain(s, role, st, prize); err != nil {
		return err
	}
	if err := s.Send(12741, proto.Message{"msg": gainTip(prize)}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

func (a *adv) doSkill(s *gateway.Session, st *advState, r *advRoom, e advEventCfg, reply func(int64, string) error) error {
	i := weighted(newRng(), e.pairs())
	if i < 0 {
		return reply(0, "没有可用的道具")
	}
	sid := e.pairs()[i][0]
	cfg, ok := advCfg.skill(sid)
	if !ok {
		return reply(0, "没有可用的道具")
	}
	h := st.Skills[sid]
	if limit := cfg.MaxNum; limit == 0 || h.Num < limit {
		h.Num++
	}
	st.Skills[sid] = h
	if err := s.Send(20625, proto.Message{"id": r.ID, "skill_id": sid}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "获得"+cfg.Name)
}

// heroTeam makes the side of the selected hero.
func (a *adv) heroTeam(role *store.Role, st *advState, h5 bool) ([]fightUnit, []proto.Message, *advHero, error) {
	h := st.heroOf(st.Selected)
	if h == nil || h.Per == 0 {
		for i := range st.Heroes {
			if st.Heroes[i].Per > 0 {
				h = &st.Heroes[i]
				st.Selected = h.PID
				break
			}
		}
	}
	if h == nil || h.Per == 0 {
		return nil, nil, nil, nil
	}
	team, objects, err := a.fights.teamFor(role, 1, []store.FormationSlot{{Pos: 3, PartnerID: h.PID}}, h5)
	if err != nil || len(team) == 0 {
		return nil, nil, nil, err
	}
	team[0].hp = max(1, team[0].maxHP*h.Per/1000)
	for _, o := range objects {
		if o.Int("object_type") == 2 {
			o["hp"] = team[0].hp
		}
	}
	return team, objects, h, nil
}

func (a *adv) doGuard(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, action int64, reply func(int64, string) error) error {
	if action == 0 {
		if err := s.Send(20624, proto.Message{"hp_per": r.HPPer / 10}); err != nil {
			return err
		}
		return reply(1, "")
	}
	unitCfg, ok := advCfg.Units[strconv.FormatInt(e.UnitID, 10)]
	if !ok || len(unitCfg.Monsters) == 0 {
		return reply(0, "守卫不见了")
	}
	team, objects, hero, err := a.heroTeam(role, st, isH5RoleSession(s))
	if err != nil {
		return err
	}
	if hero == nil {
		return reply(0, "没有可用英雄")
	}
	var foes []fightUnit
	for i, m := range unitCfg.Monsters {
		if i == len(monsterPos) {
			break
		}
		unit, obj := monsterOf(m, i)
		unit.hp = max(1, unit.maxHP*r.HPPer/1000)
		obj["hp"] = unit.hp
		foes = append(foes, unit)
		objects = append(objects, obj)
	}
	rounds, won, sim := playFightOpts(team, foes, newRng(), maxFightRounds, false)
	heroPer, foePer := int64(0), int64(0)
	var foeHP, foeMax int64
	for _, u := range sim.units {
		if u.ours {
			heroPer = max(0, u.hp) * 1000 / max(1, u.maxHP)
		} else {
			foeHP += max(0, u.hp)
			foeMax += u.maxHP
		}
	}
	if foeMax > 0 {
		foePer = foeHP * 1000 / foeMax
	}
	intro := proto.Message{
		"combat_type": combatAdventure, "formation": []proto.Message{{"group": 1, "formation_type": 1, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
		"objects": objects, "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1, "extra_args": []proto.Message{{"param": r.ID}}, "string_ext_args": []proto.Message{},
		"a_object_num": 1, "actor_role_name": role.Name, "target_role_name": unitCfg.Group, "suppress": 0, "halo_list": []proto.Message{},
		"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	if err := reply(2, ""); err != nil { // code 2: the client closes the event view and the fight begins
		return err
	}
	roomID, heroID := r.ID, hero.PID
	return a.fights.begin(s, role, combatAdventure, intro, rounds, won, func(s *gateway.Session, won bool) error {
		return a.with(s, func(role *store.Role, st *advState) error {
			r := st.room(roomID)
			if r == nil {
				return nil
			}
			if h := st.heroOf(heroID); h != nil {
				h.Per = heroPer
			}
			result := int64(2)
			if won {
				result = 1
				r.HPPer = 0
				st.Kills++
				if e.EvtType == evtBoss { // the boss leaves the way on in its room
					st.Boss = true
					st.PassID = max(st.PassID, st.Floor)
					r.Evt = advEventID(st.Floor, 99999)
				}
			} else {
				r.HPPer = max(foePer, 1)
			}
			p, _ := a.partners(role)
			stats := []proto.Message{}
			if pp, ok := p[heroID]; ok {
				stats = append(stats, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": pp.PartnerID, "bid": pp.BID, "star": pp.Star, "lev": pp.Lev,
					"camp_type": partnerTable[pp.BID].Camp, "dps": partnerMessage(pp).Int("power"), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
			}
			if err := s.Send(20006, proto.Message{"result": result, "item_rewards": []proto.Message{}, "show_panel_type": 1, "current_time": 0, "best_time": 0,
				"combat_type": combatAdventure, "target_role_name": unitCfg.Group, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}},
				"adventure_end_hp": heroPer, "room_id": roomID, "ext_list": []proto.Message{}}); err != nil {
				return err
			}
			if won {
				if err := a.finish(s, st, r); err != nil {
					return err
				}
				if err := a.sendBoxes(s, st); err != nil { // the kills toward the round boxes, as captured after a win
					return err
				}
			}
			return a.sendHeroes(s, role, st)
		})
	})
}

func (a *adv) baseMessage(st *advState) proto.Message {
	return proto.Message{"pass_id": st.PassID, "pass_num": st.PassNum, "id": st.Floor, "room": st.LastRoom, "is_kill_boss": b2i(st.Boss),
		"map_id": advMapID(st.Floor), "is_all_finish": b2i(st.allOver()), "start_time": st.Start, "end_time": st.End, "current_id": st.Floor}
}

// doGuess plays rock paper scissors: a bet is chosen (type 2) and a finger (type 1). Win, tie and loss come
// out equally often (the client's weights) and pay 1.3, 1.1 and 1 times the bet back, the bet included.
func (a *adv) doGuess(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, action int64, ext map[int64]int64, reply func(int64, string) error) error {
	if action == 0 {
		return reply(1, "")
	}
	finger, bet := ext[1], ext[2]
	if finger < 1 || finger > 3 || bet < 1 || int(bet) > len(e.Lose) {
		return reply(0, "请下注并出拳")
	}
	stake := [2]int64{e.Lose[bet-1][0], e.Lose[bet-1][1]}
	rng := newRng()
	ret := int64(rng.Intn(3)) // 0 tie, 1 win, 2 loss
	opp := finger
	switch ret {
	case 1:
		opp = finger%3 + 1 // 1 beats 2, 2 beats 3, 3 beats 1: the opponent plays the one we beat
	case 2:
		opp = (finger+1)%3 + 1
	}
	mult := int64(1000)
	for _, p := range e.pairs() {
		if p[0] == ret {
			mult = p[1]
		}
	}
	back := stake[1] * mult / 1000
	if ok, err := a.pay(s, role, [][2]int64{stake}); err != nil || !ok {
		if err != nil {
			return err
		}
		return reply(0, "下注的资源不足")
	}
	if err := a.roomGain(s, role, st, [][2]int64{{stake[0], back}}); err != nil {
		return err
	}
	if err := s.Send(20621, proto.Message{"sel_val": finger, "ret_val": opp, "ret": ret, "items": itemMessages([][2]int64{{stake[0], back}})}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

func (a *adv) quizReward(e advEventCfg, right int64) [][2]int64 { return e.quiz()[right] }

// advKnownAnswers are the right options the original server gave (capture 2026-11-04, SKY_NOTES.md: right of the 20623
// that answers each question). The client has no answer key, so a question not listed here counts any answer as right.
var advKnownAnswers = map[int64]int64{
	50408: 1, 50414: 3, 50387: 3, 50431: 3, 50333: 4, 50399: 1, 50309: 1, 50401: 3, 50055: 2, 50413: 2, 50427: 4, 50031: 3,
}

// advQuizEnd is the original's line for the number of right answers (captured for 0-2; the one for three is ours).
var advQuizEnd = []string{"运气真是不好呢~", "才对了一道，有点可惜啦~", "还差一道就能全对啦，加油！", "全部答对，太厉害啦！"}

// doAnswer asks three questions (20623). ret 1 is right, ret 2 wrong; right is the right option of the question just
// answered while bid is already the next question, as the original sends them.
func (a *adv) doAnswer(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, action int64, ext map[int64]int64, reply func(int64, string) error) error {
	if len(r.Qs) == 0 {
		var ids []int64
		for k := range advCfg.Answers {
			if n, err := strconv.ParseInt(k, 10, 64); err == nil {
				ids = append(ids, n)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		rng := newRng()
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		r.Qs, r.Step, r.Right = ids[:3], 0, 0
	}
	push := func(ret, sel, right, bid int64, retMsg string, retItems [][2]int64) error {
		now, top := itemMessages(a.quizReward(e, r.Right)), itemMessages(a.quizReward(e, 3))
		if bid == 0 {
			now, top = []proto.Message{}, []proto.Message{}
		}
		return s.Send(20623, proto.Message{"ret": ret, "sel_val": sel, "right": right, "num": min(r.Step+1, 3), "max": 3, "bid": bid, "ret_msg": retMsg,
			"now_items": now, "max_items": top, "ret_items": itemMessages(retItems)})
	}
	if action == 0 {
		if err := push(0, 0, 0, r.Qs[r.Step], "", nil); err != nil {
			return err
		}
		return reply(1, "")
	}
	sel := ext[1]
	if sel < 1 || sel > 4 {
		return reply(0, "请选择答案")
	}
	right, known := advKnownAnswers[r.Qs[r.Step]]
	if !known {
		right = sel
	}
	ret := int64(2)
	if sel == right {
		ret = 1
		r.Right++
	}
	r.Step++
	if r.Step < 3 {
		if err := push(ret, sel, right, r.Qs[r.Step], "", nil); err != nil {
			return err
		}
		return reply(1, "")
	}
	prize := a.quizReward(e, r.Right)
	if err := a.roomGain(s, role, st, prize); err != nil {
		return err
	}
	if err := push(ret, sel, right, 0, advQuizEnd[min(r.Right, 3)], prize); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

// doNpc runs an npc: one question (20627 with the group of the client's npc table), and the answer ends it (20628), as
// both captures show. The client does not say what an npc pays and the original's lines do not follow the question;
// the pay is the captured kind: 37001 ×1-2 from the npc (14), 37001 ×2 or 100000 beli from the talking npc (15). A
// question with a price (lose) pays 5 ore for it.
func (a *adv) doNpc(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, action int64, ext map[int64]int64, reply func(int64, string) error) error {
	groups := advCfg.Npc[strconv.FormatInt(r.Evt, 10)]
	if len(groups) == 0 {
		return reply(0, "没有对话")
	}
	if len(r.Qs) == 0 {
		r.Qs, r.Step = []int64{int64(newRng().Intn(len(groups)))}, 0
	}
	cur := groups[r.Qs[0]]
	if action == 0 {
		if err := s.Send(20627, proto.Message{"evt_id": r.Evt, "id": cur[0].ID}); err != nil {
			return err
		}
		return reply(1, "")
	}
	opts := append([]advNpcOption(nil), cur...)
	sort.Slice(opts, func(i, j int) bool { return opts[i].Num < opts[j].Num })
	pick := ext[1]
	if pick < 1 || int(pick) > len(opts) {
		return reply(0, "请选择")
	}
	o, rng := opts[pick-1], newRng()
	prize, msg := [][2]int64{{37001, 1 + int64(rng.Intn(2))}}, "谢谢你的回答"
	switch {
	case len(o.Lose) > 0:
		ok, err := a.pay(s, role, pairsOf(o.Lose))
		if err != nil {
			return err
		}
		if !ok {
			return reply(0, "资源不足")
		}
		prize, msg = [][2]int64{{advOre, 5}}, "交易成功"
	case e.EvtType == evtNpcTalk && rng.Intn(2) == 0:
		prize, msg = [][2]int64{{1, 100000}}, "咦，这次发财了"
	case e.EvtType == evtNpcTalk:
		prize, msg = [][2]int64{{37001, 2}}, "不好，上当了！"
	}
	if err := a.roomGain(s, role, st, prize); err != nil {
		return err
	}
	if err := s.Send(20628, proto.Message{"msg": msg, "items": itemMessages(prize)}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

// doBox opens the ancient treasure (evt type 4) with a key of the floor (lose 10152 ×1) and pays one of the shards it
// shows (box_show_item), as captured: 20630 with the items, and code 0 with the hint when there is no key.
func (a *adv) doBox(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, reply func(int64, string) error) error {
	if len(e.BoxShow) == 0 {
		return reply(0, "宝箱是空的")
	}
	ok, err := a.pay(s, role, pairsOf(e.Lose))
	if err != nil {
		return err
	}
	if !ok {
		return reply(0, "秘宝钥匙就藏在本层的某个角落噢")
	}
	prize := pairsOf([][]int64{e.BoxShow[newRng().Intn(len(e.BoxShow))]})
	if err := a.roomGain(s, role, st, prize); err != nil {
		return err
	}
	if err := s.Send(20630, proto.Message{"items": itemMessages(prize)}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

// merchantGoods picks the goods a merchant shows: three of the catalog, a fifth off.
func merchantGoods(first int64) []advGood {
	idx := newRng().Perm(len(advCatalog))[:3]
	var out []advGood
	for n, i := range idx {
		g := advCatalog[i]
		g.ID, g.Discount = first+int64(n), 8
		g.Price = g.Price * 8 / 10
		out = append(out, g)
	}
	return out
}

func (a *adv) doShop(s *gateway.Session, role *store.Role, st *advState, r *advRoom, action int64, ext map[int64]int64, reply func(int64, string) error) error {
	if len(r.Goods) == 0 {
		r.Goods = merchantGoods(1)
	}
	if action == 0 {
		if err := s.Send(20631, proto.Message{"type": 1, "list": goodMessages(r.Goods)}); err != nil {
			return err
		}
		return reply(1, "")
	}
	for i := range r.Goods {
		g := &r.Goods[i]
		if g.ID != ext[1] {
			continue
		}
		if g.Bought {
			return reply(0, "该物品已被购买")
		}
		ok, err := a.pay(s, role, [][2]int64{{g.Pay, g.Price}})
		if err != nil {
			return err
		}
		if !ok {
			return reply(0, "资源不足")
		}
		if err := a.gain(s, role, [][2]int64{{g.Bid, g.Num}}); err != nil {
			return err
		}
		g.Bought = true
		if r.Status == roomOpen {
			if err := a.finish(s, st, r); err != nil {
				return err
			}
		}
		if err := s.Send(20631, proto.Message{"type": 1, "list": goodMessages(r.Goods)}); err != nil {
			return err
		}
		return reply(1, "购买成功")
	}
	return reply(0, "该物品不存在")
}

func (a *adv) nextFloor(s *gateway.Session, role *store.Role, st *advState, r *advRoom, reply func(int64, string) error) error {
	if r.Status != roomOver || !st.Boss {
		return reply(0, "击败守卫后才能前往下一层")
	}
	if st.Floor >= advFloors {
		return reply(0, "已达冒险最顶层,请等待冒险重置")
	}
	cleared := st.Floor
	if prize := pairsOf(advCfg.Floors[cleared-1].FloorReward); len(prize) > 0 { // empty in the client's table
		if err := a.roomGain(s, role, st, prize); err != nil {
			return err
		}
	}
	var got [][2]int64
	for bid, num := range st.Gains {
		got = append(got, [2]int64{bid, num})
	}
	sort.Slice(got, func(i, j int) bool { return got[i][0] < got[j][0] })
	st.buildFloor(cleared + 1)
	st.PassID = max(st.PassID, st.Floor)
	// the original order: the new floor (20600, 20602), what the floor left behind paid (20606: the floor result window),
	// then code 2
	if err := sendBase(s, st); err != nil {
		return err
	}
	if err := a.sendRooms(s, st, 20602, nil); err != nil {
		return err
	}
	if err := s.Send(20606, proto.Message{"items_list": itemMessages(got), "vip_buff": 0}); err != nil {
		return err
	}
	return reply(2, "")
}

func (a *adv) skills(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		return s.Send(20609, proto.Message{"skill_list": a.skillList(st)})
	})
}

func (a *adv) skillList(st *advState) []proto.Message {
	out := []proto.Message{}
	for _, c := range advCfg.Skills {
		h := st.Skills[c.ID]
		out = append(out, proto.Message{"bid": c.ID, "num": h.Num, "use_count": h.Used})
	}
	return out
}

func (a *adv) useSkill(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		sid, val := req.Int("skill_id"), req.Int("val")
		refuse := func(msg string) error { return s.Send(20607, proto.Message{"code": 0, "msg": msg}) }
		h := st.Skills[sid]
		if _, ok := advCfg.skill(sid); !ok || h.Num <= 0 {
			return refuse("道具数量不足")
		}
		switch sid {
		case 1: // the life potion heals half of a hero's hp
			hero := st.heroOf(val)
			if hero == nil || hero.Per == 0 {
				return refuse("该英雄无法使用")
			}
			hero.Per = min(1000, hero.Per+500)
		case 2: // the soul potion kills a guard
			r := st.room(val)
			e := advEventCfg{}
			if r != nil {
				e = r.cfg()
			}
			if r == nil || e.EvtType == evtBoss || !(e.EvtType == evtMon || e.EvtType == evtMon1 || e.EvtType == evtMon2 || e.EvtType == evtMon3) ||
				(r.Status != roomCanOpen && r.Status != roomOpen) {
				return refuse("无法对该目标使用")
			}
			r.HPPer = 0
			st.Kills++
			if err := a.finish(s, st, r); err != nil {
				return err
			}
		case 3: // the merchant
			st.Merchant = merchantGoods(101)
			if err := s.Send(20631, proto.Message{"type": 2, "list": goodMessages(st.Merchant)}); err != nil {
				return err
			}
		}
		h.Num--
		h.Used++
		st.Skills[sid] = h
		if err := s.Send(20607, proto.Message{"code": 1, "msg": "使用成功"}); err != nil {
			return err
		}
		if err := s.Send(20609, proto.Message{"skill_list": a.skillList(st)}); err != nil {
			return err
		}
		return a.sendHeroes(s, role, st)
	})
}

// guards lists the guards a soul potion may kill (20611).
func (a *adv) guards(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		list := []proto.Message{}
		for _, r := range st.Rooms {
			t := r.cfg().EvtType
			if (t == evtMon || t == evtMon1 || t == evtMon2 || t == evtMon3) && (r.Status == roomCanOpen || r.Status == roomOpen) {
				list = append(list, proto.Message{"id": r.ID, "evt_id": r.Evt})
			}
		}
		return s.Send(20611, proto.Message{"room_list": list})
	})
}

// buyPotions (20636) buys soul potions with gems, 30 each (the client's poison_price), up to the cap.
func (a *adv) buyPotions(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		n := req.Int("num")
		cfg, _ := advCfg.skill(2)
		h := st.Skills[2]
		if n < 1 || h.Num+n > cfg.MaxNum {
			return s.Send(20636, proto.Message{"code": 0, "msg": "超出可持有数量"})
		}
		ok, err := a.pay(s, role, [][2]int64{{3, 30 * n}})
		if err != nil {
			return err
		}
		if !ok {
			return s.Send(20636, proto.Message{"code": 0, "msg": "钻石不足"})
		}
		h.Num += n
		st.Skills[2] = h
		if err := s.Send(20636, proto.Message{"code": 1, "msg": "购买成功"}); err != nil {
			return err
		}
		return s.Send(20609, proto.Message{"skill_list": a.skillList(st)})
	})
}

func (a *adv) shopList(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		return s.Send(20632, proto.Message{"list": goodMessages(st.Shop)})
	})
}

func (a *adv) shopBuy(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id := req.Int("id")
		refuse := func(msg string) error { return s.Send(20633, proto.Message{"code": 0, "msg": msg, "id": id}) }
		list := st.Shop
		if id >= 100 {
			list = st.Merchant
		}
		for i := range list {
			g := &list[i]
			if g.ID != id {
				continue
			}
			if g.Bought {
				return refuse("该物品已被购买")
			}
			ok, err := a.pay(s, role, [][2]int64{{g.Pay, g.Price}})
			if err != nil {
				return err
			}
			if !ok {
				return refuse("资源不足")
			}
			if err := a.gain(s, role, [][2]int64{{g.Bid, g.Num}}); err != nil {
				return err
			}
			g.Bought = true
			return s.Send(20633, proto.Message{"code": 1, "msg": "购买成功", "id": id})
		}
		return refuse("该物品不存在")
	})
}

// boxes (20634) are the round boxes: one more opens at 5, 10, 15, 20 and 30 guards killed.
func (a *adv) boxes(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error { return a.sendBoxes(s, st) })
}

func (a *adv) sendBoxes(s *gateway.Session, st *advState) error {
	list := []proto.Message{}
	for _, b := range advCfg.RoundRewards {
		status := int64(0)
		switch {
		case st.BoxTaken[b.ID]:
			status = 2
		case st.Kills >= b.Count:
			status = 1
		}
		list = append(list, proto.Message{"id": b.ID, "status": status})
	}
	return s.Send(20634, proto.Message{"list": list, "kill_mon": st.Kills})
}

// takeBox (20635) takes a round box; id 0 takes every box the kills have opened, as the main window's box button sends
// (capture 2026-10-29: 20635 {id 0}). The box list (20634) follows.
func (a *adv) takeBox(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id := req.Int("id")
		var prize [][2]int64
		for _, b := range advCfg.RoundRewards {
			if (id != 0 && b.ID != id) || st.BoxTaken[b.ID] || st.Kills < b.Count {
				continue
			}
			st.BoxTaken[b.ID] = true
			prize = append(prize, pairsOf(b.Items)...)
		}
		if len(prize) == 0 {
			return s.Send(20635, proto.Message{"code": 0, "msg": "无法领取"})
		}
		if err := a.gain(s, role, prize); err != nil {
			return err
		}
		if err := s.Send(20635, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		return a.sendBoxes(s, st)
	})
}

// Register the persisted state without loading or changing gameplay data.
func init() {
	registerAdminModule(AdminModule{Key: advModule, Title: "空岛冒险", Window: "20600–20636", Scope: "role", New: adminState[advState]})
}
