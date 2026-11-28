package game

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestCampaignSpeedWornSkillsAndSets(t *testing.T) {
	h := store.Partner{BID: 30512, PartnerID: 1, Star: 5, Lev: 1, ResSlot: 1, ShownLev: 81, ShownBreak: 1,
		Talents: []store.Talent{{SkillID: 700075}}, Artifacts: []store.ArtPiece{{Skills: []int64{700073, 700076}, Pending: []int64{800081}}},
		Equips: []store.Equip{{Slot: 1, BaseID: 40122}, {Slot: 2, BaseID: 40222}},
		Holy:   []store.HolyPiece{{Base: 13010001, Slot: 1}, {Base: 13010002, Slot: 2}, {Base: 13010003, Slot: 3}, {Base: 13010004, Slot: 4}}}
	// Super Swift +40 and half effective level, +15%; two Emperor +10%, two/four Holy +3% each.
	if got := wornSpeed(h); got != (speedBonus{Flat: 40, Level: 500, Per: 310}) {
		t.Fatalf("worn modifiers %+v", got)
	}
	_, _, _, base := partnerStats(h)
	team := func() fightUnit {
		us, _ := sideUnits(1, "local", []store.FormationSlot{{Pos: 1, PartnerID: 1}}, 1, map[int64]store.Partner{1: h}, true)
		return us[0]
	}
	u := team()
	if !slices.Contains(u.skills, int64(527201)) || u.speed != (base+40+40+10)*1310/1000 {
		t.Fatalf("unlocked skill / effective level: speed=%d, base=%d, skills=%v", u.speed, base, u.skills)
	}
	h.ShownBreak = 0
	u = team()
	if slices.Contains(u.skills, int64(527201)) {
		t.Fatal("locked hero passive added")
	}
	if got := skillSpeed([]int64{759201}); got.Flat != 10 || got.Per != 0 {
		t.Fatalf("Sniper King's original fixed points: %+v", got)
	}
	if got := wornSpeed(store.Partner{Talents: []store.Talent{{SkillID: 700305}}}); got.Per != -100 {
		t.Fatal(got)
	}
}

func TestCampaignSpeedMixedHolyStars(t *testing.T) {
	h := store.Partner{Holy: []store.HolyPiece{{Base: 13100001}, {Base: 13080002}, {Base: 13050003}, {Base: 13020004}}}
	if got := wornSpeed(h).Per; got != 70 {
		t.Fatalf("2-piece at 8 stars +3%%, 4-piece at 2 stars +4%%: %d", got)
	}
	h.Holy = h.Holy[:3]
	if got := wornSpeed(h).Per; got != 30 {
		t.Fatalf("three mixed same-family pieces: %d", got)
	}
}

func TestCampaignSpeedEquippedRoleTraining(t *testing.T) {
	e := newEnv(t, time.Hour)
	_, role := e.enter("speedtrain", "速度")
	rid := role.RID
	checked := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	checked(e.st.SaveState(rid, hallowsModule, hwState{Hallows: map[string]*hwOne{"1": {ID: 1, Step: 12}, "2": {ID: 2, Step: 11}}}))
	checked(e.st.SaveState(rid, halidomModule, hdState{Halidoms: map[string]*hdOne{"1": {Lev: 1, Step: 3}}}))
	checked(e.st.SaveState(rid, emoModule, emoState{Fruits: map[string]emoFruit{"1": {Level: 2, Star: 1, Juexing: 1}}}))
	checked(e.st.GrantSkin(rid, 111, 1200))
	checked(e.st.GrantLook(rid, store.LookTitle, 91307, 1200))
	checked(e.st.UseLook(rid, store.LookTitle, 91307, 1000))
	read := func(hallow, now int64) campaignTraining {
		c, err := campaignTrainingOf(e.st, rid, store.Formation{HallowsID: hallow}, now)
		checked(err)
		return c
	}
	// The current ice fruit level gives 10 and awakening gives 5. Only the worn level-12 hallow gives 40.
	full := read(1, 1100)
	if full.all != (speedBonus{Flat: 55, Per: 20}) || full.camp[1].Flat != 5 || full.camp[2].Flat != 0 {
		t.Fatalf("%+v, camps=%v", full.all, full.camp)
	}
	if got := full.forHero(store.Partner{BID: 20507, UseSkin: 111}); got.Per != 60 {
		t.Fatal(got)
	}
	if got := full.forHero(store.Partner{BID: 10502, UseSkin: 111}); got.Per != 20 {
		t.Fatal(got)
	}
	for _, id := range []int64{0, 2, 5} {
		expired := read(id, 1200)
		if expired.all != (speedBonus{Flat: 15}) || expired.forHero(store.Partner{BID: 20507, UseSkin: 111}).Per != 0 {
			t.Fatal(id, expired)
		}
	}
	elf := elfinState{Break: 4, Slots: []int64{111001}}
	items := map[int64]int64{111001: 1, 111002: 1}
	helpers := []elfinSlot{{1, 111001}, {5, 111002}, {5, 111002}, {6, 111002}}
	if got := campaignHelperSpeed(elf, helpers, items); got != 12 {
		t.Fatalf("help attribute=%d", got)
	}
	if got := campaignHelperSpeed(elf, helpers, nil); got != 0 {
		t.Fatalf("unowned helper=%d", got)
	}
}

func TestCampaignSpeedPriorityOnlyWhenCasting(t *testing.T) {
	unit := func(pos, sp int64, ours bool, skills ...int64) fightUnit {
		return fightUnit{pos: pos, speed: sp, hp: 100000, maxHP: 100000, atk: 8, ours: ours, skills: skills}
	}
	hawk := unit(1, 20, true, 515001, 515101)
	sanji := unit(10, 10, false, 556001, 556101)
	fast := unit(11, 300, false, 151001)
	r, _, _ := playFightOpts([]fightUnit{hawk}, []fightUnit{sanji, fast}, rand.New(rand.NewSource(10)), 4, true)
	first := func(n int) int64 { return r[n-1].plays[0].Int("actor") }
	if first(1) != 1 || r[0].plays[1].Int("skill_bid") != 556101 || first(2) != 11 || first(3) != 11 || first(4) != 10 {
		t.Fatalf("priority/cooldown: %d %d %d %d", first(1), first(2), first(3), first(4))
	}
	stun := func() *activeBuff { return &activeBuff{bid: 99911, kind: "stun", remain: 2, negative: true} }
	hawk.buffs = []*activeBuff{stun()}
	sanji.buffs = []*activeBuff{stun()}
	r, _, sim := playFightOpts([]fightUnit{hawk}, []fightUnit{sanji, fast}, rand.New(rand.NewSource(21)), 1, true)
	if r[0].plays[0].Int("skill_bid") != 515101 || sim.units[0].has("stun") != nil || len(r[0].plays) != 2 {
		t.Fatal("Hawk must cleanse and act, controlled Sanji must wait")
	}
}

func TestCampaignSpeedNativeStatusesAndReactions(t *testing.T) {
	f := fightSim{round: 1, rng: rand.New(rand.NewSource(7)), units: []fightUnit{
		{pos: 1, ours: true, speed: 100, hp: 100, maxHP: 100, skills: []int64{763401}},
		{pos: 2, ours: true, speed: 100, hp: 100, maxHP: 100, skills: []int64{527201}},
		{pos: 10, speed: 200, hp: 100, maxHP: 100, skills: []int64{517401, 509401}},
	}}
	f.roundSpeedPassives(nil)
	a, b, c := &f.units[0], &f.units[1], &f.units[2]
	if speedOf(a) != 110 || speedOf(b) != 111 || speedOf(c) != 200 || !c.hasBid(11401) || b.stackCount(16101) != 1 {
		t.Fatalf("opening speeds %d %d %d", speedOf(a), speedOf(b), speedOf(c))
	}
	f.orderLocked = true
	for i := 0; i < 8; i++ {
		f.afterSpeedAction(a, 151001, []proto.Message{{"is_hit": 1, "target": c.pos}})
	}
	if c.stackCount(10731) != 5 || speedOf(c) != 220 {
		t.Fatalf("on hit cap %d/%d", c.stackCount(10731), speedOf(c))
	}
	f.grantStatus(a, 23051, speedRule(10741), []*fightUnit{b})
	if speedOf(b) != 123 {
		t.Fatalf("snapshotted caster-scaled speed %d", speedOf(b))
	}
	a.speed = 300
	if speedOf(b) != 123 {
		t.Fatal("caster speed was reevaluated after the cast")
	}
	f.round = 2
	f.orderLocked = false
	f.startOfRound()
	if !b.hasBid(66021) || !b.hasBid(10741) {
		t.Fatal("opening and in-turn effects expired early")
	}
	f.round = 3
	f.startOfRound()
	if b.hasBid(66021) || !b.hasBid(10741) {
		t.Fatal("opening two-round and mid-round two-round effects ended together")
	}
	r := speedRule(23002)
	f.addBuff(a, c, r)
	f.addBuff(a, c, speedRule(23003))
	if speedOf(c) != 190 {
		t.Fatalf("highest slow with five haste stacks: %d", speedOf(c))
	}
}

func TestCampaignSpeedTriggeredStacksAndSideAuras(t *testing.T) {
	f := fightSim{round: 1, orderLocked: true, rng: rand.New(rand.NewSource(91)), realDamage: true, units: []fightUnit{
		{pos: 1, hp: 10000, maxHP: 10000, atk: 2, speed: 100, ours: true, lastCast: map[int64]int{}},
		{pos: 10, hp: 790, maxHP: 1000, atk: 2, speed: 100, camp: 1, skills: []int64{541402}, lastCast: map[int64]int{}},
		{pos: 11, hp: 1000, maxHP: 1000, atk: 2, speed: 200, camp: 4, skills: []int64{778301, 778401}, lastCast: map[int64]int{}},
	}}
	source, marine := &f.units[0], &f.units[2]
	hit := []proto.Message{{"target": 10, "is_hit": 1}}
	for k := 1; k <= 3; k++ {
		effects := f.afterSpeedAction(source, 151101, hit)
		if marine.stackCount(77911) != k {
			t.Fatalf("Niji stacks after %d active casts: %d", k, marine.stackCount(77911))
		}
		counter := false
		for _, e := range effects {
			if e.Int("skill_bid_of_effect") == 778301 {
				counter = true
			}
		}
		if counter != (k == 3) {
			t.Fatalf("passive counter on cast %d: %v", k, counter)
		}
	}
	f.afterSpeedAction(source, 151001, hit)
	if marine.stackCount(77911) != 3 || len(marine.lastCast) != 1 || speedOf(marine) != 206 {
		t.Fatal("a normal turn must not trigger Niji or an immediate marine aura")
	}
	f.round = 2
	f.orderLocked = false
	f.startOfRound()
	f.roundSpeedPassives(nil)
	if speedOf(marine) != 221 || speedOf(source) != 100 || speedOf(&f.units[1]) != 100 {
		t.Fatal("7.5% Asura aura applies to allied Marines only", speedOf(marine))
	}
}

func TestCampaignSpeedConditionalBuffRecipients(t *testing.T) {
	for seed := int64(0); seed < 16; seed++ {
		f := fightSim{round: 1, orderLocked: true, rng: rand.New(rand.NewSource(seed)), units: []fightUnit{
			{pos: 1, hp: 100, ours: true}, {pos: 10, speed: 100, hp: 100, maxHP: 100, sex: 1, skills: []int64{543404}},
			{pos: 11, speed: 100, hp: 100, maxHP: 100},
		}}
		f.afterSpeedAction(&f.units[0], 151001, []proto.Message{{"target": 10, "is_hit": 1}})
		male, unclassified := &f.units[1], &f.units[2]
		if male.hasBid(54345) {
			if speedOf(male) != 110 || speedOf(unclassified) != 100 {
				t.Fatal("encouragement recipients")
			}
		} else if !male.hasBid(54342) || speedOf(male) != 90 || speedOf(unclassified) != 90 {
			t.Fatal("discouragement recipients")
		}
	}
}
