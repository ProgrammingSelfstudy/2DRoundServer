package game

import (
	"errors"
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
	Evt    int64
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

// buildFloor makes the rooms of a floor: the ones without a precondition can be opened at once.
func (st *advState) buildFloor(floor int64) {
	st.Floor, st.Boss = floor, false
	st.Rooms = st.Rooms[:0]
	for _, p := range advLayout(floor) {
		r := advRoom{ID: p.ID, Evt: advEventID(floor, p.Off), HPPer: 1000}
		if len(p.Pre) == 0 {
			r.Status = roomCanOpen
		}
		st.Rooms = append(st.Rooms, r)
	}
}

// refresh unlocks the rooms whose preconditions are over; it returns the rooms that changed.
func (st *advState) refresh() []int64 {
	var changed []int64
	for _, p := range advLayout(st.Floor) {
		r := st.room(p.ID)
		if r == nil || r.Status != roomLock {
			continue
		}
		ok := true
		for _, pre := range p.Pre {
			if q := st.room(pre); q == nil || q.Status != roomOver {
				ok = false
			}
		}
		if p.ID == advDoorRoom { // the way on opens when the guard behind the mysterious room is down
			if ok && st.Boss {
				r.Status = roomOver
				changed = append(changed, r.ID)
			}
			continue
		}
		if ok {
			r.Status = roomCanOpen
			changed = append(changed, r.ID)
		}
	}
	return changed
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

// newRound starts a round: a round lasts a week, the island goes back to its first floor, the heroes are
// whole again, and a round whose boxes were not all taken leaves a buff for the next one.
func (st *advState) newRound(now time.Time) {
	taken := int64(len(st.BoxTaken))
	last := int64(99)
	if st.Start != 0 && taken < int64(len(advCfg.RoundRewards)) {
		last = min(taken, 4)
	}
	st.Start, st.End = now.Unix(), now.Add(advRoundDays*24*time.Hour).Unix()
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
	res := int64(1)
	if m := advMapID(st.Floor); m >= 1 && int(m) <= len(advCfg.Map) {
		res = advCfg.Map[m-1].ResID
	}
	return proto.Message{"id": r.ID, "status": r.Status, "lock": b2i(r.Status == roomLock), "evt_id": r.Evt, "res_id": res}
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
	r.Handle(20608, a.openRoom)
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
	if !ok || s.Now().Unix() >= st.End {
		st.newRound(s.Now())
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
	return s.Send(20600, proto.Message{"pass_id": st.PassID, "pass_num": st.PassNum, "id": st.Floor, "room": 0, "is_kill_boss": b2i(st.Boss),
		"map_id": advMapID(st.Floor), "is_all_finish": b2i(st.allOver()), "start_time": st.Start, "end_time": st.End, "current_id": st.Floor})
}

func (a *adv) buffs(s *gateway.Session, _ proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		list := []proto.Message{}
		for _, b := range st.Buffs {
			list = append(list, proto.Message{"bid": b, "time": st.End})
		}
		return s.Send(20601, proto.Message{"buff_list": list, "holiday_buff_list": []proto.Message{}})
	})
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

// setForm (20605) chooses the heroes that go to the island (at most six, the first one fights).
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
			if _, ok := byID[id]; !ok || seen[id] || len(heroes) == formMaxHeroes {
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

func (a *adv) openRoom(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id := req.Int("room_id")
		r := st.room(id)
		if r == nil || r.Status != roomCanOpen {
			return s.Send(20608, proto.Message{"code": 0, "msg": "该区域暂时无法探索", "room_id": id})
		}
		if e := r.cfg(); e.EvtType == evtMysterious { // the mysterious room shows the guard behind it
			if p := e.pairs(); len(p) > 0 {
				r.Evt = p[0][0]
			}
		}
		r.Status = roomOpen
		if err := s.Send(20608, proto.Message{"code": 1, "msg": "", "room_id": id}); err != nil {
			return err
		}
		return a.sendRooms(s, st, 20603, []int64{id})
	})
}

// finish marks a room over, unlocks what that opens and tells the client.
func (a *adv) finish(s *gateway.Session, st *advState, r *advRoom) error {
	r.Status = roomOver
	changed := append([]int64{r.ID}, st.refresh()...)
	return a.sendRooms(s, st, 20603, changed)
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
	if err := s.Send(20622, proto.Message{}); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "获得增益效果")
}

func (a *adv) doCollect(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, reply func(int64, string) error) error {
	if err := a.gain(s, role, pairsOf(e.BaseItems)); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return s.Send(20628, proto.Message{"msg": "收集成功", "items": itemMessages(pairsOf(e.BaseItems))})
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
func (a *adv) heroTeam(role *store.Role, st *advState) ([]fightUnit, []proto.Message, *advHero, error) {
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
	team, objects, err := a.fights.teamFor(role, 1, []store.FormationSlot{{Pos: 3, PartnerID: h.PID}})
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
	team, objects, hero, err := a.heroTeam(role, st)
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
	if err := reply(1, ""); err != nil {
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
				if e.EvtType == evtBoss {
					st.Boss = true
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
			}
			if err := a.sendHeroes(s, role, st); err != nil {
				return err
			}
			return s.Send(20600, a.baseMessage(st))
		})
	})
}

func (a *adv) baseMessage(st *advState) proto.Message {
	return proto.Message{"pass_id": st.PassID, "pass_num": st.PassNum, "id": st.Floor, "room": 0, "is_kill_boss": b2i(st.Boss),
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
	if err := a.gain(s, role, [][2]int64{{stake[0], back}}); err != nil {
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

// doAnswer asks three questions; every answer counts as right because the client has no answer key.
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
	push := func(ret, sel int64, bid int64, ret_msg string, ret_items [][2]int64) error {
		return s.Send(20623, proto.Message{"ret": ret, "sel_val": sel, "right": sel, "num": min(r.Step+1, 3), "max": 3, "bid": bid, "ret_msg": ret_msg,
			"now_items": itemMessages(a.quizReward(e, r.Right)), "max_items": itemMessages(a.quizReward(e, 3)), "ret_items": itemMessages(ret_items)})
	}
	if action == 0 {
		if err := push(0, 0, r.Qs[r.Step], "", nil); err != nil {
			return err
		}
		return reply(1, "")
	}
	sel := ext[1]
	if sel < 1 || sel > 4 {
		return reply(0, "请选择答案")
	}
	r.Right++
	r.Step++
	if r.Step < 3 {
		if err := push(1, sel, r.Qs[r.Step], "", nil); err != nil {
			return err
		}
		return reply(1, "")
	}
	prize := a.quizReward(e, r.Right)
	if err := a.gain(s, role, prize); err != nil {
		return err
	}
	if err := push(1, sel, 0, "答题结束", prize); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return reply(1, "")
}

// doNpc runs an npc: a quiz-like npc asks three of its questions and pays at the end, a one-question npc
// (a trade or a remains to search) acts at once. The client does not say what they pay, so the pay is ours.
func (a *adv) doNpc(s *gateway.Session, role *store.Role, st *advState, r *advRoom, e advEventCfg, action int64, ext map[int64]int64, reply func(int64, string) error) error {
	groups := advCfg.Npc[strconv.FormatInt(r.Evt, 10)]
	if len(groups) == 0 {
		return reply(0, "没有对话")
	}
	if len(r.Qs) == 0 {
		idx := newRng().Perm(len(groups))
		for _, i := range idx[:min(3, len(groups))] {
			r.Qs = append(r.Qs, int64(i))
		}
		r.Step = 0
	}
	cur := groups[r.Qs[r.Step]]
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
	o := opts[pick-1]
	var prize [][2]int64
	if len(o.Lose) > 0 {
		ok, err := a.pay(s, role, pairsOf(o.Lose))
		if err != nil {
			return err
		}
		if !ok {
			return reply(0, "资源不足")
		}
		prize = [][2]int64{{advOre, 5}}
	} else if e.EvtType == evtNpcTalk && pick == 1 {
		prize = [][2]int64{{1, 3000}}
	}
	r.Step++
	if e.EvtType == evtNpc && int(r.Step) < len(r.Qs) {
		if err := s.Send(20627, proto.Message{"evt_id": r.Evt, "id": groups[r.Qs[r.Step]][0].ID}); err != nil {
			return err
		}
		return reply(1, "")
	}
	if e.EvtType == evtNpc {
		prize = [][2]int64{{1, 5000 * r.Step}}
	}
	if err := a.gain(s, role, prize); err != nil {
		return err
	}
	if err := a.finish(s, st, r); err != nil {
		return err
	}
	return s.Send(20628, proto.Message{"msg": "对话结束", "items": itemMessages(prize)})
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
	st.PassID = max(st.PassID, cleared)
	st.PassNum++
	prize := pairsOf(advCfg.Floors[cleared-1].FloorReward)
	if len(prize) > 0 {
		if err := a.gain(s, role, prize); err != nil {
			return err
		}
	}
	st.buildFloor(cleared + 1)
	if len(prize) > 0 {
		if err := s.Send(20606, proto.Message{"items_list": itemMessages(prize), "vip_buff": 0}); err != nil {
			return err
		}
	}
	if err := reply(1, ""); err != nil {
		return err
	}
	if err := sendBase(s, st); err != nil {
		return err
	}
	return a.sendRooms(s, st, 20602, nil)
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
	return a.with(s, func(role *store.Role, st *advState) error {
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
	})
}

func (a *adv) takeBox(s *gateway.Session, req proto.Message) error {
	return a.with(s, func(role *store.Role, st *advState) error {
		id := req.Int("id")
		for _, b := range advCfg.RoundRewards {
			if b.ID != id {
				continue
			}
			if st.BoxTaken[id] || st.Kills < b.Count {
				return s.Send(20635, proto.Message{"code": 0, "msg": "无法领取"})
			}
			st.BoxTaken[id] = true
			if err := a.gain(s, role, pairsOf(b.Items)); err != nil {
				return err
			}
			return s.Send(20635, proto.Message{"code": 1, "msg": "领取成功"})
		}
		return s.Send(20635, proto.Message{"code": 0, "msg": "无法领取"})
	})
}
