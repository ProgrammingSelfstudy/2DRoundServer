package game

import (
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func plFormationOfReq(req proto.Message) (formType, hallows int64, slots []pfSlot) {
	for _, e := range req.Arr("ext_list") {
		switch e.Int("type") {
		case plExtFormation:
			formType = e.Int("val1")
		case plExtHallows:
			hallows = e.Int("val1")
		case plExtOwnHero:
			slots = append(slots, pfSlot{Pos: e.Int("val1"), ID: e.Int("val2"), Flag: 0})
		case plExtHiredHero:
			slots = append(slots, pfSlot{Pos: e.Int("val1"), ID: e.Int("val2"), Flag: 1})
		}
	}
	return
}

func plExtOf(req proto.Message, typ int64) (int64, bool) {
	for _, e := range req.Arr("ext_list") {
		if e.Int("type") == typ {
			return e.Int("val1"), true
		}
	}
	return 0, false
}

func (p *plSvc) heroOf(st *plState, flag, pid int64) *pfHero {
	for i := range st.Heroes {
		if st.Heroes[i].Flag == flag && st.Heroes[i].PID == pid {
			return &st.Heroes[i]
		}
	}
	return nil
}

// finish closes the event of a tile; the other tiles of its group of "pick one" tiles disappear.
func (p *plSvc) finish(s *gateway.Session, st *plState, f *plFloor, t *plTile) error {
	e := plCfg.events[t.Evt]
	changed := []*plTile{t}
	t.Down = true
	if e.Group > 0 {
		for i := range f.Tiles {
			o := &f.Tiles[i]
			if o != t && !o.Down && plCfg.events[o.Evt].Group == e.Group {
				o.Down, o.Evt = true, 0
				changed = append(changed, o)
			}
		}
	}
	return p.pushTiles(s, st, changed...)
}

// plKillReward is what a guard pays: beli and hero exp that grow with the role's level.
func plKillReward(role *store.Role, floor int64, elite bool) [][2]int64 {
	beli := 8000 + 400*int64(role.Lev)*floor
	if elite {
		beli *= 2
	}
	return [][2]int64{{1, beli}, {22, beli / 8}}
}

func (p *plSvc) act(s *gateway.Session, req proto.Message) error {
	index, action := req.Int("index"), req.Int("action")
	return p.with(s, func(role *store.Role, st *plState) error {
		if done, err := plOpenFail(s, role, 23104, proto.Message{"code": 0, "index": index, "action": action}); done || err != nil {
			return err
		}
		reply := func(code int64, msg string) error {
			return s.Send(23104, proto.Message{"code": code, "msg": msg, "index": index, "action": action})
		}
		f := st.floorOf(st.Floor)
		if st.Dun == 0 || f == nil {
			return reply(0, "尚未进入副本")
		}
		if index == 0 { // the hire window answers without a tile: it means the one being hired from
			index = st.RentalTile
		}
		t := f.tile(index)
		if t == nil {
			return reply(0, "该位置没有事件")
		}
		cx, cy := plXY(f.Pos)
		x, y := plXY(index)
		if plDistance(cx, cy, x, y) > 3 {
			return reply(0, "距离太远")
		}
		e := plCfg.events[t.Evt]
		if t.Down || t.Evt == 0 {
			return reply(0, "该事件已经完成")
		}
		switch e.Type {
		case plEvtMonster, plEvtGuard:
			return p.fightTile(s, role, st, f, t, e, action, req, reply)
		case plEvtBox:
			scale := int64(1)
			if e.Name == "大宝箱" {
				scale = 3
			}
			prize := [][2]int64{{1, scale * (10000 + 300*int64(role.Lev))}, {22, scale * 2000}}
			if err := p.gain(s, role, prize); err != nil {
				return err
			}
			if err := reply(1, ""); err != nil {
				return err
			}
			return p.finish(s, st, f, t)
		case plEvtStart:
			return reply(1, "")
		case plEvtBoard:
			if err := s.Send(23108, proto.Message{"dun_id": st.Dun, "floor": st.Floor, "index": index, "ext_list": []proto.Message{{"type": 8, "val1": e.extInt(0), "val2": 0}}}); err != nil {
				return err
			}
			return reply(1, "")
		case plEvtDialog:
			if err := s.Send(23108, proto.Message{"dun_id": st.Dun, "floor": st.Floor, "index": index, "ext_list": []proto.Message{{"type": 5, "val1": e.dialogID(), "val2": 0}}}); err != nil {
				return err
			}
			if err := reply(1, ""); err != nil {
				return err
			}
			return p.finish(s, st, f, t)
		case plEvtGoods:
			prize := [][2]int64{{1, 12000 + 500*int64(role.Lev)}, {10040, 2}}
			if err := p.gain(s, role, prize); err != nil {
				return err
			}
			if err := s.Send(23108, proto.Message{"dun_id": st.Dun, "floor": st.Floor, "index": index, "ext_list": []proto.Message{{"type": 9, "val1": 10040, "val2": 0}}}); err != nil {
				return err
			}
			if err := reply(1, ""); err != nil {
				return err
			}
			return p.finish(s, st, f, t)
		case plEvtRecover:
			for i := range st.Heroes {
				if st.Heroes[i].HP > 0 {
					st.Heroes[i].HP = min(100, st.Heroes[i].HP+e.extRow("all_add_hp", 1))
				}
			}
			return p.done(s, st, f, t, reply)
		case plEvtRevive:
			p.revive(st, e)
			return p.done(s, st, f, t, reply)
		case plEvtPortal:
			target := e.extInt(0)
			if target < 1 || target > plFloorCount {
				return reply(0, "传送门无效")
			}
			st.Floor = target
			if err := reply(1, ""); err != nil {
				return err
			}
			return s.Send(23101, p.mapMessage(st))
		case plEvtLease:
			return p.lease(s, role, st, f, t, e, action, req, reply)
		case plEvtBuff:
			return p.relics(s, st, f, t, e, action, req, reply)
		}
		return reply(0, "该事件暂不支持")
	})
}

func (p *plSvc) done(s *gateway.Session, st *plState, f *plFloor, t *plTile, reply func(int64, string) error) error {
	if err := p.pushHP(s, st); err != nil {
		return err
	}
	if err := reply(1, ""); err != nil {
		return err
	}
	return p.finish(s, st, f, t)
}

func (p *plSvc) revive(st *plState, e plEventCfg) {
	var dead []int
	low := -1
	for i, h := range st.Heroes {
		if h.HP <= 0 {
			dead = append(dead, i)
		} else if low < 0 || h.HP < st.Heroes[low].HP {
			low = i
		}
	}
	if len(dead) > 0 {
		st.Heroes[dead[newRng().Intn(len(dead))]].HP = e.extRow("revive", 1)
	} else if low >= 0 {
		st.Heroes[low].HP = e.extRow("revive", 2)
	}
}

// relics: asking shows the three relics of the event's quality (101.. blue, 201.. purple, 301.. orange), choosing takes one.
func (p *plSvc) relics(s *gateway.Session, st *plState, f *plFloor, t *plTile, e plEventCfg, action int64, req proto.Message, reply func(int64, string) error) error {
	if action == 0 {
		if st.BuffTile != t.Index || len(st.BuffOpts) == 0 {
			have := map[int64]bool{}
			for _, b := range st.Buffs {
				have[b] = true
			}
			quality := e.extInt(0) / 100
			var pool []int64
			for k, b := range plCfg.Buffs {
				id, _ := strconv.ParseInt(k, 10, 64)
				if b.Quality == quality && !have[id] {
					pool = append(pool, id)
				}
			}
			sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
			st.BuffOpts, st.BuffTile = nil, t.Index
			for _, i := range newRng().Perm(len(pool)) {
				if len(st.BuffOpts) == 3 {
					break
				}
				st.BuffOpts = append(st.BuffOpts, pool[i])
			}
		}
		list := []proto.Message{}
		for _, b := range st.BuffOpts {
			list = append(list, proto.Message{"type": 7, "val1": b, "val2": 0})
		}
		if err := s.Send(23108, proto.Message{"dun_id": st.Dun, "floor": st.Floor, "index": t.Index, "ext_list": list}); err != nil {
			return err
		}
		return reply(1, "")
	}
	pick, _ := plExtOf(req, plExtBuff)
	for _, b := range st.BuffOpts {
		if b == pick && st.BuffTile == t.Index {
			st.Buffs = append(st.Buffs, b)
			st.BuffOpts = nil
			if err := reply(1, "获得遗物"); err != nil {
				return err
			}
			return p.finish(s, st, f, t)
		}
	}
	return reply(0, "请选择遗物")
}

func (p *plSvc) lease(s *gateway.Session, role *store.Role, st *plState, f *plFloor, t *plTile, e plEventCfg, action int64, req proto.Message, reply func(int64, string) error) error {
	if action == 0 {
		lev, star := int64(1), int64(1)
		for _, h := range st.Heroes {
			lev, star = max(lev, h.Lev), max(star, h.Star)
		}
		var bids []int64
		for bid := range partnerTable {
			if bid >= 10000 && bid < 60000 {
				bids = append(bids, bid)
			}
		}
		sort.Slice(bids, func(i, j int) bool { return bids[i] < bids[j] })
		n := int(e.extRow("num", 1))
		st.Rentals, st.RentalTile = nil, t.Index
		for i, k := range newRng().Perm(len(bids))[:min(max(n, 1), len(bids))] {
			st.Rentals = append(st.Rentals, pfRental{Pos: int64(i + 1), BID: bids[k], Lev: lev, Star: star})
		}
		list := []proto.Message{}
		for _, r := range st.Rentals {
			m := partnerMessage(store.Partner{PartnerID: pfHireBase + r.Pos, RID: role.RID, BID: r.BID, Lev: int(r.Lev), Star: int(r.Star)})
			list = append(list, proto.Message{"pos": r.Pos, "bid": r.BID, "lev": r.Lev, "star": r.Star, "break_skills": []proto.Message{}, "break_lev": m["break_lev"], "power": m["power"],
				"use_skin": 0, "end_time": 0, "atk": m["atk"], "def_p": m["def_p"], "def_s": m["def_s"], "hp": m["hp"], "speed": m["speed"], "hit_rate": m["hit_rate"],
				"dodge_rate": m["dodge_rate"], "crit_rate": m["crit_rate"], "crit_ratio": m["crit_ratio"], "hit_magic": m["hit_magic"], "dodge_magic": m["dodge_magic"],
				"def": m["def"], "eqms": []proto.Message{}, "artifacts": []proto.Message{}, "holy_eqm": []proto.Message{}, "dower_skill": []proto.Message{}})
		}
		if err := s.Send(23107, proto.Message{"load_partner": list}); err != nil {
			return err
		}
		return reply(1, "")
	}
	pos, _ := plExtOf(req, plExtHire)
	for _, r := range st.Rentals {
		if r.Pos == pos && st.RentalTile == t.Index {
			st.NextHire++
			st.Heroes = append(st.Heroes, pfHero{Flag: 1, PID: pfHireBase + st.NextHire, BID: r.BID, Lev: r.Lev, Star: r.Star, HP: 100})
			st.Rentals = nil
			return p.done(s, st, f, t, reply)
		}
	}
	return reply(0, "请选择一位雇佣英雄")
}

// ---- fights

func (p *plSvc) monsterInfo(s *gateway.Session, role *store.Role, st *plState, t *plTile, e plEventCfg) error {
	all, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	best := bestHeroes(all, 5)
	factor := t.Power * 1000 / max(1, rolePower(best))
	guards := []proto.Message{}
	for i, h := range best {
		pp := store.Partner{BID: h.BID, Lev: int(max(1, int64(h.Lev)*factor/1000)), Star: h.Star, BreakLev: h.BreakLev}
		guards = append(guards, proto.Message{"partner_id": i + 1, "pos": i + 1, "bid": h.BID, "lev": pp.Lev, "star": h.Star, "ext_data": []proto.Message{}, "hp_per": 100})
		_ = pp
	}
	quality := map[int64]int64{}
	for _, b := range st.Buffs {
		quality[plCfg.Buffs[strconv.FormatInt(b, 10)].Quality]++
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
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	return s.Send(23109, proto.Message{"power": t.Power, "buffs": buffs, "rid": 0, "srv_id": role.SrvID, "name": e.Name, "face": face, "lev": role.Lev, "guards_power": t.Power,
		"formation_type": 1, "guards": guards, "sprites": []proto.Message{}, "hallows_id": 0, "sprite_lev": 0, "strength": 1000})
}

func (p *plSvc) fightTile(s *gateway.Session, role *store.Role, st *plState, f *plFloor, t *plTile, e plEventCfg, action int64, req proto.Message, reply func(int64, string) error) error {
	if action == 0 {
		if err := p.monsterInfo(s, role, st, t, e); err != nil {
			return err
		}
		return reply(1, "")
	}
	formType, hallows, slots := plFormationOfReq(req)
	if len(slots) == 0 {
		return reply(0, "请选择出战英雄")
	}
	pf, msg, err := planesFight(p.st, p.fights, role, st.Heroes, slots, formType, t.Power, e.Name, []proto.Message{{"param": st.Dun}, {"param": t.Index}}, combatPlanesOld)
	if err != nil {
		return err
	}
	if msg != "" {
		return reply(0, msg)
	}
	st.FormType, st.Hallows, st.Form = pf.formType, hallows, pf.fighters
	if err := reply(1, ""); err != nil {
		return err
	}
	floor, index, fighters, ft, left := st.Floor, t.Index, pf.fighters, pf.formType, pf.left
	return p.fights.begin(s, role, combatPlanesOld, pf.intro, pf.rounds, pf.won, func(s *gateway.Session, won bool) error {
		return p.settleFight(s, floor, index, fighters, ft, left, won)
	})
}

func (p *plSvc) settleFight(s *gateway.Session, floor, index int64, fighters []pfSlot, formType int64, left map[int64]int64, won bool) error {
	return p.with(s, func(role *store.Role, st *plState) error {
		f := st.floorOf(floor)
		if f == nil {
			return nil
		}
		t := f.tile(index)
		if t == nil {
			return nil
		}
		e := plCfg.events[t.Evt]
		for _, sl := range fighters {
			if h := p.heroOf(st, sl.Flag, sl.ID); h != nil {
				h.HP = left[battleCell(formType, sl.Pos)]
			}
		}
		var prize [][2]int64
		if won {
			prize = append(plKillReward(role, floor, t.Evt%1000/100 >= 2), pairsOf(e.Reward)...)
			if err := p.gain(s, role, prize); err != nil {
				return err
			}
		}
		stats := []proto.Message{}
		var dps, best, bestBID, bestLev, bestStar int64
		all, _ := p.st.PartnersOf(role.RID)
		byID := map[int64]store.Partner{}
		for _, h := range all {
			byID[h.PartnerID] = h
		}
		for _, sl := range fighters {
			h := p.heroOf(st, sl.Flag, sl.ID)
			if h == nil {
				continue
			}
			pp := store.Partner{PartnerID: h.PID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
			if own, ok := byID[h.PID]; ok && h.Flag == 0 {
				pp = own
			}
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
		if err := s.Send(23110, proto.Message{"all_dps": dps, "best_partner": bestBID, "bid": bestBID, "lev": bestLev, "star": bestStar, "use_skin": 0, "target_role_name": e.Name,
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "award_list": awards, "evt_index": index}); err != nil {
			return err
		}
		if err := p.pushHP(s, st); err != nil {
			return err
		}
		if !won {
			return nil
		}
		st.Val++
		if st.Max > 0 && st.Val >= st.Max {
			st.CanReward[strconv.FormatInt(st.Dun, 10)] = true
		}
		return p.finish(s, st, f, t)
	})
}
