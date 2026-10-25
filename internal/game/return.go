package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// return.json: the rules of the hero return (tools/extract_return.py).
//
//go:embed data/return.json
var returnJSON []byte

var returnRules = func() struct {
	Cost  [][2]int64
	Star  int
	Lev   int
	Stage int
	Rate  int64
	Stars []int
	Rows  map[string]struct {
		Num   int
		Award [][2]int64
	}
} {
	var r struct {
		Cost  [][2]int64
		Star  int
		Lev   int
		Stage int
		Rate  int64
		Stars []int
		Rows  map[string]struct {
			Num   int
			Award [][2]int64
		}
	}
	if err := json.Unmarshal(returnJSON, &r); err != nil || len(r.Rows) == 0 || len(r.Cost) == 0 {
		panic("game: bad return.json")
	}
	return r
}()

// returnPlan is what returning one hero changes and gives.
type returnPlan struct {
	star, lev, brk int
	dropPos        []int64
	copies         []store.PartnerSeed
	assets, items  map[int64]int64
	preview        []proto.Message // the {is_partner, id, num, star, lev} list the client shows
}

// planReturn works out the return of a hero, or why it cannot. Our reading of the client's
// text: the hero goes back to star 10, keeps its equipment, its level is capped at 255 (and by
// the new star's cap) and its break step at 6; of the level and break cost that falls away it
// gets 80% back (the rate is the client's 800 per mille), the talents of slots that close come
// back in full, and the table row of its star and camp adds copies of the hero (at its first
// star, level 1) and general materials. The 12 / 13 star "天启星纹石" is not given (unknown).
func planReturn(p store.Partner) (returnPlan, string) {
	info := partnerTable[p.BID]
	row, ok := returnRules.Rows[strconv.Itoa(p.Star)+"_"+strconv.Itoa(info.Camp)]
	eligible := false
	for _, s := range returnRules.Stars {
		eligible = eligible || s == p.Star
	}
	if !eligible || !ok {
		return returnPlan{}, "该英雄无法回退"
	}
	target, has := info.Stars[strconv.Itoa(returnRules.Star)]
	if !has {
		return returnPlan{}, "该英雄无法回退"
	}
	pl := returnPlan{star: returnRules.Star, brk: p.BreakLev, assets: map[int64]int64{}, items: map[int64]int64{}}
	if pl.brk > returnRules.Stage {
		pl.brk = returnRules.Stage
	}
	for pl.brk > 0 {
		if _, ok := growth.Brach[brachKey(info, pl.brk)]; ok {
			break
		}
		pl.brk--
	}
	np := p
	np.Star, np.BreakLev = pl.star, pl.brk
	max, _ := levelCap(np)
	if max == 0 || target.LevMax > max {
		max = target.LevMax
	}
	pl.lev = p.Lev
	for _, cap := range []int{returnRules.Lev, max} {
		if cap > 0 && pl.lev > cap {
			pl.lev = cap
		}
	}
	add := func(list []sellGain) {
		var pos []sellGain
		for _, g := range list {
			if g.Amount > 0 {
				pos = append(pos, g)
			}
		}
		splitGains(pos, 1, pl.assets, pl.items)
	}
	// 80% of what the cut level and break step cost
	lost := map[int64]int64{}
	sub := func(sign int64, lev, brk int) {
		var l []sellGain
		if lev >= 1 && lev <= len(growth.Gets) {
			l = append(l, growth.Gets[lev-1]...)
		}
		if b, ok := growth.Brach[brachKey(info, brk)]; ok {
			l = append(l, gains(b.Get)...)
		}
		for _, g := range l {
			lost[g.ID] += sign * g.Amount
		}
	}
	sub(1, p.Lev, p.BreakLev)
	sub(-1, pl.lev, pl.brk)
	var back []sellGain
	for id, n := range lost {
		if n = n * returnRules.Rate / 1000; n > 0 {
			back = append(back, sellGain{id, n})
		}
	}
	add(back)
	// the talents whose slot closes at the new star
	for _, sl := range talent.Pos {
		if int64(pl.star) >= sl.Star {
			continue
		}
		for _, t := range p.Talents {
			if t.Pos == sl.Pos {
				pl.dropPos = append(pl.dropPos, sl.Pos)
				refundTalent(t, add)
			}
		}
	}
	for _, g := range gains(row.Award) {
		add([]sellGain{g})
	}
	for i := 0; i < row.Num; i++ {
		pl.copies = append(pl.copies, store.PartnerSeed{BID: p.BID, Star: info.InitStar})
	}
	pl.preview = []proto.Message{}
	if row.Num > 0 {
		pl.preview = append(pl.preview, proto.Message{"is_partner": 1, "id": p.BID, "num": row.Num, "star": info.InitStar, "lev": 1})
	}
	for _, m := range refundList(pl.assets, pl.items) {
		pl.preview = append(pl.preview, proto.Message{"is_partner": 0, "id": m.Int("id"), "num": m.Int("num"), "star": 0, "lev": 0})
	}
	return pl, ""
}

// RegisterReturn registers the hero return: 11066 previews it (code 0 with the reason when the
// hero cannot return), 11065 does it for the cost of returnRules.Cost (a "时光璃华" item). The
// answer comes after the new currencies, backpack, the changed hero (11002) and the copies (11001).
func RegisterReturn(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session, req proto.Message) (*store.Partner, returnPlan, string, error) {
		role := s.Role()
		p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return nil, returnPlan{}, "英雄不存在", nil
		}
		if err != nil {
			return nil, returnPlan{}, "", err
		}
		pl, msg := planReturn(*p)
		return p, pl, msg, nil
	}
	r.Handle(11066, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		_, pl, msg, err := load(s, req)
		if err != nil {
			return err
		}
		if msg != "" {
			return s.Send(11066, proto.Message{"code": 0, "msg": msg, "list": []proto.Message{}})
		}
		return s.Send(11066, proto.Message{"code": 1, "msg": "", "list": pl.preview})
	})
	r.Handle(11065, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("partner_id")
		reply := func(ok int, msg string, list []proto.Message) error {
			return s.Send(11065, proto.Message{"result": ok, "msg": msg, "list": list, "partner_id": id})
		}
		p, pl, msg, err := load(s, req)
		if err != nil {
			return err
		}
		if msg != "" {
			return reply(0, msg, []proto.Message{})
		}
		buys, err := st.HeroSlotBuys(role.RID)
		if err != nil {
			return err
		}
		sa, si := map[int64]int64{}, map[int64]int64{}
		splitGains(gains(returnRules.Cost), 1, sa, si)
		got, err := st.ReturnPartner(role.RID, id, p.Star, p.Lev, p.BreakLev, pl.star, pl.lev, pl.brk, pl.dropPos, pl.copies, heroSlotCapacity(buys), sa, si, pl.assets, pl.items)
		if errors.Is(err, store.ErrConflict) {
			return reply(0, "材料不足或英雄背包已满", []proto.Message{})
		}
		if err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if len(got) > 0 {
			msgs := make([]proto.Message, 0, len(got))
			for _, g := range got {
				msgs = append(msgs, partnerMessage(g))
			}
			if err := s.Send(11001, proto.Message{"partners": msgs}); err != nil {
				return err
			}
		}
		np, err := st.PartnerByID(role.RID, id)
		if err != nil {
			return err
		}
		if err := s.Send(11002, partnerMessage(*np)); err != nil {
			return err
		}
		return reply(1, "", pl.preview)
	})
}
