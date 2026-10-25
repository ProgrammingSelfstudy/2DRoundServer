package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// resonate.json: the crystal slot rules of the client (tools/extract_resonate.py).
//
//go:embed data/resonate.json
var resonateJSON []byte

var resonate = func() struct {
	CellInit, CellMax, CoolMinutes int
	Flush                          [][2]int64
	Star                           int
	Cost                           []struct {
		Item [][2]int64
		Gold [][2]int64
	}
	Refine   [][2]int64
	Alchemy  int64
	AmpStars int
	Levels   []stoneLevel
	StarRio  map[int]int64
} {
	var r struct {
		CellInit, CellMax, CoolMinutes int
		Flush                          [][2]int64
		Star                           int
		Cost                           []struct {
			Item [][2]int64
			Gold [][2]int64
		}
		Refine   [][2]int64
		Alchemy  int64
		AmpStars int
		Levels   []stoneLevel
		StarRio  map[int]int64
	}
	// the JSON keys are cell_init, cell_max, cool_minutes, ...
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resonateJSON, &raw); err != nil {
		panic("game: bad resonate.json")
	}
	get := func(k string, v any) {
		if err := json.Unmarshal(raw[k], v); err != nil {
			panic("game: bad resonate.json " + k)
		}
	}
	get("cell_init", &r.CellInit)
	get("cell_max", &r.CellMax)
	get("cool_minutes", &r.CoolMinutes)
	get("flush", &r.Flush)
	get("star", &r.Star)
	get("cost", &r.Cost)
	get("refine", &r.Refine)
	get("alchemy", &r.Alchemy)
	get("amp_stars", &r.AmpStars)
	get("levels", &r.Levels)
	get("star_rio", &r.StarRio)
	if r.CellMax == 0 || len(r.Cost) == 0 || len(r.Levels) == 0 || len(r.Refine) == 0 {
		panic("game: bad resonate.json")
	}
	return r
}()

// stoneLevel is one row of the stone tablet's level table.
type stoneLevel struct {
	Lev     int
	Expend  [][2]int64
	Attr    [][]any
	Limit   int64 // the most alchemy units in one batch
	Seconds int64 // seconds one alchemy unit takes
}

const (
	crystalHeroes = 5   // heroes that set the crystal's level (con_list)
	crystalMaxLev = 550 // max_cystal_lev of the capture
)

// crystalOf works out the crystal from a role's heroes (the ones in a slot have ResSlot > 0): the
// five highest-level heroes that sit in no slot, by level and then hero id, are its core and its level
// is the lowest of them (capture original-2026-10-25: the level went 30 -> 35 when the weakest of
// the five did). The core is listed weakest first as positions 1-5. Fewer than five candidates
// give level 0: no crystal yet.
func crystalOf(all []store.Partner) (lev int, core []store.Partner) {
	var cand []store.Partner
	for _, p := range all {
		if p.ResSlot == 0 {
			cand = append(cand, p)
		}
	}
	if len(cand) < crystalHeroes {
		return 0, nil
	}
	sort.Slice(cand, func(i, j int) bool {
		if cand[i].Lev != cand[j].Lev {
			return cand[i].Lev > cand[j].Lev
		}
		return cand[i].PartnerID < cand[j].PartnerID
	})
	core = cand[:crystalHeroes]
	sort.Slice(core, func(i, j int) bool {
		if core[i].Lev != core[j].Lev {
			return core[i].Lev < core[j].Lev
		}
		return core[i].PartnerID < core[j].PartnerID
	})
	lev = core[0].Lev
	if lev > crystalMaxLev {
		lev = crystalMaxLev
	}
	return lev, core
}

// resonateBreak is the break step a hero shows at the crystal's level: the first step whose cap is
// above it (capture: level 30 and 35 both show step 1), else the last step it has.
func resonateBreak(p store.Partner, lev int) int {
	info := partnerTable[p.BID]
	last := 0
	for b := 0; ; b++ {
		st, ok := growth.Brach[brachKey(info, b)]
		if !ok {
			return last
		}
		last = b
		if st.LevMax > lev {
			return b
		}
	}
}

func init() {
	store.SetResonanceRule(func(all []store.Partner) (int, func(store.Partner) int) {
		lev, _ := crystalOf(all)
		return lev, func(p store.Partner) int { return resonateBreak(p, lev) }
	})
}

// shownLevBreak is the level and break step a hero shows: the crystal's while it sits in a slot.
func shownLevBreak(p store.Partner) (int, int) {
	if p.ResSlot > 0 && p.ShownLev > 0 {
		return p.ShownLev, p.ShownBreak
	}
	return p.Lev, p.BreakLev
}

// resonanceSnapshot remembers what the heroes in a slot show, to find who changed afterwards.
func resonanceSnapshot(st *store.Store, rid int64) (map[int64][2]int, error) {
	ps, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	out := map[int64][2]int{}
	for _, p := range ps {
		l, b := shownLevBreak(p)
		out[p.PartnerID] = [2]int{l, b}
	}
	return out, nil
}

// pushResonanceShifts sends 11002 for every hero whose shown level or break step differs from
// before (the heroes in a slot follow the crystal's level; capture original-2026-10-25 sends one for
// each of them before the answer of the command that moved the level).
func pushResonanceShifts(s *gateway.Session, st *store.Store, rid int64, before map[int64][2]int, skip int64) error {
	ps, err := st.PartnersOf(rid)
	if err != nil {
		return err
	}
	for _, p := range ps {
		l, b := shownLevBreak(p)
		if was, ok := before[p.PartnerID]; ok && was == [2]int{l, b} || p.PartnerID == skip {
			continue
		}
		if err := s.Send(11002, partnerMessage(p)); err != nil {
			return err
		}
	}
	return nil
}

// crystalMessage is the 26425 answer: the core, the open slots, how many were unlocked and the level.
func crystalMessage(st *store.Store, rid, now int64) (proto.Message, error) {
	ps, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	lev, core := crystalOf(ps)
	slots, gold, item, err := st.ResonanceOf(rid)
	if err != nil {
		return nil, err
	}
	byPos := map[int64]store.ResSlot{}
	for _, sl := range slots {
		byPos[sl.Pos] = sl
	}
	con := []proto.Message{}
	for i, p := range core {
		con = append(con, proto.Message{"id": p.PartnerID, "pos": i + 1})
	}
	open := openSlots(gold, item)
	res := []proto.Message{}
	for pos := int64(1); pos <= int64(open); pos++ {
		sl := byPos[pos]
		cool := sl.CoolTime
		if cool <= now {
			cool = 0
		}
		res = append(res, proto.Message{"pos": pos, "id": sl.PartnerID, "cool_time": cool})
	}
	return proto.Message{"lev": lev, "con_list": con, "res_list": res, "gold_count": gold, "item_count": item,
		"max_cystal_lev": crystalMaxLev, "is_break": 0}, nil
}

func openSlots(gold, item int) int {
	n := resonate.CellInit + gold + item
	if n > resonate.CellMax {
		n = resonate.CellMax
	}
	return n
}

// RegisterResonance registers the slots of the resonance crystal: 26425 the state, 26426 put a hero
// in a slot (it then shows the crystal's level), 26427 take it out (the slot cools down for a day),
// 26428 end a cool-down for gems, 26429 unlock the next slot for items or gems. The costs, the
// cool-down and the star a hero needs are the client's (`resonate.json`); the rest of the system (26400
// attributes, extraction 26410-26414, empowerment 26420-26423, the crystal's amplification 26430-26432)
// is not implemented. Our reading, from capture original-2026-10-25: a hero may be put in when it is
// not one of the five core heroes and its level does not exceed the crystal's.
func RegisterResonance(r *gateway.Router, st *store.Store) {
	send := func(s *gateway.Session) error {
		m, err := crystalMessage(st, s.Role().RID, s.Now().Unix())
		if err != nil {
			return err
		}
		return s.Send(26425, m)
	}
	r.Handle(26425, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return send(s)
	})
	reply := func(s *gateway.Session, cmd uint16, ok int, msg string, pos int64) error {
		m := proto.Message{"result": ok, "msg": msg}
		if cmd != 26426 && cmd != 26427 {
			m["pos"] = pos
		}
		return s.Send(cmd, m)
	}
	r.Handle(26426, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, pos := req.Int("partner_id"), req.Int("pos")
		slots, gold, item, err := st.ResonanceOf(role.RID)
		_ = slots
		if err != nil {
			return err
		}
		if pos < 1 || pos > int64(openSlots(gold, item)) {
			return reply(s, 26426, 0, "槽位尚未解锁", pos)
		}
		p, err := st.PartnerByID(role.RID, id)
		if errors.Is(err, store.ErrNotFound) {
			return reply(s, 26426, 0, "英雄不存在", pos)
		}
		if err != nil {
			return err
		}
		all, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		lev, core := crystalOf(all)
		switch {
		case p.Star < resonate.Star:
			return reply(s, 26426, 0, "只有"+strconv.Itoa(resonate.Star)+"星或以上的英雄才能放入圣阵", pos)
		case lev == 0:
			return reply(s, 26426, 0, "共鸣水晶尚未激活", pos)
		case p.Lev > lev:
			return reply(s, 26426, 0, "英雄等级高于水晶等级", pos)
		}
		for _, c := range core {
			if c.PartnerID == id {
				return reply(s, 26426, 0, "水晶核心英雄不能放入圣阵", pos)
			}
		}
		before, err := resonanceSnapshot(st, role.RID)
		if err != nil {
			return err
		}
		if err := st.PlaceResonance(role.RID, pos, id, s.Now().Unix()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 26426, 0, "该槽位不可用或英雄已在圣阵中", pos)
			}
			return err
		}
		if err := pushResonanceShifts(s, st, role.RID, before, 0); err != nil {
			return err
		}
		if err := send(s); err != nil {
			return err
		}
		return reply(s, 26426, 1, "", pos)
	})
	r.Handle(26427, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, pos := req.Int("partner_id"), req.Int("pos")
		before, err := resonanceSnapshot(st, role.RID)
		if err != nil {
			return err
		}
		until := s.Now().Unix() + int64(resonate.CoolMinutes)*60
		if err := st.TakeOffResonance(role.RID, pos, id, until); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 26427, 0, "该英雄不在此槽位", pos)
			}
			return err
		}
		if err := pushResonanceShifts(s, st, role.RID, before, 0); err != nil {
			return err
		}
		if err := send(s); err != nil {
			return err
		}
		return reply(s, 26427, 1, "", pos)
	})
	r.Handle(26428, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		pos := req.Int("pos")
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(gains(resonate.Flush), 1, assets, items)
		if err := st.EndResonanceCool(role.RID, pos, s.Now().Unix(), assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 26428, 0, "该槽位无需刷新或钻石不足", pos)
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := send(s); err != nil {
			return err
		}
		return reply(s, 26428, 1, "", pos)
	})
	r.Handle(26429, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		pos, byGold := req.Int("pos"), req.Int("type") == 2
		_, gold, item, err := st.ResonanceOf(role.RID)
		if err != nil {
			return err
		}
		have := item
		if byGold {
			have = gold
		}
		open := openSlots(gold, item)
		if pos != int64(open)+1 || open >= resonate.CellMax || have >= len(resonate.Cost) {
			return reply(s, 26429, 0, "无法解锁该槽位", pos)
		}
		row := resonate.Cost[have]
		cost := row.Item
		if byGold {
			cost = row.Gold
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(gains(cost), 1, assets, items)
		if err := st.UnlockResonance(role.RID, byGold, have, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 26429, 0, "材料不足", pos)
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := send(s); err != nil {
			return err
		}
		return reply(s, 26429, 1, "", pos)
	})
}
