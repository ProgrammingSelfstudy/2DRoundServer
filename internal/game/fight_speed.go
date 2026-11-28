package game

import (
	"slices"

	"2droundserver/internal/proto"
)

// The original join rules stack individual instances, refresh a group or select
// its highest rank. Different groups contribute independently.
func speedOf(u *fightUnit) int64 {
	total := speedBonus{Per: u.marineAura}
	strongest := map[int64]*activeBuff{}
	var fallbackSlow int64
	for _, b := range u.buffs {
		if b.remain <= 0 {
			continue
		}
		n, ok := initiative.Buffs[b.bid]
		if !ok || n.Stats == (speedBonus{}) {
			switch b.kind {
			case "speed_up":
				total.Per += 10 * b.pct
			case "speed_down":
				fallbackSlow = max(fallbackSlow, 10*b.pct)
			}
			continue
		}
		if b.bid == 54345 && u.sex != 1 {
			continue
		}
		if n.Type == 3 {
			old := strongest[n.Group]
			if old == nil || n.Rank > initiative.Buffs[old.bid].Rank || n.Rank == initiative.Buffs[old.bid].Rank && b.speedFlat > old.speedFlat {
				strongest[n.Group] = b
			}
			continue
		}
		total = total.plus(n.Stats)
		total.Flat += b.speedFlat
	}
	for group, b := range strongest {
		n := initiative.Buffs[b.bid]
		if group == 230 {
			fallbackSlow = max(fallbackSlow, -n.Stats.Per)
			continue
		}
		total = total.plus(n.Stats)
		total.Flat += b.speedFlat
	}
	total.Per -= fallbackSlow
	return total.apply(u.speed, 0)
}

func hasStatusGroup(u *fightUnit, group int64) bool {
	for _, b := range u.buffs {
		if b.remain > 0 && initiative.Buffs[b.bid].Group == group {
			return true
		}
	}
	return false
}

func (f *fightSim) injured(side bool) bool {
	for i := range f.units {
		u := &f.units[i]
		if u.ours == side && u.hp > 0 && u.hp < u.maxHP {
			return true
		}
	}
	return false
}

func familySkill(u *fightUnit, group int64) int64 {
	for _, id := range u.skills {
		if id/10 == group {
			return id
		}
	}
	return 0
}

func speedRule(bid int64) buffRule {
	n := initiative.Buffs[bid]
	r := buffRule{Bid: bid, Kind: "other", Rounds: max(1, n.Rounds), Chance: 100, Keep: n.Keep, Negative: n.Negative, Stack: n.Type == 1}
	if n.Stats != (speedBonus{}) {
		r.Kind = "speed_up"
		if n.Stats.Per < 0 {
			r.Kind = "speed_down"
		}
	}
	return r
}

func speedEffect(caster, target *fightUnit, sid int64, entries []proto.Message) proto.Message {
	e := roundStatusEffect(target, 0, 0, entries)
	eid := skillEffect(sid)
	if effects := skillTable[sid].Effects; len(effects) > 0 {
		eid = effects[0]
	}
	e["actor"], e["effect_bid"], e["skill_bid_of_effect"] = caster.pos, eid, sid
	return e
}

func (f *fightSim) grantStatus(caster *fightUnit, sid int64, rule buffRule, targets []*fightUnit) (out []proto.Message) {
	for _, t := range targets {
		if e := f.addBuff(caster, t, rule); e != nil {
			out = append(out, speedEffect(caster, t, sid, []proto.Message{e}))
		}
	}
	return
}

// Blackbeard reacts once to each simultaneous grant, including when its caster
// receives the same status as several teammates. Its own reaction is terminal.
func (f *fightSim) hasteReactions(effects []proto.Message) (out []proto.Message) {
	var gained []int64
	for _, e := range effects {
		for _, b := range e.Arr("buff_list") {
			if b.Int("action_type") != 1 {
				continue
			}
			a := initiative.Buffs[b.Int("buff_bid")].Stats
			if a.Flat > 0 || a.Per > 0 || a.Caster > 0 {
				gained = append(gained, b.Int("target"))
			}
		}
	}
	if len(gained) == 0 {
		return
	}
	for i := range f.units {
		u := &f.units[i]
		sid := familySkill(u, 52720)
		if u.hp <= 0 || sid == 0 {
			continue
		}
		for _, pos := range gained {
			if pos != u.pos && (pos <= 9) == u.ours {
				out = append(out, f.grantStatus(u, sid, speedRule(initiative.Refs[sid][0]), []*fightUnit{u})...)
				break
			}
		}
	}
	return
}

func (f *fightSim) statusBatch(u *fightUnit, sid int64, rule buffRule, targets []*fightUnit) []proto.Message {
	effects := f.grantStatus(u, sid, rule, targets)
	return append(effects, f.hasteReactions(effects)...)
}

func indexEffects(effects []proto.Message) {
	for i, e := range effects {
		e["order"] = i + 1
	}
}

func (f *fightSim) roundSpeedPassives(plays []proto.Message) []proto.Message {
	emit := func(u *fightUnit, sid int64, rule buffRule, targets []*fightUnit) {
		e := f.statusBatch(u, sid, rule, targets)
		if len(e) == 0 {
			return
		}
		indexEffects(e)
		plays = append(plays, proto.Message{"order": len(plays) + 1, "actor": u.pos, "target": e[0].Int("target"), "skill_bid": sid, "talk_pos": 0, "talk_content": "", "effect_play": e})
	}
	var aura [2]int64
	for i := range f.units {
		u := &f.units[i]
		if u.hp <= 0 {
			continue
		}
		for _, b := range u.buffs {
			if b.remain > 0 {
				side := b2i(u.ours)
				aura[side] = max(aura[side], initiative.Buffs[b.bid].Marine)
			}
		}
	}
	for i := range f.units {
		u := &f.units[i]
		if u.camp == 4 {
			u.marineAura = aura[b2i(u.ours)]
		}
	}
	for _, side := range []bool{true, false} {
		friends := f.alive(side)
		for _, u := range friends {
			for _, sid := range u.skills {
				refs := initiative.Refs[sid]
				grade := sid % 10
				switch sid / 10 {
				case 76340: // Soul King
					if f.round == 1 {
						emit(u, sid, speedRule(refs[0]), friends)
					}
				case 77040: // Lunarian
					if f.round == 1 {
						emit(u, sid, speedRule(refs[0]), []*fightUnit{u})
						emit(u, sid, speedRule(19851), []*fightUnit{u})
					}
				case 77640: // Four Emperors, distinct self and team recipients
					if f.round == 1 {
						emit(u, sid, speedRule(refs[1]), friends)
						for _, bid := range []int64{refs[0], refs[2]} {
							emit(u, sid, speedRule(bid), []*fightUnit{u})
						}
					}
				case 55320: // Justice
					emit(u, sid, speedRule(refs[0]), friends)
				case 78240: // one roll for the entire team
					emit(u, sid, speedRule(78231), []*fightUnit{u})
					var r buffRule
					switch f.rng.Intn(4) {
					case 0:
						r = speedRule(78200 + grade)
					case 1:
						r = speedRule(10160 + min(grade, 3))
						r.Kind, r.Pct, r.Rounds = "atk_up", grade*10, 1
					case 2:
						r = speedRule(285010 + grade)
						r.Kind, r.Pct, r.Rounds = "dmg_reduce", grade*10, 1
					case 3:
						r = speedRule(78220 + grade)
					}
					emit(u, sid, r, friends)
				case 77440: // one of Hina's three Cages
					emit(u, sid, speedRule(refs[f.rng.Intn(3)]), []*fightUnit{u})
				}
			}
		}
	}
	// Compare both sides against one snapshot, so iterating sides cannot grant
	// the second comparison a bonus from the first one.
	speeds := map[*fightUnit]int64{}
	fastest := map[bool]int64{}
	for i := range f.units {
		u := &f.units[i]
		if u.hp > 0 {
			speeds[u] = speedOf(u)
			fastest[u.ours] = max(fastest[u.ours], speeds[u])
		}
	}
	for i := range f.units {
		u := &f.units[i]
		if u.hp <= 0 {
			continue
		}
		if sid := familySkill(u, 51740); sid != 0 {
			bid := initiative.Refs[sid][0]
			if speeds[u] > fastest[!u.ours] {
				bid = initiative.Refs[sid][1]
			}
			r := speedRule(bid)
			r.Keep = true
			emit(u, sid, r, []*fightUnit{u})
		}
		if sid := familySkill(u, 77440); sid != 0 {
			var slower []*fightUnit
			for _, t := range f.alive(!u.ours) {
				if speeds[t] < speeds[u] {
					slower = append(slower, t)
				}
			}
			emit(u, sid, speedRule(initiative.Refs[sid][3]), slower)
		}
	}
	return plays
}

// buff_des includes conditional and secondary effects. Those are applied by
// their triggers, never as unconditional hits on the primary target.
func handledSpeedStatus(sid, bid int64) bool {
	switch sid / 10 {
	case 25210, 26310, 26330:
		return initiative.Buffs[bid].Stats.Per < 0 // bonus damage against an already slowed target
	case 55350, 55410, 77010, 77030, 2305, 94310, 96530:
		return initiative.Buffs[bid].Stats != (speedBonus{})
	}
	return false
}

func (f *fightSim) afterSpeedAction(u *fightUnit, sid int64, effects []proto.Message) []proto.Message {
	main := effects
	effects = append(effects, f.hasteReactions(main)...)
	allies := f.alive(u.ours)
	didHit, killed := false, false
	give := func(caster *fightUnit, skill, bid int64, targets []*fightUnit) {
		effects = append(effects, f.statusBatch(caster, skill, speedRule(bid), targets)...)
	}
	for _, e := range main {
		if e.Int("is_hit") == 0 || (e.Int("target") <= 9) == u.ours {
			continue
		}
		if skillTable[sid].Dmg == 0 && skillTable[sid].Normal != 1 {
			continue
		}
		didHit = true
		if e.Int("is_dead") != 0 {
			killed = true
			continue
		}
		var t *fightUnit
		for i := range f.units {
			if f.units[i].pos == e.Int("target") {
				t = &f.units[i]
				break
			}
		}
		if t == nil {
			continue
		}
		if be := familySkill(t, 50940); be != 0 {
			give(t, be, initiative.Refs[be][0], []*fightUnit{t})
		}
		if cat := familySkill(t, 55840); cat != 0 && !t.awakened && t.hp*100 <= t.maxHP*(cat%10)*10 {
			t.awakened = true
			for _, bid := range initiative.Refs[cat] {
				give(t, cat, bid, []*fightUnit{t})
			}
		}
		if tea := familySkill(t, 54340); tea != 0 {
			refs := initiative.Refs[tea]
			if f.rng.Intn(100) < []int{10, 21, 33, 43, 43}[min(tea%10, 5)-1] {
				give(t, tea, refs[2], f.alive(t.ours))
			} else {
				var men []*fightUnit
				for _, ally := range f.alive(t.ours) {
					if ally.sex == 1 {
						men = append(men, ally)
					}
				}
				give(t, tea, refs[3], men)
			}
		}
		if asura := familySkill(t, 54140); asura != 0 && t.hp*100 < t.maxHP*80 {
			last, used := t.lastCast[asura]
			if !used || f.round-last > 4 {
				t.lastCast[asura] = f.round
				give(t, asura, initiative.Refs[asura][0], []*fightUnit{t})
			}
		}
		if sid/10 == 54210 && f.rng.Intn(100) < []int{11, 17, 26, 35, 35}[min(sid%10, 5)-1] || sid/10 == 54510 && f.rng.Intn(12) == 0 {
			give(u, sid, 23001, []*fightUnit{t})
		}
	}
	grade := sid % 10
	switch sid / 10 {
	case 50410, 50430:
		if grade >= 2 && (sid/10 != 50430 || grade != 2 || f.rng.Intn(100) < 80) {
			targets := slices.DeleteFunc(slices.Clone(allies), func(t *fightUnit) bool { return t == u })
			if sid/10 == 50410 {
				targets = pickTargets(targets, skillRule{How: "random", N: 2}, f.rng)
			}
			bid := int64(10701)
			if grade >= 3 {
				bid = 10702
			}
			give(u, sid, bid, targets)
		}
	case 2305:
		if refs := initiative.Refs[sid]; len(refs) > 0 {
			give(u, sid, refs[0], allies)
		}
	case 94310:
		for _, bid := range initiative.Refs[sid] {
			if initiative.Buffs[bid].Stats != (speedBonus{}) {
				give(u, sid, bid, []*fightUnit{u})
			}
		}
	case 55350:
		give(u, sid, initiative.Refs[sid][1], allies)
	case 55410:
		if killed {
			give(u, sid, initiative.Refs[sid][1], []*fightUnit{u})
		}
	}
	if didHit {
		if yamato := familySkill(u, 54020); yamato != 0 && f.rng.Intn(100) < []int{50, 75, 90, 90, 90}[min(yamato%10, 5)-1] {
			give(u, yamato, 54061, []*fightUnit{u})
		}
		if king := familySkill(u, 77040); king != 0 {
			refs := initiative.Refs[king]
			if sid/10 != 77030 {
				give(u, king, refs[0], []*fightUnit{u})
			} else {
				var dropped []proto.Message
				for _, b := range slices.Clone(u.buffs) {
					if !b.negative {
						u.dropBuff(b)
						b.remain = 0
						dropped = append(dropped, buffEntry(2, b, u.pos, f.round))
					}
				}
				if len(dropped) > 0 {
					effects = append(effects, speedEffect(u, u, sid, dropped))
				}
				give(u, king, refs[1], []*fightUnit{u})
				give(u, king, refs[2], []*fightUnit{u})
			}
		}
	}
	if skillTable[sid].Type == "active_skill" && skillTable[sid].Normal != 1 {
		for _, t := range f.alive(!u.ours) {
			if niji := familySkill(t, 77840); niji != 0 {
				bid := initiative.Refs[niji][0]
				give(t, niji, bid, []*fightUnit{t})
				counter := familySkill(t, 77830)
				last, used := t.lastCast[counter]
				if counter != 0 && t.stackCount(bid) >= initiative.Buffs[bid].Cap && (!used || f.round-last > 1) {
					if extra := f.castSkill(t, counter, 0); extra != nil {
						effects = append(effects, extra.Arr("effect_play")...)
					}
				}
			}
		}
	}
	indexEffects(effects)
	return effects
}

func (u *fightUnit) stackCount(bid int64) int {
	count := 0
	for _, b := range u.buffs {
		if b.bid == bid && b.remain > 0 {
			count++
		}
	}
	return count
}

func (u *fightUnit) kingReady() bool {
	sid := familySkill(u, 77040)
	return sid != 0 && u.stackCount(initiative.Refs[sid][0]) >= 5
}
