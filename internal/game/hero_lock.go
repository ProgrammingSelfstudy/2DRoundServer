package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterHeroLock registers 11015: the owner locks (type 1) or unlocks (type 0) a hero in
// the hero window. A locked hero is not taken as star-up fodder. The answer carries the hero
// and the type; the client then sets the lock on its copy.
func RegisterHeroLock(r *gateway.Router, st *store.Store) {
	r.Handle(11015, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, typ := req.Int("partner_id"), req.Int("type")
		reply := func(result int, msg string) error {
			return s.Send(11015, proto.Message{"result": result, "msg": msg, "partner_id": id, "type": typ})
		}
		if typ != 0 && typ != 1 {
			return reply(0, "操作不正确")
		}
		if err := st.SetPartnerLocked(role.RID, id, typ == 1); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "英雄不存在")
			}
			return err
		}
		return reply(1, "")
	})
}
