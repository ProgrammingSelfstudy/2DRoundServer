package game

import (
	"math/rand"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// playFight plays the fight that follows a 13003 reply, the way the client does
// (capture original-2026-10-20), and returns the result message (13016).
func (c *client) playFight() proto.Message {
	c.t.Helper()
	c.recv(20063)
	if m := c.recv(20000); m.Int("combat_map") != fightMap || m.Int("combat_type") != combatDrama {
		c.t.Fatalf("20000: %v", m)
	}
	c.recv(20027)
	c.send(20001, proto.Message{})
	c.recv(20001)
	c.recv(20002)
	for round := 0; round < 20; round++ {
		c.send(20019, proto.Message{})
		c.recv(20004)
		c.send(20005, proto.Message{})
		cmd, m := c.recvNext()
		switch cmd {
		case 20002:
		case 13016:
			return m
		default:
			c.t.Fatalf("after round %d got %d", round+1, cmd)
		}
	}
	c.t.Fatal("the fight never ended")
	return nil
}

// recvNext reads the next frame whatever it is.
func (c *client) recvNext() (uint16, proto.Message) {
	c.t.Helper()
	c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	fr, err := proto.ReadFrame(c.br)
	if err != nil {
		c.t.Fatal(err)
	}
	m, err := proto.Decode(proto.DefaultOptions(), c.schema.Recv[fr.Command], fr.Payload)
	if err != nil {
		c.t.Fatalf("decode %d: %v", fr.Command, err)
	}
	return fr.Command, m
}

// The fight is a script the client plays: 20027 introduces the units, every round
// has a start (20002) and the attacks (20004), the heroes win and only then does
// the result (13016) come.
func TestFightScript(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(13003, proto.Message{"is_auto": 1})
	if r := c.recv(13003); r.Int("code") != 1 {
		t.Fatalf("13003: %v", r)
	}
	c.recv(20063)
	c.recv(20000)
	intro := c.recv(20027)
	var ours, foes int
	for _, o := range intro.Arr("objects") {
		switch o.Int("object_type") {
		case 2:
			ours++
			if o.Int("hp") <= 0 || len(o.Arr("skills")) == 0 || o.Int("owner_id") != role.RID {
				t.Fatalf("hero unit: %v", o)
			}
		case 3:
			foes++
		}
	}
	if ours == 0 || foes != len(dungeonUnits[dramaChain.First].Monsters) || intro.Int("combat_type") != combatDrama || intro.Int("is_auto") != 1 || intro.Arr("extra_args")[0].Int("param") != dramaChain.First {
		t.Fatalf("20027: %d heroes %d monsters %v", ours, foes, intro)
	}

	// a confirmation out of order does nothing
	c.send(20005, proto.Message{})
	c.send(20001, proto.Message{})
	c.recv(20001)
	r1 := c.recv(20002)
	if r1.Int("action_count") != 1 || len(r1.Arr("all_alive")) != ours+foes {
		t.Fatalf("round 1 start: %v", r1)
	}
	deadFoes := 0
	for round := 1; ; round++ {
		c.send(20019, proto.Message{})
		atk := c.recv(20004)
		if atk.Int("action_count") != int64(round) || len(atk.Arr("skill_plays")) == 0 {
			t.Fatalf("round %d attacks: %v", round, atk)
		}
		for _, p := range atk.Arr("skill_plays") {
			ef := p.Arr("effect_play")[0]
			rule := skillTable[p.Int("skill_bid")]
			heal := rule.Side == 2 && rule.Heal > 0 // a heal puts hp back
			pure := rule.pure()                     // a status skill changes no hp
			if (!heal && !pure && ef.Int("hp_changed") >= 0) || (heal && ef.Int("hp_changed") < 0) || (pure && ef.Int("hp_changed") != 0) || ef.Int("hp") < 0 {
				t.Fatalf("hit: %v", p)
			}
			if ef.Int("is_dead") == 1 && ef.Int("target") >= 15 {
				deadFoes++
			}
		}
		c.send(20005, proto.Message{})
		cmd, m := c.recvNext()
		if cmd == 13016 {
			if deadFoes != foes || m.Int("result") != 1 {
				t.Fatalf("result after %d rounds with %d dead monsters: %v", round, deadFoes, m)
			}
			break
		}
		if cmd != 20002 || m.Int("action_count") != int64(round+1) || round > maxFightRounds {
			t.Fatalf("round %d end: got %d %v", round, cmd, m)
		}
	}
	// nothing is paid before the end, and a second 20005 later does nothing
	c.recv(13001)
	c.recv(10302)
	c.recv(10500)
	c.send(20005, proto.Message{})
	c.expectNothing()
}

// fixedFight builds a fight with a seeded dice so the checks are repeatable.
func fixedFight(team, foes []fightUnit, seed int64) []fightRound {
	rounds, _ := buildFight(team, foes, rand.New(rand.NewSource(seed)))
	return rounds
}

func foe(pos, hp int64, skills ...int64) fightUnit {
	return fightUnit{pos: pos, speed: 50, hp: hp, maxHP: hp, atk: 5, skills: skills}
}

// Round 1 is all active skills and round 2 normal attacks (cd 2 puts the next cast
// on round 4), as in the capture.
func TestFightSkillsFollowCooldown(t *testing.T) {
	// 200101 is the active skill of hero 10401 (cd 2, first round 1), 200001 its normal attack
	hero := fightUnit{pos: 9, speed: 80, hp: 5000, maxHP: 5000, atk: 100, skills: []int64{200101, 200001}, ours: true}
	rounds := fixedFight([]fightUnit{hero}, []fightUnit{foe(15, 4000, 267001), foe(16, 4000, 267001)}, 1)
	var casts []int64
	for _, r := range rounds {
		for _, p := range r.plays {
			if p.Int("actor") == 9 {
				casts = append(casts, p.Int("skill_bid"))
			}
		}
	}
	if len(casts) < 4 || casts[0] != 200101 || casts[1] != 200001 || casts[2] != 200001 || casts[3] != 200101 {
		t.Fatalf("hero casts: %v", casts)
	}
}

// A skill that hits several targets has one effect per target, the damage is the
// skill's percent of the attack, and a kill is marked.
func TestFightAreaSkillHitsEveryTarget(t *testing.T) {
	// 244101: "random 3 enemies, 77% magic damage"
	hero := fightUnit{pos: 4, speed: 80, hp: 5000, maxHP: 5000, atk: 100, skills: []int64{244101, 244001}, ours: true}
	foes := []fightUnit{foe(15, 4000), foe(16, 4000), foe(18, 4000), foe(17, 4000)}
	rounds := fixedFight([]fightUnit{hero}, foes, 3)
	var play proto.Message
	for _, p := range rounds[0].plays {
		if p.Int("actor") == 4 {
			play = p
		}
	}
	ef := play.Arr("effect_play")
	if play.Int("skill_bid") != 244101 || len(ef) != 3 {
		t.Fatalf("area skill: %v", play)
	}
	seen := map[int64]bool{}
	for _, e := range ef {
		seen[e.Int("target")] = true
		// our floor makes every hit at least half of the biggest monster's hp, times 77%
		if e.Int("hp_changed") >= 0 || e.Int("effect_bid") != 2441001 {
			t.Fatalf("effect: %v", e)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("three different targets expected: %v", ef)
	}
}

// A heal skill puts hp back on the hurt ally (a positive hp_changed) and is not
// cast while nobody is hurt.
func TestFightHealSkill(t *testing.T) {
	// 228101: "heal the lowest-hp ally for 117% of the attack"
	healer := fightUnit{pos: 6, speed: 90, hp: 800, maxHP: 800, atk: 100, skills: []int64{228101, 228001}, ours: true}
	tank := fightUnit{pos: 9, speed: 10, hp: 300, maxHP: 900, atk: 10, skills: []int64{200001}, ours: true}
	rounds := fixedFight([]fightUnit{healer, tank}, []fightUnit{foe(15, 3000, 267001)}, 5)
	var heal proto.Message
	for _, r := range rounds {
		for _, p := range r.plays {
			if p.Int("skill_bid") == 228101 && heal == nil {
				heal = p
			}
		}
	}
	if heal == nil {
		t.Fatal("the healer never healed although the tank was hurt")
	}
	e := heal.Arr("effect_play")[0]
	if e.Int("target") != 9 || e.Int("hp_changed") != 117 || e.Int("hp") != 417 {
		t.Fatalf("heal effect: %v", e)
	}

	// nobody hurt: no heal, a normal attack instead
	full := fightUnit{pos: 9, speed: 10, hp: 900, maxHP: 900, atk: 10, skills: []int64{200001}, ours: true}
	rounds = fixedFight([]fightUnit{healer, full}, []fightUnit{foe(15, 3000)}, 5)
	for _, p := range rounds[0].plays {
		if p.Int("actor") == 6 && p.Int("skill_bid") == 228101 {
			t.Fatal("healed a team that is not hurt")
		}
	}
}

// With a crit chance of 100% every hit is a crit with the crit ratio on top.
func TestFightCrit(t *testing.T) {
	hero := fightUnit{pos: 9, speed: 80, hp: 5000, maxHP: 5000, atk: 100, crit: 1000, critRatio: 2000, skills: []int64{200001}, ours: true}
	plain := hero
	plain.crit = 0
	hit := func(u fightUnit) proto.Message {
		rounds := fixedFight([]fightUnit{u}, []fightUnit{foe(15, 100000)}, 1)
		for _, p := range rounds[0].plays {
			if p.Int("actor") == 9 {
				return p.Arr("effect_play")[0]
			}
		}
		return nil
	}
	c, n := hit(hero), hit(plain)
	if c.Int("is_crit") != 1 || n.Int("is_crit") != 0 || c.Int("hp_changed") != 2*n.Int("hp_changed") {
		t.Fatalf("crit %v vs plain %v", c, n)
	}
	// the crit chance of heroes is their own plus the base one
	h := []int64{}
	for i := 0; i < 400; i++ {
		u := fightUnit{pos: 9, speed: 80, hp: 1 << 40, maxHP: 1 << 40, atk: 10, crit: baseCritRate, critRatio: 1500, skills: []int64{200001}, ours: true}
		for _, r := range fixedFight([]fightUnit{u}, []fightUnit{foe(15, 1<<30)}, int64(i))[:1] {
			for _, p := range r.plays {
				if p.Int("actor") == 9 {
					h = append(h, p.Arr("effect_play")[0].Int("is_crit"))
				}
			}
		}
	}
	crits := 0
	for _, v := range h {
		crits += int(v)
	}
	if crits < 15 || crits > 70 { // 10% of 400 = 40
		t.Fatalf("%d crits in %d hits at a 10%% chance", crits, len(h))
	}
}

// withSkill installs a test skill for the length of one test.
func withSkill(t *testing.T, sid int64, r skillRule) {
	t.Helper()
	old, had := skillTable[sid]
	skillTable[sid] = r
	t.Cleanup(func() {
		if had {
			skillTable[sid] = old
		} else {
			delete(skillTable, sid)
		}
	})
}

// gentle installs a weak normal attack (5% of the floor damage), so a fight with a
// big monster lasts several rounds.
func gentle(t *testing.T) int64 {
	withSkill(t, 990010, skillRule{Name: "poke", Normal: 1, Type: "active_skill", Side: 1, How: "front", N: 1, Dmg: 5})
	return 990010
}

func castBy(rounds []fightRound, round int, pos int64) proto.Message {
	for _, p := range rounds[round-1].plays {
		if p.Int("actor") == pos {
			return p
		}
	}
	return nil
}

// stun: the target cannot act while the status lasts. A 2-round stun cast in round 1
// by a faster unit costs the target its turn in rounds 1 and 2; the status entries
// are added (1), counted down (3) and removed (2) the way the capture shows.
func TestFightStun(t *testing.T) {
	withSkill(t, 990001, skillRule{Name: "stunner", Type: "active_skill", CD: 9, FireCD: 1, Side: 1, How: "front", N: 1, Dmg: 5,
		Buffs: []buffRule{{Bid: 28201, Name: "眩晕", Kind: "stun", Rounds: 2, Negative: true, Chance: 100}}})
	hero := fightUnit{pos: 9, speed: 90, hp: 5000, maxHP: 5000, atk: 10, skills: []int64{990001, gentle(t)}, ours: true}
	slow := foe(15, 100000, 990010)
	slow.speed = 10
	rounds := fixedFight([]fightUnit{hero}, []fightUnit{slow}, 1)
	if len(rounds) < 4 {
		t.Fatalf("only %d rounds", len(rounds))
	}
	if castBy(rounds, 1, 15) != nil || castBy(rounds, 2, 15) != nil || castBy(rounds, 3, 15) == nil {
		t.Fatalf("the monster must miss rounds 1 and 2 and act in round 3")
	}
	add := castBy(rounds, 1, 9).Arr("effect_play")[0].Arr("buff_list")
	if len(add) != 1 || add[0].Int("action_type") != 1 || add[0].Int("buff_bid") != 28201 || add[0].Int("id") != 1 ||
		add[0].Int("remain_round") != 2 || add[0].Int("end_round") != 3 || add[0].Int("target") != 15 {
		t.Fatalf("added: %v", add)
	}
	tick := rounds[1].start[0].Arr("effect_play")[0].Arr("buff_list")
	if len(tick) != 1 || tick[0].Int("action_type") != 3 || tick[0].Int("remain_round") != 1 {
		t.Fatalf("round 2 tick: %v", tick)
	}
	gone := rounds[2].start[0].Arr("effect_play")[0].Arr("buff_list")
	if len(gone) != 1 || gone[0].Int("action_type") != 2 || gone[0].Int("remain_round") != 0 {
		t.Fatalf("round 3 removal: %v", gone)
	}
}

// damage over time: each round start costs the caster's attack times the percent,
// and it stacks (two casts, two statuses with their own ids).
func TestFightDamageOverTime(t *testing.T) {
	withSkill(t, 990002, skillRule{Name: "burn", Type: "active_skill", CD: 0, FireCD: 1, Side: 1, How: "front", N: 1, Dmg: 5,
		Buffs: []buffRule{{Bid: 21001, Name: "灼烧", Kind: "dot", Pct: 15, Rounds: 2, Stack: true, Negative: true, Chance: 100}}})
	hero := fightUnit{pos: 9, speed: 90, hp: 5000, maxHP: 5000, atk: 200, skills: []int64{990002, gentle(t)}, ours: true}
	rounds := fixedFight([]fightUnit{hero}, []fightUnit{foe(15, 100000, 990010)}, 1)
	// the status of round 1 ticks in round 2 for 15% of 200 = 30
	e := rounds[1].start[0].Arr("effect_play")[0]
	if e.Int("target") != 15 || e.Int("hp_changed") != -30 {
		t.Fatalf("round 2 dot: %v", e)
	}
	// round 2 cast a second one: round 3 start has two statuses, 60 damage, different ids
	e = rounds[2].start[0].Arr("effect_play")[0]
	ids := map[int64]bool{}
	for _, b := range e.Arr("buff_list") {
		ids[b.Int("id")] = true
	}
	if e.Int("hp_changed") != -60 || len(ids) != 2 {
		t.Fatalf("round 3 dot: %v", e)
	}
}

// a shield takes one hit: that hit changes nothing and the shield is removed.
func TestFightShieldBlocksOneHit(t *testing.T) {
	withSkill(t, 990003, skillRule{Name: "shield", Type: "active_skill", CD: 9, FireCD: 1, Side: 2, How: "min_hp", N: 1, Heal: 100,
		Buffs: []buffRule{{Bid: 12501, Name: "格挡盾", Kind: "shield", Rounds: 99, Chance: 100}}})
	// the healer is hurt, so it heals (and shields) itself in round 1
	healer := fightUnit{pos: 6, speed: 90, hp: 500, maxHP: 900, atk: 100, skills: []int64{990003, 228001}, ours: true}
	atk := foe(15, 100000, 267001)
	rounds := fixedFight([]fightUnit{healer}, []fightUnit{atk}, 1)
	sh := castBy(rounds, 1, 6).Arr("effect_play")[0].Arr("buff_list")
	if len(sh) != 1 || sh[0].Int("buff_bid") != 12501 || sh[0].Int("remain_round") != 1 || sh[0].Int("end_round") != shieldEnd {
		t.Fatalf("shield entry: %v", sh)
	}
	hit := castBy(rounds, 1, 15).Arr("effect_play")[0]
	if hit.Int("hp_changed") != 0 || hit.Int("target") != 6 {
		t.Fatalf("the first hit must be blocked: %v", hit)
	}
	if rm := hit.Arr("buff_list"); len(rm) != 1 || rm[0].Int("action_type") != 2 {
		t.Fatalf("shield removal: %v", rm)
	}
	if next := castBy(rounds, 2, 15).Arr("effect_play")[0]; next.Int("hp_changed") >= 0 {
		t.Fatalf("the second hit must land: %v", next)
	}
}

// attack down lowers the damage of the unit that carries it (the floor that keeps
// real fights short is off here: maxEnemy 0).
func TestFightAttackDown(t *testing.T) {
	hero := fightUnit{pos: 9, speed: 10, hp: 5000, maxHP: 5000, atk: 1000, skills: []int64{200001}, ours: true, lastCast: map[int64]int{}}
	tank := fightUnit{pos: 15, speed: 10, hp: 1 << 40, maxHP: 1 << 40, atk: 5, skills: []int64{200001}, lastCast: map[int64]int{}}
	f := &fightSim{units: []fightUnit{hero, tank}, rng: rand.New(rand.NewSource(1)), round: 1}
	hit := func() int64 {
		return f.act(&f.units[0], 2).Arr("effect_play")[0].Int("hp_changed")
	}
	plain := hit()
	f.units[0].buffs = []*activeBuff{{id: 1, bid: 23203, kind: "atk_down", pct: 20, remain: 2}}
	weak := hit()
	if plain != -1000 || weak != -800 {
		t.Fatalf("plain %d, weakened %d (want -1000 and -800)", plain, weak)
	}
	if atkOf(&f.units[0]) != 800 {
		t.Fatalf("atkOf %d", atkOf(&f.units[0]))
	}
}

// simOf makes a script state to try the rules of one turn; the units have no
// statuses and no floor on the damage (maxEnemy 0).
func simOf(seed int64, units ...fightUnit) *fightSim {
	f := &fightSim{units: units, rng: rand.New(rand.NewSource(seed)), round: 1}
	for i := range f.units {
		f.units[i].lastCast = map[int64]int{}
	}
	return f
}

func buffOn(u *fightUnit, kind string, pct int64, remain int) *activeBuff {
	b := &activeBuff{id: int64(len(u.buffs) + 1), bid: 4000 + int64(len(u.buffs)), kind: kind, pct: pct, remain: remain}
	u.buffs = append(u.buffs, b)
	return b
}

func swing(f *fightSim, i int) proto.Message {
	return f.act(&f.units[i], 2).Arr("effect_play")[0]
}

// attack up and damage up make the unit hit harder, damage reduce makes the target
// take less (never more than 90% less).
func TestFightDamageStatuses(t *testing.T) {
	hero := fightUnit{pos: 9, speed: 10, hp: 5000, maxHP: 5000, atk: 1000, skills: []int64{200001}, ours: true}
	tank := fightUnit{pos: 15, speed: 10, hp: 1 << 40, maxHP: 1 << 40, atk: 5, skills: []int64{200001}}
	f := simOf(1, hero, tank)
	if got := swing(f, 0).Int("hp_changed"); got != -1000 {
		t.Fatalf("plain hit %d", got)
	}
	buffOn(&f.units[0], "atk_up", 50, 2)
	if got := swing(f, 0).Int("hp_changed"); got != -1500 {
		t.Fatalf("attack up 50%%: %d", got)
	}
	buffOn(&f.units[0], "dmg_up", 20, 2)
	if got := swing(f, 0).Int("hp_changed"); got != -1800 { // 1500 x 120%
		t.Fatalf("damage up 20%%: %d", got)
	}
	buffOn(&f.units[1], "dmg_reduce", 50, 2)
	if got := swing(f, 0).Int("hp_changed"); got != -900 {
		t.Fatalf("damage reduce 50%%: %d", got)
	}
	buffOn(&f.units[1], "dmg_reduce", 90, 2) // 140% in all: the floor of 10% holds
	if got := swing(f, 0).Int("hp_changed"); got != -180 {
		t.Fatalf("damage reduce capped: %d", got)
	}
}

// crit up raises the chance (a client percent is ten per mille) and crit damage the ratio.
func TestFightCritStatuses(t *testing.T) {
	hero := fightUnit{pos: 9, speed: 10, hp: 5000, maxHP: 5000, atk: 1000, crit: 0, critRatio: 1500, skills: []int64{200001}, ours: true}
	tank := fightUnit{pos: 15, speed: 10, hp: 1 << 40, maxHP: 1 << 40, atk: 5, skills: []int64{200001}}
	f := simOf(1, hero, tank)
	if e := swing(f, 0); e.Int("is_crit") != 0 {
		t.Fatalf("no crit chance: %v", e)
	}
	buffOn(&f.units[0], "crit_up", 100, 2) // +100% = always
	if e := swing(f, 0); e.Int("is_crit") != 1 || e.Int("hp_changed") != -1500 {
		t.Fatalf("crit up: %v", e)
	}
	buffOn(&f.units[0], "crit_dmg_up", 50, 2) // x1.5 -> x2.0
	if e := swing(f, 0); e.Int("is_crit") != 1 || e.Int("hp_changed") != -2000 {
		t.Fatalf("crit damage up: %v", e)
	}
}

// speed up moves a unit earlier in the order of a round.
func TestFightSpeedUp(t *testing.T) {
	fast := fightUnit{speed: 15}
	slow := fightUnit{speed: 10}
	if speedOf(&slow) >= speedOf(&fast) {
		t.Fatal("setup")
	}
	buffOn(&slow, "speed_up", 100, 2)
	if speedOf(&slow) != 20 || speedOf(&slow) <= speedOf(&fast) {
		t.Fatalf("speed %d", speedOf(&slow))
	}
}

// a pure status skill on the caster: one effect on itself with no hp change and a
// status entry; it is not cast again while the status lasts (a normal attack
// instead) and is cast again once it has run out.
func TestFightSelfBuffSkill(t *testing.T) {
	withSkill(t, 990021, skillRule{Name: "rally", Type: "active_skill", CD: 0, FireCD: 1, Side: 5, How: "self", N: 1,
		Buffs: []buffRule{{Bid: 4002, Name: "攻击提升", Kind: "atk_up", Pct: 25, Rounds: 2, Chance: 100}}})
	hero := fightUnit{pos: 9, speed: 90, hp: 5000, maxHP: 5000, atk: 100, skills: []int64{990021, gentle(t)}, ours: true}
	rounds := fixedFight([]fightUnit{hero}, []fightUnit{foe(15, 100000, 990010)}, 1)
	if len(rounds) < 4 {
		t.Fatalf("only %d rounds", len(rounds))
	}
	p := castBy(rounds, 1, 9)
	e := p.Arr("effect_play")[0]
	buf := e.Arr("buff_list")
	if p.Int("skill_bid") != 990021 || e.Int("target") != 9 || e.Int("hp_changed") != 0 ||
		len(buf) != 1 || buf[0].Int("action_type") != 1 || buf[0].Int("buff_bid") != 4002 || buf[0].Int("remain_round") != 2 || buf[0].Int("target") != 9 {
		t.Fatalf("self buff play: %v", p)
	}
	if castBy(rounds, 2, 9).Int("skill_bid") != 990010 {
		t.Fatalf("round 2 must be a normal attack while the status lasts: %v", castBy(rounds, 2, 9))
	}
	if castBy(rounds, 3, 9).Int("skill_bid") != 990021 {
		t.Fatalf("round 3 casts it again: %v", castBy(rounds, 3, 9))
	}
}

// a regen status heals at every round start, up to the full health, from the
// caster's attack or its health cap.
func TestFightRegen(t *testing.T) {
	withSkill(t, 990020, skillRule{Name: "regen", Type: "active_skill", CD: 9, FireCD: 1, Side: 5, How: "self", N: 1,
		Buffs: []buffRule{{Bid: 4001, Name: "持续回复", Kind: "regen", Pct: 10, Base: "atk", Rounds: 3, Chance: 100}}})
	hero := fightUnit{pos: 9, speed: 90, hp: 300, maxHP: 1000, atk: 200, skills: []int64{990020, gentle(t)}, ours: true}
	rounds := fixedFight([]fightUnit{hero}, []fightUnit{foe(15, 100000, 990010)}, 1)
	// the monster hits for 1 in round 1; round 2 starts with 10% of 200 = 20 back
	e := rounds[1].start[0].Arr("effect_play")[0]
	if e.Int("target") != 9 || e.Int("hp_changed") != 20 || e.Int("hp") != 319 {
		t.Fatalf("round 2 regen: %v", e)
	}
	if b := e.Arr("buff_list"); len(b) != 1 || b[0].Int("action_type") != 3 || b[0].Int("remain_round") != 2 {
		t.Fatalf("regen tick: %v", e.Arr("buff_list"))
	}
	// at the health cap nothing is added
	sim := simOf(1, fightUnit{pos: 9, speed: 10, hp: 995, maxHP: 1000, atk: 200, ours: true}, foe(15, 10))
	b := buffOn(&sim.units[0], "regen", 10, 3)
	b.casterAtk = 200
	sim.round = 2
	if e := sim.startOfRound()[0].Arr("effect_play")[0]; e.Int("hp") != 1000 || e.Int("hp_changed") != 5 {
		t.Fatalf("regen at the cap: %v", e)
	}
}

// purge removes negative statuses (the newest first), but not the ones that cannot
// be purged, and it needs something to remove.
func TestFightPurge(t *testing.T) {
	withSkill(t, 990022, skillRule{Name: "cleanse", Type: "active_skill", CD: 0, FireCD: 1, Side: 2, How: "min_hp", N: 1, Purge: 1, PurgeChance: 100})
	healer := fightUnit{pos: 6, speed: 90, hp: 900, maxHP: 900, atk: 100, skills: []int64{990022, 200001}, ours: true}
	ally := fightUnit{pos: 9, speed: 10, hp: 100, maxHP: 900, atk: 10, skills: []int64{200001}, ours: true}
	f := simOf(1, healer, ally, foe(15, 1<<40, 200001))
	// two negatives (a stun older, a burn newer) and one that cannot be purged
	stun := buffOn(&f.units[1], "stun", 0, 2)
	stun.negative = true
	keep := buffOn(&f.units[1], "dot", 15, 2)
	keep.negative, keep.keep = true, true
	burn := buffOn(&f.units[1], "dot", 15, 2)
	burn.negative = true
	e := swing(f, 0)
	gone := e.Arr("buff_list")
	if e.Int("target") != 9 || len(gone) != 1 || gone[0].Int("action_type") != 2 || gone[0].Int("id") != burn.id {
		t.Fatalf("purge: %v", e)
	}
	if len(f.units[1].buffs) != 2 {
		t.Fatalf("two statuses must stay: %d", len(f.units[1].buffs))
	}
	// the stun goes next, the unpurgeable one never
	f.units[0].lastCast = map[int64]int{}
	swing(f, 0)
	if f.units[1].has("stun") != nil || !f.units[1].hasBid(keep.bid) {
		t.Fatalf("after two purges: %+v", f.units[1].buffs)
	}
	// nothing left to remove: a normal attack instead
	f.units[0].lastCast = map[int64]int{}
	if p := f.act(&f.units[0], 2); p.Int("skill_bid") != 200001 {
		t.Fatalf("nothing to purge, got skill %d", p.Int("skill_bid"))
	}
}

// the pure status skills of the real skill table are read the way their text says.
func TestSkillTablePureSkills(t *testing.T) {
	check := func(sid int64, side int, how string, n int, kind string, pct int64) skillRule {
		t.Helper()
		r, ok := skillTable[sid]
		if !ok || !r.pure() || r.Side != side || r.How != how || r.N != n {
			t.Fatalf("skill %d: %+v", sid, r)
		}
		for _, b := range r.Buffs {
			if b.Kind == kind && b.Pct == pct && b.plays() && !b.Negative {
				return r
			}
		}
		t.Fatalf("skill %d lacks %s %d%%: %+v", sid, kind, pct, r.Buffs)
		return r
	}
	if r := check(216301, 2, "max_atk", 2, "atk_up", 15); r.Purge != 1 || r.PurgeChance != 100 { // 幽冥公主: purge 1, attack +15%
		t.Fatalf("216301 purge: %+v", r)
	}
	check(240301, 5, "self", 1, "dmg_reduce", 15) // 模仿伪装
	check(200301, 5, "self", 1, "dmg_reduce", 20) // 天空护
	check(274101, 2, "random", 1, "atk_up", 25)   // 大舞刀 (a wave monster)
	check(274101, 2, "random", 1, "crit_dmg_up", 20)
	if r := skillTable[245301]; r.Heal != 146 || r.Purge != 99 || r.PurgeChance != 60 || r.How != "min_hp" || r.N != 3 { // 生命释放: heal 3 and purge
		t.Fatalf("245301: %+v", r)
	}
}

// the script says whether the heroes won: a lost fight is every hero down, or
// monsters still standing after the round limit.
func TestFightOutcome(t *testing.T) {
	hero := fightUnit{pos: 9, speed: 80, hp: 5000, maxHP: 5000, atk: 100, skills: []int64{200001}, ours: true}
	if rounds, won := buildFight([]fightUnit{hero}, []fightUnit{foe(15, 400, 200001)}, rand.New(rand.NewSource(1))); !won || len(rounds) == 0 {
		t.Fatalf("an easy fight must be won (%d rounds)", len(rounds))
	}
	// a hero with 1 hp against a monster that always hits: down in round 1
	withSkill(t, 990030, skillRule{Name: "smash", Normal: 1, Type: "active_skill", Side: 1, How: "front", N: 1, Dmg: 1000000})
	frail := fightUnit{pos: 9, speed: 10, hp: 1, maxHP: 1, atk: 1, skills: []int64{200001}, ours: true}
	killer := fightUnit{pos: 15, speed: 90, hp: 100000, maxHP: 100000, atk: 5, skills: []int64{990030}}
	if rounds, won := buildFight([]fightUnit{frail}, []fightUnit{killer}, rand.New(rand.NewSource(1))); won || len(rounds) != 1 {
		t.Fatalf("a dead team loses at once: %d rounds, won %v", len(rounds), won)
	}
	// weak hits against a big monster: the round limit ends it
	gentle(t)
	weak := fightUnit{pos: 9, speed: 80, hp: 1 << 40, maxHP: 1 << 40, atk: 1, skills: []int64{990010}, ours: true}
	big := foe(15, 100000, 990010)
	if rounds, won := buildFight([]fightUnit{weak}, []fightUnit{big}, rand.New(rand.NewSource(1))); won || len(rounds) != maxFightRounds {
		t.Fatalf("round limit: %d rounds, won %v", len(rounds), won)
	}
}

// monsters that kill any hero in one hit, for the lost-fight tests.
func deadlyMonsters(t *testing.T, dun int64) {
	for _, m := range dungeonUnits[dun].Monsters {
		for _, sid := range m.Skills {
			withSkill(t, sid, skillRule{Name: "smash", Normal: 1, Type: "active_skill", Side: 1, How: "front", N: 1, Dmg: 1000000})
		}
	}
}

// easyDungeons gives every dungeon the monster of the first one for a test: a
// test that walks through many fights must not depend on the dice of real ones.
func easyDungeons(t *testing.T) {
	saved := map[int64]dungeonUnit{}
	easy := dungeonUnits[dramaChain.First]
	for id := range dungeonUnits {
		saved[id] = dungeonUnits[id]
		dungeonUnits[id] = easy
	}
	t.Cleanup(func() {
		for id, u := range saved {
			dungeonUnits[id] = u
		}
	})
}

// a lost fight shows the defeat result (13016 with a result other than 1), pays
// nothing and does not move the campaign on.
func TestLostFightPaysNothing(t *testing.T) {
	const dun = 10060 // three monsters of level 3: the hero cannot kill them all in the first round
	deadlyMonsters(t, dun)
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	d := dungeonByID[dun]
	if err := e.st.SaveDrama(store.Drama{RID: role.RID, Mode: d.Mode, ChapterID: d.Chapter, DunID: dun, Status: dungeonNotCleared, MaxDunID: dun}); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(13003, proto.Message{"is_auto": 0})
	c.recv(13003)
	res := c.playFight()
	if res.Int("result") == 1 || len(res.Arr("item_rewards")) != 0 || res.Int("combat_type") != combatDrama {
		t.Fatalf("defeat result: %v", res)
	}
	if len(res.Arr("hurt_statistics")) == 0 {
		t.Fatalf("the result window reads the damage table: %v", res)
	}
	c.expectNothing() // no 13001, no assets: the campaign did not move
	after, _ := e.st.RoleAssets(role.RID)
	if len(before) != len(after) || after[1] != before[1] {
		t.Fatalf("assets moved: %v -> %v", before, after)
	}
	if got, err := e.st.Drama(role.RID); err != nil || got.DunID != dun {
		t.Fatalf("progress moved: %+v %v", got, err)
	}
	// the dungeon can be challenged again
	c.send(13003, proto.Message{})
	if r := c.recv(13003); r.Int("code") != 1 {
		t.Fatalf("13003 after a loss: %v", r)
	}
}

// the skip button (20062) ends the running fight at once with the script's result.
func TestSkipFight(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	first := dramaChain.Dungeons[0]

	// without a fight it is refused
	c.send(20062, proto.Message{})
	if r := c.recv(20062); r.Int("code") != 0 || r.Str("msg") == "" {
		t.Fatalf("skip without a fight: %v", r)
	}

	c.send(13003, proto.Message{"is_auto": 0})
	c.recv(13003)
	c.recv(20063)
	c.recv(20000)
	c.recv(20027)
	c.send(20001, proto.Message{})
	c.recv(20001)
	c.recv(20002)
	c.send(20062, proto.Message{}) // skipped in round 1
	if r := c.recv(20062); r.Int("code") != 1 {
		t.Fatalf("skip: %v", r)
	}
	if res := c.recv(13016); res.Int("result") != 1 || len(res.Arr("item_rewards")) != 4 {
		t.Fatalf("skipped result: %v", res)
	}
	up := c.recv(13001)
	if up.Int("dun_id") != first.Next {
		t.Fatalf("13001: %v", up)
	}
	c.recv(10302)
	c.recv(10500)
	// the fight is over: late confirmations of the client change nothing, and a second skip is refused
	c.send(20005, proto.Message{})
	c.send(20019, proto.Message{})
	c.expectNothing()
	c.send(20062, proto.Message{})
	if r := c.recv(20062); r.Int("code") != 0 {
		t.Fatalf("second skip: %v", r)
	}
	if d, _ := e.st.Drama(role.RID); d.DunID != first.Next {
		t.Fatalf("stored: %+v", d)
	}
}
