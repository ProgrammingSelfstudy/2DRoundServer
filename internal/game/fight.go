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
	Name   string `json:"name"`
	Normal int    `json:"normal"`
	Type   string `json:"type"`
	CD     int    `json:"cd"`
	FireCD int    `json:"fire_cd"`
	Side   int    `json:"side"`
	How    string `json:"how"`
	N      int    `json:"n"`
	Dmg    int64  `json:"dmg"`
	Heal   int64  `json:"heal"`
	// Purge is how many negative statuses the skill removes from its targets (99 =
	// all), each target with chance PurgeChance percent.
	Purge       int   `json:"purge"`
	PurgeChance int64 `json:"purge_chance"`
	// Buffs are the statuses the skill adds to its targets, each with its chance.
	Buffs []buffRule `json:"buffs"`
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
	case "stun", "dot", "shield", "atk_down", "atk_up", "dmg_up", "crit_up", "crit_dmg_up", "dmg_reduce", "speed_up", "regen":
		return true
	}
	return false
}

// activeBuff is a status on a unit while the script is made.
type activeBuff struct {
	id, bid   int64 // id is unique in the fight (the client tells buffs apart by it)
	kind      string
	pct       int64
	remain    int // rounds left; a shield does not count down
	casterAtk int64
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
	pos, speed, hp, maxHP, atk int64
	crit, critRatio            int64 // per mille; critRatio 1500 = x1.5
	skills                     []int64
	ours                       bool
	lastCast                   map[int64]int // skill -> round of its last cast
	buffs                      []*activeBuff
}

// fightRound is what the client is sent for one round.
type fightRound struct {
	alive []int64         // positions alive at the start (20002)
	start []proto.Message // what happens at the start: status ticks and their damage (20002)
	plays []proto.Message // the attacks (20004)
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
}

// FightService plays the campaign fights. It keeps one fight per role.
type FightService struct {
	Store *store.Store
	// Rand makes the dice of a fight (targets, crits); nil = seeded from the clock.
	Rand func() *rand.Rand

	mu     sync.Mutex
	active map[int64]*fightState
}

// skillEffect is the effect id of a skill: 150101 -> 1501001, 200001 -> 2000001
// (the pattern seen in the capture).
func skillEffect(sid int64) int64 { return sid/100*1000 + sid%100 }

// usable reports whether the unit can cast the skill in this round: it must be an
// active skill with a damage or heal rule, past its first round and its cooldown,
// and a heal needs someone hurt.
func (u *fightUnit) usable(sid int64, round int, hurtAllies bool) bool {
	r, ok := skillTable[sid]
	if !ok || r.Type != "active_skill" || r.Normal == 1 {
		return false
	}
	if r.Side != 1 && r.Side != 2 && r.Side != 5 {
		return false // the other target kinds are not understood yet
	}
	if r.pure() && !r.givesStatus() {
		return false // only statuses the script cannot play
	}
	if r.Heal > 0 && r.Dmg == 0 && (!hurtAllies || u.atk*r.Heal/100 == 0) {
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
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].atk > sorted[j].atk })
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
	units    []fightUnit
	rng      *rand.Rand
	round    int
	nextBuff int64
	maxEnemy int64
	teamSize int64 // heroes at the start; the monsters hit lighter when there are fewer
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
		if b.kind == kind && b.remain > 0 {
			n += b.pct
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

// speedOf is the speed that orders the actions of a round, with speed-up statuses.
func speedOf(u *fightUnit) int64 { return u.speed * (100 + sumPct(u, "speed_up")) / 100 }

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
	if t.hp <= 0 || f.rng.Int63n(100) >= r.Chance {
		return nil
	}
	rounds, end := r.Rounds, f.round+r.Rounds
	if r.Kind == "shield" {
		rounds, end = 1, shieldEnd
	}
	if !r.Stack {
		for _, b := range t.buffs {
			if b.bid == r.Bid {
				b.remain, b.casterAtk, b.casterMaxHP = rounds, atkOf(caster), caster.maxHP
				return buffEntry(1, b, t.pos, end)
			}
		}
	}
	f.nextBuff++
	b := &activeBuff{id: f.nextBuff, bid: r.Bid, kind: r.Kind, pct: r.Pct, remain: rounds, casterAtk: atkOf(caster),
		casterMaxHP: caster.maxHP, base: r.Base, negative: r.Negative, keep: r.Keep}
	t.buffs = append(t.buffs, b)
	return buffEntry(1, b, t.pos, end)
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
			var dmg, heal int64
			for _, b := range u.buffs {
				switch b.kind {
				case "dot":
					d := b.casterAtk * b.pct / 100
					if d < 1 {
						d = 1
					}
					dmg += d
				case "regen":
					base := b.casterAtk
					if b.base == "maxhp" {
						base = b.casterMaxHP
					}
					if h := base * b.pct / 100; h > 0 {
						heal += h
					}
				}
			}
			if u.ours && dmg >= u.hp { // our heroes are never killed by a status
				dmg = u.hp - 1
			}
			if dmg > u.hp {
				dmg = u.hp
			}
			before := u.hp
			u.hp -= dmg
			if u.hp > 0 {
				if u.hp += heal; u.hp > u.maxHP {
					u.hp = u.maxHP
				}
			}
			var entries []proto.Message
			for _, b := range append([]*activeBuff(nil), u.buffs...) {
				if b.kind != "shield" {
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
			effects = append(effects, proto.Message{
				"order": len(effects) + 1, "actor": u.pos, "target": u.pos, "effect_bid": 0, "is_hit": 1, "is_crit": 0,
				"hp_changed": u.hp - before, "hp": u.hp, "is_dead": b2i(u.hp <= 0), "camp_restrain": 0,
				"buff_list": nonNil(entries), "summon_list": []proto.Message{}, "sub_effect_play_list": []proto.Message{},
				"is_blind": 0, "skill_bid_of_effect": 0, "actor_hp_changed": 0, "actor_is_dead": 0, "aid_actor": 0,
				"total_hurt": 0, "hurt_reward": []proto.Message{},
			})
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
	if u.hp <= 0 || u.has("stun") != nil {
		return nil
	}
	foes, friends := f.alive(!u.ours), f.alive(u.ours)
	if len(foes) == 0 {
		return nil
	}
	hurt := false
	for _, fr := range friends {
		if fr.hp < fr.maxHP {
			hurt = true
		}
	}
	sid := u.pickSkill(f.round, hurt)
	rule := skillTable[sid]
	targets := f.targetsOf(u, rule, foes, friends)
	if rule.pure() && !f.worth(rule, targets) {
		sid = u.normalSkill()
		rule = skillTable[sid]
		targets = f.targetsOf(u, rule, foes, friends)
	}
	u.lastCast[sid] = f.round
	if len(targets) == 0 {
		return nil
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
				if half := (f.maxEnemy + 1) / 2; base < half {
					base = half
				}
			} else if d := t.maxHP / 40 * f.teamShare() / 5; d > base {
				base = d
			}
			dmg := base * pct / 100 * dealtPct(u) / 100
			if c := critOf(u); c > 0 && f.rng.Int63n(1000) < c {
				crit = 1
				dmg = dmg * critRatioOf(u) / 1000
			}
			if dmg = dmg * takenPct(t) / 100; dmg < 1 {
				dmg = 1
			}
			if dmg > t.hp {
				dmg = t.hp
			}
			delta = -dmg
		}
		t.hp += delta
		for _, br := range rule.Buffs {
			if br.Negative == (rule.Side == 1) {
				if e := f.addBuff(u, t, br); e != nil {
					buffList = append(buffList, e)
				}
			}
		}
		if rule.Purge > 0 && rule.Side != 1 && f.rng.Int63n(100) < rule.PurgeChance {
			buffList = append(buffList, f.purge(t, rule.Purge)...)
		}
		if t.hp <= 0 {
			t.buffs = nil
		}
		effects = append(effects, proto.Message{
			"order": len(effects) + 1, "actor": u.pos, "target": t.pos, "effect_bid": skillEffect(sid), "is_hit": 1, "is_crit": crit,
			"hp_changed": delta, "hp": t.hp, "is_dead": b2i(t.hp <= 0), "camp_restrain": 0,
			"buff_list": nonNil(buffList), "summon_list": []proto.Message{}, "sub_effect_play_list": []proto.Message{},
			"is_blind": 0, "skill_bid_of_effect": sid, "actor_hp_changed": 0, "actor_is_dead": 0, "aid_actor": 0,
			"total_hurt": 0, "hurt_reward": []proto.Message{},
		})
	}
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
	f := &fightSim{units: append(append([]fightUnit(nil), team...), monsters...), rng: rng, teamSize: int64(len(team))}
	for i := range f.units {
		f.units[i].lastCast = map[int64]int{}
	}
	for _, m := range monsters {
		if m.maxHP > f.maxEnemy {
			f.maxEnemy = m.maxHP
		}
	}
	var rounds []fightRound
	for len(rounds) < maxFightRounds && len(f.alive(false)) > 0 && len(f.alive(true)) > 0 {
		f.round = len(rounds) + 1
		var r fightRound
		if f.round > 1 {
			r.start = f.startOfRound()
		}
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
		sort.SliceStable(order, func(i, j int) bool { return speedOf(order[i]) > speedOf(order[j]) })
		for _, u := range order {
			if p := f.act(u, len(r.plays)+2); p != nil {
				r.plays = append(r.plays, p)
			}
		}
		rounds = append(rounds, r)
	}
	return rounds, len(f.alive(false)) == 0 && len(f.alive(true)) > 0
}

// fightSetup collects the fighters of a role: its campaign formation against the
// wave, and the 20027 message that introduces them.
func (f *FightService) fightSetup(role *store.Role, d store.Drama, auto int64) ([]fightRound, bool, proto.Message, error) {
	// the starter hero is normally granted when the hero list is first opened; a
	// role that fights before that gets it now (granted once, never twice)
	if _, err := f.Store.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		return nil, false, nil, err
	}
	form, err := formationOf(f.Store, role.RID, formDrama)
	if err != nil {
		return nil, false, nil, err
	}
	heroes, err := f.Store.PartnersOf(role.RID)
	if err != nil {
		return nil, false, nil, err
	}
	byID := map[int64]store.Partner{}
	for _, h := range heroes {
		byID[h.PartnerID] = h
	}
	slots := append([]store.FormationSlot(nil), form.Slots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Pos < slots[j].Pos })

	objects := []proto.Message{{
		"camp_type": 0, "career": 0, "extra_data": []proto.Message{}, "face_id": 0, "group": 1, "hp": 34856, "hp_max": 34856, "lev": 1,
		"object_bid": 1, "object_id": 1, "object_name": "精灵", "object_type": 5, "owner_id": 0, "owner_srv_id": "", "pos": spiritPos,
		"sex": 0, "skills": []proto.Message{}, "sprites": []proto.Message{{"item_bid": 0, "pos": 1}}, "star": 0,
	}}
	var team, foes []fightUnit
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok || len(team) == formMaxHeroes {
			continue
		}
		info, known := partnerTable[h.BID]
		if !known {
			continue
		}
		m := partnerMessage(h)
		pos := battleCell(form.FormationType, sl.Pos)
		skills := []proto.Message{}
		var sids []int64
		for _, sk := range m.Arr("skills") {
			sids = append(sids, sk.Int("skill_bid"))
			skills = append(skills, proto.Message{"skill_bid": sk.Int("skill_bid"), "end_round": 0})
		}
		team = append(team, fightUnit{pos: pos, speed: m.Int("speed"), hp: m.Int("hp"), maxHP: m.Int("hp"), atk: m.Int("atk"),
			crit: m.Int("crit_rate") + baseCritRate, critRatio: m.Int("crit_ratio"), skills: sids, ours: true})
		objects = append(objects, proto.Message{
			"camp_type": info.Camp, "career": info.Type, "extra_data": []proto.Message{{"extra_key": 5, "extra_value": 0}, {"extra_key": 10, "extra_value": 0}, {"extra_key": 11, "extra_value": 0}},
			"face_id": 0, "group": 1, "hp": m.Int("hp"), "hp_max": m.Int("hp"), "lev": h.Lev, "object_bid": h.BID, "object_id": h.PartnerID,
			"object_name": info.Name, "object_type": 2, "owner_id": role.RID, "owner_srv_id": role.SrvID, "pos": pos, "sex": 0, "skills": skills,
			"sprites": []proto.Message{}, "star": h.Star,
		})
	}
	// the monsters of the dungeon that is being challenged
	wave, ok := dungeonUnits[d.DunID]
	if !ok || len(wave.Monsters) == 0 {
		return nil, false, nil, errNoWave
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
		return nil, false, nil, errNoHeroes
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	if f.Rand != nil {
		rng = f.Rand()
	}
	rounds, won := buildFight(team, foes, rng)
	return rounds, won, start, nil
}

var errNoHeroes = errors.New("no hero can fight")

var errNoWave = errors.New("the dungeon has no monsters")

// start begins a fight: the confirmation of 13003, the map and the fight data.
func (f *FightService) start(s *gateway.Session, role *store.Role, d store.Drama, auto int64) error {
	rounds, won, intro, err := f.fightSetup(role, d, auto)
	if err != nil {
		return err
	}
	f.mu.Lock()
	if f.active == nil {
		f.active = map[int64]*fightState{}
	}
	f.active[role.RID] = &fightState{rounds: rounds, won: won}
	f.mu.Unlock()
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

func roundStart(r fightRound, n int) proto.Message {
	return proto.Message{
		"action_count": n + 1, "all_alive": posList(r.alive), "combat_type": combatDrama, "countdown_time": 0,
		"hallows_list": []proto.Message{{"group": 1, "val": n + 1}, {"group": 2, "val": n + 1}},
		"owner_id":     0, "owner_srv_id": "", "pos": 0, "round_buff": []proto.Message{}, "skill_plays": nonNil(r.start),
		"sprite_cd_info": []proto.Message{{"cd_info": []proto.Message{}, "pos": spiritPos}},
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
	delete(f.active, role.RID)
	f.mu.Unlock()
	if st == nil {
		return s.Send(20062, proto.Message{"code": 0, "msg": "当前没有可以跳过的战斗"})
	}
	if err := s.Send(20062, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
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
	if st == nil {
		return nil // not in a fight: stale confirmation
	}
	switch {
	case cmd == 20001 && st.stage == waitReady:
		st.stage = waitRoundOver
		if err := s.Send(20001, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(20002, roundStart(st.rounds[0], 0))
	case cmd == 20019 && st.stage == waitRoundOver:
		st.stage = waitPlayed
		r := st.rounds[st.cur]
		return s.Send(20004, proto.Message{
			"action_count": st.cur + 1, "combat_type": combatDrama, "round_buff": []proto.Message{}, "skill_plays": r.plays,
			"sprite_cd_info": []proto.Message{{"cd_info": []proto.Message{}, "pos": spiritPos}}, "star_list": []proto.Message{},
		})
	case cmd == 20005 && st.stage == waitPlayed:
		if st.cur+1 < len(st.rounds) {
			st.cur++
			st.stage = waitRoundOver
			return s.Send(20002, roundStart(st.rounds[st.cur], st.cur))
		}
		f.mu.Lock()
		delete(f.active, role.RID)
		f.mu.Unlock()
		return settle(st.won)
	}
	return nil
}
