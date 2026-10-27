package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The plays below need what a single server cannot give: the peak champion needs 256 players of the cross-server arena in zones
// (a season "cannot open" with fewer, the client's own rule), the cross champion the same across servers, the god battle a field
// that moves everyone at once, the merge-server voting a merge. They are answered as "not open", so that the windows show that
// instead of waiting, and no state is made.
func closedMsg(msg string) proto.Message {
	return proto.Message{"code": 0, "result": 0, "flag": 0, "msg": msg}
}

var closedReplies = map[uint16]proto.Message{
	// peak champion (27700~27731); a closed season has step_status 0 (unopened), no zone and no lists
	27700: {"start_time": 0, "end_time": 0, "step": 0, "step_status": 0, "step_status_time": 0, "round": 0, "round_status": 0, "round_status_time": 0, "flag": 0, "period": 0, "zone_id": 0, "max_zone_id": 0},
	27701: {"rank": 0, "best_rank": 0, "can_bet": 0, "group_256": 0, "group_64": 0},
	27702: {}, 27703: {}, 27705: {"list": []proto.Message{}}, 27706: {}, 27707: {}, 27708: {"list": []proto.Message{}},
	27709: {"list": []proto.Message{}}, 27710: {"pos_list": []proto.Message{}}, 27711: {}, 27712: {}, 27713: {"arena_peak_rank": []proto.Message{}},
	27714: {"rank": 0, "worship": 0, "power": 0, "score": 0, "rank_list": []proto.Message{}}, 27715: {"time": 0, "rank_list": []proto.Message{}}, 27716: {},
	27720: {"zone_info": []proto.Message{}}, 27730: {"point_info": []proto.Message{}}, 27731: {},
	// cross champion (26200~26216)
	26200: {"start_time": 0, "end_time": 0, "step": 0, "step_status": 0, "step_status_time": 0, "round": 0, "round_status": 0, "round_status_time": 0, "flag": 0, "srv_id": "", "is_open": 0},
	26201: {"rank": 0, "best_rank": 0, "can_bet": 0, "group": 0},
	26202: {}, 26203: {}, 26205: {"list": []proto.Message{}}, 26206: {}, 26208: {"list": []proto.Message{}}, 26209: {"list": []proto.Message{}},
	26210: {"pos_list": []proto.Message{}}, 26211: {}, 26212: {}, 26213: {"rank_list": []proto.Message{}},
	26214: {"rank": 0, "worship": 0, "power": 0, "rank_list": []proto.Message{}, "score": 0}, 26215: {"time": 0, "rank_list": []proto.Message{}}, 26216: {},
	// god battle
	21410: {"type": 0, "role_list": []proto.Message{}},
	// cross show, the list of the servers that take part
	22150: {"start_time": 0, "end_time": 0, "srv_list": []proto.Message{}},
	// merge-server voting
	10990: {"flag": 0, "cmd": 0, "img": ""},
	10991: {"status": 0, "is_vote": 0, "flag": 0, "last_time": 0},
	24950: {},
}

// closedRefusals are the requests that ask for something the closed play could give: they are told why not.
var closedRefusals = map[uint16]proto.Message{
	27704: closedMsg("巅峰冠军赛未开启"), 26204: closedMsg("跨服冠军赛未开启"),
	21401: closedMsg("战场未开启"), 21403: {"code": 0}, 21421: closedMsg("战场未开启"), 21427: closedMsg("战场未开启"),
	10992: closedMsg("没有进行中的合服投票"), 10993: closedMsg("没有进行中的合服投票"),
	20204: closedMsg("暂不能查看"), 23203: {"code": 0, "msg": "暂未开放", "is_share": 0, "is_day_share": 0}, 23304: closedMsg("暂未开放"),
}

type peakFormations struct {
	Items []struct {
		Order, Type, Hallows int64
		Pos                  [][2]int64 // place and hero
	}
}

// RegisterCrossClosed registers the answers of the plays that cannot be open here, and the saving of the peak champion's line-ups
// (27725, 27726), which needs nothing but the role's own state.
func RegisterCrossClosed(r *gateway.Router, st *store.Store) {
	for _, set := range []map[uint16]proto.Message{closedReplies, closedRefusals} {
		for cmd, m := range set {
			cmd, m := cmd, m
			r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error {
				cp := proto.Message{}
				for k, v := range m {
					cp[k] = v
				}
				return s.Send(cmd, cp)
			})
		}
	}
	r.Handle(27725, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d := &peakFormations{}
		for _, f := range req.Arr("formations") {
			it := struct {
				Order, Type, Hallows int64
				Pos                  [][2]int64
			}{Order: f.Int("order"), Type: f.Int("formation_type"), Hallows: f.Int("hallows_id")}
			for _, p := range f.Arr("pos_info") {
				it.Pos = append(it.Pos, [2]int64{p.Int("pos"), p.Int("id")})
			}
			d.Items = append(d.Items, it)
		}
		if err := st.SaveState(role.RID, "peakformations", d); err != nil {
			return err
		}
		return s.Send(27725, proto.Message{"code": 1, "msg": ""})
	})
	r.Handle(27726, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d := &peakFormations{}
		if _, err := st.LoadState(role.RID, "peakformations", d); err != nil {
			return err
		}
		list := []proto.Message{}
		for _, it := range d.Items {
			pos := []proto.Message{}
			for _, p := range it.Pos {
				pos = append(pos, proto.Message{"pos": p[0], "id": p[1]})
			}
			list = append(list, proto.Message{"order": it.Order, "formation_type": it.Type, "pos_info": pos, "hallows_id": it.Hallows})
		}
		return s.Send(27726, proto.Message{"formations": list})
	})
}
