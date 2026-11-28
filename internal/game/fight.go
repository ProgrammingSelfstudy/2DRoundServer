package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The campaign fight, played the way the original server does it (capture
// original-2026-10-20):
//
//	c2s 13003
//	s2c 13003, 20000, 20027            the fight starts
//	c2s 20001                          the client finished loading the scene
//	s2c 20001, 20002 (round 1 start)
//	c2s 20019                          the round-start animation is over
//	s2c 20004                          the attacks of the round
//	c2s 20005                          the attacks were played
//	s2c 20002 (round 2) ... or, after the last round, the result (13016 ...)
//
// The server decides the whole fight when it starts: a script in which the heroes
// always win (our policy, the real damage rules are not known).

// skills.json holds the skills of the heroes and the wave's monsters
// (tools/extract_skills.py): cooldown, first round, targets, damage / heal percent.
//
//go:embed data/skills.json
var skillsJSON []byte

// skillRule is what the battle script needs to know of a skill. Dmg and Heal are
// percents of the caster's attack; How says which targets (front, random, max_atk,
// min_hp, all) and N how many; Side 1 targets the enemy, 2 the own side.
type skillRule struct {
	Description string `json:"des"`
	Name        string `json:"name"`
	Normal      int    `json:"normal"`
	Type        string `json:"type"`
	CD          int    `json:"cd"`
	FireCD      int    `json:"fire_cd"`
	Side        int    `json:"side"`
	How         string `json:"how"`
	N           int    `json:"n"`
	Dmg         int64  `json:"dmg"`
	Heal        int64  `json:"heal"`
	// Purge is how many negative statuses the skill removes from its targets (99 =
	// all), each target with chance PurgeChance percent.
	Purge       int   `json:"purge"`
	PurgeChance int64 `json:"purge_chance"`
	// Buffs are the statuses the skill adds to its targets, each with its chance.
	Buffs   []buffRule `json:"buffs"`
	Effects []int64    `json:"effects"`
}

// pure reports a skill that neither hurts nor heals: it only adds statuses or purges.
func (r skillRule) pure() bool { return r.Normal != 1 && r.Dmg == 0 && r.Heal == 0 }

// givesStatus reports whether the script can play something for the skill's statuses.
func (r skillRule) givesStatus() bool {
	if r.Purge > 0 {
		return true
	}
	for _, b := range r.Buffs {
		if b.plays() {
			return true
		}
	}
	return false
}

// buffRule is one status a skill adds (from the client's buff table). Kind is what
// the script plays: stun (cannot act), dot (loses Pct of the caster's attack each
// round), shield (blocks one hit), atk_down (attack lowered by Pct); anything else
// is shown but changes nothing. Negative ones go to the enemy targets of a skill,
// the others to the own side.
type buffRule struct {
	Bid      int64  `json:"bid"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Pct      int64  `json:"pct"`
	Rounds   int    `json:"rounds"`
	Stack    bool   `json:"stack"`
	Negative bool   `json:"negative"`
	Base     string `json:"base"`   // regen: what Pct is a percent of, "atk" or "maxhp"
	Keep     bool   `json:"keep"`   // cannot be purged
	Chance   int64  `json:"chance"` // percent
}

// plays reports whether the script turns the status into numbers; the other kinds
// are only shown on the client.
func (r buffRule) plays() bool {
	switch r.Kind {
	case "stun", "dot", "shield", "atk_down", "atk_up", "dmg_up", "crit_up", "crit_dmg_up", "dmg_reduce", "speed_up", "speed_down", "regen":
		return true
	}
	return false
}

// activeBuff is a status on a unit while the script is made.
type activeBuff struct {
	id, bid         int64 // id is unique in the fight (the client tells buffs apart by it)
	kind            string
	pct             int64
	remain          int   // rounds left; a shield does not count down
	firstSpeedRound int   // the first turn order that uses this speed modifier
	speedFlat       int64 // snapshotted contribution based on the caster
	casterAtk       int64
	caster          *fightUnit
	// casterMaxHP and base size a regen; negative statuses can be purged unless keep
	casterMaxHP int64
	base        string
	negative    bool
	keep        bool
}

// shieldEnd is the end_round the capture shows for a shield: it lasts until hit.
const shieldEnd = 100

var skillTable = func() map[int64]skillRule {
	var f struct {
		Skills map[int64]skillRule `json:"skills"`
	}
	if err := json.Unmarshal(skillsJSON, &f); err != nil || len(f.Skills) == 0 {
		panic("game: bad skills.json")
	}
	return f.Skills
}()

// baseCritRate is the crit chance (per mille) every hero has on top of its own
// crit_rate: the starter heroes have none and the capture shows no crit at all, so
// without it the crit display could never be seen (our policy).
const baseCritRate = 100

// maxFightRounds is the round limit the client shows ("第1/15回合"): monsters still
// standing after it mean a lost fight.
const maxFightRounds = 15

// fightMap is the battle map id of the campaign (combat_map of 20000).
const fightMap = 20001

// spiritPos is where the spirit unit stands; the client lists it in every round.
const spiritPos = 41

// fightUnit is one fighter while the script is made.
type fightUnit struct {
	guild                                  guildCombatBonus
	pos, speed, hp, maxHP, atk             int64
	power, breakLev                        int64
	damage, taken, healing                 int64
	crit, critRatio                        int64 // per mille; critRatio 1500 = x1.5
	skills                                 []int64
	camp                                   int64
	openingMitigation                      int64
	marineAura, sex, baseSpeed, level, def int64
	training                               speedBonus
	awakened                               bool
	ours                                   bool
	lastCast                               map[int64]int // skill -> round of its last cast
	buffs                                  []*activeBuff
}

func (u *fightUnit) loseHP(source *fightUnit, amount int64) {
	u.hp -= amount
	u.taken += amount
	if source != nil && source.ours != u.ours {
		source.damage += amount
	}
}

func (u *fightUnit) gainHP(source *fightUnit, amount int64) {
	u.hp += amount
	if source != nil {
		source.healing += amount
	}
}

// fightRound is what the client is sent for one round.
type fightRound struct {
	alive []int64         // positions alive at the start (20002)
	start []proto.Message // what happens at the start: status ticks and their damage (20002)
	plays []proto.Message // the attacks (20004)
}

func fightStatistics(sim *fightSim, objects []proto.Message) []proto.Message {
	byPos := make(map[int64]fightUnit, len(sim.units))
	for _, unit := range sim.units {
		byPos[unit.pos] = unit
	}
	groups := map[int64][]proto.Message{1: {}, 2: {}}
	for _, object := range objects {
		unit, ok := byPos[object.Int("pos")]
		if !ok {
			continue
		}
		group := object.Int("group")
		groups[group] = append(groups[group], proto.Message{
			"rid": object.Int("owner_id"), "srvid": object.Str("owner_srv_id"), "id": object.Int("object_id"),
			"bid": object.Int("object_bid"), "star": object.Int("star"), "lev": object.Int("lev"), "camp_type": object.Int("camp_type"),
			"dps": unit.damage, "be_hurt": unit.taken, "cure": unit.healing, "ext_data": []proto.Message{},
		})
	}
	return []proto.Message{{"type": 2, "partner_hurts": groups[2]}, {"type": 1, "partner_hurts": groups[1]}}
}

// fightStage is what the server waits for next.
type fightStage int

const (
	waitReady     fightStage = iota // 20001
	waitRoundOver                   // 20019
	waitPlayed                      // 20005
)

type fightState struct {
	rounds []fightRound
	won    bool // how the script ends: the heroes win, or lose when they are all down or the rounds run out
	cur    int
	stage  fightStage
	owner  *gateway.Session
	played bool // all rounds were acknowledged through 20005
	// combat is the client's fight type (0 = the campaign) and settle what ends the fight (nil = the
	// campaign's own settling, passed by the caller).
	combat int64
	settle func(s *gateway.Session, won bool) error
}

func (st *fightState) combatType() int64 {
	if st.combat == 0 {
		return combatDrama
	}
	return st.combat
}

var fightServices sync.Map // *store.Store -> *FightService

// fightServiceOf is the one fight service of a store, shared by the campaign and the other modes that play fights.
func fightServiceOf(st *store.Store) *FightService {
	v, _ := fightServices.LoadOrStore(st, &FightService{Store: st})
	return v.(*FightService)
}

// FightService plays the campaign fights. It keeps one fight per role.
type FightService struct {
	Store *store.Store
	// Rand makes the dice of a fight (targets, crits); nil = seeded from the clock.
	Rand func() *rand.Rand

	mu     sync.Mutex
	active map[int64]*fightState
	// passes are the fights of the frontier dungeons that were kept for the pass videos, by role, until the fight is settled
	passes map[int64]passRecord
}

// takePass hands over (once) the pass video kept for the role's running campaign fight.
func (f *FightService) takePass(rid int64) (passRecord, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.passes[rid]
	delete(f.passes, rid)
	return rec, ok
}

// skillEffect is the effect id of a skill: 150101 -> 1501001, 200001 -> 2000001
// (the pattern seen in the capture).
func skillEffect(sid int64) int64 { return sid/100*1000 + sid%100 }

// usable reports whether the unit can cast the skill in this round: it must be an
// active skill with a damage or heal rule, past its first round and its cooldown,
// and a heal needs someone hurt.
func (u *fightUnit) usable(sid int64, round int, hurtAllies bool) bool {
	r, ok := skillTable[sid]
	if sid/10 == 77030 && !u.kingReady() {
		return false
	}
	if !ok || r.Type != "active_skill" || r.Normal == 1 {
		return false
	}
	if r.Side != 1 && r.Side != 2 && r.Side != 5 {
		return false // the other target kinds are not understood yet
	}
	_, priority := initiative.First[sid]
	if r.pure() && !r.givesStatus() && !priority {
		return false // only statuses the script cannot play
	}
	if r.Heal > 0 && r.Dmg == 0 && (!hurtAllies || u.atk*r.Heal/100 == 0) && !(sid/100 == 5043 && sid%100 >= 2) {
		return false // nobody to heal, or a heal of nothing
	}
	first := r.FireCD
	if first < 1 {
		first = 1
	}
	if round < first {
		return false
	}
	if last, cast := u.lastCast[sid]; cast && round-last <= r.CD {
		return false
	}
	return true
}

// pickSkill is the skill the unit casts in a round: its first usable active skill,
// else its normal attack (the unit's first skill when none is marked normal).
func (u *fightUnit) pickSkill(round int, hurtAllies bool) int64 {
	for _, sid := range u.skills {
		if _, first := initiative.First[sid]; first && u.usable(sid, round, hurtAllies) {
			return sid
		}
		if sid/10 == 77030 && u.kingReady() {
			return sid
		}
	}
	for _, sid := range u.skills {
		if u.usable(sid, round, hurtAllies) {
			return sid
		}
	}
	return u.normalSkill()
}

// normalSkill is the unit's normal attack (its first skill when none is marked).
func (u *fightUnit) normalSkill() int64 {
	for _, sid := range u.skills {
		if r, ok := skillTable[sid]; ok && r.Normal == 1 {
			return sid
		}
	}
	if len(u.skills) > 0 {
		return u.skills[0]
	}
	return 0
}

// pickTargets chooses the units a skill hits from the candidates (all alive).
func pickTargets(cands []*fightUnit, r skillRule, rng *rand.Rand) []*fightUnit {
	if len(cands) == 0 {
		return nil
	}
	n := r.N
	if n < 1 {
		n = 1
	}
	if n > len(cands) {
		n = len(cands)
	}
	sorted := append([]*fightUnit(nil), cands...)
	switch r.How {
	case "random":
		rng.Shuffle(len(sorted), func(i, j int) { sorted[i], sorted[j] = sorted[j], sorted[i] })
	case "max_atk":
		sort.SliceStable(sorted, func(i, j int) bool { return atkOf(sorted[i]) > atkOf(sorted[j]) })
	case "min_hp": // the lowest share of health first
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].hp*sorted[j].maxHP < sorted[j].hp*sorted[i].maxHP
		})
	default: // front: the lowest position first
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].pos < sorted[j].pos })
	}
	if r.How == "all" {
		return sorted
	}
	return sorted[:n]
}

// fightSim is the state while the script is made.
type fightSim struct {
	orderLocked bool // the acting order for the current round has already been decided
	units       []fightUnit
	rng         *rand.Rand
	round       int
	nextBuff    int64
	maxEnemy    int64
	// realDamage turns off the campaign's floor on the heroes' damage (a boss fight counts the damage)
	realDamage bool
	teamSize   int64 // heroes at the start; the monsters hit lighter when there are fewer
}

// teamShare is how many of the five hero places a dungeon is made for are filled
// (at most 5). The dungeons are meant for a team of five, which a lone starter hero
// cannot match, so the monsters' damage is scaled by it: our rule, to keep the
// campaign playable with the heroes a player really has.
func (f *fightSim) teamShare() int64 {
	if f.teamSize < 1 {
		return 5
	}
	if f.teamSize > 5 {
		return 5
	}
	return f.teamSize
}

func (f *fightSim) alive(ours bool) []*fightUnit {
	var out []*fightUnit
	for i := range f.units {
		if f.units[i].ours == ours && f.units[i].hp > 0 {
			out = append(out, &f.units[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pos < out[j].pos })
	return out
}

// sumPct adds up the percents of the unit's statuses of a kind.
func sumPct(u *fightUnit, kind string) int64 {
	var n int64
	for _, b := range u.buffs {
		if b.remain <= 0 {
			continue
		}
		if b.kind == kind {
			n += b.pct
			continue
		}
		// Multi-stat statuses use a single native icon and a single instance.
		def := initiative.Buffs[b.bid]
		switch kind {
		case "atk_up":
			n += def.Atk
		case "crit_up":
			n += def.Crit
		case "dmg_reduce":
			n += def.Mitigation
		case "dmg_up":
			n += def.Damage
		}
	}
	return n
}

// atkOf is the unit's attack with its attack-up and attack-down statuses on it.
func atkOf(u *fightUnit) int64 {
	atk := u.atk * (100 + sumPct(u, "atk_up")) / 100
	for _, b := range u.buffs {
		if b.kind == "atk_down" {
			atk = atk * (100 - b.pct) / 100
		}
	}
	if atk < 1 && u.atk > 0 {
		atk = 1
	}
	return atk
}

// critOf and critRatioOf are the crit chance (per mille) and ratio with the
// crit-up statuses; a percent of the client is ten per mille.
func critOf(u *fightUnit) int64 {
	c := u.crit + 10*sumPct(u, "crit_up")
	if c > 1000 {
		c = 1000
	}
	return c
}

func critRatioOf(u *fightUnit) int64 { return u.critRatio + 10*sumPct(u, "crit_dmg_up") }

// The training camp breaks speed ties by battlefield column, then by power.
// Cells 1–3 and 10–12 are the two front columns, facing each other.
func battleColumn(pos int64) int64 {
	column := (pos - 1) / 3
	if pos <= 9 {
		return 2 - column
	}
	return column
}

func actsBefore(a, b *fightUnit) bool {
	if sa, sb := speedOf(a), speedOf(b); sa != sb {
		return sa > sb
	}
	if ca, cb := battleColumn(a.pos), battleColumn(b.pos); ca != cb {
		return ca < cb
	}
	return a.power > b.power
}

// dealtPct and takenPct scale the damage a unit deals and takes (damage-up and
// damage-reduce statuses; the reduction stops at 90%).
func dealtPct(u *fightUnit) int64 { return 100 + sumPct(u, "dmg_up") }

func takenPct(u *fightUnit) int64 {
	p := 100 - sumPct(u, "dmg_reduce")
	if p < 10 {
		p = 10
	}
	return p
}

func (u *fightUnit) hasBid(bid int64) bool {
	for _, b := range u.buffs {
		if b.bid == bid {
			return true
		}
	}
	return false
}

func (u *fightUnit) has(kind string) *activeBuff {
	for _, b := range u.buffs {
		if b.kind == kind && (b.remain > 0 || kind == "shield") {
			return b
		}
	}
	return nil
}

func (u *fightUnit) dropBuff(b *activeBuff) {
	for i, x := range u.buffs {
		if x == b {
			u.buffs = append(u.buffs[:i], u.buffs[i+1:]...)
			return
		}
	}
}

// buffEntry is one entry of a buff_list: action 1 adds a status, 2 removes it and
// 3 is the per-round update (action 3 is what the capture shows at the start of
// round 2; 2 is our guess for the end, the capture is too short to show one).
func buffEntry(action int64, b *activeBuff, target int64, endRound int) proto.Message {
	return proto.Message{"action_type": action, "buff_bid": b.bid, "change_type": 0, "change_value": 0, "end_round": endRound,
		"id": b.id, "is_dead": 0, "remain_round": b.remain, "target": target}
}

// addBuff gives a status to a unit when the chance comes up; it returns the
// buff_list entry or nil. A status that does not stack only refreshes its rounds.
func (f *fightSim) addBuff(caster, t *fightUnit, r buffRule) proto.Message {
	if t.hp <= 0 || r.Negative && (t.hasBid(19851) || t.hasBid(63040) || r.Kind == "stun" && (t.hasBid(78231) || t.hasBid(77741) || t.hasBid(77102) || hasStatusGroup(t, 162))) {
		return nil
	}
	native, known := initiative.Buffs[r.Bid]
	var flat int64
	if known && native.Stats.Caster != 0 {
		flat = speedOf(caster) * native.Stats.Caster / 1000
	}
	control := caster.guild.control - t.guild.resistControl
	if r.Kind == "stun" && control != 0 {
		if f.rng.Int63n(1000) >= r.Chance*10+control {
			return nil
		}
	} else if f.rng.Int63n(100) >= r.Chance {
		return nil
	}
	rounds, end := r.Rounds, f.round+r.Rounds
	firstSpeedRound := 0
	if r.Kind == "speed_up" || r.Kind == "speed_down" || native.Stats != (speedBonus{}) || native.Marine > 0 {
		firstSpeedRound = f.round
		if f.orderLocked {
			firstSpeedRound++
		}
		end = firstSpeedRound + rounds - 1
	}
	if r.Kind == "shield" {
		rounds, end = 1, shieldEnd
	}
	// A refresh reuses the instance ID so the client keeps the same icon and stack count.
	var replace *activeBuff
	count := 0
	for _, b := range t.buffs {
		if b.bid == r.Bid || known && native.Type == 2 && initiative.Buffs[b.bid].Group == native.Group {
			count++
			if replace == nil || b.remain < replace.remain {
				replace = b
			}
		}
	}
	if replace != nil && (!r.Stack || native.Cap > 0 && count >= native.Cap) {
		replace.bid, replace.kind, replace.pct, replace.remain, replace.caster = r.Bid, r.Kind, r.Pct, rounds, caster
		replace.keep, replace.negative, replace.speedFlat = r.Keep, r.Negative, flat
		replace.casterAtk, replace.casterMaxHP, replace.firstSpeedRound = atkOf(caster), caster.maxHP, firstSpeedRound
		return buffEntry(1, replace, t.pos, end)
	}
	f.nextBuff++
	b := &activeBuff{id: f.nextBuff, bid: r.Bid, kind: r.Kind, pct: r.Pct, remain: rounds, caster: caster, casterAtk: atkOf(caster),
		casterMaxHP: caster.maxHP, speedFlat: flat, base: r.Base, negative: r.Negative, keep: r.Keep, firstSpeedRound: firstSpeedRound}
	t.buffs = append(t.buffs, b)
	return buffEntry(1, b, t.pos, end)
}

// The four grades of the battle-start mitigation talent and artifact skill share
// one status icon (3102). Only the highest equipped or learned grade applies.
func mitigationSkill(p store.Partner) int64 {
	var best int64
	consider := func(id int64) {
		if id >= 750100 && id <= 750103 && id > best {
			best = id
		}
	}
	for _, talent := range p.Talents {
		consider(talent.SkillID)
	}
	for _, art := range p.Artifacts {
		for _, id := range art.Skills {
			consider(id)
		}
	}
	return best
}

func (f *fightSim) openingPassives() []proto.Message {
	var plays []proto.Message
	cast := func(caster *fightUnit, id, eid int64, rule buffRule, targets []*fightUnit) {
		var effects []proto.Message
		for _, target := range targets {
			entry := f.addBuff(caster, target, rule)
			if entry == nil {
				continue
			}
			effect := roundStatusEffect(target, len(effects)+1, 0, []proto.Message{entry})
			effect["actor"], effect["effect_bid"], effect["skill_bid_of_effect"] = caster.pos, eid, id
			effects = append(effects, effect)
		}
		if len(effects) > 0 {
			plays = append(plays, proto.Message{"order": len(plays) + 1, "actor": caster.pos, "target": effects[0].Int("target"),
				"skill_bid": id, "talk_pos": 0, "talk_content": "", "effect_play": effects})
		}
	}
	for i := range f.units {
		u := &f.units[i]
		id := u.openingMitigation
		if id == 0 || u.hp <= 0 {
			continue
		}
		grade := id - 750100
		percent := [...]int64{10, 20, 30, 50}
		rule := buffRule{Bid: 37020 + grade, Kind: "dmg_reduce", Pct: percent[grade], Rounds: 2, Chance: 100}
		cast(u, id, 7501001+grade, rule, []*fightUnit{u})
	}
	// Heat Field applies before the first ordering, on either side. As with
	// Marine Instructor, multiple casters on one side give a single opening effect.
	for _, side := range []bool{true, false} {
		var mentor *fightUnit
		var ace *fightUnit
		var field int64
		var skill int64
		var marines []*fightUnit
		for _, ally := range f.alive(side) {
			if ally.camp == 4 {
				marines = append(marines, ally)
			}
			for _, id := range ally.skills {
				if isHeatField(id) && id > field {
					ace, field = ally, id
				}
				if id >= 535201 && id <= 535204 && id > skill {
					mentor, skill = ally, id
				}
			}
		}
		if mentor != nil {
			rule := skillTable[skill]
			cast(mentor, skill, rule.Effects[0], rule.Buffs[0], marines)
		}
		if ace != nil {
			rule := skillTable[field]
			cast(ace, field, rule.Effects[0], rule.Buffs[0], f.alive(!side))
		}
	}
	return plays
}

func isHeatField(id int64) bool { return id >= 151401 && id <= 151404 }

func roundStatusEffect(u *fightUnit, order int, change int64, entries []proto.Message) proto.Message {
	return proto.Message{
		"order": order, "actor": u.pos, "target": u.pos, "effect_bid": 0, "is_hit": 1, "is_crit": 0,
		"hp_changed": change, "hp": u.hp, "is_dead": b2i(u.hp <= 0), "camp_restrain": 0,
		"buff_list": nonNil(entries), "summon_list": []proto.Message{}, "sub_effect_play_list": []proto.Message{},
		"is_blind": 0, "skill_bid_of_effect": 0, "actor_hp_changed": 0, "actor_is_dead": 0, "aid_actor": 0,
		"total_hurt": 0, "hurt_reward": []proto.Message{},
	}
}

// startOfRound plays the statuses at the start of a round: damage over time hits,
// every status counts one round down (action 3) and the ones that ran out go
// (action 2). The capture shows this as one play at the start of round 2.
func (f *fightSim) startOfRound() []proto.Message {
	var effects []proto.Message
	for _, side := range []bool{true, false} {
		for _, u := range f.alive(side) {
			if len(u.buffs) == 0 {
				continue
			}
			before := u.hp
			var minHP int64
			if u.ours { // our heroes are never killed by a status
				minHP = 1
			}
			// Resolve damage before regeneration. Each status is credited for the HP it actually changes,
			// including when several statuses reach the same cap in one round.
			for _, b := range u.buffs {
				if b.kind == "dot" {
					amount := b.casterAtk * b.pct / 100
					if b.base == "target_maxhp" {
						amount = min(u.maxHP*b.pct/100, 10*b.casterAtk)
					}
					damage := min(max(1, amount), u.hp-minHP)
					u.loseHP(b.caster, damage)
				}
			}
			if u.hp > 0 {
				for _, b := range u.buffs {
					if b.kind != "regen" {
						continue
					}
					base := b.casterAtk
					if b.base == "maxhp" {
						base = b.casterMaxHP
					}
					u.gainHP(b.caster, min(max(0, base*b.pct/100), u.maxHP-u.hp))
				}
			}
			var entries []proto.Message
			for _, b := range append([]*activeBuff(nil), u.buffs...) {
				if b.kind != "shield" && (b.firstSpeedRound == 0 || f.round > b.firstSpeedRound) {
					b.remain--
				}
				switch {
				case u.hp <= 0 || (b.remain <= 0 && b.kind != "shield"):
					b.remain = 0
					entries = append(entries, buffEntry(2, b, u.pos, f.round))
					u.dropBuff(b)
				default:
					end := f.round - 1 + b.remain
					if b.kind == "shield" {
						end = shieldEnd
					}
					entries = append(entries, buffEntry(3, b, u.pos, end))
				}
			}
			effects = append(effects, roundStatusEffect(u, len(effects)+1, u.hp-before, entries))
		}
	}
	if len(effects) == 0 {
		return nil
	}
	first := f.alive(true)
	if len(first) == 0 {
		return nil
	}
	return []proto.Message{{"order": 1, "actor": first[0].pos, "target": first[0].pos, "skill_bid": 0, "talk_pos": 0, "talk_content": "", "effect_play": effects}}
}

// targetsOf is who a skill hits: the enemies for a skill of side 1, the caster for
// a self skill (side 5), else units of the own side.
func (f *fightSim) targetsOf(u *fightUnit, r skillRule, foes, friends []*fightUnit) []*fightUnit {
	switch {
	case r.Side == 1 && r.How == "front_middle":
		var front []*fightUnit
		for _, t := range foes {
			cell := t.pos
			if cell > 9 {
				cell -= 10
			}
			if cell <= 6 {
				front = append(front, t)
			}
		}
		return front
	case r.Side == 1:
		return pickTargets(foes, r, f.rng)
	case r.Side == 5 || r.How == "self":
		return []*fightUnit{u}
	default:
		return pickTargets(friends, r, f.rng)
	}
}

// worth reports whether a pure status skill would change anything: a status the
// target does not carry yet (or one that stacks), or a negative status to purge.
func (f *fightSim) worth(r skillRule, targets []*fightUnit) bool {
	for _, t := range targets {
		for _, br := range r.Buffs {
			if br.plays() && br.Negative == (r.Side == 1) && (br.Stack || !t.hasBid(br.Bid)) {
				return true
			}
		}
		if r.Purge > 0 {
			for _, b := range t.buffs {
				if b.negative && !b.keep {
					return true
				}
			}
		}
	}
	return false
}

// purge removes up to n negative statuses of a unit, the newest first, and returns
// their buff_list entries (action 2).
func (f *fightSim) purge(t *fightUnit, n int) []proto.Message {
	var out []proto.Message
	for i := len(t.buffs) - 1; i >= 0 && n > 0; i-- {
		if b := t.buffs[i]; b.negative && !b.keep {
			b.remain = 0
			out = append(out, buffEntry(2, b, t.pos, f.round))
			t.buffs = append(t.buffs[:i], t.buffs[i+1:]...)
			n--
		}
	}
	return out
}

// act plays one unit's turn: it casts an active skill when one is ready, else attacks
// normally; a stunned unit does nothing. A pure status skill that would change
// nothing is replaced by a normal attack. Returns the play, or nil.
func (f *fightSim) act(u *fightUnit, order int) proto.Message {
	if u.hp <= 0 {
		return nil
	}
	return f.castSkill(u, u.pickSkill(f.round, f.injured(u.ours)), order)
}

func (f *fightSim) castSkill(u *fightUnit, sid int64, order int) proto.Message {
	foes, friends := f.alive(!u.ours), f.alive(u.ours)
	if u.hp <= 0 || len(foes) == 0 {
		return nil
	}
	ignore, first := initiative.First[sid]
	if u.has("stun") != nil && !ignore {
		return nil
	}
	rule := skillTable[sid]
	targets := f.targetsOf(u, rule, foes, friends)
	if rule.pure() && !first && !(sid/100 == 5043 && sid%100 >= 2) && !f.worth(rule, targets) {
		sid = u.normalSkill()
		rule = skillTable[sid]
		targets = f.targetsOf(u, rule, foes, friends)
	}
	u.lastCast[sid] = f.round
	if len(targets) == 0 {
		return nil
	}
	eid := skillEffect(sid)
	if len(rule.Effects) > 0 && (sid >= 536101 && sid <= 536104 || first) {
		// Soul Pocus uses a separate attack effect from the normal attack at every grade.
		eid = rule.Effects[0]
	}
	pct := rule.Dmg
	if pct == 0 {
		pct = rule.Heal
	}
	if pct == 0 { // an unknown normal attack: a plain hit
		pct = 100
	}
	var effects []proto.Message
	for _, t := range targets {
		var delta, crit int64
		var buffList []proto.Message
		switch {
		case rule.pure(): // only statuses: nothing is hurt or healed
		case rule.Heal > 0 && rule.Dmg == 0:
			delta = atkOf(u) * pct / 100
			if missing := t.maxHP - t.hp; delta > missing {
				delta = missing
			}
		case t.has("shield") != nil:
			sh := t.has("shield")
			t.dropBuff(sh) // the shield takes the hit: nothing is lost
			sh.remain = 0
			buffList = append(buffList, buffEntry(2, sh, t.pos, f.round))
		default:
			base := int64(1)
			if u.ours {
				base = atkOf(u)
				if half := (f.maxEnemy + 1) / 2; base < half && !f.realDamage {
					base = half
				}
			} else if d := t.maxHP / 40 * f.teamShare() / 5; d > base {
				base = d
			}
			dmg := base * pct / 100 * dealtPct(u) / 100
			dmg += dmg * u.guild.damage / 1000
			if c := critOf(u) - t.guild.tenacity; c > 0 && f.rng.Int63n(1000) < c {
				crit = 1
				dmg = dmg * critRatioOf(u) / 1000
			}
			dmg = dmg * takenPct(t) / 100
			dmg -= dmg * t.guild.resistDamage(rule) / 1000
			if dmg < 1 {
				dmg = 1
			}
			if dmg > t.hp {
				dmg = t.hp
			}
			delta = -dmg
		}
		if delta < 0 {
			t.loseHP(u, -delta)
		} else if delta > 0 {
			t.gainHP(u, delta)
		}
		for _, br := range rule.Buffs {
			if br.Negative != (rule.Side == 1) || handledSpeedStatus(sid, br.Bid) {
				continue
			}
			// Soul Pocus always wounds a surviving target; the extra Soul Steal requires strictly lower attack.
			if (br.Bid == 53616 || br.Bid == 53626 || br.Bid == 53636) && atkOf(t) >= atkOf(u) {
				continue
			}
			if e := f.addBuff(u, t, br); e != nil {
				buffList = append(buffList, e)
			}
		}
		if rule.Purge > 0 && rule.Side != 1 && f.rng.Int63n(100) < rule.PurgeChance {
			buffList = append(buffList, f.purge(t, rule.Purge)...)
		}
		if t.hp <= 0 {
			t.buffs = nil
		}
		effects = append(effects, proto.Message{
			"order": len(effects) + 1, "actor": u.pos, "target": t.pos, "effect_bid": eid, "is_hit": 1, "is_crit": crit,
			"hp_changed": delta, "hp": t.hp, "is_dead": b2i(t.hp <= 0), "camp_restrain": 0,
			"buff_list": nonNil(buffList), "summon_list": []proto.Message{}, "sub_effect_play_list": []proto.Message{},
			"is_blind": 0, "skill_bid_of_effect": sid, "actor_hp_changed": 0, "actor_is_dead": 0, "aid_actor": 0,
			"total_hurt": 0, "hurt_reward": []proto.Message{},
		})
	}
	effects = f.afterSpeedAction(u, sid, effects)
	return proto.Message{
		"order": order, "actor": u.pos, "target": targets[0].pos, "skill_bid": sid, "talk_pos": 0, "talk_content": "",
		"effect_play": effects,
	}
}

// buildFight makes the script. Every living unit acts once per round, in order of
// speed: it casts an active skill when one is ready (the capture shows round 1 as
// all active skills, round 2 as normal attacks), else attacks normally, and the
// skills' statuses (stun, damage over time, shield, attack down) play out. Our
// heroes hit hard enough to end each monster in about two hits and the monsters
// hit lightly, so the heroes win and the fight is over in a few rounds. It is lost
// when every hero is down or the monsters outlast maxFightRounds; the second result
// says which.
func buildFight(team []fightUnit, monsters []fightUnit, rng *rand.Rand) ([]fightRound, bool) {
	rounds, won, _ := playFight(team, monsters, rng, maxFightRounds)
	return rounds, won
}

// playFight is buildFight with its own round limit; it also returns the simulation, whose units tell what
// is left of each side (a boss fight counts the damage).
func playFight(team []fightUnit, monsters []fightUnit, rng *rand.Rand, maxRounds int) ([]fightRound, bool, *fightSim) {
	return playFightOpts(team, monsters, rng, maxRounds, false)
}

// playFightOpts: with realDamage the heroes hit for their own attack; otherwise they hit at least half of the
// strongest monster's hp, which makes the campaign winnable (see act).
func playFightOpts(team []fightUnit, monsters []fightUnit, rng *rand.Rand, maxRounds int, realDamage bool) ([]fightRound, bool, *fightSim) {
	f := &fightSim{units: append(append([]fightUnit(nil), team...), monsters...), rng: rng, teamSize: int64(len(team)), realDamage: realDamage}
	for i := range f.units {
		f.units[i].lastCast = map[int64]int{}
	}
	for _, m := range monsters {
		if m.maxHP > f.maxEnemy {
			f.maxEnemy = m.maxHP
		}
	}
	var rounds []fightRound
	for len(rounds) < maxRounds && len(f.alive(false)) > 0 && len(f.alive(true)) > 0 {
		f.round = len(rounds) + 1
		f.orderLocked = false
		var r fightRound
		if f.round == 1 {
			r.start = f.openingPassives()
		} else {
			r.start = f.startOfRound()
		}
		r.start = f.roundSpeedPassives(r.start)
		if len(f.alive(false)) == 0 { // a status killed the last monster
			rounds = append(rounds, r)
			break
		}
		for _, side := range []bool{true, false} {
			for _, u := range f.alive(side) {
				r.alive = append(r.alive, u.pos)
			}
		}
		order := append(f.alive(true), f.alive(false)...)
		priority := map[*fightUnit]bool{}
		for _, u := range order {
			sid := u.pickSkill(f.round, f.injured(u.ours))
			ignore, first := initiative.First[sid]
			priority[u] = first && (u.has("stun") == nil || ignore)
		}
		sort.SliceStable(order, func(i, j int) bool {
			if priority[order[i]] != priority[order[j]] {
				return priority[order[i]]
			}
			return actsBefore(order[i], order[j])
		})
		f.orderLocked = true
		for _, u := range order {
			if p := f.act(u, len(r.plays)+2); p != nil {
				r.plays = append(r.plays, p)
			}
		}
		rounds = append(rounds, r)
	}
	return rounds, len(f.alive(false)) == 0 && len(f.alive(true)) > 0, f
}

// fightSetup collects the fighters of a role: its campaign formation against the
// wave, and the 20027 message that introduces them.
// teamSide makes a role's own side of a fight: its campaign formation, and the 20027 objects (the spirit and
// the heroes).
func (f *FightService) teamSide(role *store.Role, h5 bool) ([]fightUnit, []proto.Message, store.Formation, error) {
	return f.teamSideFor(role, formDrama, h5)
}

func (f *FightService) teamSideFor(role *store.Role, kind int64, h5 bool) ([]fightUnit, []proto.Message, store.Formation, error) {
	// the starter hero is normally granted when the hero list is first opened; a
	// role that fights before that gets it now (granted once, never twice)
	if _, err := starterGrant(f.Store, role); err != nil {
		return nil, nil, store.Formation{}, err
	}
	lineup, err := combatFormationFor(f.Store, role.RID, kind, h5)
	if err != nil {
		return nil, nil, lineup.form, err
	}
	team, objects := ownFightSide(role, lineup.form.FormationType, lineup.form.Slots, lineup.byID)
	return team, objects, lineup.form, nil
}

// teamFor makes a role's side of a fight from the heroes it puts in the given places.
func (f *FightService) teamFor(role *store.Role, formType int64, formSlots []store.FormationSlot, h5 bool) ([]fightUnit, []proto.Message, error) {
	heroes, err := f.Store.PartnersOf(role.RID)
	if err != nil {
		return nil, nil, err
	}
	team, objects := buildFightTeam(role, formType, formSlots, playablePartners(heroes, h5))
	return team, objects, nil
}

func buildFightTeam(role *store.Role, formType int64, formSlots []store.FormationSlot, heroes []store.Partner) ([]fightUnit, []proto.Message) {
	return ownFightSide(role, formType, formSlots, partnersByID(heroes))
}

func ownFightSide(role *store.Role, formType int64, formSlots []store.FormationSlot, byID map[int64]store.Partner) ([]fightUnit, []proto.Message) {
	slots := append([]store.FormationSlot(nil), formSlots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Pos < slots[j].Pos })

	objects := []proto.Message{{
		"camp_type": 0, "career": 0, "extra_data": []proto.Message{}, "face_id": 0, "group": 1, "hp": 34856, "hp_max": 34856, "lev": 1,
		"object_bid": 1, "object_id": 1, "object_name": "精灵", "object_type": 5, "owner_id": 0, "owner_srv_id": "", "pos": spiritPos,
		"sex": 0, "skills": []proto.Message{}, "sprites": []proto.Message{{"item_bid": 0, "pos": 1}}, "star": 0,
	}}
	team, heroObjects := sideUnits(role.RID, role.SrvID, slots, formType, byID, true)
	return team, append(objects, heroObjects...)
}

func (f *FightService) fightSetup(role *store.Role, d store.Drama, auto int64, h5 bool, now int64) ([]fightRound, bool, proto.Message, *fightSim, error) {
	if _, err := starterGrant(f.Store, role); err != nil {
		return nil, false, nil, nil, err
	}
	lineup, err := combatFormationFor(f.Store, role.RID, formDrama, h5)
	if err != nil {
		return nil, false, nil, nil, err
	}
	form := lineup.form
	training, err := campaignTrainingOf(f.Store, role.RID, form, now)
	if err != nil {
		return nil, false, nil, nil, err
	}
	team, objects := ownFightSide(role, form.FormationType, form.Slots, lineup.byID)
	for i := range team {
		h := lineup.byID[objects[i+1].Int("object_id")]
		team[i].addTraining(training.forHero(h))
	}
	var foes []fightUnit
	// the monsters of the dungeon that is being challenged
	wave, ok := dungeonUnits[d.DunID]
	if !ok || len(wave.Monsters) == 0 {
		return nil, false, nil, nil, errNoWave
	}
	for i, m := range wave.Monsters {
		if i == len(monsterPos) {
			break
		}
		u, obj := monsterOf(m, i)
		foes = append(foes, u)
		objects = append(objects, obj)
	}
	start := proto.Message{
		"combat_type": combatDrama, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
		"objects": objects, "is_auto": auto, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{{"param": d.DunID}}, "string_ext_args": []proto.Message{},
		"a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": wave.Group, "suppress": 0,
		"halo_list":    []proto.Message{},
		"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	if len(team) == 0 {
		return nil, false, nil, nil, errNoHeroes
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	if f.Rand != nil {
		rng = f.Rand()
	}
	rounds, won, sim := playFight(team, foes, rng, maxFightRounds)
	return rounds, won, start, sim, nil
}

// sideUnits makes the fighters and their 20027 objects for the heroes in a formation: ours stand on the
// left (group 1), the others on the right (group 2, ten cells further).
func sideUnits(ownerRID int64, ownerSrv string, slots []store.FormationSlot, formType int64, byID map[int64]store.Partner, ours bool) ([]fightUnit, []proto.Message) {
	group, shift := 1, int64(0)
	if !ours {
		group, shift = 2, 10
	}
	var units []fightUnit
	var objects []proto.Message
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok || len(units) == formMaxHeroes {
			continue
		}
		info, known := partnerTable[h.BID]
		if !known {
			continue
		}
		m := partnerMessage(h)
		pos := battleCell(formType, sl.Pos) + shift
		skills := []proto.Message{}
		var sids []int64
		unlocked, hasSlots := initiative.Slots[brachKey(info, int(m.Int("break_lev")))]
		for _, sk := range m.Arr("skills") {
			id := sk.Int("skill_bid")
			if hasSlots && sk.Int("pos") > unlocked {
				continue
			}
			sids = append(sids, id)
			skills = append(skills, proto.Message{"skill_bid": id, "end_round": 0})
		}
		u := fightUnit{pos: pos, sex: int64(b2i(initiative.Male[h.BID])), baseSpeed: m.Int("speed"), level: m.Int("lev"), def: m.Int("def"), hp: m.Int("hp"), maxHP: m.Int("hp"), atk: m.Int("atk"), breakLev: m.Int("break_lev"),
			crit: m.Int("crit_rate") + baseCritRate, critRatio: m.Int("crit_ratio"), camp: int64(info.Camp), skills: sids, ours: ours, guild: guildCombatBonusOf(h), openingMitigation: mitigationSkill(h)}
		u.addTraining(skillSpeed(sids).plus(wornSpeed(h)))
		units = append(units, u)
		objects = append(objects, proto.Message{
			"camp_type": info.Camp, "career": info.Type, "extra_data": []proto.Message{{"extra_key": 5, "extra_value": h.UseSkin}, {"extra_key": 10, "extra_value": 0}, {"extra_key": 11, "extra_value": 0}},
			"face_id": 0, "group": group, "hp": m.Int("hp"), "hp_max": m.Int("hp"), "lev": h.Lev, "object_bid": h.BID, "object_id": h.PartnerID,
			"object_name": info.Name, "object_type": 2, "owner_id": ownerRID, "owner_srv_id": ownerSrv, "pos": pos, "sex": 0, "skills": skills,
			"sprites": []proto.Message{}, "star": h.Star,
		})
	}
	return units, objects
}

var errNoHeroes = errors.New("no hero can fight")

var errNoWave = errors.New("the dungeon has no monsters")

// start begins a fight: the confirmation of 13003, the map and the fight data.
func (f *FightService) start(s *gateway.Session, role *store.Role, d store.Drama, auto int64) error {
	h5 := isH5RoleSession(s)
	rounds, won, intro, sim, err := f.fightSetup(role, d, auto, h5, s.Now().Unix())
	if err != nil {
		if h5 && errors.Is(err, errNoHeroes) {
			return s.Send(13003, proto.Message{"code": 0, "msg": "没有可以出战的英雄"})
		}
		return err
	}
	var pass *passRecord
	if won && d.DunID == d.MaxDunID { // the clear that takes the role past the dungeon
		var power int64
		if h5 {
			if lineup, err := h5FormationTeamOf(f.Store, role.RID, formDrama); err == nil {
				_, power = describeArenaLineup(lineup.form.Slots, lineup.byID)
			}
		} else {
			_, _, power, _ = lineupOf(f.Store, role.RID)
		}
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		id := recordReplay(f.Store, combatDrama, replaySide{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Power: power},
			replaySide{Name: dungeonUnits[d.DunID].Group}, intro, rounds, true, s.Now().Unix(), replayDetails{Map: fightMap, Sim: sim})
		if id != 0 {
			pass = &passRecord{Replay: id, Power: power, Time: int64(len(rounds)) * dramaPassSeconds}
		}
	}
	f.mu.Lock()
	if f.active == nil {
		f.active = map[int64]*fightState{}
	}
	f.active[role.RID] = &fightState{rounds: rounds, won: won, owner: s}
	if f.passes == nil {
		f.passes = map[int64]passRecord{}
	}
	delete(f.passes, role.RID)
	if pass != nil {
		f.passes[role.RID] = *pass
	}
	f.mu.Unlock()
	fillHalo(intro)
	for _, m := range []struct {
		cmd  uint16
		body proto.Message
	}{
		{13003, proto.Message{"code": 1, "msg": ""}},
		{20063, proto.Message{"type_list": []proto.Message{}}},
		{20000, proto.Message{"combat_map": fightMap, "combat_type": combatDrama}},
		{20027, intro},
	} {
		if err := s.Send(m.cmd, m.body); err != nil {
			return err
		}
	}
	return nil
}

// begin starts a fight that was made elsewhere: the map and the fight data (no 13003, which is the campaign's own).
func (f *FightService) begin(s *gateway.Session, role *store.Role, combat int64, intro proto.Message, rounds []fightRound, won bool, settle func(*gateway.Session, bool) error) error {
	return f.beginFight(s, role, intro, &fightState{rounds: rounds, won: won, combat: combat, settle: settle})
}

func (f *FightService) hasFight(s *gateway.Session, rid int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[rid] != nil && f.active[rid].owner == s
}

func (f *FightService) beginFight(s *gateway.Session, role *store.Role, intro proto.Message, fight *fightState) error {
	f.mu.Lock()
	if f.active == nil {
		f.active = map[int64]*fightState{}
	}
	fight.owner = s
	f.active[role.RID] = fight
	f.mu.Unlock()
	fillHalo(intro)
	for _, m := range []struct {
		cmd  uint16
		body proto.Message
	}{
		{20063, proto.Message{"type_list": []proto.Message{}}},
		{20000, proto.Message{"combat_map": fightMap, "combat_type": fight.combat}},
		{20027, intro},
	} {
		if err := s.Send(m.cmd, m.body); err != nil {
			return err
		}
	}
	return nil
}

func roundStart(r fightRound, n int, combat int64) proto.Message {
	return proto.Message{
		"action_count": n + 1, "all_alive": posList(r.alive), "combat_type": combat, "countdown_time": 0,
		"hallows_list": []proto.Message{{"group": 1, "val": n + 1}, {"group": 2, "val": n + 1}},
		"owner_id":     0, "owner_srv_id": "", "pos": 0, "round_buff": []proto.Message{}, "skill_plays": nonNil(r.start),
		"sprite_cd_info": []proto.Message{{"cd_info": []proto.Message{}, "pos": spiritPos}},
	}
}

// roundPlays is the 20004 of a round: the attacks.
func roundPlays(r fightRound, n int, combat int64) proto.Message {
	return proto.Message{
		"action_count": n + 1, "combat_type": combat, "round_buff": []proto.Message{}, "skill_plays": r.plays,
		"sprite_cd_info": []proto.Message{{"cd_info": []proto.Message{}, "pos": spiritPos}}, "star_list": []proto.Message{},
	}
}

func posList(ps []int64) []proto.Message {
	out := make([]proto.Message, 0, len(ps))
	for _, p := range ps {
		out = append(out, proto.Message{"pos": p})
	}
	return out
}

// skip ends the running fight at once (the client's skip button, 20062) and settles
// it with the outcome of the script. Without a fight nothing happens.
func (f *FightService) skip(s *gateway.Session, settle func(won bool) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	f.mu.Lock()
	st := f.active[role.RID]
	if st != nil && st.combat == combatStarTower && st.owner != s {
		st = nil
	}
	if st != nil {
		delete(f.active, role.RID)
	}
	f.mu.Unlock()
	if st == nil {
		return s.Send(20062, proto.Message{"code": 0, "msg": "当前没有可以跳过的战斗"})
	}
	if err := s.Send(20062, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if st.settle != nil {
		return st.settle(s, st.won)
	}
	return settle(st.won)
}

// advance moves the fight on for a client confirmation; settle is called once after
// the last round has been played, with the outcome of the script.
func (f *FightService) advance(s *gateway.Session, cmd uint16, settle func(won bool) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	f.mu.Lock()
	st := f.active[role.RID]
	f.mu.Unlock()
	if st == nil || (st.combat == combatStarTower && st.owner != s) {
		return nil // not in a fight: stale confirmation
	}
	switch {
	case cmd == 20001 && st.stage == waitReady:
		st.stage = waitRoundOver
		if err := s.Send(20001, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(20002, roundStart(st.rounds[0], 0, st.combatType()))
	case cmd == 20019 && st.stage == waitRoundOver:
		st.stage = waitPlayed
		r := st.rounds[st.cur]
		return s.Send(20004, roundPlays(r, st.cur, st.combatType()))
	case cmd == 20005 && st.stage == waitPlayed:
		if st.cur+1 < len(st.rounds) {
			st.cur++
			st.stage = waitRoundOver
			return s.Send(20002, roundStart(st.rounds[st.cur], st.cur, st.combatType()))
		}
		f.mu.Lock()
		if f.active[role.RID] != st {
			f.mu.Unlock()
			return nil
		}
		st.played = true
		delete(f.active, role.RID)
		f.mu.Unlock()
		if st.settle != nil {
			return st.settle(s, st.won)
		}
		return settle(st.won)
	}
	return nil
}
