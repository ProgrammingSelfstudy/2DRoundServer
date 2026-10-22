package game

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// partner_growth.json: what each level costs and the break steps with their
// level caps (tools/extract_partners.py).
//
//go:embed data/partner_growth.json
var growthJSON []byte

type brachStep struct {
	LevMax int        `json:"lev_max"`
	Add    [4]int64   `json:"add"`
	Expend [][2]int64 `json:"expend"`
	Limit  bool       `json:"limit"` // the step has an extra condition the client does not spell out
}

type growthTables struct {
	Costs [][]sellGain // Costs[lev-1]: what going from lev to lev+1 costs
	Brach map[string]brachStep
}

var growth = func() growthTables {
	var f struct {
		Lev []struct {
			Expend [][2]int64 `json:"expend"`
		} `json:"lev"`
		Brach map[string]brachStep `json:"brach"`
	}
	if err := json.Unmarshal(growthJSON, &f); err != nil || len(f.Lev) == 0 || len(f.Brach) == 0 {
		panic("game: bad partner_growth.json")
	}
	g := growthTables{Brach: f.Brach}
	for _, l := range f.Lev {
		var c []sellGain
		for _, e := range l.Expend {
			c = append(c, sellGain{e[0], e[1]})
		}
		g.Costs = append(g.Costs, c)
	}
	return g
}()

func brachKey(info partnerInfo, breakLev int) string {
	return fmt.Sprintf("%d_%d_%d", info.Type, info.BreakID, breakLev)
}

// levelCap is the highest level a hero may reach without a break, and whether a
// break step follows (the client's HeroCalculate.getHeroShowLevelStatus).
func levelCap(p store.Partner) (max int, hasNext bool) {
	info := partnerTable[p.BID]
	cur, ok := growth.Brach[brachKey(info, p.BreakLev)]
	if !ok {
		return 0, false
	}
	_, hasNext = growth.Brach[brachKey(info, p.BreakLev+1)]
	max = cur.LevMax
	if !hasNext && info.Stars[fmt.Sprint(p.Star)].LevMax > max {
		max = info.Stars[fmt.Sprint(p.Star)].LevMax
	}
	return max, hasNext
}

const (
	heroLevelsAtOnce    = 5  // levels one 11003 may raise (the client's button)
	heroSingleLevelFrom = 60 // from this level on one 11003 raises one level
)

// RegisterPartnerGrowth registers 11003 (level up one level) and 11004 (break
// through), each answered with its result, then the changed hero (11001, a one-hero list update), the
// resources (10302) and the backpack (10500).
func RegisterPartnerGrowth(r *gateway.Router, st *store.Store) {
	// 11016 is the break panel's preview: the hero's attributes one break step on
	// (the same as now when there is no further step).
	r.Handle(11016, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if err != nil {
			return nil
		}
		if _, ok := partnerTable[p.BID]; !ok {
			return nil
		}
		if _, next := growth.Brach[brachKey(partnerTable[p.BID], p.BreakLev+1)]; next {
			p.BreakLev++
		}
		return s.Send(11016, partnerMessage(*p))
	})
	// do runs one growth step. plan(p, n) gives the step for the n-th attempt (0 = the
	// biggest one): the new level and break, the cost, or a refusal message. An attempt
	// the role cannot pay falls through to the next one, up to tries attempts.
	do := func(cmd uint16, tries int, plan func(p store.Partner, n int) (toLev, toBreak int, cost []sellGain, msg string)) gateway.Handler {
		return func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			reply := func(ok int, msg string) error { return s.Send(cmd, proto.Message{"result": ok, "msg": msg}) }
			p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
			if err != nil {
				return reply(0, "英雄不存在")
			}
			if _, known := partnerTable[p.BID]; !known {
				return reply(0, "英雄不存在")
			}
			var toLev, toBreak int
			paid := false
			for n := 0; n < tries && !paid; n++ {
				var cost []sellGain
				var msg string
				toLev, toBreak, cost, msg = plan(*p, n)
				if msg != "" {
					if n == 0 {
						return reply(0, msg)
					}
					break
				}
				assets, items := map[int64]int64{}, map[int64]int64{}
				splitGains(cost, 1, assets, items)
				err := st.UpgradePartner(role.RID, p.PartnerID, p.Lev, p.BreakLev, toLev, toBreak, assets, items)
				switch {
				case err == nil:
					paid = true
				case err != store.ErrConflict:
					return err
				}
			}
			if !paid {
				return reply(0, "材料不足")
			}
			// the original's order (capture 2026-10-21b): the new assets and items, the hero
			// (11002), then the answer with an empty msg
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			np := *p
			np.Lev, np.BreakLev = toLev, toBreak
			if err := s.Send(11002, partnerMessage(np)); err != nil {
				return err
			}
			return reply(1, "")
		}
	}
	// One 11003 raises the hero by up to heroLevelsAtOnce levels while the role can pay
	// each of them, the way the client's button ("升5级", HeroMainTabTrainPanel) counts it;
	// from level heroSingleLevelFrom on it is one level. Confirmed by the capture of
	// 2026-10-21b: 16 -> 21 -> 26 -> 30 (the cap) with the costs of our table.
	r.Handle(11003, do(11003, heroLevelsAtOnce, func(p store.Partner, n int) (int, int, []sellGain, string) {
		max, _ := levelCap(p)
		if p.Lev >= max || p.Lev > len(growth.Costs) {
			return 0, 0, nil, "已达到等级上限"
		}
		want := heroLevelsAtOnce
		if p.Lev >= heroSingleLevelFrom {
			want = 1
		}
		if room := max - p.Lev; room < want {
			want = room
		}
		if room := len(growth.Costs) - p.Lev + 1; room < want {
			want = room
		}
		levels := want - n
		if levels < 1 {
			return 0, 0, nil, "材料不足"
		}
		var cost []sellGain
		for lv := p.Lev; lv < p.Lev+levels; lv++ {
			cost = append(cost, growth.Costs[lv-1]...)
		}
		return p.Lev + levels, p.BreakLev, cost, ""
	}))
	r.Handle(11004, do(11004, 1, func(p store.Partner, _ int) (int, int, []sellGain, string) {
		info := partnerTable[p.BID]
		cur, ok := growth.Brach[brachKey(info, p.BreakLev)]
		next, hasNext := growth.Brach[brachKey(info, p.BreakLev+1)]
		switch {
		case !ok || !hasNext:
			return 0, 0, nil, "已突破到最高阶段"
		case p.Lev < cur.LevMax:
			return 0, 0, nil, "等级未达到突破要求"
		case next.Limit:
			return 0, 0, nil, "该突破暂未开放" // an extra condition the client does not spell out
		}
		var cost []sellGain
		for _, e := range cur.Expend {
			cost = append(cost, sellGain{e[0], e[1]})
		}
		return p.Lev, p.BreakLev + 1, cost, ""
	}))
}
