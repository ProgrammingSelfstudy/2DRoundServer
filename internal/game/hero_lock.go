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
		p, err := st.PartnerByID(role.RID, id)
		if err != nil {
			return err
		}
		m := partnerMessage(*p)
		// Unlock increments explicitly retain lock_type1/is_lock0. Unlocked
		// login baselines continue using an empty list.
		m["is_lock"] = []proto.Message{{"lock_type": heroLockType, "is_lock": typ}}
		if err := s.Send(11002, m); err != nil {
			return err
		}
		text := "解锁成功"
		if typ == 1 {
			text = "锁定成功"
		}
		return reply(1, text)
	})
}
