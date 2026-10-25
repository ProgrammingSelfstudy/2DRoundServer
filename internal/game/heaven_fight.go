package game

import (
	"encoding/json"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// heavenFacts is what the star goals look at in a fight (the client's names).
type heavenFacts struct {
	won        bool
	camps      map[int64]int64
	careers    map[int64]int64
	formType   int64
	rounds     int64
	dead       int64
	hpPerMill  int64 // the survivors' hp, in per mille, on average
	hpSum, hpN int64
}

// goalMet evaluates one star goal (a row of the client's star_cond table) against the facts.
func heavenGoalMet(id int64, f heavenFacts) bool {
	cond, ok := heavenCfg.StarCond[strconv.FormatInt(id, 10)]
	if !ok {
		return false
	}
	for _, raw := range cond.Condition {
		var row []json.RawMessage
		if json.Unmarshal(raw, &row) != nil || len(row) != 3 {
			return false
		}
		var op string
		var want int64
		_ = json.Unmarshal(row[1], &op)
		_ = json.Unmarshal(row[2], &want)
		var have int64
		var key string
		if json.Unmarshal(row[0], &key) == nil { // a plain name
			switch key {
			case "combat_result":
				have = int64(b2i(f.won))
			case "formation_type":
				have = f.formType
			case "combat_order":
				have = f.rounds
			case "dead_num":
				have = f.dead
			case "attk_hp_per":
				have = f.hpPerMill
			default:
				return false
			}
		} else { // [name, value]
			var pair []json.RawMessage
			if json.Unmarshal(row[0], &pair) != nil || len(pair) != 2 {
				return false
			}
			var name string
			var which int64
			_ = json.Unmarshal(pair[0], &name)
			_ = json.Unmarshal(pair[1], &which)
			switch name {
			case "combat_camp_type":
				have = f.camps[which]
			case "career_type":
				have = f.careers[which]
			default:
				return false
			}
		}
		okc := false
		switch op {
		case "==":
			okc = have == want
		case ">=":
			okc = have >= want
		case "=<":
			okc = have <= want
		}
		if !okc {
			return false
		}
	}
	return true
}

// heavenEnemyPower: the monsters are made as strong as the team they face, a little more in the later chapters.
func heavenEnemyPower(own, chapter, order int64) int64 {
	return own * (700 + 15*(chapter-1) + 10*order) / 1000
}

func (h *heavenSvc) challenge(s *gateway.Session, req proto.Message) error {
	ch, order := req.Int("id"), req.Int("order_id")
	return h.open(s, 25205, proto.Message{"code": 0}, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error { return s.Send(25205, proto.Message{"code": 0, "msg": msg}) }
		cu, ok := heavenCfg.custom[hkey(ch, order)]
		switch {
		case !ok || !st.chapterOpen(heavenCfg.Chapters[ch-1]):
			return refuse("关卡未开启")
		case order > 1 && !st.cleared(ch, order-1):
			return refuse("请先通关上一关")
		case h.left(st) <= 0:
			return refuse("挑战次数不足")
		}
		forms := st.Forms[strconv.FormatInt(int64(len(cu.Units)), 10)]
		if len(forms) == 0 {
			return refuse("请先布阵")
		}
		all, err := h.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		own := rolePower(bestHeroes(all, 5))
		var roster []pfHero
		for _, p := range all {
			roster = append(roster, pfHero{PID: p.PartnerID, BID: p.BID, Lev: int64(p.Lev), Star: int64(p.Star), HP: 100})
		}
		// make the fight of the first team and the rest of the teams' fights at once, then play them one after the other
		type team struct {
			plan  *planesFightPlan
			slots []pfSlot
		}
		var teams []team
		for i := range cu.Units {
			if i >= len(forms) || len(forms[i].Pos) == 0 {
				continue
			}
			var slots []pfSlot
			for _, p := range forms[i].Pos {
				slots = append(slots, pfSlot{Pos: p.Pos, ID: p.ID})
			}
			plan, msg, err := planesFight(h.st, h.fights, role, roster, slots, forms[i].FormType, heavenEnemyPower(own, ch, order), "天界守卫", []proto.Message{{"param": ch}, {"param": order}}, combatHeaven)
			if err != nil {
				return err
			}
			if msg != "" {
				return refuse(msg)
			}
			teams = append(teams, team{plan, slots})
		}
		if len(teams) == 0 {
			return refuse("请先布阵")
		}
		if err := s.Send(25205, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		var play func(i int, facts heavenFacts) error
		play = func(i int, facts heavenFacts) error {
			t := teams[i]
			return h.fights.begin(s, role, combatHeaven, t.plan.intro, t.plan.rounds, t.plan.won, func(s *gateway.Session, won bool) error {
				facts.rounds = max(facts.rounds, int64(len(t.plan.rounds)))
				for _, sl := range t.slots {
					hp := t.plan.left[battleCell(t.plan.formType, sl.Pos)]
					for _, p := range all {
						if p.PartnerID == sl.ID {
							facts.camps[int64(partnerTable[p.BID].Camp)]++
							facts.careers[int64(partnerTable[p.BID].Type)]++
							if hp == 0 {
								facts.dead++
							}
						}
					}
					if hp := t.plan.left[battleCell(t.plan.formType, sl.Pos)]; hp > 0 {
						facts.hpSum += hp
						facts.hpN++
					}
				}
				if i == 0 {
					facts.formType = t.plan.formType
				}
				if !won {
					return h.settle(s, ch, order, facts, false)
				}
				if i+1 < len(teams) {
					return play(i+1, facts)
				}
				return h.settle(s, ch, order, facts, true)
			})
		}
		return play(0, heavenFacts{camps: map[int64]int64{}, careers: map[int64]int64{}})
	})
}

func (h *heavenSvc) settle(s *gateway.Session, ch, order int64, facts heavenFacts, won bool) error {
	return h.with(s, func(role *store.Role, st *heavenState) error {
		cu := heavenCfg.custom[hkey(ch, order)]
		facts.won = won
		if facts.hpN > 0 {
			facts.hpPerMill = facts.hpSum * 10 / facts.hpN
		}
		var award [][]int64
		stars := []proto.Message{}
		key := hkey(ch, order)
		mask := st.Stars[key]
		if won {
			first := mask&1 == 0
			for i, c := range cu.Cond {
				if len(c) == 2 && heavenGoalMet(c[1], facts) {
					mask |= 1 << uint(i)
				}
			}
			st.Stars[key] = mask
			st.Used++
			st.MaxDun = max(st.MaxDun, cu.DunID)
			award = append(award, cu.Auto...)
			if first {
				award = append(award, cu.First...)
			}
			if err := h.gain(s, role, award); err != nil {
				return err
			}
		}
		for i, c := range cu.Cond {
			state := int64(0)
			if won && heavenGoalMet(c[1], facts) {
				state = 1
			}
			stars = append(stars, proto.Message{"id": c[0], "state": state})
			_ = i
		}
		items := []proto.Message{}
		for _, a := range award {
			items = append(items, proto.Message{"item_id": a[0], "num": a[1]})
		}
		result := int64(2)
		if won {
			result = 1
		}
		if err := s.Send(25206, proto.Message{"result": result, "id": ch, "order_id": order, "star_info": stars, "award": items, "all_hurt_statistics": []proto.Message{}}); err != nil {
			return err
		}
		return h.push(s, st, ch)
	})
}

// sweep (25208): a custom with its three stars; it costs a challenge and pays the repeat reward.
func (h *heavenSvc) sweep(s *gateway.Session, req proto.Message) error {
	ch, order := req.Int("id"), req.Int("order_id")
	return h.open(s, 25208, proto.Message{"code": 0, "count": 0}, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error {
			return s.Send(25208, proto.Message{"code": 0, "msg": msg, "count": h.left(st)})
		}
		cu, ok := heavenCfg.custom[hkey(ch, order)]
		switch {
		case !ok || st.Stars[hkey(ch, order)] != 7:
			return refuse("三星通关后才能扫荡")
		case h.left(st) <= 0:
			return refuse("挑战次数不足")
		}
		st.Used++
		if err := h.gain(s, role, cu.Auto); err != nil {
			return err
		}
		if err := s.Send(25208, proto.Message{"code": 1, "msg": "扫荡成功", "count": h.left(st)}); err != nil {
			return err
		}
		return h.push(s, st, ch)
	})
}

// ---- the dial

func (h *heavenSvc) dialOf(st *heavenState, g int64, now int64) *heavenDial {
	k := strconv.FormatInt(g, 10)
	d := st.Dial[k]
	if d == nil {
		d = &heavenDial{Awards: map[string]bool{}, Lucky: map[string]int64{}}
		st.Dial[k] = d
	}
	if d.Awards == nil {
		d.Awards = map[string]bool{}
	}
	if d.Lucky == nil {
		d.Lucky = map[string]int64{}
	}
	if day := now; d.DayKey != day {
		d.DayKey, d.DayCount = day, 0
	}
	return d
}

func (h *heavenSvc) group(id int64) (heavenGroup, bool) {
	for _, g := range heavenCfg.Dial.Groups {
		if g.GroupID == id {
			return g, true
		}
	}
	return heavenGroup{}, false
}

// dialOpen: the second ship wants level 120 and holy equipment of 10000 score, which nothing here gives.
func (h *heavenSvc) dialOpen(role *store.Role, g heavenGroup) bool {
	if g.GroupID == 1 {
		return int64(role.Lev) >= heavenCfg.num("open_lev")
	}
	return false
}

func (h *heavenSvc) dialInfo(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, st *heavenState) error {
		list := []proto.Message{}
		for _, g := range heavenCfg.Dial.Groups {
			d := h.dialOf(st, g.GroupID, dayKey(s.Now()))
			free := int64(0)
			if d.FreeDay != dayKey(s.Now()) {
				free = g.FreeTimes
			}
			lucky := []proto.Message{}
			for _, pos := range sortedKeys(d.Lucky) {
				lucky = append(lucky, proto.Message{"pos": pos, "lucky_holy_eqm": d.Lucky[strconv.FormatInt(pos, 10)]})
			}
			awards := []proto.Message{}
			for _, a := range heavenCfg.Dial.Awards[g.GroupID-1] {
				if d.Awards[strconv.FormatInt(a.ID, 10)] {
					awards = append(awards, proto.Message{"award_id": a.ID})
				}
			}
			next := dayStart(s.Now()).Add(24 * 3600e9).Unix()
			list = append(list, proto.Message{"group_id": g.GroupID, "free_times": free, "reset_time": next, "baodi_count": d.Baodi, "lucky_holy_eqm": lucky, "all_award_count": d.Pulls,
				"do_awards": awards, "day_count": d.DayCount, "is_open": b2i(h.dialOpen(role, g))})
		}
		return s.Send(25219, proto.Message{"recruit_list": list})
	})
}

func sortedKeys(m map[string]int64) []int64 {
	var out []int64
	for k := range m {
		n, _ := strconv.ParseInt(k, 10, 64)
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// pullOnce draws one reward from the odds the group's text lists (fragments, ore, equipment of 1-5 stars and three
// grades). Every 20th pull gives equipment of the good grade at least (the client's rule text).
func (h *heavenSvc) pullOnce(g heavenGroup, d *heavenDial, rng interface{ Intn(int) int }) (int64, int64, bool) {
	var total int64
	for _, o := range g.Odds {
		var p int64
		_ = json.Unmarshal(o[1], &p)
		total += p
	}
	force := d.Baodi+1 >= 20
	for try := 0; try < 50; try++ {
		n := int64(rng.Intn(int(total)))
		for _, o := range g.Odds {
			var p int64
			var name string
			_ = json.Unmarshal(o[1], &p)
			_ = json.Unmarshal(o[0], &name)
			if n >= p {
				n -= p
				continue
			}
			good := len([]rune(name)) >= 3 && ([]rune(name)[2] == '良' || []rune(name)[2] == '极')
			if force && !good {
				break
			}
			switch {
			case name == "神装碎片":
				return 28, 10, false
			case name == "空岛原石":
				return 10030, 1, false
			default:
				equips := []int64{}
				for _, a := range g.Award {
					if len(a) > 0 && a[0] > 9000000 {
						equips = append(equips, a[0])
					}
				}
				if len(equips) == 0 {
					return 28, 10, false
				}
				return equips[rng.Intn(len(equips))], 1, true
			}
		}
	}
	return 28, 10, false
}

// pull (25217): times 1 or 10; recruit_type 1 the day's free pull, 2 the ticket, 3 gems.
func (h *heavenSvc) pull(s *gateway.Session, req proto.Message) error {
	gid, times, way := req.Int("group_id"), req.Int("times"), req.Int("recruit_type")
	return h.open(s, 25217, proto.Message{"group_id": gid, "times": times, "recruit_type": way, "rewards": []proto.Message{}}, func(role *store.Role, st *heavenState) error {
		g, ok := h.group(gid)
		refuse := func(msg string) error {
			return s.Send(25217, proto.Message{"group_id": gid, "times": times, "recruit_type": way, "rewards": []proto.Message{}, "msg": msg})
		}
		if !ok || !h.dialOpen(role, g) || (times != 1 && times != 10) {
			return refuse("无法维修")
		}
		now := dayKey(s.Now())
		d := h.dialOf(st, gid, now)
		if d.DayCount+times > g.DayLimit {
			return refuse("今日维修次数已达上限")
		}
		switch {
		case way == 1:
			if times != 1 || d.FreeDay == now {
				return refuse("今日免费维修已使用")
			}
			d.FreeDay = now
		case way == 2:
			cost := g.ItemOnce
			if times == 10 {
				cost = g.ItemTen
			}
			paid, err := h.pay(s, role, cost, 1)
			if err != nil {
				return err
			}
			if !paid {
				return refuse("道具不足")
			}
		default:
			gold := g.GoldOnce
			if times == 10 {
				gold = g.GoldTen
			}
			paid, err := h.pay(s, role, [][]int64{{3, gold}}, 1)
			if err != nil {
				return err
			}
			if !paid {
				return refuse("钻石不足")
			}
		}
		rng := newRng()
		var prize [][]int64
		rewards := []proto.Message{}
		for i := int64(0); i < times; i++ {
			bid, num, equip := h.pullOnce(g, d, rng)
			if equip {
				d.Baodi = 0
			} else {
				d.Baodi++
			}
			prize = append(prize, []int64{bid, num})
			rewards = append(rewards, proto.Message{"id": i + 1, "base_id": bid, "num": num})
			if equip {
				st.Log = append(st.Log, heavenLog{Name: role.Name, Bid: bid, Num: num, Group: gid})
				if len(st.Log) > 50 {
					st.Log = st.Log[1:]
				}
			}
		}
		prize = append(prize, g.GainOnce...)
		for _, p := range g.GainOnce {
			_ = p
		}
		d.Pulls += times
		d.DayCount += times
		if err := h.gain(s, role, prize); err != nil {
			return err
		}
		return s.Send(25217, proto.Message{"group_id": gid, "times": times, "recruit_type": way, "rewards": rewards})
	})
}

func (h *heavenSvc) log(s *gateway.Session, req proto.Message) error {
	t, gid := req.Int("type"), req.Int("group_id")
	return h.with(s, func(role *store.Role, st *heavenState) error {
		list := []proto.Message{}
		for _, l := range st.Log {
			if l.Group == gid || gid == 0 {
				list = append(list, proto.Message{"role_name": l.Name, "bid": l.Bid, "num": l.Num})
			}
		}
		return s.Send(25218, proto.Message{"type": t, "group_id": gid, "log_list": list})
	})
}

func (h *heavenSvc) setLucky(s *gateway.Session, req proto.Message) error {
	gid := req.Int("group_id")
	return h.open(s, 25230, proto.Message{"flag": 0}, func(role *store.Role, st *heavenState) error {
		g, ok := h.group(gid)
		if !ok {
			return s.Send(25230, proto.Message{"flag": 0, "msg": "心愿水晶不存在"})
		}
		d := h.dialOf(st, gid, dayKey(s.Now()))
		allowed := map[int64]bool{}
		for _, id := range g.CanSetIDs {
			allowed[id] = true
		}
		next := map[string]int64{}
		for _, l := range req.Arr("lucky_holy_eqm") {
			id := l.Int("lucky_holy_eqm")
			if id == 0 {
				continue
			}
			if !allowed[id] || int64(len(next)) >= g.CanSetNum {
				return s.Send(25230, proto.Message{"flag": 0, "msg": "无法放入该神装"})
			}
			next[strconv.FormatInt(l.Int("pos"), 10)] = id
		}
		d.Lucky = next
		return s.Send(25230, proto.Message{"flag": 1, "msg": "设置成功"})
	})
}

func (h *heavenSvc) dialAward(s *gateway.Session, req proto.Message) error {
	gid, id := req.Int("group_id"), req.Int("id")
	return h.open(s, 25231, proto.Message{"flag": 0}, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error { return s.Send(25231, proto.Message{"flag": 0, "msg": msg}) }
		if gid < 1 || int(gid) > len(heavenCfg.Dial.Awards) {
			return refuse("奖励不存在")
		}
		d := h.dialOf(st, gid, dayKey(s.Now()))
		for _, a := range heavenCfg.Dial.Awards[gid-1] {
			if a.ID != id {
				continue
			}
			k := strconv.FormatInt(id, 10)
			switch {
			case d.Awards[k]:
				return refuse("已经领取")
			case d.Pulls < a.Times:
				return refuse("维修次数不足")
			}
			d.Awards[k] = true
			if err := h.gain(s, role, a.Reward); err != nil {
				return err
			}
			return s.Send(25231, proto.Message{"flag": 1, "msg": "领取成功"})
		}
		return refuse("奖励不存在")
	})
}
