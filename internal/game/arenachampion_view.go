package game

import (
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// champPhase is where the match is at a moment.
type champPhase struct {
	ev     *champEvent // the event of the moment, nil when none is running or lately over
	round  int         // the round that has begun last, -1 before the first
	over   bool
	status int64 // 1 get ready, 2 bet, 3 fight
}

func (c *champSvc) phaseOf(stt *champState, now time.Time) champPhase {
	p := champPhase{round: -1}
	last, _ := c.eventAt(now)
	ev := stt.Ev
	if ev == nil || ev.Skip || last == 0 || ev.T0 != last {
		return p
	}
	p.ev = ev
	t := now.Unix()
	for i := champRounds - 1; i >= 0; i-- {
		if start, guess, fight, end := champTimes(ev.T0, i); t >= start {
			p.round = i
			switch {
			case t >= end && i == champRounds-1:
				p.over, p.status = true, 3
			case t < guess:
				p.status = 1
			case t < fight:
				p.status = 2
			default:
				p.status = 3
			}
			break
		}
	}
	return p
}

// baseInfo is 20250.
func (c *champSvc) baseInfo(stt *champState, now time.Time, flag int64) proto.Message {
	m := c.baseInfoOf(stt, now, flag)
	if c.cross { // the cross window: srv_id of the zone (one server: none) and whether a match is on or announced
		m["srv_id"], m["is_open"] = "", b2i(m.Int("step") != 0)
	}
	return m
}

func (c *champSvc) baseInfoOf(stt *champState, now time.Time, flag int64) proto.Message {
	p := c.phaseOf(stt, now)
	t := now.Unix()
	m := proto.Message{"start_time": 0, "end_time": 0, "step": 0, "step_status": 0, "step_status_time": 0, "round": 0, "round_status": 0, "round_status_time": 0, "flag": flag}
	if p.ev == nil {
		if _, next := c.eventAt(now); next != 0 && dayStart(time.Unix(next, 0).In(dayZone)).Unix() == dayStart(now.In(dayZone)).Unix() {
			m["start_time"], m["end_time"] = dayStart(time.Unix(next, 0).In(dayZone)).Unix(), champEndShown(next)
			m["step"], m["step_status_time"] = champStepScore, next
		}
		return m
	}
	ev := p.ev
	m["start_time"], m["end_time"] = dayStart(time.Unix(ev.T0, 0).In(dayZone)).Unix(), champEndShown(ev.T0)
	switch {
	case t < ev.T0:
		m["step"], m["step_status_time"] = champStepScore, ev.T0
	case p.over:
		if t > champEndShown(ev.T0) {
			m["start_time"], m["end_time"] = 0, 0
			return m
		}
		m["step"], m["step_status"], m["round"], m["round_status"] = champStep4, 2, 2, 3
	default:
		i := p.round
		_, guess, fight, end := champTimes(ev.T0, i)
		step, round, stepEnd := int64(champStepScore), int64(i+1), ev.T0+champOffsets[champScoreRnds]
		if i >= champScoreRnds+3 {
			step, round, stepEnd = champStep4, int64(i-champScoreRnds-2), ev.T0+champOffsets[champRounds]
		} else if i >= champScoreRnds {
			step, round, stepEnd = champStep32, int64(i-champScoreRnds+1), ev.T0+champOffsets[champScoreRnds+3]
		}
		m["step"], m["step_status"], m["step_status_time"], m["round"], m["round_status"] = step, 1, stepEnd, round, p.status
		switch p.status {
		case 1:
			m["round_status_time"] = guess
		case 2:
			m["round_status_time"] = fight
		default:
			m["round_status_time"] = end
		}
	}
	return m
}

// ---- who is who

func (c *champSvc) indexOf(ev *champEvent, rid int64) int {
	if ev == nil {
		return -1
	}
	for i := range ev.Entries {
		if !ev.Entries[i].Robot && ev.Entries[i].RID == rid {
			return i
		}
	}
	return -1
}

// ranks gives every entrant's place as it stands: by the selection total until the groups are drawn, then the places the
// ones out and the ones alive hold.
func (c *champSvc) ranks(ev *champEvent) []int64 {
	out := make([]int64, len(ev.Entries))
	if ev.Paired[champScoreRnds] {
		for i := range ev.Entries {
			out[i] = ev.Entries[i].Rank
		}
		return out
	}
	for pos, i := range ev.ordered() {
		out[i] = int64(pos + 1)
	}
	return out
}

func (c *champSvc) best(rid int64, ev *champEvent, idx int) int64 {
	r := &champRole{}
	_, _ = c.st.LoadState(rid, c.module, r)
	if ev != nil && ev.Done && idx >= 0 && ev.Entries[idx].Rank > 0 && (r.Best == 0 || ev.Entries[idx].Rank < r.Best) {
		return ev.Entries[idx].Rank
	}
	return r.Best
}

// grant hands out the bet chips of the rounds that have begun and gives the role's purse.
func (c *champSvc) grant(ev *champEvent, rid int64, ri int, entrant bool) *champChips {
	ch := ev.chipsOf(rid)
	if !ch.Touched {
		ch.Touched = true
		if !entrant {
			ch.Granted = max(ri, 0)
		}
	}
	for k := ch.Granted; k <= ri; k++ {
		if k < champScoreRnds {
			ch.Have += champCfg.num("guess_number1")
		} else {
			ch.Have += champCfg.num("guess_number2")
		}
		ch.Granted = k + 1
	}
	return ch
}

// ---- messages

func champHeroes(line []arenaHero) []proto.Message {
	out := []proto.Message{}
	for _, h := range line {
		out = append(out, proto.Message{"pos": h.Pos, "bid": h.BID, "lev": h.Lev, "quality": h.Star, "star": h.Star, "break_lev": h.Break,
			"hurt": 0, "behurt": 0, "curt": 0, "ext_data": []proto.Message{}})
	}
	return out
}

// pools gives what has been bet on each side of a match.
func (ev *champEvent) pools(round, match int) (a, b int64) {
	for _, bets := range ev.Bets {
		for _, x := range bets {
			if int(x.Round) == round && int(x.Match) == match {
				if x.Side == 1 {
					a += x.Amount
				} else {
					b += x.Amount
				}
			}
		}
	}
	return a, b
}

// champOdds is what a chip bet on a side pays per thousand: the less is on it, the more.
func champOdds(own, other int64) int64 {
	r := 1000 * (own + other + 2*champOddsSeed) / (own + champOddsSeed)
	return min(max(r, champOddsMin), champOddsMax)
}

// matchMsg describes a match; me is the entrant who sees it (-1 for none) and stands on the A side when in it.
func (c *champSvc) matchMsg(ev *champEvent, round, mi int, now int64, me int) proto.Message {
	m := ev.Rounds[round][mi]
	a, b := m.A, m.B
	aline, bline, apow, bpow, aform, bform := m.ALine, m.BLine, m.APow, m.BPow, m.AForm, m.BForm
	ret := int64(0)
	_, _, fight, end := champTimes(ev.T0, round)
	if now >= end {
		ret = m.Ret
	}
	replay := int64(0)
	if now >= fight {
		replay = m.Replay
	}
	pa, pb := ev.pools(round, mi)
	swap := me >= 0 && m.B == me
	if swap {
		a, b, aline, bline, apow, bpow, aform, bform = b, a, bline, aline, bpow, apow, bform, aform
		pa, pb = pb, pa
		if ret != 0 {
			ret = 3 - ret
		}
	}
	if !m.Locked {
		ea, eb := ev.Entries[a], ev.Entries[b]
		c.refresh(&ea)
		c.refresh(&eb)
		aline, bline, apow, bpow, aform, bform = ea.Lineup, eb.Lineup, ea.Power, eb.Power, ea.FormType, eb.FormType
	}
	out := proto.Message{"step": m.Step, "round": m.Round, "group": m.Group, "ret": ret, "replay_id": replay}
	put := func(p string, e *champEntrant, line []arenaHero, power, form, bet int64) {
		out[p+"bet"], out[p+"rid"], out[p+"srv_id"], out[p+"name"], out[p+"lev"] = bet, e.RID, e.SrvID, e.Name, e.Lev
		out[p+"face"], out[p+"face_update_time"], out[p+"face_file"], out[p+"avatar_id"], out[p+"sex"] = e.Face, 0, "", 0, e.Sex
		out[p+"power"], out[p+"formation_type"], out[p+"formation_lev"], out[p+"plist"] = power, form, 1, champHeroes(line)
		out[p+"sprite_lev"], out[p+"sprites"] = 0, []proto.Message{}
	}
	put("a_", &ev.Entries[a], aline, apow, aform, pa)
	put("b_", &ev.Entries[b], bline, bpow, bform, pb)
	return out
}

// myMatchIndex finds the latest match of an entrant that has begun.
func (c *champSvc) myMatchIndex(ev *champEvent, me, upto int) (round, mi int, ok bool) {
	for r := upto; r >= 0; r-- {
		for j, m := range ev.Rounds[r] {
			if m.A == me || m.B == me {
				return r, j, true
			}
		}
	}
	return 0, 0, false
}

// ---- handlers

// view runs a handler on the state of the role's server.
func (c *champSvc) view(s *gateway.Session, fn func(role *store.Role, stt *champState, p champPhase, now time.Time) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.Now()
	stt, _, err := c.load(role.SrvID, now)
	if err != nil {
		return err
	}
	return fn(role, stt, c.phaseOf(stt, now), now)
}

func (c *champSvc) save(srv string, stt *champState) error {
	return c.st.SaveGlobal(srv, c.module, stt)
}

func (c *champSvc) roleInfo(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		msg := proto.Message{"rank": 0, "best_rank": c.best(role.RID, p.ev, c.indexOf(p.ev, role.RID)), "can_bet": 0, "group": 0}
		if p.ev != nil {
			idx := c.indexOf(p.ev, role.RID)
			ch := c.grant(p.ev, role.RID, p.round, idx >= 0)
			msg["can_bet"] = ch.Have
			if idx >= 0 {
				msg["rank"] = c.ranks(p.ev)[idx]
				if p.ev.Entries[idx].Seed > 0 {
					msg["group"] = p.ev.Entries[idx].Group
				}
			}
			if err := c.save(role.SrvID, stt); err != nil {
				return err
			}
		}
		if err := s.Send(c.id(20251), msg); err != nil {
			return err
		}
		base := c.baseInfo(stt, now, 0)
		if key := champSnapshot(base); c.pushed[role.RID] != key {
			c.pushed[role.RID] = key
			return s.Send(c.id(20250), base)
		}
		return nil
	})
}

func (c *champSvc) myMatch(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		empty := proto.Message{"step": 0, "round": 0, "group": 0, "ret": 0, "replay_id": 0}
		if p.ev != nil {
			if me := c.indexOf(p.ev, role.RID); me >= 0 {
				if r, j, ok := c.myMatchIndex(p.ev, me, p.round); ok {
					return s.Send(c.id(20252), c.matchMsg(p.ev, r, j, now.Unix(), me))
				}
			}
		}
		return s.Send(c.id(20252), empty)
	})
}

func (c *champSvc) guessMatch(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		if p.ev == nil || p.round < 0 {
			return s.Send(c.id(20253), proto.Message{"step": 0, "round": 0, "group": 0, "ret": 0, "replay_id": 0})
		}
		ev, r := p.ev, p.round
		j := ev.Guess[r]
		msg := c.matchMsg(ev, r, j, now.Unix(), -1)
		pa, pb := ev.pools(r, j)
		msg["a_bet_ratio"], msg["b_bet_ratio"] = champOdds(pa, pb), champOdds(pb, pa)
		msg["bet_type"], msg["bet_val"] = 0, 0
		for _, b := range ev.Bets[role.RID] {
			if int(b.Round) == r {
				msg["bet_type"], msg["bet_val"] = b.Side, b.Amount
			}
		}
		return s.Send(c.id(20253), msg)
	})
}

func (c *champSvc) bet(s *gateway.Session, req proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		refuse := func(msg string) error {
			return s.Send(c.id(20254), proto.Message{"code": 0, "msg": msg, "can_bet": 0, "bet_type": 0, "bet_val": 0})
		}
		ev := p.ev
		if ev == nil || p.over || p.round < 0 || p.status != 2 {
			return refuse("现在不是竞猜时间")
		}
		side, amount := req.Int("bet_type"), req.Int("bet_val")
		if side != 1 && side != 2 {
			return refuse("请选择要竞猜的一方")
		}
		ch := c.grant(ev, role.RID, p.round, c.indexOf(ev, role.RID) >= 0)
		limit := min(ch.Have, champCfg.num("guess_limit"))
		if amount <= 0 || amount > limit {
			return refuse("竞猜币不足")
		}
		for _, b := range ev.Bets[role.RID] {
			if int(b.Round) == p.round {
				return refuse("本轮已经竞猜过了")
			}
		}
		j := ev.Guess[p.round]
		pa, pb := ev.pools(p.round, j)
		ratio := champOdds(pa, pb)
		if side == 2 {
			ratio = champOdds(pb, pa)
		}
		ch.Have -= amount
		ev.Bets[role.RID] = append(ev.Bets[role.RID], champBet{Round: int64(p.round), Match: int64(j), Side: side, Amount: amount, Ratio: ratio})
		if err := c.save(role.SrvID, stt); err != nil {
			return err
		}
		if err := s.Send(c.id(20254), proto.Message{"code": 1, "msg": "竞猜成功", "can_bet": ch.Have, "bet_type": side, "bet_val": amount}); err != nil {
			return err
		}
		pa, pb = ev.pools(p.round, j)
		return s.Send(c.id(20257), proto.Message{"a_bet": pa, "b_bet": pb, "a_bet_ratio": champOdds(pa, pb), "b_bet_ratio": champOdds(pb, pa)})
	})
}

func (c *champSvc) myBets(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		list := []proto.Message{}
		if p.ev != nil {
			for k, b := range p.ev.Bets[role.RID] {
				m := c.matchMsg(p.ev, int(b.Round), int(b.Match), now.Unix(), -1)
				m["id"], m["target"], m["bet"], m["get_bet"] = k+1, b.Side, b.Amount, b.Get
				list = append(list, m)
			}
		}
		return s.Send(c.id(20255), proto.Message{"list": list})
	})
}

// result is the window that congratulates a role on its place, once, after the match.
func (c *champSvc) result(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		ev := stt.Ev
		idx := c.indexOf(ev, role.RID)
		if ev == nil || !ev.Done || idx < 0 || ev.Seen[role.RID] {
			return nil
		}
		ev.Seen[role.RID] = true
		if err := c.save(role.SrvID, stt); err != nil {
			return err
		}
		e := ev.Entries[idx]
		return s.Send(c.id(20256), proto.Message{"rank": e.Rank, "cnum": e.Fights, "win": e.Wins})
	})
}

func (c *champSvc) myLog(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		list := []proto.Message{}
		if ev := p.ev; ev != nil {
			if me := c.indexOf(ev, role.RID); me >= 0 {
				for r := 0; r <= p.round; r++ {
					for j, m := range ev.Rounds[r] {
						if m.A != me && m.B != me {
							continue
						}
						msg := c.matchMsg(ev, r, j, now.Unix(), me)
						score := int64(0)
						if m.Step == champStepScore && now.Unix() >= ev.T0+champOffsets[r+1] && msg.Int("ret") == 1 {
							score = champWinPoints
						}
						msg["id"], msg["score"] = r+1, score
						list = append(list, msg)
					}
				}
			}
		}
		return s.Send(c.id(20258), proto.Message{"list": list})
	})
}

// slotRet is how the player in a slot did in the match it stands in, as far as the round has ended.
func (c *champSvc) slotRet(ev *champEvent, round int, group, slot int64, now int64) (ret, replay int64) {
	pos := slot
	if pos%2 == 0 {
		pos--
	}
	for _, m := range ev.Rounds[round] {
		if m.Group != group || m.Pos != pos {
			continue
		}
		_, _, fight, end := champTimes(ev.T0, round)
		if now >= fight {
			replay = m.Replay
		}
		if now >= end {
			win := slot == pos // slot is the A side of the match
			if (m.Ret == 1) == win {
				return 1, replay
			}
			return 2, replay
		}
	}
	return 0, replay
}

func (c *champSvc) slotMsg(ev *champEvent, idx int, pos int64, ret, replay int64) proto.Message {
	e := ev.Entries[idx]
	return proto.Message{"pos": pos, "rid": e.RID, "srv_id": e.SrvID, "name": e.Name, "ret": ret, "replay_id": replay, "face": e.Face, "face_update_time": 0, "face_file": ""}
}

func (c *champSvc) group32(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		list := []proto.Message{}
		if ev := p.ev; ev != nil && ev.Paired[champScoreRnds] {
			for g := 0; g < champGroups; g++ {
				slots := []proto.Message{}
				for sl := 1; sl <= 14; sl++ {
					idx := ev.Groups[g][sl-1]
					if idx < 0 {
						continue
					}
					round := champScoreRnds
					switch {
					case sl > 12:
						round += 2
					case sl > 8:
						round++
					}
					ret, replay := c.slotRet(ev, round, int64(g+1), int64(sl), now.Unix())
					slots = append(slots, c.slotMsg(ev, idx, int64(sl), ret, replay))
				}
				list = append(list, proto.Message{"group": g + 1, "pos_list": slots})
			}
		}
		return s.Send(c.id(20260), proto.Message{"list": list})
	})
}

func (c *champSvc) top4(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		slots := []proto.Message{}
		if ev := p.ev; ev != nil && ev.Paired[champScoreRnds+3] {
			for sl := 1; sl <= 7; sl++ {
				idx := ev.Top4[sl-1]
				if idx < 0 {
					continue
				}
				round := champScoreRnds + 3
				if sl > 4 {
					round++
				}
				ret, replay := int64(1), int64(0)
				if sl < 7 {
					ret, replay = c.slotRet(ev, round, 0, int64(sl), now.Unix())
				}
				slots = append(slots, c.slotMsg(ev, idx, int64(sl), ret, replay))
			}
		}
		return s.Send(c.id(20261), proto.Message{"pos_list": slots})
	})
}

func (c *champSvc) guessAt(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		msg := proto.Message{"group": 0, "pos": 0}
		if p.ev != nil && p.round >= champScoreRnds && !p.over {
			m := p.ev.Rounds[p.round][p.ev.Guess[p.round]]
			msg["group"], msg["pos"] = m.Group, m.Pos
		}
		return s.Send(c.id(20262), msg)
	})
}

func (c *champSvc) matchAt(s *gateway.Session, req proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		if ev := p.ev; ev != nil {
			for r := champScoreRnds; r <= p.round; r++ {
				for j, m := range ev.Rounds[r] {
					if m.Group == req.Int("group") && m.Pos == req.Int("pos") {
						return s.Send(c.id(20263), c.matchMsg(ev, r, j, now.Unix(), -1))
					}
				}
			}
		}
		return s.Send(c.id(20263), proto.Message{"step": 0, "round": 0, "group": 0, "ret": 0, "replay_id": 0})
	})
}

// worshipOf is how often a role was worshipped and whether the viewer did it today.
func (c *champSvc) worshipOf(viewer, rid int64, now time.Time) (int64, int64) {
	n, _ := c.st.Counter(rid, worshipGot)
	done, _ := c.st.Counter(viewer, worshipPerDay+strconv.FormatInt(dayKey(now), 10)+"_0_"+strconv.FormatInt(rid, 10))
	return n, int64(b2i(done > 0))
}

func (c *champSvc) statues(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		list := []proto.Message{}
		for _, t := range stt.Last {
			w, st := c.worshipOf(role.RID, t.RID, now)
			list = append(list, proto.Message{"rid": t.RID, "srv_id": t.SrvID, "name": t.Name, "lev": t.Lev, "rank": t.Rank, "sex": t.Sex,
				"lookid": defaultLookID(int(t.Face)), "worship": w, "worship_status": st})
		}
		return s.Send(c.id(20280), proto.Message{"rank_list": list})
	})
}

func (c *champSvc) topMessage(stt *champState, now time.Time) proto.Message {
	list := []proto.Message{}
	for _, t := range stt.Last {
		list = append(list, proto.Message{"rid": t.RID, "srv_id": t.SrvID, "name": t.Name, "lev": t.Lev, "face": t.Face, "sex": t.Sex, "rank": t.Rank,
			"face_update_time": 0, "face_file": ""})
	}
	return proto.Message{"time": now.Unix(), "rank_list": list}
}

func (c *champSvc) ranking(s *gateway.Session, _ proto.Message) error {
	return c.view(s, func(role *store.Role, stt *champState, p champPhase, now time.Time) error {
		msg := proto.Message{"rank": 0, "worship": 0, "power": 0, "rank_list": []proto.Message{}}
		if ev := p.ev; ev != nil {
			ranks := c.ranks(ev)
			order := make([]int, len(ev.Entries))
			for i := range order {
				order[i] = i
			}
			sort.SliceStable(order, func(x, y int) bool { return ranks[order[x]] < ranks[order[y]] })
			list := []proto.Message{}
			for _, i := range order {
				e := ev.Entries[i]
				w, st := int64(0), int64(0)
				if !e.Robot {
					w, st = c.worshipOf(role.RID, e.RID, now)
				}
				list = append(list, proto.Message{"rid": e.RID, "srv_id": e.SrvID, "name": e.Name, "lev": e.Lev, "face": e.Face, "sex": e.Sex, "rank": ranks[i],
					"score": e.total(), "power": e.Power, "face_update_time": 0, "face_file": "", "worship": w, "worship_status": st})
			}
			msg["rank_list"] = list
			if me := c.indexOf(ev, role.RID); me >= 0 {
				msg["rank"], msg["power"], msg["score"] = ranks[me], ev.Entries[me].Power, ev.Entries[me].total()
				msg["worship"], _ = c.worshipOf(role.RID, role.RID, now)
			}
		}
		return s.Send(c.id(20281), msg)
	})
}
