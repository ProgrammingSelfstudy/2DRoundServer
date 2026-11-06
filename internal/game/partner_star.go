package game

import (
	"errors"
	"sort"
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
// came in, only that every slot is filled by distinct owned heroes.
//
// Hero souls (`item_expend`, items 90500 to 91000) stand in for the heroes of a camp slot: a soul fills a slot of
// its own camp and star (the client lists a soul only for `camp_type == slot camp and eqm_jie == slot star`, so
// the any-camp souls fill the any-camp slots), one soul per hero, and the souls are part of the cost. Souls cannot
// fill the slots of one exact hero. From star 21 the row's `self` entry names the hero itself with a count of
// more than one (the fusion rows; the capture of 2026-11-05 evening raised a hero from 7 to 25 stars with
// expend1 equal to `fodder` and no extra hero for `self`), so there only the base id has to match.
func starPlan(p store.Partner, owned map[int64]store.Partner, chosen []int64, souls map[int64]int64) (consumed []int64, cost []sellGain, msg string) {
	info := partnerTable[p.BID]
	row, ok := info.Stars[strconv.Itoa(p.Star+1)]
	if !ok {
		return nil, nil, "已升至最高星级"
	}
	if len(row.Self)+len(row.Fodder)+len(row.CampFodder)+len(row.Cost) == 0 {
		// the client's table has no materials for this row (the 蛋头岛 heroes 70533 to 70540 have empty rows from star 6 to 25): do not hand out stars for free
		return nil, nil, "该英雄暂不可升星"
	}
	for _, sf := range row.Self {
		if sf[0] != p.BID || (sf[2] == 1 && sf[1] != int64(p.Star)) {
			return nil, nil, "该升星方式暂未开放"
		}
	}
	// the camp slots the souls fill first; what is left of each slot is for heroes
	camps := append([][3]int64(nil), row.CampFodder...)
	soulIDs := make([]int64, 0, len(souls))
	for id, n := range souls {
		if _, _, ok := heroSoul(id); !ok || n <= 0 {
			return nil, nil, "所选材料与升星要求不符"
		}
		soulIDs = append(soulIDs, id)
	}
	sort.Slice(soulIDs, func(i, j int) bool { return soulIDs[i] < soulIDs[j] })
	soulLeft := map[int64]int64{}
	for id, n := range souls {
		soulLeft[id] = n
	}
	for i := range camps {
		for _, id := range soulIDs {
			camp, star, _ := heroSoul(id)
			if camp != camps[i][0] || star != camps[i][1] || soulLeft[id] == 0 || camps[i][2] == 0 {
				continue
			}
			use := soulLeft[id]
			if use > camps[i][2] {
				use = camps[i][2]
			}
			camps[i][2] -= use
			soulLeft[id] -= use
		}
	}
	for _, id := range soulIDs {
		if soulLeft[id] != 0 {
			return nil, nil, "所选材料与升星要求不符"
		}
		cost = append(cost, sellGain{id, souls[id]})
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

// heroSoul says whether an item is a hero soul and for which camp and star (items 90500 to 90505 are the 5-star
// souls of camp 0 to 5, 906xx the 6-star ones, 909xx the 9-star ones and 91000 the 10-star any-camp soul; camp 0 is any).
func heroSoul(id int64) (camp, star int64, ok bool) {
	switch {
	case id >= 90500 && id <= 90505:
		return id - 90500, 5, true
	case id >= 90600 && id <= 90605:
		return id - 90600, 6, true
	case id >= 90900 && id <= 90905:
		return id - 90900, 9, true
	case id == 91000:
		return 0, 10, true
	}
	return 0, 0, false
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
		souls := map[int64]int64{}
		for _, a := range req.Arr("item_expend") {
			souls[a.Int("item_id")] += a.Int("num")
		}
		consumed, cost, msg := starPlan(*p, owned, chosen, souls)
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
		if len(souls) > 0 && len(shown) == 0 {
			// the capture's star-ups with souls all carried a 12742 with an empty list when no hero gave anything back
			if err := s.Send(12742, proto.Message{"asset_list": []proto.Message{}, "source": 0}); err != nil {
				return err
			}
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
