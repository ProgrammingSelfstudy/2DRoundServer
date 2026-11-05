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

// releaseWorn takes the artifacts and holy equipment off the heroes that are about to be used
// up and returns what went back to the bag, by base id. The consumed heroes' ordinary
// equipment is released by the star-up transaction itself.
func releaseWorn(st *store.Store, rid int64, gone map[int64]bool) (map[int64]int64, error) {
	back := map[int64]int64{}
	arts, err := loadArtifacts(st, rid)
	if err != nil {
		return nil, err
	}
	for i, a := range arts.Pieces {
		if gone[a.Partner] && a.Pos > 0 {
			arts.Pieces[i].Partner, arts.Pieces[i].Pos = 0, 0
			back[a.Base]++
		}
	}
	holy, err := loadHoly(st, rid)
	if err != nil {
		return nil, err
	}
	for i, x := range holy.Pieces {
		if gone[x.Partner] && x.Slot > 0 {
			holy.Pieces[i].Partner, holy.Pieces[i].Slot = 0, 0
			back[x.Base]++
		}
	}
	if len(back) == 0 {
		return back, nil
	}
	if err := st.Exchange(rid, nil, nil, nil, back); err != nil {
		return nil, err
	}
	if err := st.SaveArtifacts(rid, arts); err != nil {
		return nil, err
	}
	return back, st.SaveHoly(rid, holy)
}

// RegisterPartnerStar registers 11005 (raise a hero's star). The consumed heroes give back
// what their levels, break steps, talents and worn gear cost. The order follows the captures
// of 2026-11-02 and 2026-11-03: the resources (10302, only when they changed), the bag
// changes (10510/10511/10512), the refund notice (12742), the hero (11002) and the heroes of
// the resonance slots that followed the crystal, the consumed heroes (11006), the answer with
// an empty msg and, when the total power set a new high, 10306.
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
		// what the consumed heroes give back: shown in full, but the worn ordinary equipment is
		// credited by the transaction that releases it
		shown, gainAssets, gainItems := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
		gone := map[int64]bool{}
		for _, id := range consumed {
			gone[id] = true
			h := owned[id]
			a, i := refund(h)
			for k, v := range a {
				shown[k] += v
			}
			for k, v := range i {
				shown[k] += v
			}
			h.Equips = nil
			a, i = refund(h)
			for k, v := range a {
				gainAssets[k] += v
			}
			for k, v := range i {
				gainItems[k] += v
			}
		}
		before, err := snapshotBags(st, role.RID)
		if err != nil {
			return err
		}
		slots, err := resonanceSnapshot(st, role.RID)
		if err != nil {
			return err
		}
		if err := st.AscendPartner(role.RID, p.PartnerID, p.Star, consumed, assets, items, gainAssets, gainItems); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "所需材料不足")
			}
			return err
		}
		worn, err := releaseWorn(st, role.RID, gone)
		if err != nil {
			return err
		}
		for k, v := range worn {
			shown[k] += v
		}
		if len(assets)+len(gainAssets) > 0 {
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
		}
		after, err := snapshotBags(st, role.RID)
		if err != nil {
			return err
		}
		if err := sendBagDelta(s, before, after); err != nil {
			return err
		}
		notice := make([]sellGain, 0, len(shown))
		for _, id := range mergedKeys(shown, nil) {
			notice = append(notice, sellGain{id, shown[id]})
		}
		if err := sendGainList(s, notice); err != nil {
			return err
		}
		np, err := st.PartnerByID(role.RID, p.PartnerID)
		if err != nil {
			return err
		}
		if err := s.Send(11002, partnerMessage(*np)); err != nil {
			return err
		}
		if err := pushResonanceShifts(s, st, role.RID, slots, p.PartnerID); err != nil {
			return err
		}
		growStar(st, role.RID, partnerTable[p.BID].Name, np.Star, s.Now().Unix())
		goneList := make([]proto.Message, 0, len(consumed))
		for _, id := range consumed {
			goneList = append(goneList, proto.Message{"partner_id": id})
		}
		if err := s.Send(11006, proto.Message{"expend2": goneList}); err != nil {
			return err
		}
		if err := reply(1, ""); err != nil {
			return err
		}
		if err := sendPowerIfHigh(s, st, role.RID, rolePower(all)); err != nil {
			return err
		}
		return CheckLimitGifts(s, st, s.Now()) // a star that opens a limited gift
	})
}
