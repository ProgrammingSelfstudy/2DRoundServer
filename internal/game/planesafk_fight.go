package game

import (
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// formationOfReq reads the formation a 28600 carries: type 1 the formation type, 2 the hallows, 3 / 4 a hero of the role /
// a hired hero with its place.
func formationOfReq(req proto.Message) (formType, hallows int64, slots []pfSlot) {
	for _, e := range req.Arr("ext_list") {
		switch e.Int("type") {
		case pfExtFormation:
			formType = e.Int("val1")
		case pfExtHallows:
			hallows = e.Int("val1")
		case pfExtOwnHero:
			slots = append(slots, pfSlot{Pos: e.Int("val1"), ID: e.Int("val2"), Flag: 0})
		case pfExtHiredHero:
			slots = append(slots, pfSlot{Pos: e.Int("val1"), ID: e.Int("val2"), Flag: 1})
		}
	}
	return
}

// monsterInfo pushes 28607: who stands on a tile.
func (p *pfSvc) monsterInfo(s *gateway.Session, role *store.Role, st *pfState, t *pfTile, e pfEventCfg) error {
	_, lineup, err := p.enemyLineup(role, st, t)
	if err != nil {
		return err
	}
	quality := map[int64]int64{}
	for _, b := range st.Buffs {
		quality[pfCfg.Buffs[strconv.FormatInt(b, 10)].Quality]++
	}
	var qs []int64
	for q := range quality {
		qs = append(qs, q)
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i] < qs[j] })
	buffs := []proto.Message{}
	for _, q := range qs {
		buffs = append(buffs, proto.Message{"quality": q, "num": quality[q]})
	}
	guards := []proto.Message{}
	for i, h := range lineup {
		pp := store.Partner{BID: h.BID, Lev: int(h.Lev), Star: int(h.Star), BreakLev: int(h.Break)}
		hp, atk, _, _ := partnerStats(pp)
		guards = append(guards, proto.Message{"partner_id": i + 1, "pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "ext_data": []proto.Message{{"key": 5, "val": 0}}, "hp_per": 100,
			"hp_max": hp, "atk": atk, "guards_power": partnerMessage(pp).Int("power")})
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	// power is 0 and srv_id empty in the original (capture original-2026-10-27, 16-planes-fight); the strength is in guards_power
	return s.Send(28607, proto.Message{"power": 0, "buffs": buffs, "rid": 0, "srv_id": "", "name": e.Name, "face": face, "lev": role.Lev, "guards_power": t.Power,
		"formation_type": 1, "guards": guards, "sprites": []proto.Message{}, "hallows_id": 0, "sprite_lev": 0, "strength": 1000, "face_update_time": 0, "face_file": "", "is_spar": 0})
}

// enemyLineup is a scaled copy of the role's own best heroes; the scale makes its power the tile's.
func (p *pfSvc) enemyLineup(role *store.Role, st *pfState, t *pfTile) (arenaOpp, []arenaHero, error) {
	heroes, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return arenaOpp{}, nil, err
	}
	best := bestHeroes(heroes, 5)
	own := rolePower(best)
	factor := t.Power * 1000 / max(1, own)
	o := arenaOpp{RID: arenaRobotBase + t.Evt, SrvID: role.SrvID, Name: pfCfg.events[t.Evt].Name, Robot: true, FormType: 1, Power: t.Power}
	for i, h := range best {
		o.Lineup = append(o.Lineup, arenaHero{Pos: int64(i + 1), BID: h.BID, Lev: max(1, int64(h.Lev)*factor/1000), Star: int64(h.Star), Break: int64(h.BreakLev)})
	}
	return o, o.Lineup, nil
}

func (p *pfSvc) fightTile(s *gateway.Session, role *store.Role, st *pfState, t *pfTile, e pfEventCfg, action int64, req proto.Message, reply func(int64, string) error) error {
	if action == 0 {
		if err := p.monsterInfo(s, role, st, t, e); err != nil {
			return err
		}
		return reply(1, "")
	}
	formType, hallows, slots := formationOfReq(req)
	if len(slots) == 0 {
		return reply(0, "请选择出战英雄")
	}
	pf, msg, err := planesFight(p.st, p.fights, role, st.Heroes, slots, formType, t.Power, e.Name, []proto.Message{{"param": t.Line}, {"param": t.Index}}, combatPlanes)
	if err != nil {
		return err
	}
	if msg != "" {
		return reply(0, msg)
	}
	st.FormType, st.Hallows, st.Form = pf.formType, hallows, pf.fighters
	// choosing a tile hides the line's other tiles before the fight starts: 28606 lists just those (capture original-2026-10-27)
	st.Chosen[strconv.FormatInt(t.Line, 10)] = t.Index
	if err := p.hideLine(s, st, t); err != nil {
		return err
	}
	if err := reply(1, ""); err != nil {
		return err
	}
	line, index, fighters, ft, left := t.Line, t.Index, pf.fighters, pf.formType, pf.left
	return p.fights.begin(s, role, combatPlanes, pf.intro, pf.rounds, pf.won, func(s *gateway.Session, won bool) error {
		return p.settleFight(s, line, index, fighters, ft, left, won)
	})
}

func (p *pfSvc) settleFight(s *gateway.Session, line, index int64, fighters []pfSlot, formType int64, left map[int64]int64, won bool) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		t := st.tile(line, index)
		if t == nil {
			return nil
		}
		e := pfCfg.events[t.Evt]
		for _, sl := range fighters {
			if h := p.heroOf(st, sl.Flag, sl.ID); h != nil {
				h.HP = left[battleCell(formType, sl.Pos)]
			}
		}
		var prize [][2]int64
		result := int64(2)
		if won {
			result = 1
			rate := int64(1000)
			if rates := pfCfg.nums("planes_reward_rate"); st.Diff >= 1 && int(st.Diff) <= len(rates) {
				rate = rates[st.Diff-1]
			}
			for _, id := range e.RewardList {
				for _, g := range pfRewardFor(id, int64(role.Lev)) {
					prize = append(prize, [2]int64{g[0], g[1] * rate / 1000})
				}
			}
			prize = append(prize, pairsOf(e.Reward)...)
			if line == pfBossLine {
				st.Cleared[strconv.FormatInt(st.Floor, 10)] = true
				st.Points += pfBossPoints[st.Floor]
			}
			st.Loot = append(st.Loot, prize...)
			if err := p.gain(s, role, prize); err != nil {
				return err
			}
		}
		stats := []proto.Message{}
		var dps, best, bestBID, bestLev, bestStar int64
		all, _ := p.st.PartnersOf(role.RID)
		for _, sl := range fighters {
			h := p.heroOf(st, sl.Flag, sl.ID)
			if h == nil {
				continue
			}
			pp, ok := p.partnerOf(role, *h)
			if !ok {
				continue
			}
			_ = all
			pw := partnerMessage(pp).Int("power")
			stats = append(stats, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": h.PID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": partnerTable[h.BID].Camp,
				"dps": pw, "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
			dps += pw
			if pw > best {
				best, bestBID, bestLev, bestStar = pw, h.BID, h.Lev, h.Star
			}
		}
		awards := []proto.Message{}
		for _, g := range prize {
			awards = append(awards, proto.Message{"bid": g[0], "num": g[1]})
		}
		send28608 := func() error {
			return s.Send(28608, proto.Message{"result": result, "all_dps": dps, "best_partner": bestBID, "bid": bestBID, "lev": bestLev, "star": bestStar, "use_skin": 0,
				"target_role_name": e.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "award_list": awards, "line": line, "index": index, "replay_id": 0})
		}
		if !won {
			if err := send28608(); err != nil {
				return err
			}
			if err := p.pushHP(s, st); err != nil {
				return err
			}
			return s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()})
		}
		st.Chosen[strconv.FormatInt(t.Line, 10)] = t.Index
		// a won fight with a relic turns the tile into the relic event and the role stays where it is; the options come when the
		// tile is asked (28600 action 0) and the role steps onto it when one is chosen (capture original-2026-10-27, 20-planes-afk)
		relic := false
		if q, ok := map[int64]int64{3021: 1, 3022: 2, 3023: 3}[e.Exchange]; ok {
			if opts := p.relicOptions(st, q); len(opts) > 0 {
				st.BuffOpts = opts
				t.Evt = e.Exchange
				relic = true
			}
		}
		if !relic {
			st.Line, st.Index = t.Line, t.Index
			st.Done[tileKey(t.Line, t.Index)] = true
		}
		// the original, after the assets: for a boss the war order and the chest flag, then the tiles, the report and the hp of
		// the heroes that fought (capture original-2026-10-27, 21-planes-afk-2)
		if line == pfBossLine {
			if err := s.Send(28616, p.orderMessage(s, st)); err != nil {
				return err
			}
			if err := s.Send(28624, proto.Message{"is_can_reward": 1, "is_reward": b2i(st.Taken[strconv.FormatInt(st.Floor, 10)])}); err != nil {
				return err
			}
		}
		if err := s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()}); err != nil {
			return err
		}
		if err := send28608(); err != nil {
			return err
		}
		fought := []proto.Message{}
		for _, sl := range fighters {
			if h := p.heroOf(st, sl.Flag, sl.ID); h != nil {
				fought = append(fought, proto.Message{"flag": h.Flag, "partner_id": h.PID, "hp_per": h.HP})
			}
		}
		return s.Send(28609, proto.Message{"partners": fought})
	})
}

// relicOptions picks three relics of a quality the role does not have yet.
func (p *pfSvc) relicOptions(st *pfState, quality int64) []int64 {
	have := map[int64]bool{}
	for _, b := range st.Buffs {
		have[b] = true
	}
	var pool []int64
	for k, b := range pfCfg.Buffs {
		id, _ := strconv.ParseInt(k, 10, 64)
		if b.Quality == quality && !have[id] && id < 90000 {
			pool = append(pool, id)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
	rng := newRng()
	var out []int64
	for _, i := range rng.Perm(len(pool)) {
		if len(out) == 3 {
			break
		}
		out = append(out, pool[i])
	}
	return out
}

func (p *pfSvc) pushBuffOptions(s *gateway.Session, st *pfState, t *pfTile) error {
	list := []proto.Message{}
	for _, b := range st.BuffOpts {
		list = append(list, proto.Message{"type": 1, "val1": b, "val2": 0})
	}
	return s.Send(28614, proto.Message{"floor": st.Floor, "line": t.Line, "index": t.Index, "ext_list": list})
}

// chooseBuff handles the relic of the tile the role stands on: asking shows the three again, choosing takes one.
func (p *pfSvc) chooseBuff(s *gateway.Session, role *store.Role, st *pfState, t *pfTile, action int64, ext map[int64][2]int64, reply func(int64, string) error) error {
	if action == 0 {
		if err := p.pushBuffOptions(s, st, t); err != nil {
			return err
		}
		return reply(1, "")
	}
	pick := ext[pfExtBuff][0]
	for _, b := range st.BuffOpts {
		if b == pick {
			st.Buffs = append(st.Buffs, b)
			st.BuffOpts = nil
			// the role steps onto the tile, which is empty now; 28606 comes before the answer, whose msg is empty
			st.Line, st.Index = t.Line, t.Index
			st.Done[tileKey(t.Line, t.Index)] = true
			if err := s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()}); err != nil {
				return err
			}
			return reply(1, "")
		}
	}
	return reply(0, "请选择遗物")
}

// hideLine tells the client to hide the other tiles of the line the role chose a tile in: 28606 lists just those.
func (p *pfSvc) hideLine(s *gateway.Session, st *pfState, t *pfTile) error {
	hidden := []proto.Message{}
	for _, m := range st.tileList() {
		if m.Int("line") == t.Line && m.Int("index") != t.Index {
			hidden = append(hidden, m)
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	return s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": hidden})
}
