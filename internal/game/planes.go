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

const plModule = "planes"

// ext types of 23104 (PlanesConst.Proto_23104)
const (
	plExtFormation  = 6
	plExtHallows    = 7
	plExtOwnHero    = 8
	plExtBuff       = 9
	plExtHire       = 10
	plExtHiredHero  = 11
	combatPlanesOld = 40
)

type plTile struct {
	Index, Evt, Power int64
	Down              bool
}

type plFloor struct {
	Tiles  []plTile
	Pos    int64
	Walked []int64
}

type plState struct {
	Round             int64
	Dun, Floor        int64
	Floors            []plFloor
	Heroes            []pfHero
	Buffs             []int64
	FormType, Hallows int64
	Form              []pfSlot
	Rentals           []pfRental
	RentalTile        int64
	BuffOpts          []int64
	BuffTile          int64
	Val, Max          int64
	CanReward, Taken  map[string]bool
	NextHire          int64
}

type plSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterPlanes registers the old planes ("秘境", 23100-23123): five dungeons of two maps each, walked tile by tile; the same
// kinds of events as the planes adventure plus boards, elders and portals between the two maps. The client has no
// event placement, no monster stats and no relic effects (the server makes them), so those are ours. The puzzle
// pieces (switches, lifts, barriers and the hammer) are not made. Level 30.
func RegisterPlanes(r *gateway.Router, db *store.Store) {
	p := &plSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(23100, p.base)
	r.Handle(23103, p.move)
	r.Handle(23104, p.act)
	r.Handle(23105, p.choose)
	r.Handle(23106, p.sendMap)
	r.Handle(23112, p.bag)
	r.Handle(23115, p.roster)
	r.Handle(23116, p.rentalInfo)
	r.Handle(23117, p.takeFirst)
	r.Handle(23118, p.buffList)
	r.Handle(23120, p.formPower)
	r.Handle(23121, p.saveForm)
	r.Handle(23122, p.getForm)
	r.Handle(23123, func(*gateway.Session, proto.Message) error { return nil })
}

// plRound is the number of the two-day round a moment is in (the client's update_day and start_unixtime).
func plRound(now time.Time) (round, end int64) {
	start := int64(1576530000)
	period := int64(plResetDays * 86400)
	round = (now.Unix() - start) / period
	return round, start + (round+1)*period
}

func (p *plSvc) with(s *gateway.Session, fn func(role *store.Role, st *plState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := p.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &plState{}
	if _, err := p.st.LoadState(role.RID, plModule, st); err != nil {
		return err
	}
	if st.CanReward == nil {
		st.CanReward = map[string]bool{}
	}
	if st.Taken == nil {
		st.Taken = map[string]bool{}
	}
	if round, _ := plRound(s.Now()); st.Round != round { // the dungeons start over; what was taken for good stays
		*st = plState{Round: round, CanReward: st.CanReward, Taken: st.Taken}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return p.st.SaveState(role.RID, plModule, st)
}

func (p *plSvc) gain(s *gateway.Session, role *store.Role, list [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(list), 1, ga, gi)
	if err := p.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, p.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, p.st)
}

func plOpenFail(s *gateway.Session, role *store.Role, cmd uint16, fail proto.Message) (bool, error) {
	need := plCfg.num("open_lev")
	if int64(role.Lev) >= need {
		return false, nil
	}
	fail["msg"] = fmt.Sprintf("玩法达%d级开启", need)
	return true, s.Send(cmd, fail)
}

// ---- who can go where

func (p *plSvc) power(role *store.Role) (int64, error) {
	heroes, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return 0, err
	}
	return rolePower(heroes), nil
}

// choosable lists the dungeons the role could enter: its level and power, and the dungeon before it at full progress.
func (p *plSvc) choosable(role *store.Role, st *plState, power int64) []int64 {
	var out []int64
	for _, c := range plCfg.Customs {
		ok := int64(role.Lev) >= c.LevLimit && power >= c.PowerLimit
		for _, pre := range c.IDLimit {
			ok = ok && (st.CanReward[strconv.FormatInt(pre, 10)] || st.Taken[strconv.FormatInt(pre, 10)])
		}
		if ok {
			out = append(out, c.ID)
		}
	}
	return out
}

func (p *plSvc) base(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		power, err := p.power(role)
		if err != nil {
			return err
		}
		ids, can, got := []proto.Message{}, []proto.Message{}, []proto.Message{}
		for _, id := range p.choosable(role, st, power) {
			ids = append(ids, proto.Message{"id": id})
		}
		for _, c := range plCfg.Customs {
			k := strconv.FormatInt(c.ID, 10)
			if st.Taken[k] {
				got = append(got, proto.Message{"reward_id": c.ID})
			} else if st.CanReward[k] {
				can = append(can, proto.Message{"can_reward_id": c.ID})
			}
		}
		_, end := plRound(s.Now())
		return s.Send(23100, proto.Message{"dun_id": st.Dun, "val": st.Val, "max_val": max(st.Max, 1), "ids": ids, "reward_ids": got, "can_reward_ids": can,
			"update_time": end, "look_id": 0, "is_holiday": 0, "list": []proto.Message{}})
	})
}

// ---- making a dungeon

func (st *plState) floorOf(f int64) *plFloor {
	if f < 1 || int(f) > len(st.Floors) {
		return nil
	}
	return &st.Floors[f-1]
}

func (f *plFloor) tile(index int64) *plTile {
	for i := range f.Tiles {
		if f.Tiles[i].Index == index {
			return &f.Tiles[i]
		}
	}
	return nil
}

// walkableCells lists the road cells of a map from the top to the bottom.
func plCells(mapID int64) []int64 {
	m := plCfg.maps[mapID]
	var out []int64
	for y := int64(1); y <= int64(len(m.Block)); y++ {
		for x := int64(1); x <= int64(len(m.Block[y-1])); x++ {
			if m.walkable(x, y) {
				out = append(out, plIndex(x, y))
			}
		}
	}
	return out
}

// buildFloor places the events of a floor along the road: the start at the top, the way to the other map and the
// boss at the bottom, guards every few cells (stronger further down), four groups of "pick one" tiles, chests, a
// board, an elder and a random reward.
func buildFloor(dun, floor int64, power int64, rng *rand.Rand) plFloor {
	cells := plCells(plMapID(dun, floor))
	n := len(cells)
	f := plFloor{Pos: cells[0]}
	used := map[int64]bool{}
	put := func(pos int, evt int64, pw int64) bool {
		if pos < 0 || pos >= n || used[cells[pos]] {
			return false
		}
		if _, ok := plCfg.events[evt]; !ok {
			return false
		}
		used[cells[pos]] = true
		f.Tiles = append(f.Tiles, plTile{Index: cells[pos], Evt: evt, Power: pw})
		return true
	}
	base := dun*1000000 + floor*10000
	put(0, 1001, 0)
	if floor == 1 {
		put(n-1, base+1301, 0)
	} else {
		put(1, base+1301, 0)
		put(n-1, base+5701, power*13/10)
	}
	put(3, base+1201, 0)
	put(n/4, base+1501, 0)
	put(n/2+2, base+1601, 0)
	for g := int64(1); g <= 4; g++ { // four groups of "pick one" tiles
		at := int(g) * n / 5
		for k := int64(1); k <= 4; k++ {
			put(at+int(k)-1, base+4000+g*100+k, 0)
		}
		if g == 4 {
			put(at+4, base+4405, 0)
		}
	}
	chests := []int64{2001, 2002, 2003, 2005, 2011}
	for i, c := range chests {
		for try := 0; try < 6; try++ {
			if put(8+i*(n/6)+rng.Intn(3)+try, c, 0) {
				break
			}
		}
	}
	for pos := 6; pos < n-3; pos += 4 { // guards along the road
		factor := int64(400 + 700*pos/n) // 40% of the role's power at the top, 110% at the bottom
		kind := int64(5101)
		if floor == 2 {
			kind = 5501
		}
		pw := power * factor / 1000
		if (pos/4)%3 == 2 {
			kind += 100
			pw = pw * 13 / 10
		}
		put(pos, base+kind, pw)
	}
	return f
}

func (p *plSvc) newDungeon(role *store.Role, st *plState, dun int64, now int64) error {
	power, err := p.power(role)
	if err != nil {
		return err
	}
	all, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	st.Dun, st.Floor = dun, 1
	rng := rand.New(rand.NewSource(now + dun))
	st.Floors = []plFloor{buildFloor(dun, 1, power, rng), buildFloor(dun, 2, power, rng)}
	st.Val, st.Max = 0, 0
	for _, f := range st.Floors {
		for _, t := range f.Tiles {
			if k := plCfg.events[t.Evt].Type; k == plEvtMonster || k == plEvtGuard {
				st.Max++
			}
		}
	}
	st.Heroes, st.Buffs, st.Form = nil, nil, nil
	for _, h := range bestHeroes(all, plRosterSize) {
		st.Heroes = append(st.Heroes, pfHero{PID: h.PartnerID, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star), HP: 100})
	}
	for i := range st.Floors {
		st.Floors[i].Walked = []int64{st.Floors[i].Pos}
	}
	return nil
}

func (p *plSvc) choose(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return p.with(s, func(role *store.Role, st *plState) error {
		if done, err := plOpenFail(s, role, 23105, proto.Message{"flag": 0, "id": id, "floor": 0}); done || err != nil {
			return err
		}
		refuse := func(msg string) error {
			return s.Send(23105, proto.Message{"flag": 0, "msg": msg, "id": id, "floor": 0})
		}
		if st.Dun != 0 && st.Dun != id {
			return refuse("本轮已选择其他副本")
		}
		if st.Dun == 0 {
			power, err := p.power(role)
			if err != nil {
				return err
			}
			ok := false
			for _, c := range p.choosable(role, st, power) {
				ok = ok || c == id
			}
			if !ok {
				return refuse("未满足该副本的开启条件")
			}
			if err := p.newDungeon(role, st, id, s.Now().UnixNano()); err != nil {
				return err
			}
		}
		return s.Send(23105, proto.Message{"flag": 1, "msg": "", "id": st.Dun, "floor": st.Floor})
	})
}

// ---- the map

func (st *plState) tileList(f *plFloor) []proto.Message {
	out := []proto.Message{}
	for _, t := range f.Tiles {
		status := int64(1)
		if t.Down {
			status = 2
		}
		e := plCfg.events[t.Evt]
		out = append(out, proto.Message{"index": t.Index, "evtid": t.Evt, "status": status, "is_walk": e.IsWalk, "res_id": 0, "platform": 0, "switch": 0, "is_hide": 0})
	}
	return out
}

func (st *plState) walkTiles(f *plFloor) []proto.Message {
	out := []proto.Message{}
	for _, w := range f.Walked {
		out = append(out, proto.Message{"pos": w})
	}
	return out
}

func (p *plSvc) mapMessage(st *plState) proto.Message {
	f := st.floorOf(st.Floor)
	return proto.Message{"floor": st.Floor, "index": f.Pos, "map_id": plMapID(st.Dun, st.Floor), "tile_list": st.tileList(f), "walk_tile": st.walkTiles(f)}
}

// sendMap (23106) is what the map window asks for when it opens: the answer, then the map (23101).
func (p *plSvc) sendMap(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		if st.Dun == 0 || st.floorOf(st.Floor) == nil {
			return s.Send(23106, proto.Message{"flag": 0, "msg": "尚未进入副本"})
		}
		if f := req.Int("floor"); f >= 1 && f <= plFloorCount {
			st.Floor = f
		}
		if err := s.Send(23106, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(23101, p.mapMessage(st))
	})
}

func (p *plSvc) pushTiles(s *gateway.Session, st *plState, tiles ...*plTile) error {
	if len(tiles) == 0 {
		return nil
	}
	sub := &plFloor{}
	for _, t := range tiles {
		sub.Tiles = append(sub.Tiles, *t)
	}
	return s.Send(23102, proto.Message{"floor": st.Floor, "tile_list": st.tileList(sub)})
}

// move (23103): one step to a road cell next to the one the role stands on.
func (p *plSvc) move(s *gateway.Session, req proto.Message) error {
	index := req.Int("index")
	return p.with(s, func(role *store.Role, st *plState) error {
		refuse := func(msg string) error { return s.Send(23103, proto.Message{"code": 0, "msg": msg, "index": index}) }
		f := st.floorOf(st.Floor)
		if st.Dun == 0 || f == nil {
			return refuse("尚未进入副本")
		}
		x, y := plXY(index)
		cx, cy := plXY(f.Pos)
		m := plCfg.maps[plMapID(st.Dun, st.Floor)]
		switch {
		case !m.walkable(x, y):
			return refuse("目标点为不可行走区域，请选择其他目标点")
		case plDistance(cx, cy, x, y) > 2:
			return refuse("目标点太远")
		}
		if t := f.tile(index); t != nil && !t.Down {
			if e := plCfg.events[t.Evt]; e.IsWalk == 0 && e.Type != plEvtStart && e.Type != plEvtPortal && e.Type != plEvtBoard && e.Type != plEvtDialog {
				// an event that stands in the way: the role steps next to it, the click on it is the event's own request
				if plDistance(cx, cy, x, y) == 0 {
					return refuse("目标点太远")
				}
			}
		}
		f.Pos = index
		seen := false
		for _, w := range f.Walked {
			seen = seen || w == index
		}
		if !seen {
			f.Walked = append(f.Walked, index)
		}
		return s.Send(23103, proto.Message{"code": 1, "msg": "", "index": index})
	})
}

func (p *plSvc) bag(s *gateway.Session, _ proto.Message) error {
	return s.Send(23112, proto.Message{"secret_item": []proto.Message{}})
}

// ---- heroes, formation, relics

func (p *plSvc) roster(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		list := []proto.Message{}
		all, _ := p.st.PartnersOf(role.RID)
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		for _, h := range st.Heroes {
			pp := store.Partner{PartnerID: h.PID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
			if h.Flag == 0 {
				if own, ok := byID[h.PID]; ok {
					pp = own
				}
			}
			list = append(list, proto.Message{"flag": h.Flag, "partner_id": h.PID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "power": partnerMessage(pp).Int("power"),
				"hp_per": h.HP, "ext_data": []proto.Message{}})
		}
		return s.Send(23115, proto.Message{"partners": list})
	})
}

func (p *plSvc) pushHP(s *gateway.Session, st *plState) error {
	list := []proto.Message{}
	for _, h := range st.Heroes {
		list = append(list, proto.Message{"flag": h.Flag, "partner_id": h.PID, "hp_per": h.HP})
	}
	return s.Send(23119, proto.Message{"partners": list})
}

func (p *plSvc) formPower(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		all, _ := p.st.PartnersOf(role.RID)
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		var total int64
		for _, e := range req.Arr("partner_ids") {
			for _, h := range st.Heroes {
				if h.Flag == e.Int("flag") && h.PID == e.Int("id") {
					pp := store.Partner{PartnerID: h.PID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
					if own, ok := byID[h.PID]; ok && h.Flag == 0 {
						pp = own
					}
					total += partnerMessage(pp).Int("power")
				}
			}
		}
		return s.Send(23120, proto.Message{"power": total})
	})
}

func (p *plSvc) saveForm(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		var form []pfSlot
		seen := map[int64]bool{}
		for _, e := range req.Arr("pos_info") {
			slot := pfSlot{Pos: e.Int("pos"), ID: e.Int("id"), Flag: e.Int("flag")}
			ok := false
			for _, h := range st.Heroes {
				ok = ok || (h.PID == slot.ID && h.Flag == slot.Flag)
			}
			if !ok || seen[slot.ID] || len(form) == formMaxHeroes {
				continue
			}
			seen[slot.ID] = true
			form = append(form, slot)
		}
		if len(form) == 0 {
			return s.Send(23121, proto.Message{"flag": 0, "msg": "请选择出战英雄"})
		}
		st.Form, st.FormType, st.Hallows = form, req.Int("formation_type"), req.Int("hallows_id")
		return s.Send(23121, proto.Message{"flag": 1, "msg": ""})
	})
}

func (p *plSvc) getForm(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		form, ft := st.Form, st.FormType
		if len(form) == 0 {
			ft = 1
			for i, h := range st.Heroes {
				if i == 5 {
					break
				}
				form = append(form, pfSlot{Pos: int64(i + 1), ID: h.PID, Flag: h.Flag})
			}
		}
		out := []proto.Message{}
		for _, e := range form {
			out = append(out, proto.Message{"pos": e.Pos, "id": e.ID, "flag": e.Flag})
		}
		return s.Send(23122, proto.Message{"formation_type": ft, "pos_info": out, "hallows_id": st.Hallows})
	})
}

func (p *plSvc) buffList(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		out := []proto.Message{}
		for _, b := range st.Buffs {
			out = append(out, proto.Message{"buff_id": b})
		}
		return s.Send(23118, proto.Message{"buffs": out})
	})
}

func (p *plSvc) takeFirst(s *gateway.Session, req proto.Message) error {
	id := req.Int("dun_id")
	return p.with(s, func(role *store.Role, st *plState) error {
		key := strconv.FormatInt(id, 10)
		if id < 1 || id > plDunCount || !st.CanReward[key] || st.Taken[key] {
			return s.Send(23117, proto.Message{"flag": 0, "msg": "无法领取", "dun_id": id})
		}
		st.Taken[key] = true
		delete(st.CanReward, key)
		if err := p.gain(s, role, pairsOf(plCfg.Customs[id-1].FirstReward)); err != nil {
			return err
		}
		return s.Send(23117, proto.Message{"flag": 1, "msg": "领取成功", "dun_id": id})
	})
}

func (p *plSvc) rentalInfo(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		pos := req.Int("pos")
		for _, r := range st.Rentals {
			if r.Pos == pos {
				m := partnerMessage(store.Partner{PartnerID: pfHireBase + pos, RID: role.RID, BID: r.BID, Lev: int(r.Lev), Star: int(r.Star)})
				out := proto.Message{"pos": pos, "holy_eqm": []proto.Message{}}
				for _, k := range []string{"bid", "lev", "star", "skills", "break_lev", "power", "use_skin", "end_time", "atk", "def_p", "def_s", "hp", "speed", "hit_rate",
					"dodge_rate", "crit_rate", "crit_ratio", "hit_magic", "dodge_magic", "def", "eqms", "artifacts", "dower_skill"} {
					out[k] = m[k]
				}
				return s.Send(23116, out)
			}
		}
		return nil
	})
}

var _ = errors.New
var _ = sort.Ints
