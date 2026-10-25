package game

import (
	"errors"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	rebornMaxLev   = 100 // "100级或以下的英雄才能重生" (hero_reset_comfirm_panel)
	rebornPerDay   = 3   // the client's default when data_partner_const has no born_num
	rebornCooldown = 60  // seconds before the same hero may be reborn again (our policy)
	offerMax       = 100 // heroes in one offering (our policy)
)

// refund is what resetting or offering a hero gives back, as currencies and items:
// the cumulative cost of its levels and break steps (get_item in the client's level and break
// tables, 100% as the client's text says), the learned talents' stones and the worn equipment.
func refund(p store.Partner) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	add := func(list []sellGain) {
		var pos []sellGain
		for _, g := range list {
			if g.Amount > 0 {
				pos = append(pos, g)
			}
		}
		splitGains(pos, 1, assets, items)
	}
	if p.Lev >= 1 && p.Lev <= len(growth.Gets) {
		add(growth.Gets[p.Lev-1])
	}
	if b, ok := growth.Brach[brachKey(partnerTable[p.BID], p.BreakLev)]; ok {
		add(gains(b.Get))
	}
	for _, t := range p.Talents {
		refundTalent(t, add)
	}
	for _, e := range p.Equips {
		items[e.BaseID]++
	}
	return
}

// refundTalent gives back what a learned talent cost: the learn stone of its first level, then each
// level-up it went through.
func refundTalent(t store.Talent, add func([]sellGain)) {
	for id, n := t.SkillID, 0; n < 16; n++ {
		prev, ok := talent.Prev[id]
		if !ok {
			add(gains(talent.Learn[id].Cost))
			return
		}
		add(gains(talent.Level[prev].Cost))
		id = prev
	}
}

// refundList is a refund as the {id, num} list the client shows, by id.
func refundList(assets, items map[int64]int64) []proto.Message {
	sum := map[int64]int64{}
	for id, n := range assets {
		sum[id] += n
	}
	for id, n := range items {
		sum[id] += n
	}
	ids := make([]int64, 0, len(sum))
	for id := range sum {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []proto.Message{}
	for _, id := range ids {
		out = append(out, proto.Message{"id": id, "num": sum[id]})
	}
	return out
}

func rebornKey(day int64) string { return "hero_reborn_" + strconv.FormatInt(day, 10) }

// RegisterReset registers the hero reset commands of the hero window: 11068 reborn (free:
// back to level 1 and break step 0, the star stays, 100% of the level and break costs, the
// talents and the equipment come back; heroes up to level 100, rebornPerDay a day, and
// rebornCooldown seconds between two rebirths of one hero), 11067 how often and until when,
// and the offering: 11075 previews what a list of heroes would give, 11076 gives them up for
// it. The client's text promises the star-up and talent materials too, but fodder heroes are
// not tracked, so those are not returned; the offering's extra base reward ("进阶石和其他材料")
// has no table in the client and is not given. All of it is our reading, not captured.
func RegisterReset(r *gateway.Router, st *store.Store) {
	r.Handle(11067, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("partner_id")
		until, _ := st.PartnerResetUntil(role.RID, id)
		used, err := st.Counter(role.RID, rebornKey(dayKey(s.Now())))
		if err != nil {
			return err
		}
		return s.Send(11067, proto.Message{"partner_id": id, "end_time": until, "day_num": used})
	})
	r.Handle(11068, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("partner_id")
		reply := func(code int, msg string) error { return s.Send(11068, proto.Message{"code": code, "msg": msg}) }
		p, err := st.PartnerByID(role.RID, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "英雄不存在")
			}
			return err
		}
		now := s.Now().Unix()
		key := rebornKey(dayKey(s.Now()))
		used, err := st.Counter(role.RID, key)
		if err != nil {
			return err
		}
		switch {
		case p.Lev > rebornMaxLev:
			return reply(0, "100级或以下的英雄才能重生")
		case p.Lev == 1 && p.BreakLev == 0 && len(p.Equips) == 0 && len(p.Talents) == 0:
			return reply(0, "该英雄无需重生")
		case used >= rebornPerDay:
			return reply(0, "今日重生次数已达上限")
		}
		if until, _ := st.PartnerResetUntil(role.RID, id); until > now {
			return reply(0, "不要频繁重生同一位英雄哦！")
		}
		assets, items := refund(*p)
		if err := st.RebornPartner(role.RID, id, p.Lev, p.BreakLev, now, now+rebornCooldown, key, rebornPerDay, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "重生失败，请重试")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		np, err := st.PartnerByID(role.RID, id)
		if err != nil {
			return err
		}
		if err := s.Send(11002, partnerMessage(*np)); err != nil {
			return err
		}
		if err := reply(1, "重生成功"); err != nil {
			return err
		}
		return s.Send(11067, proto.Message{"partner_id": id, "end_time": now + rebornCooldown, "day_num": used + 1})
	})
	// offering: the heroes asked for, checked the same way by the preview and the act
	offered := func(role *store.Role, req proto.Message) (ids []int64, assets, items map[int64]int64, msg string, err error) {
		list := req.Arr("list")
		if len(list) == 0 {
			return nil, nil, nil, "没有放入英雄", nil
		}
		if len(list) > offerMax {
			return nil, nil, nil, "一次放入的英雄过多", nil
		}
		assets, items = map[int64]int64{}, map[int64]int64{}
		seen := map[int64]bool{}
		for _, e := range list {
			id := e.Int("partner_id")
			p, perr := st.PartnerByID(role.RID, id)
			switch {
			case errors.Is(perr, store.ErrNotFound) || seen[id]:
				return nil, nil, nil, "英雄不存在", nil
			case perr != nil:
				return nil, nil, nil, "", perr
			case p.Locked:
				return nil, nil, nil, "已锁定的英雄不能献祭", nil
			}
			seen[id] = true
			ids = append(ids, id)
			a, i := refund(*p)
			for k, v := range a {
				assets[k] += v
			}
			for k, v := range i {
				items[k] += v
			}
		}
		return ids, assets, items, "", nil
	}
	r.Handle(11075, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		_, assets, items, msg, err := offered(role, req)
		if err != nil {
			return err
		}
		if msg != "" {
			return s.Send(11075, proto.Message{"code": 0, "msg": msg, "list": []proto.Message{}})
		}
		return s.Send(11075, proto.Message{"code": 1, "msg": "", "list": refundList(assets, items)})
	})
	r.Handle(11076, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ids, assets, items, msg, err := offered(role, req)
		if err != nil {
			return err
		}
		if msg == "" {
			if err = st.OfferPartners(role.RID, ids, assets, items); errors.Is(err, store.ErrConflict) {
				msg, err = "献祭失败，请重试", nil
			}
		}
		if err != nil {
			return err
		}
		if msg != "" {
			return s.Send(11076, proto.Message{"code": 0, "msg": msg, "list": []proto.Message{}})
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		out := []proto.Message{}
		for _, id := range ids {
			out = append(out, proto.Message{"partner_id": id})
		}
		return s.Send(11076, proto.Message{"code": 1, "msg": "献祭成功", "list": out})
	})
}
