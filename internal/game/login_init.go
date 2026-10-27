package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterLoginInit answers the small state questions that the client asks right after login and whose answer here is the same
// for everyone: the red dots of the ladder (none), the arena log (none), the weekly gift tab (open, see cyclegift.go), the
// score summon (not the first time), the drama buffs (none), the growth carnival (nothing) and the server's opening time for
// the invite code.
func RegisterLoginInit(r *gateway.Router, st *store.Store) {
	fixed := map[uint16]proto.Message{
		10394: {},
		13011: {"buff_list": []proto.Message{}},
		20223: {"flag": 0},
		21021: {"flag": 1},
		23235: {"flag": 1, "msg": ""},
		24313: {"code": 0},
		24314: {"code": 0},
		25833: {"id": 0, "name": ""},
	}
	for cmd, m := range fixed {
		cmd, m := cmd, m
		r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error {
			cp := proto.Message{}
			for k, v := range m {
				cp[k] = v
			}
			return s.Send(cmd, cp)
		})
	}
	r.Handle(10930, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		srv, err := st.Server(role.SrvID)
		if err != nil {
			return err
		}
		at := srv.OpenTime
		if at == 0 {
			at = srv.CreatedAt
		}
		return s.Send(10930, proto.Message{"code": 0, "open_timestamp": at})
	})
}
