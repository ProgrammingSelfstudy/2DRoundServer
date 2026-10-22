package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Rank types of the client's RankConstant.Rank_Type that this server can fill.
const (
	rankDrama = 2 // campaign progress: val1 is the furthest dungeon id
	rankPower = 6 // power: val1 is the power
)

const rankMaxLen = 100

type rankEntry struct {
	role store.RankRole
	val  int64
}

// rolePower is the power ranking's number for a role: the sum of the powers of
// its five strongest heroes (our policy; the client does not say).
func rolePower(heroes []store.Partner) int64 {
	powers := make([]int64, 0, len(heroes))
	for _, h := range heroes {
		if _, ok := partnerTable[h.BID]; ok {
			powers = append(powers, partnerMessage(h).Int("power"))
		}
	}
	sort.Slice(powers, func(i, j int) bool { return powers[i] > powers[j] })
	var sum int64
	for i := 0; i < len(powers) && i < 5; i++ {
		sum += powers[i]
	}
	return sum
}

// ranking builds one server's list for a type, best first, at most rankMaxLen
// long; nil for a type this server does not rank. Ties keep the older progress first.
func ranking(st *store.Store, srvID string, typ int64) ([]rankEntry, error) {
	roles, err := st.RankRoles(srvID)
	if err != nil {
		return nil, err
	}
	var out []rankEntry
	switch typ {
	case rankDrama:
		for _, r := range roles {
			v := r.MaxDunID
			if v == 0 {
				v = dramaChain.First
			}
			out = append(out, rankEntry{r, int64(dungeonIndex[v] + 1)})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].val != out[j].val {
				return out[i].val > out[j].val
			}
			if out[i].role.DramaAt != out[j].role.DramaAt {
				return out[i].role.DramaAt < out[j].role.DramaAt
			}
			return out[i].role.RID < out[j].role.RID
		})
		for i := range out { // val1 of the campaign rank is the dungeon id itself
			out[i].val = dramaChain.Dungeons[out[i].val-1].ID
		}
	case rankPower:
		heroes, err := st.PartnersOfServer(srvID)
		if err != nil {
			return nil, err
		}
		for _, r := range roles {
			out = append(out, rankEntry{r, rolePower(heroes[r.RID])})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].val != out[j].val {
				return out[i].val > out[j].val
			}
			return out[i].role.RID < out[j].role.RID
		})
	default:
		return nil, nil
	}
	if len(out) > rankMaxLen {
		out = out[:rankMaxLen]
	}
	return out, nil
}

func rankRow(e rankEntry, srvID string, idx int) proto.Message {
	return proto.Message{"rid": e.role.RID, "srv_id": srvID, "name": e.role.Name, "guild_name": "", "lev": e.role.Lev,
		"face_id": e.role.FaceID, "avatar_bid": 0, "val1": e.val, "val2": 0, "val3": 0, "idx": idx, "desc": "", "look_id": 0,
		"face_update_time": 0, "face_file": ""}
}

// RegisterRank registers the rankings: 12900 (a list), 12901 (when it was last
// refreshed) and 12902 (the first place of each ranking). Only the campaign and
// power rankings of the role's own server are filled; any other type is an empty list.
func RegisterRank(r *gateway.Router, st *store.Store) {
	r.Handle(12900, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ, start, num := req.Int("type"), req.Int("start"), req.Int("num")
		list, err := ranking(st, role.SrvID, typ)
		if err != nil {
			return err
		}
		myIdx, myVal := int64(0), int64(0)
		for i, e := range list {
			if e.role.RID == role.RID {
				myIdx, myVal = int64(i+1), e.val
			}
		}
		if start < 1 {
			start = 1
		}
		rows := []proto.Message{}
		for i := int(start) - 1; i < len(list) && int64(len(rows)) < num; i++ {
			rows = append(rows, rankRow(list[i], role.SrvID, i+1))
		}
		return s.Send(12900, proto.Message{"is_cluster": 0, "type": typ, "start": start, "num": len(rows), "len": len(list),
			"time": s.Now().Unix(), "my_idx": myIdx, "lev": role.Lev, "face_id": role.FaceID, "avatar_bid": 0, "name": role.Name,
			"my_val1": myVal, "my_val2": 0, "my_val3": 0, "rank_list": rows})
	})
	r.Handle(12901, func(s *gateway.Session, req proto.Message) error {
		return s.Send(12901, proto.Message{"is_cluster": 0, "type": req.Int("type"), "time": s.Now().Unix()})
	})
	r.Handle(12902, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		out := []proto.Message{}
		for _, typ := range []int64{rankDrama, rankPower} {
			list, err := ranking(st, role.SrvID, typ)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				continue
			}
			row := rankRow(list[0], role.SrvID, 1)
			out = append(out, proto.Message{"type": typ, "name": row["name"], "rid": row["rid"], "srv_id": role.SrvID, "guild_name": "",
				"face_id": row["face_id"], "avatar_bid": 0, "val1": row["val1"], "val2": 0, "val3": 0, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(12902, proto.Message{"is_cluster": 0, "rank_list": out})
	})
}
