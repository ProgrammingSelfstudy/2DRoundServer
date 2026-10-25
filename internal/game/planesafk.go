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

const (
	pfModule     = "planesafk"
	pfLines      = 11 // line 1 is the start, lines 2-10 hold events, line 11 the boss
	pfBossLine   = 11
	pfStartIndex = 3
	pfRound      = 48 * time.Hour // the planes reset every 48 hours (the client's rule text)
	pfRosterSize = 7              // the strongest seven heroes go along (planes_buff_partner_num)
	pfHireBase   = 1000000        // hired heroes get ids from here
)

// ext types of 28600 (PlanesafkConst.Proto_28600)
const (
	pfExtFormation = 1
	pfExtHallows   = 2
	pfExtOwnHero   = 3
	pfExtHiredHero = 4
	pfExtBuff      = 5
	pfExtShopEnter = 6
	pfExtShopBuy   = 7
	pfExtShopLeave = 8
	pfExtHire      = 9
)

type pfTile struct{ Line, Index, Evt, Power int64 }

type pfHero struct{ Flag, PID, BID, Lev, Star, HP int64 } // HP in per cent

type pfSlot struct{ Pos, ID, Flag int64 }

type pfRental struct{ Pos, BID, Lev, Star int64 }

type pfGood struct {
	Pos, ID int64
	Bought  bool
}

type pfState struct {
	Floor, Diff, MapID, Line, Index int64
	Tiles                           []pfTile
	Done                            map[string]bool // tiles whose event is over, "line:index"
	Chosen                          map[string]int64
	Heroes                          []pfHero
	Buffs                           []int64
	FormType, Hallows               int64
	Form                            []pfSlot
	Rentals                         []pfRental
	BuffOpts                        []int64
	ShopOpen                        bool
	ShopSelected                    bool
	Goods                           []pfGood
	Loot                            [][2]int64
	Cleared, Taken                  map[string]bool
	RunStart                        int64
	NextHire                        int64
	Points                          int64
	OrderTaken                      map[string]bool
	OrderStart                      int64
}

type pfSvc struct {
	st     *store.Store
	fights *FightService
}

func tileKey(line, index int64) string { return fmt.Sprintf("%d:%d", line, index) }

// RegisterPlanesafk registers the planes adventure (长环岛冒险, 28600-28626): a three-floor board of lines of tiles,
// one step up the board at a time; fights, relics, merchants, springs, crosses and hired heroes; the war order
// (征战之证) that counts the boss points. The client has no board layout, no monster stats and no relic effects (the
// server makes them), so the board, the enemies (a scaled copy of the role's own best heroes) and the relics
// (recorded, not applied to fights) are ours. Level 30.
func RegisterPlanesafk(r *gateway.Router, db *store.Store) {
	p := &pfSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(28600, p.act)
	r.Handle(28601, p.start)
	r.Handle(28602, p.base)
	r.Handle(28603, p.board)
	r.Handle(28604, p.nextFloor)
	r.Handle(28605, p.takeFloor)
	r.Handle(28610, p.formPower)
	r.Handle(28611, p.saveForm)
	r.Handle(28612, p.getForm)
	r.Handle(28613, p.roster)
	r.Handle(28616, p.orderInfo)
	r.Handle(28617, p.orderTake)
	r.Handle(28618, p.orderRed)
	r.Handle(28619, p.orderPop)
	r.Handle(28620, p.buffList)
	r.Handle(28623, p.rentalInfo)
	r.Handle(28625, p.lootList)
	r.Handle(28626, func(*gateway.Session, proto.Message) error { return nil })
}

func (p *pfSvc) with(s *gateway.Session, fn func(role *store.Role, st *pfState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := p.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &pfState{}
	if _, err := p.st.LoadState(role.RID, pfModule, st); err != nil {
		return err
	}
	if st.Done == nil {
		st.Done = map[string]bool{}
	}
	if st.Chosen == nil {
		st.Chosen = map[string]int64{}
	}
	if st.Cleared == nil {
		st.Cleared = map[string]bool{}
	}
	if st.Taken == nil {
		st.Taken = map[string]bool{}
	}
	if st.OrderTaken == nil {
		st.OrderTaken = map[string]bool{}
	}
	now := s.Now().Unix()
	if st.RunStart != 0 && now >= st.RunStart+int64(pfRound/time.Second) { // a new round: the board and the relics are gone
		keep := *st
		*st = pfState{Points: keep.Points, OrderTaken: keep.OrderTaken, OrderStart: keep.OrderStart, Done: map[string]bool{}, Chosen: map[string]int64{},
			Cleared: map[string]bool{}, Taken: map[string]bool{}}
	}
	if st.OrderStart == 0 {
		st.OrderStart = now
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return p.st.SaveState(role.RID, pfModule, st)
}

func (p *pfSvc) gain(s *gateway.Session, role *store.Role, list [][2]int64) error {
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

func (p *pfSvc) pay(s *gateway.Session, role *store.Role, list [][2]int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(list), 1, sa, si)
	err := p.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, sendAssets(s, p.st, role.RID)
}

// ---- the board

// bestHeroes are the role's strongest heroes, strongest first.
func bestHeroes(heroes []store.Partner, n int) []store.Partner {
	var list []store.Partner
	for _, h := range heroes {
		if _, ok := partnerTable[h.BID]; ok {
			list = append(list, h)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		return partnerMessage(list[i]).Int("power") > partnerMessage(list[j]).Int("power")
	})
	if len(list) > n {
		list = list[:n]
	}
	return list
}

// evtID is the id of an event of a line: prefix by floor (71 72 73, 74 on the hard third floor), line, kind.
func pfEvtID(floor, diff, line, kind int64) int64 {
	prefix := 70 + floor
	if floor == 3 && diff == 2 {
		prefix = 74
	}
	return prefix*10000 + line*100 + kind
}

func (p *pfSvc) newFloor(role *store.Role, st *pfState, floor, diff int64, now int64) error {
	heroes, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	power := rolePower(heroes)
	st.Floor, st.Diff, st.Line, st.Index = floor, diff, 1, pfStartIndex
	st.MapID = 2000 + floor
	if floor == 3 && diff == 2 {
		st.MapID = 2004
	}
	st.Done, st.Chosen, st.Rentals, st.BuffOpts, st.ShopOpen, st.ShopSelected, st.Goods = map[string]bool{}, map[string]int64{}, nil, nil, false, false, nil
	st.Tiles = []pfTile{{Line: 1, Index: pfStartIndex}}
	rng := rand.New(rand.NewSource(now + floor))
	rate := int64(1000)
	if rates := pfCfg.nums("planes_challenge_rate"); diff >= 1 && int(diff) <= len(rates) {
		rate = rates[diff-1]
	}
	for line := int64(2); line <= pfBossLine; line++ {
		factor := (400 + 70*(line-2)) * rate / 1000
		if line == pfBossLine {
			st.Tiles = append(st.Tiles, pfTile{Line: line, Index: pfStartIndex, Evt: pfEvtID(floor, diff, line, 1), Power: power * factor / 1000})
			continue
		}
		kinds := []int64{1, 1, 1, 2, 3, 4, 5, 6} // monster x3, advanced monster, spring, cross, hire, merchant
		for idx := int64(1); idx <= 5; idx++ {
			kind := kinds[rng.Intn(len(kinds))]
			if idx == 1+int64(rng.Intn(5)) { // every line has a fight
				kind = 1
			}
			evt := pfEvtID(floor, diff, line, kind)
			if rng.Intn(8) == 0 { // some fights are guards of a hoard
				evt = (80+floor)*10000 + line*100 + 1 + int64(rng.Intn(2))
				if floor == 3 && diff == 2 {
					evt = 84*10000 + line*100 + 1 + int64(rng.Intn(2))
				}
			}
			if _, ok := pfCfg.events[evt]; !ok {
				evt = pfEvtID(floor, diff, line, 1)
			}
			t := pfTile{Line: line, Index: idx, Evt: evt}
			if e := pfCfg.events[evt]; e.Type == pfEvtMonster || e.Type == pfEvtGuard {
				t.Power = power * factor / 1000
				if e.Type == pfEvtGuard {
					t.Power = t.Power * 12 / 10
				}
				if evt%100 == 2 {
					t.Power = t.Power * 13 / 10
				}
			}
			st.Tiles = append(st.Tiles, t)
		}
	}
	return nil
}

func (st *pfState) tile(line, index int64) *pfTile {
	for i := range st.Tiles {
		if st.Tiles[i].Line == line && st.Tiles[i].Index == index {
			return &st.Tiles[i]
		}
	}
	return nil
}

// tileMessage: is_hide 1 for the tiles the role cannot step on (not next to it, or the line's other choices), is_walk
// 1 for the tile it stands on.
func (st *pfState) tileMessage(t pfTile) proto.Message {
	evt := t.Evt
	if st.Done[tileKey(t.Line, t.Index)] {
		evt = 0
	}
	hide := int64(0)
	if c, ok := st.Chosen[strconv.FormatInt(t.Line, 10)]; ok && c != t.Index {
		hide = 1
	}
	walk := int64(0)
	if t.Line < st.Line || (t.Line == st.Line && t.Index == st.Index) {
		if c, ok := st.Chosen[strconv.FormatInt(t.Line, 10)]; (ok && c == t.Index) || (t.Line == 1 && t.Index == pfStartIndex) {
			walk = 1
		}
	}
	return proto.Message{"line": t.Line, "index": t.Index, "evt_id": evt, "is_hide": hide, "is_walk": walk, "combat_power": t.Power}
}

func (st *pfState) tileList() []proto.Message {
	out := []proto.Message{}
	for _, t := range st.Tiles {
		out = append(out, st.tileMessage(t))
	}
	return out
}

func (p *pfSvc) open(s *gateway.Session, cmd uint16, fail proto.Message, fn func(role *store.Role, st *pfState) error) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		if need := pfCfg.num("planes_open_lev"); int64(role.Lev) < need {
			fail["msg"] = fmt.Sprintf("玩法达%d级开启", need)
			return s.Send(cmd, fail)
		}
		return fn(role, st)
	})
}

func (p *pfSvc) start(s *gateway.Session, _ proto.Message) error {
	return p.open(s, 28601, proto.Message{"flag": 0}, func(role *store.Role, st *pfState) error {
		if st.Floor == 0 {
			if err := p.newFloor(role, st, 1, 1, s.Now().UnixNano()); err != nil {
				return err
			}
			st.RunStart = s.Now().Unix()
			heroes, err := p.st.PartnersOf(role.RID)
			if err != nil {
				return err
			}
			st.Heroes = nil
			for _, h := range bestHeroes(heroes, pfRosterSize) {
				st.Heroes = append(st.Heroes, pfHero{Flag: 0, PID: h.PartnerID, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star), HP: 100})
			}
		}
		return s.Send(28601, proto.Message{"flag": 1, "msg": ""})
	})
}

func (p *pfSvc) base(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		start := st.RunStart
		if start == 0 {
			start = s.Now().Unix()
		}
		return s.Send(28602, proto.Message{"update_time": start + int64(pfRound/time.Second), "look_id": 0, "list": []proto.Message{}, "is_holiday": 0})
	})
}

func (p *pfSvc) boardMessage(st *pfState) proto.Message {
	return proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "map_id": st.MapID, "tile_list": st.tileList(),
		"is_can_reward": b2i(st.Cleared[strconv.FormatInt(st.Floor, 10)]), "is_reward": b2i(st.Taken[strconv.FormatInt(st.Floor, 10)])}
}

func (p *pfSvc) board(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		if st.Floor == 0 {
			return s.Send(28603, proto.Message{"floor": 0, "line": 0, "index": 0, "map_id": 0, "tile_list": []proto.Message{}, "is_can_reward": 0, "is_reward": 0})
		}
		return s.Send(28603, p.boardMessage(st))
	})
}

// nextFloor (28604): go on to the next floor once the boss of this one is down; the hard third floor wants the star
// tower at its 300th floor (the client's rule).
func (p *pfSvc) nextFloor(s *gateway.Session, req proto.Message) error {
	floor, diff := req.Int("floor"), max(1, req.Int("difficulty"))
	return p.open(s, 28604, proto.Message{"flag": 0}, func(role *store.Role, st *pfState) error {
		refuse := func(msg string) error { return s.Send(28604, proto.Message{"flag": 0, "msg": msg}) }
		switch {
		case st.Floor == 0 || floor != st.Floor+1 || floor > pfCfg.num("planes_max_floor"):
			return refuse("无法前往该层")
		case !st.Cleared[strconv.FormatInt(st.Floor, 10)]:
			return refuse("请先击败本层首领")
		case diff != 1 && diff != 2:
			return refuse("难度错误")
		}
		if diff == 2 {
			tower, err := p.st.StarTowerOf(role.RID)
			if err != nil {
				return err
			}
			if floor != 3 || tower.MaxTower < pfCfg.num("planes_difficult_limit_lev") {
				return refuse("未达到困难难度的开启条件")
			}
		}
		if err := p.newFloor(role, st, floor, diff, s.Now().UnixNano()); err != nil {
			return err
		}
		if err := s.Send(28604, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(28603, p.boardMessage(st))
	})
}

// takeFloor (28605): the chest of a cleared floor.
func (p *pfSvc) takeFloor(s *gateway.Session, req proto.Message) error {
	floor := req.Int("floor")
	return p.open(s, 28605, proto.Message{"flag": 0}, func(role *store.Role, st *pfState) error {
		key := strconv.FormatInt(floor, 10)
		if !st.Cleared[key] || st.Taken[key] {
			return s.Send(28605, proto.Message{"flag": 0, "msg": "无法领取"})
		}
		st.Taken[key] = true
		var prize [][2]int64
		for _, id := range pfCfg.events[pfEvtID(floor, st.Diff, pfBossLine, 1)].RewardList { // the boss table of the floor
			prize = append(prize, pfRewardFor(id, int64(role.Lev))...)
		}
		st.Loot = append(st.Loot, prize...)
		if err := p.gain(s, role, prize); err != nil {
			return err
		}
		return s.Send(28605, proto.Message{"flag": 1, "msg": "领取成功"})
	})
}
