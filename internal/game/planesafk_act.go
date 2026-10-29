package game

import (
	"math/rand"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// ---- heroes and the formation

func (p *pfSvc) heroOf(st *pfState, flag, pid int64) *pfHero {
	for i := range st.Heroes {
		if st.Heroes[i].Flag == flag && st.Heroes[i].PID == pid {
			return &st.Heroes[i]
		}
	}
	return nil
}

func (p *pfSvc) partnerOf(role *store.Role, h pfHero) (store.Partner, bool) {
	if h.Flag == 1 {
		return store.Partner{PartnerID: h.PID, RID: role.RID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}, true
	}
	all, err := p.st.PartnersOf(role.RID)
	if err != nil {
		return store.Partner{}, false
	}
	for _, x := range all {
		if x.PartnerID == h.PID {
			return x, true
		}
	}
	return store.Partner{}, false
}

func (p *pfSvc) roster(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		return p.sendRoster(s, role, st)
	})
}

// sendRoster sends 28613: every hero of the run, the hired ones with flag 1. The original gives each `ext_data [{key 5, val 0}]`.
func (p *pfSvc) sendRoster(s *gateway.Session, role *store.Role, st *pfState) error {
	list := []proto.Message{}
	for _, h := range st.Heroes {
		pp, ok := p.partnerOf(role, h)
		if !ok {
			continue
		}
		list = append(list, proto.Message{"flag": h.Flag, "partner_id": h.PID, "bid": h.BID, "lev": h.Lev, "star": h.Star, "power": partnerMessage(pp).Int("power"),
			"hp_per": h.HP, "ext_data": []proto.Message{{"key": 5, "val": 0}}})
	}
	return s.Send(28613, proto.Message{"partners": list})
}

func (p *pfSvc) pushHP(s *gateway.Session, st *pfState) error {
	list := []proto.Message{}
	for _, h := range st.Heroes {
		list = append(list, proto.Message{"flag": h.Flag, "partner_id": h.PID, "hp_per": h.HP})
	}
	return s.Send(28609, proto.Message{"partners": list})
}

func (p *pfSvc) formPower(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		var total int64
		for _, e := range req.Arr("partner_ids") {
			if h := p.heroOf(st, e.Int("flag"), e.Int("id")); h != nil {
				if pp, ok := p.partnerOf(role, *h); ok {
					total += partnerMessage(pp).Int("power")
				}
			}
		}
		return s.Send(28610, proto.Message{"power": total})
	})
}

func (p *pfSvc) saveForm(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		var form []pfSlot
		seen := map[int64]bool{}
		for _, e := range req.Arr("pos_info") {
			slot := pfSlot{Pos: e.Int("pos"), ID: e.Int("id"), Flag: e.Int("flag")}
			if p.heroOf(st, slot.Flag, slot.ID) == nil || seen[slot.ID] || len(form) == formMaxHeroes {
				continue
			}
			seen[slot.ID] = true
			form = append(form, slot)
		}
		if len(form) == 0 {
			return s.Send(28611, proto.Message{"flag": 0, "msg": "请选择出战英雄"})
		}
		st.Form, st.FormType, st.Hallows = form, req.Int("formation_type"), req.Int("hallows_id")
		return s.Send(28611, proto.Message{"flag": 1, "msg": ""})
	})
}

func (p *pfSvc) getForm(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		form, ft := st.Form, st.FormType
		if len(form) == 0 {
			ft = 1
			for i, h := range st.Heroes {
				if i == 5 {
					break
				}
				form = append(form, pfSlot{Pos: int64(i + 1), ID: h.PID, Flag: h.Flag})
			}
		}
		out := []proto.Message{}
		for _, e := range form {
			out = append(out, proto.Message{"pos": e.Pos, "id": e.ID, "flag": e.Flag})
		}
		return s.Send(28612, proto.Message{"formation_type": ft, "pos_info": out, "hallows_id": st.Hallows})
	})
}

func (p *pfSvc) buffList(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		out := []proto.Message{}
		for _, b := range st.Buffs {
			out = append(out, proto.Message{"buff_id": b})
		}
		return s.Send(28620, proto.Message{"buffs": out})
	})
}

func (p *pfSvc) lootList(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error { return p.lootOf(s, st) })
}

// lootOf sends the totals of everything the round gave so far (28625).
func (p *pfSvc) lootOf(s *gateway.Session, st *pfState) error {
	sum := map[int64]int64{}
	var ids []int64
	for _, e := range st.Loot {
		if sum[e[0]] == 0 {
			ids = append(ids, e[0])
		}
		sum[e[0]] += e[1]
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []proto.Message{}
	for _, id := range ids {
		out = append(out, proto.Message{"base_id": id, "num": sum[id]})
	}
	return s.Send(28625, proto.Message{"item_list": out})
}

// rentalHeroes makes the heroes a hire event offers: strong heroes of other roles are what the original shows; we
// have no such pool, so they are known heroes at the level and star of the role's best.
func (p *pfSvc) rentalHeroes(role *store.Role, st *pfState, n int, rng *rand.Rand) {
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
	st.Rentals = nil
	for i, k := range rng.Perm(len(bids))[:min(n, len(bids))] {
		st.Rentals = append(st.Rentals, pfRental{Pos: int64(i + 1), BID: bids[k], Lev: lev, Star: star})
	}
}

func (p *pfSvc) rentalInfo(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		pos := req.Int("pos")
		for _, r := range st.Rentals {
			if r.Pos == pos {
				m := partnerMessage(store.Partner{PartnerID: pfHireBase + pos, RID: role.RID, BID: r.BID, Lev: int(r.Lev), Star: int(r.Star)})
				out := proto.Message{"pos": pos, "holy_eqm": []proto.Message{}}
				for _, k := range []string{"bid", "lev", "star", "skills", "break_lev", "power", "use_skin", "end_time", "atk", "def_p", "def_s", "hp", "speed", "hit_rate",
					"dodge_rate", "crit_rate", "crit_ratio", "hit_magic", "dodge_magic", "def", "eqms", "artifacts", "dower_skill"} {
					out[k] = m[k]
				}
				return s.Send(28623, out)
			}
		}
		return nil
	})
}

// ---- the war order (征战之证)

func (p *pfSvc) orderLevels() []pfOrderLevel {
	if len(pfCfg.Order.Levels) == 0 {
		return nil
	}
	return pfCfg.Order.Levels[0]
}

func (p *pfSvc) orderLev(st *pfState) int64 {
	lev := int64(0)
	for _, l := range p.orderLevels() {
		if st.Points >= l.WinCount {
			lev = max(lev, l.Lev)
		}
	}
	return lev
}

func (p *pfSvc) orderInfo(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		return s.Send(28616, p.orderMessage(s, st))
	})
}

func (p *pfSvc) orderMessage(s *gateway.Session, st *pfState) proto.Message {
	days := int64(44)
	if len(pfCfg.Order.Days) > 0 {
		days = pfCfg.Order.Days[0].PeriodDay
	}
	list := []proto.Message{}
	for _, l := range p.orderLevels() {
		list = append(list, proto.Message{"id": l.Lev, "award_status": b2i(st.OrderTaken[strconv.FormatInt(l.Lev, 10)]), "rmb_award_status": 0})
	}
	return proto.Message{"period": 1, "cur_day": (s.Now().Unix()-st.OrderStart)/86400 + 1, "end_time": st.OrderStart + days*86400,
		"lev": p.orderLev(st), "rmb_status": 0, "win_count": st.Points, "list": list}
}

func (p *pfSvc) orderTake(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return p.with(s, func(role *store.Role, st *pfState) error {
		key := strconv.FormatInt(id, 10)
		for _, l := range p.orderLevels() {
			if l.Lev != id {
				continue
			}
			if st.OrderTaken[key] || st.Points < l.WinCount {
				return s.Send(28617, proto.Message{"flag": 0, "msg": "无法领取"})
			}
			st.OrderTaken[key] = true
			if err := p.gain(s, role, pairsOf(l.Reward)); err != nil {
				return err
			}
			return s.Send(28617, proto.Message{"flag": 1, "msg": "领取成功"})
		}
		return s.Send(28617, proto.Message{"flag": 0, "msg": "无法领取"})
	})
}

func (p *pfSvc) orderRed(s *gateway.Session, _ proto.Message) error {
	return s.Send(28618, proto.Message{"flag": 0})
}

func (p *pfSvc) orderPop(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(role *store.Role, st *pfState) error {
		return s.Send(28619, proto.Message{"is_pop": 0, "cur_day": (s.Now().Unix()-st.OrderStart)/86400 + 1})
	})
}

// ---- the events

func extOf3(req proto.Message) map[int64][2]int64 {
	out := map[int64][2]int64{}
	for _, e := range req.Arr("ext_list") {
		out[e.Int("type")] = [2]int64{e.Int("val1"), e.Int("val2")}
	}
	return out
}

// bossPoints are the war order points of a boss: 100, 250, 400 for the three floors (the client's rule text).
var pfBossPoints = map[int64]int64{1: 100, 2: 250, 3: 400}

func (p *pfSvc) act(s *gateway.Session, req proto.Message) error {
	line, index, action := req.Int("line"), req.Int("index"), req.Int("action")
	return p.open(s, 28600, proto.Message{"code": 0, "line": line, "index": index, "action": action}, func(role *store.Role, st *pfState) error {
		reply := func(code int64, msg string) error {
			return s.Send(28600, proto.Message{"code": code, "msg": msg, "line": line, "index": index, "action": action})
		}
		t := st.tile(line, index)
		if st.Floor == 0 || t == nil {
			return reply(0, "该格子不存在")
		}
		pending := len(st.BuffOpts) > 0 && t.Evt >= 3021 && t.Evt <= 3023 // a won fight left a relic on this tile
		reachable := line == st.Line+1 && (index == st.Index+1 || index == st.Index-1 || line == pfBossLine)
		if !reachable {
			return reply(0, "无法到达该格子")
		}
		if c, ok := st.Chosen[strconv.FormatInt(line, 10)]; ok && c != index {
			return reply(0, "已经选择了本层的其他格子")
		}
		if st.Done[tileKey(line, index)] {
			return reply(0, "该格子已经完成")
		}
		ext := extOf3(req)
		if pending {
			return p.chooseBuff(s, role, st, t, action, ext, reply)
		}
		e := pfCfg.events[t.Evt]
		switch e.Type {
		case pfEvtMonster, pfEvtGuard:
			return p.fightTile(s, role, st, t, e, action, req, reply)
		case pfEvtRecover:
			before := p.hpOf(st)
			for i := range st.Heroes {
				if st.Heroes[i].HP > 0 {
					st.Heroes[i].HP = min(100, st.Heroes[i].HP+e.extNum("all_add_hp", 1))
				}
			}
			// the original tells it first and lists only the heroes whose hp changed (an empty list when all were whole)
			if err := s.Send(12741, proto.Message{"msg": "使用成功，泉水从眼前消失..."}); err != nil {
				return err
			}
			return p.step(s, st, t, reply, p.hpChanged(st, before))
		case pfEvtRevive:
			before := p.hpOf(st)
			p.revive(st, e)
			// the notice comes first, as with the fountain (capture original-2026-10-27, 21-planes-afk-2)
			if err := s.Send(12741, proto.Message{"msg": "使用成功，十字架从眼前消失..."}); err != nil {
				return err
			}
			return p.step(s, st, t, reply, p.hpChanged(st, before))
		case pfEvtLease:
			return p.lease(s, role, st, t, action, ext, reply)
		case pfEvtBusinessman:
			return p.shop(s, role, st, t, action, ext, reply)
		}
		return p.step(s, st, t, reply, nil)
	})
}

func (p *pfSvc) hpOf(st *pfState) map[int64]int64 {
	out := map[int64]int64{}
	for _, h := range st.Heroes {
		out[h.PID] = h.HP
	}
	return out
}

// hpChanged lists the heroes whose hp differs from before: what 28609 carries after a fountain.
func (p *pfSvc) hpChanged(st *pfState, before map[int64]int64) []pfHero {
	out := []pfHero{}
	for _, h := range st.Heroes {
		if was, ok := before[h.PID]; !ok || was != h.HP {
			out = append(out, h)
		}
	}
	return out
}

// step moves the role onto the tile, closes its event and tells the client: the hp of the heroes in changed (28609, nil sends
// nothing), then the tiles, then the answer last (capture original-2026-10-27, 20-planes-afk).
func (p *pfSvc) step(s *gateway.Session, st *pfState, t *pfTile, reply func(int64, string) error, changed []pfHero) error {
	st.Line, st.Index = t.Line, t.Index
	st.Chosen[strconv.FormatInt(t.Line, 10)] = t.Index
	st.Done[tileKey(t.Line, t.Index)] = true
	if changed != nil {
		list := []proto.Message{}
		for _, h := range changed {
			list = append(list, proto.Message{"flag": h.Flag, "partner_id": h.PID, "hp_per": h.HP})
		}
		if err := s.Send(28609, proto.Message{"partners": list}); err != nil {
			return err
		}
	}
	if err := s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()}); err != nil {
		return err
	}
	return reply(1, "")
}

func (p *pfSvc) revive(st *pfState, e pfEventCfg) {
	var dead []int
	low := -1
	for i, h := range st.Heroes {
		if h.HP <= 0 {
			dead = append(dead, i)
		} else if low < 0 || h.HP < st.Heroes[low].HP {
			low = i
		}
	}
	if len(dead) > 0 { // a fallen hero comes back with part of its hp
		st.Heroes[dead[newRng().Intn(len(dead))]].HP = e.extNum("revive", 1)
	} else if low >= 0 { // nobody fell: the weakest one is made whole
		st.Heroes[low].HP = e.extNum("revive", 2)
	}
}

func (p *pfSvc) lease(s *gateway.Session, role *store.Role, st *pfState, t *pfTile, action int64, ext map[int64][2]int64, reply func(int64, string) error) error {
	if action == 0 {
		n := int(pfCfg.events[t.Evt].extNum("num", 1))
		p.rentalHeroes(role, st, max(n, 1), newRng())
		list := []proto.Message{}
		for _, r := range st.Rentals {
			m := partnerMessage(store.Partner{PartnerID: pfHireBase + r.Pos, RID: role.RID, BID: r.BID, Lev: int(r.Lev), Star: int(r.Star)})
			list = append(list, proto.Message{"pos": r.Pos, "bid": r.BID, "lev": r.Lev, "star": r.Star, "break_skills": []proto.Message{}, "break_lev": m["break_lev"], "power": m["power"],
				"use_skin": 0, "end_time": 0, "atk": m["atk"], "def_p": m["def_p"], "def_s": m["def_s"], "hp": m["hp"], "speed": m["speed"], "hit_rate": m["hit_rate"],
				"dodge_rate": m["dodge_rate"], "crit_rate": m["crit_rate"], "crit_ratio": m["crit_ratio"], "hit_magic": m["hit_magic"], "dodge_magic": m["dodge_magic"],
				"def": m["def"], "eqms": []proto.Message{}, "artifacts": []proto.Message{}, "holy_eqm": []proto.Message{}, "dower_skill": []proto.Message{}})
		}
		if err := s.Send(28622, proto.Message{"line": t.Line, "index": t.Index, "planes_load_partner": list}); err != nil {
			return err
		}
		return reply(1, "")
	}
	pos := ext[pfExtHire][0]
	for _, r := range st.Rentals {
		if r.Pos == pos {
			st.NextHire++
			st.Heroes = append(st.Heroes, pfHero{Flag: 1, PID: pfHireBase + st.NextHire, BID: r.BID, Lev: r.Lev, Star: r.Star, HP: 100})
			st.Rentals = nil
			// the original: the notice, the moved role, the whole roster and then the answer (capture original-2026-10-27)
			st.Line, st.Index = t.Line, t.Index
			st.Chosen[strconv.FormatInt(t.Line, 10)] = t.Index
			st.Done[tileKey(t.Line, t.Index)] = true
			if err := s.Send(12741, proto.Message{"msg": "<div fontcolor=#fff000></div>加入我方队伍，可在英雄列表查看！"}); err != nil {
				return err
			}
			if err := s.Send(28606, proto.Message{"floor": st.Floor, "line": st.Line, "index": st.Index, "tile_list": st.tileList()}); err != nil {
				return err
			}
			if err := p.sendRoster(s, role, st); err != nil {
				return err
			}
			return reply(1, "")
		}
	}
	return reply(0, "请选择一位雇佣英雄")
}

func (p *pfSvc) goodsMessage(st *pfState, line, index int64) proto.Message {
	items := []proto.Message{}
	for i := len(st.Goods) - 1; i >= 0; i-- { // the original lists the last position first
		g := st.Goods[i]
		items = append(items, proto.Message{"pos": g.Pos, "id": g.ID, "is_flag": b2i(g.Bought)})
	}
	return proto.Message{"item_list": items, "is_select": b2i(st.ShopSelected), "line": line, "index": index}
}

// shop: a traveling merchant. Asking shows the greeting (is_select 0); going in (6) shows four goods and hides the line's other tiles; buying (7) takes
// the price in gems; leaving (8) ends the event.
func (p *pfSvc) shop(s *gateway.Session, role *store.Role, st *pfState, t *pfTile, action int64, ext map[int64][2]int64, reply func(int64, string) error) error {
	if !st.ShopOpen {
		st.ShopOpen, st.ShopSelected = true, false
		rng := newRng()
		st.Goods = nil
		for i, k := range rng.Perm(len(pfCfg.Shop))[:4] {
			st.Goods = append(st.Goods, pfGood{Pos: int64(i + 1), ID: pfCfg.Shop[k].ID})
		}
	}
	switch {
	case action == 0:
		if err := s.Send(28621, p.goodsMessage(st, t.Line, t.Index)); err != nil {
			return err
		}
		return reply(1, "")
	case hasExt(ext, pfExtShopEnter):
		st.ShopSelected = true
		if err := s.Send(28621, p.goodsMessage(st, t.Line, t.Index)); err != nil {
			return err
		}
		if err := p.hideLine(s, st, t); err != nil {
			return err
		}
		return reply(1, "")
	case hasExt(ext, pfExtShopBuy):
		pos := ext[pfExtShopBuy][0]
		for i := range st.Goods {
			g := &st.Goods[i]
			if g.Pos != pos {
				continue
			}
			if g.Bought {
				return reply(0, "该物品已被购买")
			}
			var cfg pfGoodCfg
			for _, c := range pfCfg.Shop {
				if c.ID == g.ID {
					cfg = c
				}
			}
			if ok, err := p.pay(s, role, [][2]int64{{3, cfg.Price}}); err != nil {
				return err
			} else if !ok {
				return reply(0, "钻石不足")
			}
			g.Bought = true
			if err := p.gain(s, role, [][2]int64{{cfg.BaseID, cfg.Num}}); err != nil {
				return err
			}
			if err := s.Send(28621, p.goodsMessage(st, t.Line, t.Index)); err != nil {
				return err
			}
			return reply(1, "")
		}
		return reply(0, "该物品不存在")
	case hasExt(ext, pfExtShopLeave):
		st.ShopOpen, st.ShopSelected, st.Goods = false, false, nil
		return p.step(s, st, t, reply, nil)
	}
	return reply(0, "无效的操作")
}

func hasExt(ext map[int64][2]int64, typ int64) bool { _, ok := ext[typ]; return ok }
