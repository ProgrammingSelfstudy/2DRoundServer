package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterRankPartner registers 12904, the list of the hero ranking of the rank window: the roles of the server by the power of
// their strongest single hero, each with that hero.
func RegisterRankPartner(r *gateway.Router, st *store.Store) {
	best := func(rid int64) (store.Partner, int64) {
		heroes, _ := st.PartnersOf(rid)
		var top store.Partner
		var power int64
		for _, h := range heroes {
			if p := partnerMessage(h).Int("power"); p > power {
				top, power = h, p
			}
		}
		return top, power
	}
	r.Handle(12904, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list, err := rankingAt(st, role.SrvID, rankPartner, s.Now().Unix())
		if err != nil {
			return err
		}
		start, num := req.Int("start"), req.Int("num")
		if start < 1 {
			start = 1
		}
		out := proto.Message{"start": start, "len": len(list), "time": rankingTime(st, role.SrvID, rankPartner), "my_idx": 0,
			"lev": role.Lev, "face_id": role.FaceID, "avatar_bid": 0, "name": role.Name, "pid": 0, "pbid": 0, "plev": 0, "pquality": 0, "pstar": 0,
			"power": 0, "face_update_time": 0, "face_file": ""}
		if h, p := best(role.RID); p > 0 {
			out["pid"], out["pbid"], out["plev"], out["pquality"], out["pstar"], out["power"] = h.PartnerID, h.BID, h.Lev, partnerMessage(h).Int("break_lev"), h.Star, p
		}
		rows := []proto.Message{}
		for i, e := range list {
			if e.role.RID == role.RID {
				out["my_idx"] = i + 1
			}
			if int64(i+1) < start || int64(len(rows)) >= num {
				continue
			}
			h, p := best(e.role.RID)
			rows = append(rows, proto.Message{"rid": e.role.RID, "srv_id": role.SrvID, "name": e.role.Name, "lev": e.role.Lev, "face_id": e.role.FaceID,
				"avatar_bid": 0, "power": p, "idx": i + 1, "pid": h.PartnerID, "pbid": h.BID, "plev": h.Lev, "pquality": partnerMessage(h).Int("break_lev"),
				"pstar": h.Star, "look_id": 0})
		}
		out["num"], out["rank_list"] = len(rows), rows
		return s.Send(12904, out)
	})
}
