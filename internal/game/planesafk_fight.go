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
		guards = append(guards, proto.Message{"partner_id": i + 1, "pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "ext_data": []proto.Message{}, "hp_per": 100,
			"hp_max": hp, "atk": atk, "guards_power": partnerMessage(pp).Int("power")})
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	return s.Send(28607, proto.Message{"power": t.Power, "buffs": buffs, "rid": 0, "srv_id": role.SrvID, "name": e.Name, "face": face, "lev": role.Lev, "guards_power": t.Power,
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
	if formType == 0 {
		formType = 1
	}
	var ownSlots, hiredSlots []store.FormationSlot
	hired := map[int64]store.Partner{}
	var fighters []pfSlot
	for _, sl := range slots {
		h := p.heroOf(st, sl.Flag, sl.ID)
		if h == nil || h.HP <= 0 || len(fighters) == formMaxHeroes {
			continue
		}
		fighters = append(fighters, sl)
		fs := store.FormationSlot{Pos: sl.Pos, PartnerID: sl.ID}
		if sl.Flag == 1 {
			hiredSlots = append(hiredSlots, fs)
			hired[sl.ID] = store.Partner{PartnerID: sl.ID, RID: role.RID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
		} else {
			ownSlots = append(ownSlots, fs)
		}
	}
	if len(fighters) == 0 {
		return reply(0, "没有可以出战的英雄")
	}
	team, objects, err := p.fights.teamFor(role, formType, ownSlots)
	if err != nil {
		return err
	}
	if len(hiredSlots) > 0 {
		hu, ho := sideUnits(role.RID, role.SrvID, hiredSlots, formType, hired, true)
		team, objects = append(team, hu...), append(objects, ho...)
	}
	hpOf := map[int64]*pfSlot{} // battle cell -> who stands there
	for i := range fighters {
		hpOf[battleCell(formType, fighters[i].Pos)] = &fighters[i]
	}
	for i := range team {
		if sl := hpOf[team[i].pos]; sl != nil {
			team[i].hp = max(1, team[i].maxHP*p.heroOf(st, sl.Flag, sl.ID).HP/100)
		}
	}
	for _, o := range objects {
		if o.Int("object_type") == 2 {
			if sl := hpOf[o.Int("pos")]; sl != nil {
				o["hp"] = max(1, o.Int("hp_max")*p.heroOf(st, sl.Flag, sl.ID).HP/100)
			}
		}
	}
	opp, _, err := p.enemyLineup(role, st, t)
	if err != nil {
		return err
	}
	oslots, oform, obyID, err := opp.fighters(p.st)
	if err != nil {
		return err
	}
	foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return reply(0, "守卫已无法应战")
	}
	rounds, won, sim := playFightOpts(team, foes, newRng(), maxFightRounds, false)
	left := map[int64]int64{} // battle cell -> hp per cent left
	for _, u := range sim.units {
		if u.ours {
			left[u.pos] = max(0, u.hp) * 100 / max(1, u.maxHP)
		}
	}
	intro := proto.Message{
		"combat_type": combatPlanes, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
		"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{{"param": t.Line}, {"param": t.Index}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name,
		"target_role_name": e.Name, "suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	st.FormType, st.Hallows, st.Form = formType, hallows, fighters
	if err := reply(1, ""); err != nil {
		return err
	}
	line, index := t.Line, t.Index
	return p.fights.begin(s, role, combatPlanes, intro, rounds, won, func(s *gateway.Session, won bool) error {
		return p.settleFight(s, line, index, fighters, formType, left, won)
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
		reply := func(int64, string) error { return nil }
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
		if err := s.Send(28608, proto.Message{"result": result, "all_dps": dps, "best_partner": bestBID, "bid": bestBID, "lev": bestLev, "star": bestStar, "use_skin": 0,
			"target_role_name": e.Name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "award_list": awards, "line": line, "index": index, "replay_id": 0}); err != nil {
			return err
		}
		if err := p.pushHP(s, st); err != nil {
			return err
		}
		if !won {
			return s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()})
		}
		st.Line, st.Index = t.Line, t.Index
		st.Chosen[strconv.FormatInt(t.Line, 10)] = t.Index
		st.Done[tileKey(t.Line, t.Index)] = true
		_ = reply
		if q, ok := map[int64]int64{3021: 1, 3022: 2, 3023: 3}[e.Exchange]; ok {
			if opts := p.relicOptions(st, q); len(opts) > 0 {
				st.BuffOpts = opts
				if err := p.pushBuffOptions(s, st, t); err != nil {
					return err
				}
			}
		}
		return s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()})
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
			return reply(1, "获得遗物")
		}
	}
	return reply(0, "请选择遗物")
}
