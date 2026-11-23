package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The friends' side of the expedition: a hero lent to the friends, and friends' heroes hired for the day.

func (e *expSvc) heroMessage(h store.Partner) proto.Message {
	return proto.Message{"id": h.PartnerID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "power": partnerMessage(h).Int("power"), "ext_data": []proto.Message{}}
}

func (e *expSvc) partner(rid, id int64) (*store.Partner, error) {
	heroes, err := e.st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	for i := range heroes {
		if heroes[i].PartnerID == id {
			return &heroes[i], nil
		}
	}
	return nil, nil
}

func expeditionDispatchProblem(h *store.Partner, h5 bool) string {
	switch {
	case h == nil:
		return "英雄不存在"
	case h5 && !h5HeroEligible(h.BID, h.Star):
		return "只能派遣五星及以上英雄"
	case h.ResSlot > 0:
		return "原力水晶槽位中的英雄不能派遣"
	case h.EndTime > 0:
		return "临时英雄不能派遣"
	}
	return ""
}

// mySent (24405) is the hero the role lent to the friends.
func (e *expSvc) mySent(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *expState) error {
		h5 := isH5RoleSession(s)
		list := []proto.Message{}
		if st.Sent != 0 {
			h, err := e.partner(role.RID, st.Sent)
			if err != nil {
				return err
			}
			if expeditionDispatchProblem(h, h5) == "" {
				list = append(list, e.heroMessage(*h))
			}
		}
		return s.Send(24405, proto.Message{"list": list})
	})
}

// dispatch (24407) lends a hero to the friends, one at a time.
func (e *expSvc) dispatch(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *expState) error {
		refuse := func(msg string) error { return s.Send(24407, proto.Message{"code": 0, "msg": msg, "id": id}) }
		h, err := e.partner(role.RID, id)
		if err != nil {
			return err
		}
		if problem := expeditionDispatchProblem(h, isH5RoleSession(s)); problem != "" {
			return refuse(problem)
		}
		st.Sent = id
		if err := e.save(role.RID, st, nil); errors.Is(err, store.ErrConflict) {
			return refuse("远征进度已更新，请重试")
		} else if err != nil {
			return err
		}
		return s.Send(24407, proto.Message{"code": 1, "msg": "派遣成功", "id": id})
	})
}

// friendHeroes (24406) lists the heroes lent by the friends of the same server, and whether they are hired today.
func (e *expSvc) friendHeroes(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *expState) error {
		h5 := isH5RoleSession(s)
		friends, err := e.st.Friends(role.RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, f := range friends {
			if f.SrvID != role.SrvID {
				continue
			}
			fs := &expState{}
			if _, err := e.st.LoadState(f.RID, expModule, fs); err != nil {
				return err
			}
			if fs.Sent == 0 {
				continue
			}
			h, err := e.partner(f.RID, fs.Sent)
			if err != nil {
				return err
			}
			if expeditionDispatchProblem(h, h5) != "" {
				continue
			}
			m := e.heroMessage(*h)
			m["rid"], m["srv_id"], m["name"] = f.RID, f.SrvID, f.Name
			m["is_employ"] = 0
			for _, x := range st.Hired {
				if x.RID == f.RID && x.PID == h.PartnerID {
					m["is_employ"] = 1
				}
			}
			out = append(out, m)
		}
		return s.Send(24406, proto.Message{"list": out})
	})
}

// hire (24408) hires a friend's lent hero for the day: three a day at most, none stronger than 150% of the role's best
// hero, and a hero is hired ten times a day at most. The owner gets the client's drop for it.
func (e *expSvc) hire(s *gateway.Session, req proto.Message) error {
	rid, srvID, id := req.Int("rid"), req.Str("srv_id"), req.Int("id")
	return e.with(s, func(role *store.Role, st *expState) error {
		refuse := func(msg string) error { return s.Send(24408, proto.Message{"code": 0, "msg": msg}) }
		if rid == role.RID || srvID != role.SrvID {
			return refuse("只能雇佣本服好友的英雄")
		}
		h5 := isH5RoleSession(s)
		var hired int64
		for _, h := range st.Hired {
			if h5 && !h5HeroEligible(h.BID, int(h.Star)) {
				continue
			}
			hired++
			if h.RID == rid && h.PID == id {
				return refuse("今天已经雇佣过该英雄")
			}
		}
		if hired >= expCfg.num("num_employees") {
			return refuse("今日雇佣次数已满")
		}
		ok, err := e.st.AreFriends(role.RID, rid)
		if err != nil {
			return err
		}
		owner, err := e.st.RoleByID(rid)
		if err != nil || !ok || owner.SrvID != role.SrvID {
			return refuse("只能雇佣本服好友的英雄")
		}
		now := s.Now()
		if h5 {
			now = dayTime(st.Day)
		}
		fs, err := e.load(rid, now)
		if err != nil {
			return err
		}
		target, err := e.partner(rid, id)
		if err != nil {
			return err
		}
		if fs.Sent != id || expeditionDispatchProblem(target, false) != "" {
			return refuse("该英雄未派遣")
		}
		if h5 && !h5HeroEligible(target.BID, target.Star) {
			return refuse("只能雇佣五星及以上英雄")
		}
		if fs.Borrowed >= expCfg.num("num_employment") {
			return refuse("该英雄今日被雇佣次数已满")
		}
		mine, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		var best int64
		for _, h := range playablePartners(mine, h5) {
			best = max(best, partnerMessage(h).Int("power"))
		}
		power := partnerMessage(*target).Int("power")
		if power*1000 > best*expCfg.num("war_ratio") {
			return refuse("雇佣英雄战力超过自身最高战力英雄的150%")
		}
		st.Hired = append(st.Hired, expHire{RID: rid, SrvID: owner.SrvID, PID: id, BID: target.BID,
			Lev: int64(target.Lev), Star: int64(target.Star), Break: int64(target.BreakLev), Power: power, Per: 100})
		fs.Borrowed++
		if err := e.st.CommitModuleState(expModule, expeditionChange(role.RID, st, nil), expeditionChange(rid, fs, expCfg.Drop())); errors.Is(err, store.ErrConflict) {
			return refuse("远征进度已更新，请重试")
		} else if err != nil {
			return err
		}
		st.saved = true
		return s.Send(24408, proto.Message{"code": 1, "msg": "雇佣成功"})
	})
}

// hp (24409) is the hp the heroes carry over, and the heroes hired today.
func (e *expSvc) hp(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *expState) error {
		h5 := isH5RoleSession(s)
		list := []proto.Message{}
		for _, h := range st.Hired {
			if h5 && !h5HeroEligible(h.BID, int(h.Star)) {
				continue
			}
			owner, _ := e.st.RoleByID(h.RID)
			name := ""
			if owner != nil {
				name = owner.Name
			}
			m := e.heroMessage(store.Partner{PartnerID: h.PID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star), BreakLev: int(h.Break)})
			if h.Power > 0 {
				m["power"] = h.Power
			}
			m["rid"], m["srv_id"], m["name"], m["is_used"], m["hp_per"] = h.RID, h.SrvID, name, b2i(h.Used), h.Per
			list = append(list, m)
		}
		plist := []proto.Message{}
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		for _, h := range playablePartners(all, h5) {
			if per := st.hurt(h.PartnerID); per < 100 {
				plist = append(plist, proto.Message{"id": h.PartnerID, "hp_per": per})
			}
		}
		return s.Send(24409, proto.Message{"list": list, "p_list": plist})
	})
}
