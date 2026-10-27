package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterLook registers looking at another role's hero: 11061 by owner and hero id (the hero
// tips of a role's space or a ranking row) and 11062 by hero id alone (a hero shared into
// chat). Both reply with the hero's full entry; a bid of 0 when there is no such hero (the
// reply table has no message field, so the client shows nothing).
func RegisterLook(r *gateway.Router, st *store.Store) {
	show := func(s *gateway.Session, cmd uint16, rid, partnerID int64) error {
		empty := func(msg string) error {
			return s.Send(cmd, proto.Message{"partner_id": partnerID, "bid": 0, "r_rid": rid, "r_srvid": ""})
		}
		if rid == 0 {
			return empty("英雄不存在")
		}
		owner, err := st.RoleByID(rid)
		if err != nil || owner == nil {
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			return empty("角色不存在")
		}
		p, err := st.PartnerByID(rid, partnerID)
		if errors.Is(err, store.ErrNotFound) {
			return empty("英雄不存在")
		}
		if err != nil {
			return err
		}
		if _, ok := partnerTable[p.BID]; !ok {
			return empty("英雄不存在")
		}
		m := partnerMessage(*p)
		m["r_rid"], m["r_srvid"] = rid, owner.SrvID
		return s.Send(cmd, m)
	}
	r.Handle(11061, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return show(s, 11061, req.Int("r_rid"), req.Int("partner_id"))
	})
	r.Handle(11062, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		rid, err := st.PartnerOwner(req.Int("id"))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return show(s, 11062, rid, req.Int("id"))
	})
}

// RegisterStory registers the story scenes the client plays: 11100 a step was watched, 11102
// a scene was skipped. Nothing is kept: the client decides which scenes to play from the
// campaign progress it already has.
func RegisterStory(r *gateway.Router) {
	r.Handle(11100, func(s *gateway.Session, _ proto.Message) error { return s.Send(11100, proto.Message{}) })
	r.Handle(11102, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(11102, proto.Message{"code": 1, "msg": ""})
	})
}
