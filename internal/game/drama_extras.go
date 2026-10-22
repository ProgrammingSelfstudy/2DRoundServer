package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Numbers from the client's data_drama_const; the policies are marked.
const (
	hookMaxSeconds  = 43200 // profit_time_max: the hook income stops growing after 12 hours
	quickMinutes    = 120   // quick_swap_reward: a quick battle pays 120 minutes of hook income
	sweepMaxPerCall = 5     // swap_times_max: sweeps of one dungeon in one request
	sweepPerDay     = 10    // swap_time
	sweepFreePerDay = 3     // free_swap
	sweepTicketItem = 10003 // swap_item: a sweep beyond the free ones takes one ticket
	quickTicketItem = 10004 // quick_swap_item: used first for a quick battle
	quickMaxPerDay  = 3     // data_drama_quick_cost has three rows
	clearRewardMin  = 10    // our policy: a win or a sweep pays this many minutes of the dungeon's hook income
	hookPopupHook   = 1     // 13007 type: hook income
	hookPopupQuick  = 2     // 13007 type: quick battle
	questFastCombat = "evt_dungeon_fast_combat"
	autoNumMax      = 10 // auto_num_max in 13000 and 13006 (original capture 2026-10-20); the client keeps the last one it got
)

// quickGemCost is data_drama_quick_cost: the gems of the n-th (1-based) quick battle of a day.
var quickGemCost = [...]int64{50, 100, 200}

// minutesOf scales a per-minute income list.
func minutesOf(perMinute [][2]int64, minutes int64) []sellGain {
	out := make([]sellGain, 0, len(perMinute))
	for _, g := range perMinute {
		out = append(out, sellGain{g[0], g[1] * minutes})
	}
	return out
}

// hookRate is the per-minute income the role earns: its frontier dungeon's.
func hookRate(d store.Drama) [][2]int64 { return dungeonTable[d.MaxDunID].PerHook }

// RegisterDramaExtras registers the campaign's income commands: 13017 / 13018
// (hook income), 13004 / 13006 (quick battle) and 13005 (sweep).
func RegisterDramaExtras(r *gateway.Router, st *store.Store) {
	// hookIncome is what the role has accumulated: whole minutes since the last
	// claim, at most 12 hours, at the frontier dungeon's per-minute rate.
	hookIncome := func(s *gateway.Session, rid int64) (seconds int64, since int64, items []sellGain, err error) {
		d, err := dramaState(st, rid)
		if err != nil {
			return 0, 0, nil, err
		}
		since, err = st.HookSince(rid)
		if err != nil {
			return 0, 0, nil, err
		}
		seconds = min(max(s.Now().Unix()-since, 0), hookMaxSeconds)
		return seconds, since, minutesOf(hookRate(d), seconds/60), nil
	}
	list := func(g []sellGain) []proto.Message {
		out := []proto.Message{}
		for _, x := range g {
			out = append(out, proto.Message{"bid": x.ID, "num": x.Amount})
		}
		return out
	}
	expOf := func(rid int64) (int64, error) {
		as, err := st.RoleAssets(rid)
		return as[5], err
	}
	// popup is 13007: the reward window of a hook claim or a quick battle. The
	// level and exp it shows are the role's before (oldExp, the session's level)
	// and after the reward.
	popup := func(s *gateway.Session, role *store.Role, kind int64, dun int64, seconds int64, g []sellGain, oldExp int64) error {
		fresh, err := st.RoleByID(role.RID)
		if err != nil {
			return err
		}
		newExp, err := expOf(role.RID)
		if err != nil {
			return err
		}
		return s.Send(13007, proto.Message{"type": kind, "dun_id": dun, "time": seconds, "old_lev": role.Lev, "new_lev": fresh.Lev,
			"items": list(g), "auto_start_dun_id": 0, "auto_end_dun_id": 0, "fast_add_time1": 0, "fast_add_time2": 0,
			"old_exp": oldExp, "new_exp": newExp, "vip_buff": 0, "honor_buff": 0})
	}
	refresh := func(s *gateway.Session, rid int64) error {
		if err := sendAssets(s, st, rid); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}

	r.Handle(13017, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		seconds, _, items, err := hookIncome(s, role.RID)
		if err != nil {
			return err
		}
		return s.Send(13017, proto.Message{"hook_time": seconds, "list": list(items)})
	})
	r.Handle(13018, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		seconds, since, items, err := hookIncome(s, role.RID)
		if err != nil {
			return err
		}
		if len(items) == 0 || seconds < 60 {
			return s.Send(13018, proto.Message{"code": 0, "msg": "暂无可领取的挂机收益"})
		}
		assets, bag := map[int64]int64{}, map[int64]int64{}
		splitGains(items, 1, assets, bag)
		before, err := expOf(role.RID)
		if err != nil {
			return err
		}
		if err := st.ClaimHook(role.RID, since, s.Now().Unix(), assets, bag); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return s.Send(13018, proto.Message{"code": 0, "msg": "暂无可领取的挂机收益"})
			}
			return err
		}
		d, _ := dramaState(st, role.RID)
		if err := s.Send(13018, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := popup(s, role, hookPopupHook, d.MaxDunID, seconds, items, before); err != nil {
			return err
		}
		if err := s.Send(13017, proto.Message{"hook_time": 0, "list": []proto.Message{}}); err != nil {
			return err
		}
		return refresh(s, role.RID)
	})

	quickState := func(s *gateway.Session, rid int64) (proto.Message, store.DramaDaily, error) {
		dd, err := st.DramaDaily(rid, dayKey(s.Now()))
		free := int64(0)
		if dd.Quick == 0 {
			free = 1
		}
		return proto.Message{"fast_combat_num": dd.Quick, "fast_combat_max": quickMaxPerDay, "auto_num": dd.Sweep, "auto_num_max": autoNumMax,
			"is_auto_combat": 0, "fast_combat_add_time": 0, "fast_combat_w_num": 0, "fast_combat_p_num": 0,
			"fast_combat_free_num": free, "is_holiday": 0}, dd, err
	}
	r.Handle(13006, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		m, _, err := quickState(s, role.RID)
		if err != nil {
			return err
		}
		return s.Send(13006, m)
	})
	r.Handle(13004, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		fail := func(msg string) error { return s.Send(13004, proto.Message{"code": 0, "msg": msg}) }
		d, err := dramaState(st, role.RID)
		if err != nil {
			return err
		}
		if d.MaxDunID < 10020 { // fast_combat_first: the first quick battle opens with this dungeon
			return fail("通关指定关卡后开启快速作战")
		}
		_, dd, err := quickState(s, role.RID)
		if err != nil {
			return err
		}
		if dd.Quick >= quickMaxPerDay {
			return fail("今日快速作战次数已用完")
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		if dd.Quick > 0 { // the first one of the day is free; then a quick ticket if there is one, else gems
			items, _ := st.RoleItems(role.RID)
			if items[quickTicketItem] > 0 {
				spendI[quickTicketItem] = 1
			} else {
				spendA[3] = quickGemCost[dd.Quick] // asset 3: gems
			}
		}
		gain := minutesOf(hookRate(d), quickMinutes)
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		splitGains(gain, 1, gainA, gainI)
		before, err := expOf(role.RID)
		if err != nil {
			return err
		}
		if err := st.DramaAction(role.RID, dayKey(s.Now()), 1, 0, quickMaxPerDay, 0, spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return fail("钻石不足或次数已用完")
			}
			return err
		}
		if err := s.Send(13004, proto.Message{"code": 1, "msg": "快速作战成功"}); err != nil {
			return err
		}
		if err := popup(s, role, hookPopupQuick, d.MaxDunID, quickMinutes*60, gain, before); err != nil {
			return err
		}
		m, _, err := quickState(s, role.RID)
		if err != nil {
			return err
		}
		if err := s.Send(13006, m); err != nil {
			return err
		}
		if err := refresh(s, role.RID); err != nil {
			return err
		}
		return questEvent(s, st, role.RID, questFastCombat, 1)
	})

	r.Handle(13005, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		dun, num := req.Int("dun_id"), req.Int("num")
		fail := func(msg string) error {
			return s.Send(13005, proto.Message{"code": 0, "msg": msg, "dun_id": dun, "num": num, "items": []proto.Message{}})
		}
		d, err := dramaState(st, role.RID)
		if err != nil {
			return err
		}
		_, ok := dungeonByID[dun]
		cleared := ok && (dungeonIndex[dun] < dungeonIndex[d.MaxDunID] || (dun == d.MaxDunID && d.Status == dungeonCleared))
		switch {
		case !ok || !cleared:
			return fail("只能扫荡已通关的关卡")
		case num < 1 || num > sweepMaxPerCall:
			return fail("扫荡次数不正确")
		}
		dd, err := st.DramaDaily(role.RID, dayKey(s.Now()))
		if err != nil {
			return err
		}
		if dd.Sweep+num > sweepPerDay {
			return fail("今日扫荡次数不足")
		}
		spendI := map[int64]int64{}
		if paid := dd.Sweep + num - sweepFreePerDay; paid > 0 { // sweeps past the free ones take a ticket each
			spendI[sweepTicketItem] = min(paid, num)
		}
		gain := minutesOf(dungeonTable[dun].PerHook, clearRewardMin*num)
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		splitGains(gain, 1, gainA, gainI)
		if err := st.DramaAction(role.RID, dayKey(s.Now()), 0, num, 0, sweepPerDay, nil, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return fail("扫荡券不足或次数已用完")
			}
			return err
		}
		if err := s.Send(13005, proto.Message{"code": 1, "msg": "扫荡成功", "dun_id": dun, "num": num, "items": list(gain)}); err != nil {
			return err
		}
		// 13006 carries today's sweep count, which the client keeps for the free/ticket decision
		m, _, err := quickState(s, role.RID)
		if err != nil {
			return err
		}
		if err := s.Send(13006, m); err != nil {
			return err
		}
		return refresh(s, role.RID)
	})
}
