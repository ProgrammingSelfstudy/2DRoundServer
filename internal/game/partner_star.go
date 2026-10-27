package game

import (
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// starPlan checks a star-up request (11005) against the heroes the role owns
// and returns the heroes to consume and the other cost, or why it fails.
//
// The client's star table row of the TARGET star lists what it takes: `fodder`
// are heroes of one exact base id and star, `camp_fodder` heroes of a camp
// (camp 0 = any) and star. The client sends the chosen specific ones in
// `expend1` and the camp ones in `expend2`; we do not trust which list a hero
// came in, only that every slot is filled by distinct owned heroes. Hero soul
// items (`item_expend`) are not supported. Rows whose `self` entry is not "this
// hero, one star below" (the fusion path above star 20) are refused.
func starPlan(p store.Partner, owned map[int64]store.Partner, chosen []int64, hasItems bool) (consumed []int64, cost []sellGain, msg string) {
	info := partnerTable[p.BID]
	row, ok := info.Stars[strconv.Itoa(p.Star+1)]
	switch {
	case !ok:
		return nil, nil, "已升至最高星级"
	case hasItems:
		return nil, nil, "暂不支持使用英雄魂石升星"
	}
	for _, sf := range row.Self {
		if sf[0] != p.BID || sf[1] != int64(p.Star) || sf[2] != 1 {
			return nil, nil, "该升星方式暂未开放"
		}
	}
	used := map[int64]bool{p.PartnerID: true}
	for _, id := range chosen {
		if _, ok := owned[id]; !ok || used[id] {
			return nil, nil, "所需材料不足"
		}
		if owned[id].Locked {
			return nil, nil, "英雄已锁定"
		}
		used[id] = true
	}
	left := map[int64]store.Partner{}
	for _, id := range chosen {
		left[id] = owned[id]
	}
	take := func(match func(store.Partner) bool, n int64) bool {
		for ; n > 0; n-- {
			found := int64(0)
			for id, h := range left { // map order is random, but any matching hero is as good as another
				if match(h) {
					found = id
					break
				}
			}
			if found == 0 {
				return false
			}
			consumed = append(consumed, found)
			delete(left, found)
		}
		return true
	}
	for _, f := range row.Fodder {
		f := f
		if !take(func(h store.Partner) bool { return h.BID == f[0] && int64(h.Star) == f[1] }, f[2]) {
			return nil, nil, "所需材料不足"
		}
	}
	// camp slots with a named camp first, so "any camp" slots take what is left
	camps := append([][3]int64(nil), row.CampFodder...)
	for pass := 0; pass < 2; pass++ {
		for _, f := range camps {
			f := f
			if (f[0] == 0) != (pass == 1) {
				continue
			}
			if !take(func(h store.Partner) bool {
				return int64(h.Star) == f[1] && (f[0] == 0 || int64(partnerTable[h.BID].Camp) == f[0])
			}, f[2]) {
				return nil, nil, "所需材料不足"
			}
		}
	}
	if len(left) != 0 {
		return nil, nil, "所选英雄与升星要求不符"
	}
	for _, c := range row.Cost {
		cost = append(cost, sellGain{c[0], c[1]})
	}
	return consumed, cost, ""
}

// RegisterPartnerStar registers 11005 (raise a hero's star). On success the
// client is told the consumed heroes are gone (11006), the hero changed
// (11001), and the resources (10302) and backpack (10500) are re-sent.
func RegisterPartnerStar(r *gateway.Router, st *store.Store) {
	r.Handle(11005, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(ok int, msg string) error { return s.Send(11005, proto.Message{"result": ok, "msg": msg}) }
		p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if err != nil {
			return reply(0, "英雄不存在")
		}
		if _, known := partnerTable[p.BID]; !known {
			return reply(0, "英雄不存在")
		}
		all, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		owned := map[int64]store.Partner{}
		for _, h := range all {
			owned[h.PartnerID] = h
		}
		var chosen []int64
		for _, list := range []string{"expend1", "expend2"} {
			for _, a := range req.Arr(list) {
				chosen = append(chosen, a.Int("partner_id"))
			}
		}
		consumed, cost, msg := starPlan(*p, owned, chosen, len(req.Arr("item_expend")) > 0)
		if msg != "" {
			return reply(0, msg)
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(cost, 1, assets, items)
		if err := st.AscendPartner(role.RID, p.PartnerID, p.Star, consumed, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "所需材料不足")
			}
			return err
		}
		if err := reply(1, "升星成功"); err != nil {
			return err
		}
		gone := make([]proto.Message, 0, len(consumed))
		for _, id := range consumed {
			gone = append(gone, proto.Message{"partner_id": id})
		}
		if err := s.Send(11006, proto.Message{"expend2": gone}); err != nil {
			return err
		}
		np := *p
		np.Star++
		if err := s.Send(11001, proto.Message{"partners": []proto.Message{partnerMessage(np)}}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return CheckLimitGifts(s, st, s.Now()) // a star that opens a limited gift
	})
}
