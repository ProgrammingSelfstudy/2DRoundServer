package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterRankReward registers 16650, the list of rewards by place that the ranked activities show (the sweet battle and the
// sandybeach boss; the client asks by the number it knows the activity by) and the place of the asking role.
func RegisterRankReward(r *gateway.Router, st *store.Store) {
	r.Handle(16650, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		var typ int64
		var tiers []proto.Message
		add := func(min, max int64, items [][2]int64) {
			award := []proto.Message{}
			for _, it := range items {
				award = append(award, proto.Message{"bid": it[0], "num": it[1]})
			}
			tiers = append(tiers, proto.Message{"rank1": min, "rank2": max, "award": award})
		}
		switch req.Int("bid") {
		case sweetCamp:
			typ = rankSweet
			for _, t := range sweetCfg.Ranks {
				add(t.Min, t.Max, t.Award)
			}
		case sbCamp:
			typ = rankSandybeach
			for _, t := range sbCfg.Ranks {
				add(t.Min, t.Max, t.Items)
			}
		default:
			return s.Send(16650, proto.Message{"rank": 0, "rank_award": []proto.Message{}})
		}
		list, err := rankingAt(st, role.SrvID, typ, s.Now().Unix())
		if err != nil {
			return err
		}
		idx, _, err := rankSelf(st, role.RID, typ, list)
		if err != nil {
			return err
		}
		return s.Send(16650, proto.Message{"rank": idx, "rank_award": tiers})
	})
}
