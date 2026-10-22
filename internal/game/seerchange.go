package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// registerSeerChange registers the hero swap of the seer palace: 23214 (the swap in
// progress) and 23215 (action 1 swap, 0 cancel, 2 save). A 4-star or 5-star hero of
// the water, fire or wind camp is swapped for another hero of its camp that starts
// at the same star, picked at random; it pays seer essence when the swap starts (the hero
// is locked meanwhile) and saving makes a new hero of the new base id with a new partner id
// (level, star, equipment and formation places stay) while the old hero is gone, as the
// original does (capture 2026-10-21b). Cancelling gives nothing back: our policy, the
// original is unknown.
func registerSeerChange(r *gateway.Router, st *store.Store) {
	r.Handle(23214, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		pid, bid, _, err := st.PartnerChange(role.RID)
		if err != nil {
			return err
		}
		return s.Send(23214, proto.Message{"partner_id": pid, "new_partner_bid": bid, "partner_ids": []proto.Message{}})
	})
	r.Handle(23215, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(code int, msg string) error {
			return s.Send(23215, proto.Message{"code": code, "msg": msg})
		}
		pid := req.Int("partner_id")
		switch req.Int("action") {
		case 1:
			p, err := st.PartnerByID(role.RID, pid)
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "英雄不存在")
			}
			if err != nil {
				return err
			}
			cost, ok := seerChangeCost[p.Star]
			targets := seerChangeTargets(p.BID, p.Star)
			if !ok || len(targets) == 0 {
				return reply(0, "该英雄不能转换")
			}
			target := targets[int(summonRand()*float64(len(targets)))%len(targets)]
			assets, items := map[int64]int64{}, map[int64]int64{}
			splitGains([]sellGain{{seerChangeItem, cost}}, 1, assets, items)
			if err := st.StartPartnerChange(role.RID, pid, target, assets, items); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return reply(0, "先知精华不足或已有未保存的转换")
				}
				return err
			}
			// the original's order: the cost, the hero locked (11002), the swap, then the answer
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			locked := partnerMessage(*p)
			locked["is_lock"] = []proto.Message{{"is_lock": 1, "lock_type": 2}}
			if err := s.Send(11002, locked); err != nil {
				return err
			}
			if err := s.Send(23214, proto.Message{"partner_id": pid, "new_partner_bid": target, "partner_ids": []proto.Message{}}); err != nil {
				return err
			}
			return reply(1, "")
		case 0:
			ok, err := st.CancelPartnerChange(role.RID, pid)
			if err != nil {
				return err
			}
			if !ok {
				return reply(0, "没有可取消的转换")
			}
			if err := reply(1, "已取消"); err != nil {
				return err
			}
			return s.Send(23214, proto.Message{"partner_id": 0, "new_partner_bid": 0, "partner_ids": []proto.Message{}})
		case 2:
			np, err := st.SavePartnerChange(role.RID, pid)
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "没有可保存的转换")
			}
			if err != nil {
				return err
			}
			if err := s.Send(11001, proto.Message{"partners": []proto.Message{partnerMessage(*np)}}); err != nil {
				return err
			}
			if err := s.Send(11006, proto.Message{"expend2": []proto.Message{{"partner_id": pid}}}); err != nil {
				return err
			}
			if err := s.Send(23214, proto.Message{"partner_id": 0, "new_partner_bid": 0, "partner_ids": []proto.Message{}}); err != nil {
				return err
			}
			return reply(1, "")
		}
		return reply(0, "操作不正确")
	})
}
