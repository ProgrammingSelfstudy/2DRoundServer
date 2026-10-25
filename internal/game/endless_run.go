package game

import (
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func (e *endSvc) pfHeroes(run *endRun) []pfHero {
	out := []pfHero{}
	for _, h := range run.Heroes {
		out = append(out, pfHero{Flag: h.Flag, PID: h.PID, BID: h.BID, Lev: h.Lev, Star: h.Star, HP: h.Per})
	}
	return out
}

func (e *endSvc) openMode(role *store.Role, st *endState, t int64, now int64) string {
	switch {
	case int64(role.Lev) < 25:
		return "冒险家25级开启"
	case t < 1 || t > 5:
		return "试炼不存在"
	case st.Chosen != 0 && st.Chosen != t:
		return "今天已经选择了其他试炼"
	}
	if t != endOld {
		cur, _, _ := endSelect(timeFromUnix(now))
		switch {
		case t != cur:
			return "该试炼当前未开放"
		case st.mode(endOld).Max < endCfg.num("endless_new_limit") || int64(role.Lev) < endCfg.num("endless_new_limit_lev"):
			return "未达到新试炼的开启条件"
		}
	}
	return ""
}

// start (23901): picks the team (own heroes and one hired friend's hero), then asks for the first buff.
func (e *endSvc) start(s *gateway.Session, req proto.Message) error {
	t := req.Int("type")
	return e.with(s, func(role *store.Role, st *endState) error {
		refuse := func(msg string) error { return s.Send(23901, proto.Message{"code": 0, "msg": msg}) }
		if msg := e.openMode(role, st, t, s.Now().Unix()); msg != "" {
			return refuse(msg)
		}
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		var heroes []endHero
		var form []pfSlot
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			id, owner := p.Int("id"), p.Int("owner_id")
			if len(heroes) == formMaxHeroes || seen[id] {
				continue
			}
			if owner == 0 || owner == role.RID {
				h, ok := byID[id]
				if !ok {
					continue
				}
				seen[id] = true
				heroes = append(heroes, endHero{Flag: 0, RID: role.RID, SrvID: role.SrvID, PID: id, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star), Per: 100})
				form = append(form, pfSlot{Pos: p.Int("pos"), ID: id, Flag: 0})
				continue
			}
			for _, h := range st.Hired {
				if h.PID == id && h.RID == owner {
					seen[id] = true
					h.Per = 100
					heroes = append(heroes, h)
					form = append(form, pfSlot{Pos: p.Int("pos"), ID: id, Flag: 1})
				}
			}
		}
		if len(heroes) == 0 {
			return refuse("请选择出战英雄")
		}
		m := st.mode(t)
		st.Chosen = t
		st.Run = endRun{Active: true, Type: t, Round: m.Start, Heroes: heroes}
		st.Form, st.FormType, st.Hallows = form, req.Int("formation_type"), req.Int("hallows_id")
		st.Run.Opts = e.buffOptions()
		st.Run.Choosing = true
		if err := s.Send(23901, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return e.pushBuff(s, role, st)
	})
}

// buffOptions picks one buff of each group by weight.
func (e *endSvc) buffOptions() []endBuffHold {
	var out []endBuffHold
	rng := newRng()
	for _, g := range endCfg.Buffs {
		var ids []int64
		var total int64
		for k, b := range g {
			if b.Weight > 0 {
				id, _ := strconv.ParseInt(k, 10, 64)
				ids = append(ids, id)
				total += b.Weight
			}
		}
		sortIDs(ids)
		if total == 0 {
			continue
		}
		n := int64(rng.Intn(int(total)))
		for _, id := range ids {
			b := g[strconv.FormatInt(id, 10)]
			if n < b.Weight {
				out = append(out, endBuffHold{ID: id, Group: b.GroupID, Count: 1})
				break
			}
			n -= b.Weight
		}
	}
	return out
}

func (e *endSvc) pushBuff(s *gateway.Session, role *store.Role, st *endState) error {
	list := []proto.Message{}
	for _, o := range st.Run.Opts {
		list = append(list, proto.Message{"group_id": o.Group, "buff_id": o.ID})
	}
	team := []proto.Message{}
	for i, h := range st.Run.Heroes {
		team = append(team, proto.Message{"rid": h.RID, "srv_id": h.SrvID, "pos": i + 1, "id": h.PID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "quality": 0, "hp_per": h.Per,
			"use_skin": 0, "resonate_lev": 0})
	}
	return s.Send(23910, proto.Message{"is_select": b2i(st.Run.Choosing), "list": list, "partner": team, "formation_type": max(st.FormType, 1), "formation_lev": 1, "round": st.Run.Round})
}

func (e *endSvc) buffState(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		if !st.Run.Active {
			return nil
		}
		return e.pushBuff(s, role, st)
	})
}

func (e *endSvc) battle(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error { return e.pushBattle(s, role, st) })
}

func (e *endSvc) pushBattle(s *gateway.Session, role *store.Role, st *endState) error {
	run := st.Run
	t := run.Type
	if t == 0 {
		t = st.Chosen
	}
	if t == 0 {
		t = endOld
	}
	m := st.mode(t)
	var next, limit int64
	status := int64(0)
	for _, r := range endCfg.First[t-1] {
		if !m.Taken[strconv.FormatInt(r.ID, 10)] {
			next, limit, status = r.ID, r.LimitID, int64(b2i(m.Max >= r.LimitID))
			break
		}
	}
	buffs := []proto.Message{}
	for _, b := range run.Buffs {
		buffs = append(buffs, proto.Message{"id": b.ID, "group_id": b.Group, "count": b.Count})
	}
	acc := []proto.Message{}
	for _, a := range run.Acc {
		acc = append(acc, proto.Message{"base_id": a[0], "num": a[1]})
	}
	return s.Send(23902, proto.Message{"round": run.Round, "max_round": m.Max, "buff_list": buffs, "rest_round": run.Rest, "max_reward_round": limit, "reward_flag": status,
		"acc_reward": acc, "id": next, "status": status, "type": t})
}

// chooseBuff (23911) takes a buff and goes on with the next fight.
func (e *endSvc) chooseBuff(s *gateway.Session, req proto.Message) error {
	pick := req.Int("buff_id")
	return e.with(s, func(role *store.Role, st *endState) error {
		refuse := func(msg string) error { return s.Send(23911, proto.Message{"code": 0, "msg": msg}) }
		if !st.Run.Active || !st.Run.Choosing {
			return refuse("当前不需要选择增益")
		}
		for _, o := range st.Run.Opts {
			if o.ID != pick {
				continue
			}
			held := false
			for i := range st.Run.Buffs {
				if st.Run.Buffs[i].ID == pick {
					st.Run.Buffs[i].Count++
					held = true
				}
			}
			if !held {
				st.Run.Buffs = append(st.Run.Buffs, endBuffHold{ID: pick, Group: o.Group, Count: 1})
			}
			st.Run.Choosing, st.Run.Opts, st.Run.Rest = false, nil, endCfg.num("buffchoice_interval")
			if err := s.Send(23911, proto.Message{"code": 1, "msg": ""}); err != nil {
				return err
			}
			if err := e.pushBattle(s, role, st); err != nil {
				return err
			}
			return e.fightRound(s, role, st)
		}
		return refuse("增益不存在")
	})
}

// enemyPower is the power of the enemies of a round: half of the team's at the start, then a fifth of a per cent
// stronger... twenty per mille every round.
func endEnemyPower(own, round int64) int64 { return own * (500 + 20*round) / 1000 }

func (e *endSvc) fightRound(s *gateway.Session, role *store.Role, st *endState) error {
	run := &st.Run
	all, err := e.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	own := rolePower(bestHeroes(all, 5))
	var slots []pfSlot
	for _, f := range st.Form {
		slots = append(slots, f)
	}
	pf, msg, err := planesFight(e.st, e.fights, role, e.pfHeroes(run), slots, st.FormType, endEnemyPower(own, run.Round), "试炼守卫", []proto.Message{{"param": run.Round}}, combatEndless)
	if err != nil {
		return err
	}
	if msg != "" { // nobody left to fight
		return e.finishRun(s, role, st, false)
	}
	round, ft, fighters, left := run.Round, pf.formType, pf.fighters, pf.left
	return e.fights.begin(s, role, combatEndless, pf.intro, pf.rounds, pf.won, func(s *gateway.Session, won bool) error {
		return e.settle(s, round, fighters, ft, left, won)
	})
}

func (e *endSvc) settle(s *gateway.Session, round int64, fighters []pfSlot, formType int64, left map[int64]int64, won bool) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		run := &st.Run
		if !run.Active || run.Round != round {
			return nil
		}
		for _, sl := range fighters {
			for i := range run.Heroes {
				if run.Heroes[i].PID == sl.ID && run.Heroes[i].Flag == sl.Flag {
					run.Heroes[i].Per = left[battleCell(formType, sl.Pos)]
				}
			}
		}
		alive := false
		for _, h := range run.Heroes {
			alive = alive || h.Per > 0
		}
		if !won || !alive {
			return e.finishRun(s, role, st, false)
		}
		m := st.mode(run.Type)
		var prize [][2]int64
		key := strconv.FormatInt(round, 10)
		if int(round) <= len(endCfg.Floors[run.Type-1]) && !m.Paid[key] {
			m.Paid[key] = true
			for _, p := range endCfg.Floors[run.Type-1][round-1].Items {
				prize = append(prize, [2]int64{p[0], p[1]})
			}
		}
		if len(prize) > 0 {
			var items endItem
			for _, p := range prize {
				items = append(items, []int64{p[0], p[1]})
			}
			if err := e.gain(s, role, items); err != nil {
				return err
			}
			run.Acc = append(run.Acc, prize...)
		}
		m.DayPass++
		if round > m.Max {
			m.Max = round
			if err := e.noteRank(role, run.Type, round, s.Now().Unix()); err != nil {
				return err
			}
		}
		items := []proto.Message{}
		for _, g := range prize {
			items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
		}
		if err := s.Send(20006, proto.Message{"result": 1, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0, "combat_type": combatEndless,
			"target_role_name": "试炼守卫", "hurt_statistics": []proto.Message{}, "adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}}); err != nil {
			return err
		}
		run.Round++
		run.Rest--
		if err := e.pushBattle(s, role, st); err != nil {
			return err
		}
		if run.Rest <= 0 {
			run.Choosing, run.Opts = true, e.buffOptions()
			return e.pushBuff(s, role, st)
		}
		return e.fightRound(s, role, st)
	})
}

// finishRun ends the run: the fight was lost, everybody fell, or the role left.
func (e *endSvc) finishRun(s *gateway.Session, role *store.Role, st *endState, left bool) error {
	run := st.Run
	st.Run = endRun{}
	if !run.Active {
		return nil
	}
	items := []proto.Message{}
	for _, g := range run.Acc {
		items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
	}
	if err := s.Send(20006, proto.Message{"result": 2, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0, "combat_type": combatEndless,
		"target_role_name": "试炼守卫", "hurt_statistics": []proto.Message{}, "adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}}); err != nil {
		return err
	}
	return e.sendInfo(s, role, st)
}

func (e *endSvc) sendInfo(s *gateway.Session, role *store.Role, st *endState) error {
	cur, next, nextTime := endSelect(s.Now())
	old, nw := st.mode(endOld), st.mode(cur)
	mineOld, listOld, err := e.rankOf(role, endOld)
	if err != nil {
		return err
	}
	mineNew, listNew, err := e.rankOf(role, cur)
	if err != nil {
		return err
	}
	return s.Send(23900, proto.Message{"type": st.Chosen, "select_type": cur, "next_type": next, "next_time": nextTime,
		"max_round": old.Max, "current_round": old.Start, "day_pass_round": old.DayPass, "my_idx": mineOld, "rank_list": listOld,
		"new_max_round": nw.Max, "new_current_round": nw.Start, "new_day_pass_round": nw.DayPass, "new_my_idx": mineNew, "new_rank_list": listNew,
		"is_employ": b2i(len(st.Hired) > 0), "list": []proto.Message{}, "is_appoint": b2i(len(st.Sent) > 0), "is_reward": b2i(nw.Claimed)})
}

// exit is the client leaving the fight (20008): the run ends and is settled.
func (e *endSvc) exit(s *gateway.Session) {
	_ = e.with(s, func(role *store.Role, st *endState) error {
		if !st.Run.Active {
			return nil
		}
		return e.finishRun(s, role, st, true)
	})
}

var _ = errors.New

// ---- support heroes of friends

func (e *endSvc) mySent(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		var list []endHero
		for _, id := range st.Sent {
			for _, h := range all {
				if h.PartnerID == id {
					list = append(list, endHero{RID: role.RID, SrvID: role.SrvID, PID: id, BID: h.BID, Lev: int64(h.Lev), Star: int64(h.Star)})
				}
			}
		}
		return s.Send(23905, proto.Message{"list": e.heroMessages(role, list)})
	})
}

func (e *endSvc) hired(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		out := []proto.Message{}
		for _, h := range st.Hired {
			owner, _ := e.st.RoleByID(h.RID)
			name := ""
			if owner != nil {
				name = owner.Name
			}
			m := e.heroMessages(role, []endHero{h})[0]
			out = append(out, proto.Message{"rid": h.RID, "srv_id": h.SrvID, "name": name, "id": h.PID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "quality": 0, "power": m["power"], "ext_data": []proto.Message{}})
		}
		return s.Send(23906, proto.Message{"list": out})
	})
}

// friendHeroes (23907) lists the heroes the role's friends dispatched, with whether the role has hired them.
func (e *endSvc) friendHeroes(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *endState) error {
		friends, err := e.st.Friends(role.RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, f := range friends {
			if f.SrvID != role.SrvID {
				continue // the support heroes are those of friends on the same server (the client's rule)
			}
			fs := &endState{}
			if _, err := e.st.LoadState(f.RID, endlessModule, fs); err != nil {
				return err
			}
			heroes, err := e.st.PartnersOf(f.RID)
			if err != nil {
				return err
			}
			for _, id := range fs.Sent {
				for _, h := range heroes {
					if h.PartnerID != id {
						continue
					}
					back := int64(0)
					for _, x := range st.Hired {
						if x.RID == f.RID && x.PID == id {
							back = 1
						}
					}
					pw := partnerMessage(h).Int("power")
					out = append(out, proto.Message{"rid": f.RID, "srv_id": f.SrvID, "name": f.Name, "id": id, "bid": h.BID, "lev": h.Lev, "star": h.Star, "quality": 0, "power": pw,
						"is_return": back, "ext_data": []proto.Message{}})
				}
			}
		}
		return s.Send(23907, proto.Message{"list": out})
	})
}

// dispatch (23908) lends a hero to the role's friends; one at a time.
func (e *endSvc) dispatch(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *endState) error {
		refuse := func(msg string) error { return s.Send(23908, proto.Message{"code": 0, "msg": msg, "id": id}) }
		all, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		for _, h := range all {
			if h.PartnerID != id {
				continue
			}
			if h.ResSlot > 0 {
				return refuse("原力水晶槽位中的英雄不能派遣")
			}
			st.Sent = []int64{id}
			return s.Send(23908, proto.Message{"code": 1, "msg": "派遣成功", "id": id})
		}
		return refuse("英雄不存在")
	})
}

// hire (23909): flag 0 hires a friend's hero for today (one of them, not stronger than 120% of the role's best), flag 1
// sends it back. The owner gets friend points for being hired, five times a day at most.
func (e *endSvc) hire(s *gateway.Session, req proto.Message) error {
	rid, id, flag := req.Int("rid"), req.Int("id"), req.Int("flag")
	return e.with(s, func(role *store.Role, st *endState) error {
		refuse := func(msg string) error { return s.Send(23909, proto.Message{"code": 0, "msg": msg}) }
		if flag == 1 {
			var keep []endHero
			for _, h := range st.Hired {
				if !(h.RID == rid && h.PID == id) {
					keep = append(keep, h)
				}
			}
			st.Hired = keep
			return s.Send(23909, proto.Message{"code": 1, "msg": "已归还"})
		}
		if len(st.Hired) > 0 {
			return refuse("今天已经雇佣了一名支援英雄")
		}
		if st.Run.Active {
			return refuse("挑战中无法雇佣")
		}
		ok, err := e.st.AreFriends(role.RID, rid)
		if err != nil {
			return err
		}
		owner, err := e.st.RoleByID(rid)
		if err != nil || !ok || owner.SrvID != role.SrvID {
			return refuse("只能雇佣本服好友的英雄")
		}
		fs := &endState{}
		if _, err := e.st.LoadState(rid, endlessModule, fs); err != nil {
			return err
		}
		sent := false
		for _, x := range fs.Sent {
			sent = sent || x == id
		}
		heroes, err := e.st.PartnersOf(rid)
		if err != nil {
			return err
		}
		var target *store.Partner
		for i := range heroes {
			if heroes[i].PartnerID == id {
				target = &heroes[i]
			}
		}
		if !sent || target == nil {
			return refuse("该英雄未派遣")
		}
		mine, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		var best int64
		for _, h := range mine {
			best = max(best, partnerMessage(h).Int("power"))
		}
		if partnerMessage(*target).Int("power")*100 > best*120 {
			return refuse("支援英雄战力超过自身最高战力的120%")
		}
		st.Hired = []endHero{{Flag: 1, RID: rid, SrvID: owner.SrvID, PID: id, BID: target.BID, Lev: int64(target.Lev), Star: int64(target.Star), Per: 100}}
		// the owner is paid in friend points, a few times a day
		if fs.SupportDay != dayKey(s.Now()) {
			fs.SupportDay, fs.SupportToday = dayKey(s.Now()), 0
		}
		if fs.SupportToday < endCfg.num("support_reward_limit") {
			fs.SupportToday++
			if err := e.gain(nil, owner, endItem{{14, endCfg.num("support_reward")}}); err != nil {
				return err
			}
		}
		if err := e.st.SaveState(rid, endlessModule, fs); err != nil {
			return err
		}
		return s.Send(23909, proto.Message{"code": 1, "msg": "雇佣成功"})
	})
}
