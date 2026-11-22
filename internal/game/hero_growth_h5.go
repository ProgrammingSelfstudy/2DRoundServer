package game

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const h5MinPartnerStar = 5

func isH5Session(s *gateway.Session) bool {
	account := s.Account()
	return account != nil && account.Platform == store.PlatformH5
}

func visiblePartner(p store.Partner, h5 bool) bool {
	_, known := partnerTable[p.BID]
	return known && (!h5 || p.Star >= h5MinPartnerStar)
}

// The list handlers filter their query results. Apply the same star floor to
// roster pushes, including changes sent by summons, resonance and admin grants.
func registerH5RosterPushFilters(r *gateway.Router) {
	r.FilterSend(11001, func(s *gateway.Session, msg proto.Message) proto.Message {
		if !isH5Session(s) {
			return msg
		}
		partners := msg.Arr("partners")
		visible := make([]proto.Message, 0, len(partners))
		for _, p := range partners {
			if p.Int("star") >= h5MinPartnerStar {
				visible = append(visible, p)
			}
		}
		if len(visible) == 0 {
			return nil
		}
		return proto.Message{"partners": visible}
	})
	r.FilterSend(11002, func(s *gateway.Session, msg proto.Message) proto.Message {
		if isH5Session(s) && msg.Int("star") < h5MinPartnerStar {
			return nil
		}
		return msg
	})
}

// The non-empty limits in the recovered data_partner_brach are minimum stars.
// The grouped rows apply to every career for the listed break ID and stage.
//
//go:embed data/h5_partner_break_limits.json
var h5BreakLimitsJSON []byte

var h5BreakLimits = func() map[int]map[int]int {
	var limits map[int]map[int]int
	if err := json.Unmarshal(h5BreakLimitsJSON, &limits); err != nil || len(limits) == 0 {
		panic("game: bad h5_partner_break_limits.json")
	}
	return limits
}()

func breakDenial(p store.Partner, next brachStep, h5 bool) string {
	if !next.Limit {
		return ""
	}
	if minStar := h5BreakLimits[partnerTable[p.BID].BreakID][p.BreakLev+1]; h5 && minStar > 0 {
		if p.Star < minStar {
			return fmt.Sprintf("英雄达到%d星后可进阶", minStar)
		}
		return ""
	}
	return "该突破暂未开放"
}

// In H5 the roster starts at five stars. A named five-star material whose
// base starts below five becomes one same-camp five-star slot. It can still
// be filled by an existing promoted copy of that base or a matching soul.
func h5StarRecipe(row starInfo) starInfo {
	specific := make([][3]int64, 0, len(row.Fodder))
	camps := append([][3]int64(nil), row.CampFodder...)
	for _, f := range row.Fodder {
		base, ok := partnerTable[f[0]]
		if f[1] == h5MinPartnerStar && ok && base.InitStar < h5MinPartnerStar {
			camps = append(camps, [3]int64{int64(base.Camp), f[1], f[2]})
		} else {
			specific = append(specific, f)
		}
	}
	row.Fodder, row.CampFodder = specific, camps
	return row
}
