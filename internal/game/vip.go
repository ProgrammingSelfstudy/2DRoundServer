package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterVIPGifts registers the gifts of the VIP page: 16710 (the levels whose gift
// the role bought) and 16711 (buy the gift of a level). A role may buy the gift of
// every level up to its VIP level, once each, for the price and the content of the
// client's table (Config.VipData.data_get_reward). There is no recharge, so the VIP
// level only comes from the admin page.
func RegisterVIPGifts(r *gateway.Router, st *store.Store) {
	list := func(s *gateway.Session, rid int64) error {
		levs, err := st.VIPGiftLevels(rid)
		if err != nil {
			return err
		}
		out := make([]proto.Message, 0, len(levs))
		for _, l := range levs {
			out = append(out, proto.Message{"lev": l})
		}
		return s.Send(16710, proto.Message{"list": out})
	}
	r.Handle(16710, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		return list(s, role.RID)
	})
	r.Handle(16711, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		lev := req.Int("lev")
		reply := func(code int, msg string) error {
			return s.Send(16711, proto.Message{"code": code, "msg": msg})
		}
		if lev < 0 || lev > MaxVIPLevel {
			return reply(0, "礼包不存在")
		}
		fresh, err := st.RoleByID(role.RID)
		if err != nil {
			return err
		}
		if int64(fresh.VIPLev) < lev {
			return reply(0, "VIP等级不足")
		}
		spendA, spendI, gainA, gainI := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
		for _, p := range vipTable.Price[lev] {
			if p[1] > 0 { // the level 0 gift is free
				splitGains([]sellGain{{p[0], p[1]}}, 1, spendA, spendI)
			}
		}
		for _, it := range vipTable.Items[lev] {
			splitGains([]sellGain{{it[0], it[1]}}, 1, gainA, gainI)
		}
		if err := st.VIPGiftBuy(role.RID, lev, spendA, spendI, gainA, gainI); err != nil {
			if !errors.Is(err, store.ErrConflict) {
				return err
			}
			return reply(0, "已购买或货币不足")
		}
		// the original sends the new list first and answers with an empty msg (capture 2026-10-21b)
		if err := list(s, role.RID); err != nil {
			return err
		}
		if err := reply(1, ""); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
