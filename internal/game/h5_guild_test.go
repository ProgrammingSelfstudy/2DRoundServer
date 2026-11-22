package game

import (
	"encoding/json"
	"errors"
	"math/rand"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func enterH5Guild(e *env, accountName, roleName string) (*client, *store.Role) {
	e.t.Helper()
	account, err := e.st.CreateAccount(store.PlatformH5, accountName, "", "")
	if err != nil {
		e.t.Fatal(err)
	}
	role, err := e.st.CreateRole(account.ID, "symlf_1", roleName, 0, 1, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return connectGuildRole(e, accountName, role), role
}

func connectGuildRole(e *env, accountName string, role *store.Role) *client {
	e.t.Helper()
	token, err := e.st.IssueToken(role.AccountID, 3600)
	if err != nil {
		e.t.Fatal(err)
	}
	c := e.dial(true)
	c.send(1164, loginArgs(accountName, token))
	if reply := c.recv(1164); reply.Int("code") != 1 {
		e.t.Fatalf("guild account login: %v", reply)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if reply := c.recv(10192); reply.Int("code") != 1 {
		e.t.Fatalf("guild role login: %v", reply)
	}
	return c
}

func guildView[T any](t *testing.T, c *client, view, career int) T {
	t.Helper()
	c.send(proto.H5GuildView, proto.Message{"view": view, "career": career})
	reply := c.waitFor(proto.H5GuildView)
	if reply.Int("code") != 1 {
		t.Fatalf("guild view: %v", reply)
	}
	var data T
	if err := json.Unmarshal([]byte(reply.Str("data")), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestH5GuildDonationAndSkillEffects(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 11, 26, 12, 0, 0, 0, dayZone))
	c, role := enterH5Guild(e, "guild-skill", "训练船长")
	rules := guildView[struct {
		MinimumLevel, CreationVip, CreationGold int64
		Donations                               []struct{ Cost, Rewards [][2]int64 }
	}](t, c, h5GuildRules, 0)
	if rules.MinimumLevel != 18 || rules.CreationVip != 2 || rules.CreationGold != 100 ||
		len(rules.Donations) != 3 || rules.Donations[0].Cost[0] != [2]int64{1, 20000} {
		t.Fatalf("creation and donation rules: %+v", rules)
	}
	c.send(proto.H5GuildView, proto.Message{"view": h5GuildSkill, "career": 3})
	if denied := c.waitFor(proto.H5GuildView); denied.Int("code") != 0 {
		t.Fatalf("an unaffiliated role got a skill quote: %v", denied)
	}
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetRoleVIP(role.RID, 2); err != nil {
		t.Fatal(err)
	}
	e.grantAssets(c, role, map[int64]int64{1: 50000, 3: 500})
	c.send(13500, proto.Message{"name": "远航协会", "apply_lev": 1, "apply_type": 1})
	if reply := c.waitFor(13500); reply.Int("code") != 1 {
		t.Fatalf("create: %v", reply)
	}
	membership := c.waitFor(10301)
	if membership.Int("gid") == 0 || membership.Int("position") != store.PostLeader {
		t.Fatalf("membership push: %v", membership)
	}
	c.send(13524, proto.Message{"type": 1})
	if reply := c.waitFor(13524); reply.Int("code") != 1 {
		t.Fatalf("donate: %v", reply)
	}
	if got := c.waitFor(13523); got.Int("donate_exp") != 50 || len(got.Arr("donate_list")) != 1 {
		t.Fatalf("donation progress: %v", got)
	}
	c.send(11000, proto.Message{})
	hero := c.waitFor(11000).Arr("partners")[0]
	if hero.Int("bid") != starterH5 || hero.Int("star") != 5 {
		t.Fatalf("H5 starter: %v", hero)
	}
	partnerID, career := hero.Int("partner_id"), partnerTable[hero.Int("bid")].Type
	attributes := func() proto.Message {
		c.send(11063, proto.Message{"partner_id": partnerID})
		return c.waitFor(11063)
	}
	type quotedSkills struct {
		Level, Group, ResetGold int
		Skills                  []struct {
			ID, Increment int64
			Active        bool
			Attribute     string
			Cost          [][2]int64
		}
	}
	before := attributes()
	initial := guildView[quotedSkills](t, c, h5GuildSkill, career)
	if initial.Level != 0 || len(initial.Skills) != 6 || initial.Skills[0].Attribute != "hp_max_per" {
		t.Fatalf("initial career: %+v", initial)
	}
	offer := initial.Skills[0]
	c.send(23701, proto.Message{"skill_id": offer.ID})
	if pushed := c.waitFor(10306); pushed.Int("power") <= hero.Int("power") {
		t.Fatalf("skill failed to raise power: %v, hero %v", pushed, hero)
	}
	if pushed := c.waitFor(11025).Arr("partners")[0]; pushed.Int("power") <= hero.Int("power") {
		t.Fatalf("training failed to refresh the roster: %v", pushed)
	}
	if reply := c.waitFor(23701); reply.Int("code") != 1 {
		t.Fatalf("activate: %v", reply)
	}
	after := attributes()
	if want := before.Int("hp") + before.Int("hp")*offer.Increment/1000; after.Int("hp") != want {
		t.Fatalf("expected HP %d after training, got %d", want, after.Int("hp"))
	}
	c.send(23701, proto.Message{"skill_id": offer.ID})
	if reply := c.waitFor(23701); reply.Int("code") != 0 {
		t.Fatalf("duplicate activation: %v", reply)
	}
	ledger, err := e.st.RoleAssets(role.RID)
	if err != nil || ledger[1] != 24000 || ledger[3] != 400 || ledger[10] != 90 {
		t.Fatalf("ledger after create, donate and activate: %v, %v", ledger, err)
	}
	learned := guildView[quotedSkills](t, c, h5GuildSkill, career)
	if learned.Level != 1 || !learned.Skills[0].Active || learned.ResetGold != 300 {
		t.Fatalf("updated career: %+v", learned)
	}
	c.send(23705, proto.Message{"career": career})
	if reply := c.waitFor(23705); reply.Int("code") != 1 || attributes().Int("hp") != before.Int("hp") {
		t.Fatalf("reset failed to remove the bonus: %v", reply)
	}
	ledger, err = e.st.RoleAssets(role.RID)
	if err != nil || ledger[1] != 27000 || ledger[3] != 100 || ledger[10] != 100 {
		t.Fatalf("ledger after reset: %v, %v", ledger, err)
	}
	e.advance(24 * time.Hour)
	c.send(13523, proto.Message{})
	if got := c.waitFor(13523); got.Int("donate_exp") != 0 || len(got.Arr("donate_list")) != 0 {
		t.Fatalf("donation rollover: %v", got)
	}
}

func TestH5GuildApplicationConflictKeepsPendingRequest(t *testing.T) {
	e := newEnv(t, time.Minute)
	manager, leader := enterH5Guild(e, "guild-approve-lead", "审批会长")
	c, applicant := enterH5Guild(e, "guild-approve-member", "待审船长")
	if err := e.st.SetRoleLevel(applicant.RID, 20); err != nil {
		t.Fatal(err)
	}
	now := e.gw.Now().Unix()
	gid, err := e.st.CreateGuild(store.Guild{SrvID: leader.SrvID, Name: "审批公会", LeaderRID: leader.RID, ApplyType: 1}, now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.send(13503, proto.Message{"gid": gid, "gsrv_id": leader.SrvID, "type": 1})
	if reply := c.waitFor(13503); reply.Int("code") != 1 || reply.Int("is_apply") != 1 {
		t.Fatalf("apply: %v", reply)
	}
	if err := e.st.AcceptGuildApplication(gid, applicant.RID, applicant.RID, now, 0, 2); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a non-manager accepted an application: %v", err)
	}
	if err := e.st.AcceptGuildApplication(gid, leader.RID, applicant.RID, now, 0, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a full guild admitted another member: %v", err)
	}
	if apps, err := e.st.AppliesOf(gid); err != nil || len(apps) != 1 {
		t.Fatalf("application lost: %v, %v", apps, err)
	}
	target := proto.Message{"type": 1, "rid": applicant.RID, "srv_id": applicant.SrvID}
	c.send(13505, target)
	if reply := c.waitFor(13505); reply.Int("code") != 0 {
		t.Fatalf("an applicant approved their own request: %v", reply)
	}
	manager.send(13507, proto.Message{"page": 1, "num": 50})
	if reply := manager.waitFor(13507); len(reply.Arr("guids")) != 1 || reply.Arr("guids")[0].Int("rid") != applicant.RID {
		t.Fatalf("pending applicants: %v", reply)
	}
	manager.send(13505, target)
	if reply := manager.waitFor(13505); reply.Int("code") != 1 {
		t.Fatalf("approve: %v", reply)
	}
	if pushed := c.waitFor(10301); pushed.Int("gid") != gid || pushed.Int("position") != store.PostMember {
		t.Fatalf("membership push after approval: %v", pushed)
	}
	member, err := e.st.MemberOf(applicant.RID)
	if err != nil || member.GID != gid || member.Post != store.PostMember {
		t.Fatalf("membership after approval: %v, %v", member, err)
	}
	if apps, err := e.st.AppliesOf(gid); err != nil || len(apps) != 0 {
		t.Fatalf("approved application still pending: %v, %v", apps, err)
	}
	manager.send(13520, proto.Message{"rid": applicant.RID, "srv_id": applicant.SrvID, "position": store.PostAssistant})
	if reply := manager.waitFor(13520); reply.Int("code") != 1 {
		t.Fatalf("promote: %v", reply)
	}
	if pushed := c.waitFor(10301); pushed.Int("position") != store.PostAssistant {
		t.Fatalf("assistant membership push: %v", pushed)
	}
}

func TestH5GuildBossKeepsRewardsAfterDisconnect(t *testing.T) {
	e := newEnv(t, time.Minute)
	fightServiceOf(e.st).Rand = func() *rand.Rand { return rand.New(rand.NewSource(1)) }
	c, role := enterH5Guild(e, "guild-boss-player", "远航战士")
	observer, member := enterH5Guild(e, "guild-boss-observer", "另一船长")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	now := e.gw.Now().Unix()
	gid, err := e.st.CreateGuild(store.Guild{SrvID: role.SrvID, Name: "远航公会", LeaderRID: role.RID}, now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.JoinGuild(gid, member.RID, now, 15); err != nil {
		t.Fatal(err)
	}
	// A legacy four-star hero raised to five stays out of the H5 guild lineup.
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: starterMale, Star: 5}, {BID: starterH5, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) != 2 {
		t.Fatalf("guild roster: %v, %v", heroes, err)
	}
	c.send(11211, proto.Message{"type": formGuildBoss})
	if slots := c.waitFor(11211).Arr("pos_info"); len(slots) != 1 || slots[0].Int("id") != heroes[1].PartnerID {
		t.Fatalf("H5 guild lineup: %v", slots)
	}
	type bossView struct {
		BossID, MaxHP, BuyIndex, BuyGold int64
		State                            struct {
			Count      int64
			Info       []struct{ HP int64 }
			CombatInfo []struct{ DPS int64 } `json:"combat_info"`
		}
	}
	first := guildView[bossView](t, c, h5GuildBoss, 0)
	if first.BossID != 101 || first.MaxHP != 1000000 || first.State.Count != 2 || first.State.Info[0].HP != first.MaxHP {
		t.Fatalf("initial Boss view: %+v", first)
	}
	c.send(21308, proto.Message{"boss_id": first.BossID, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": heroes[0].PartnerID}}})
	if reply := c.waitFor(21308); reply.Int("code") != 0 {
		t.Fatalf("H5 fight accepted an unlisted hero: %v", reply)
	}
	c.send(21308, proto.Message{"boss_id": first.BossID})
	if reply := c.waitFor(21308); reply.Int("code") != 1 {
		t.Fatalf("challenge: %v", reply)
	}
	opening := c.waitFor(20027)
	if opening.Int("combat_type") != combatGuildDun || opening.Int("a_object_num") != 1 {
		t.Fatalf("unexpected Boss battle: %v", opening)
	}
	for _, unit := range opening.Arr("objects") {
		if unit.Int("object_type") == 2 && unit.Int("object_id") != heroes[1].PartnerID {
			t.Fatalf("unexpected Boss fighter: %v", unit)
		}
	}
	c.c.Close()
	other := guildView[bossView](t, observer, h5GuildBoss, 0)
	if other.State.Count != 2 || other.State.Info[0].HP >= first.MaxHP {
		t.Fatalf("other member should see the damage but keep their two attempts: %+v", other)
	}

	rejoined := connectGuildRole(e, "guild-boss-player", role)
	resumed := guildView[bossView](t, rejoined, h5GuildBoss, 0)
	if resumed.State.Count != 1 || resumed.State.CombatInfo[0].DPS != first.MaxHP-other.State.Info[0].HP {
		t.Fatalf("challenge lost on reconnect: %+v", resumed)
	}
	ledger, err := e.st.RoleAssets(role.RID)
	if err != nil || ledger[10] != 10 || ledger[1] != 20000 {
		t.Fatalf("challenge rewards lost or duplicated: %v, %v", ledger, err)
	}
	rejoined.send(21317, proto.Message{"boss_id": first.BossID})
	if sweep := rejoined.waitFor(21317); sweep.Int("code") != 1 || sweep.Int("all_dps") != resumed.State.CombatInfo[0].DPS {
		t.Fatalf("sweep: %v", sweep)
	}
	e.grantAssets(rejoined, role, map[int64]int64{3: 50})
	rejoined.send(21312, proto.Message{"type": 1})
	if bought := rejoined.waitFor(21312); bought.Int("code") != 1 || bought.Int("buy_count") != 1 || bought.Int("count") != 1 {
		t.Fatalf("attempt purchase: %v", bought)
	}
	next := guildView[bossView](t, rejoined, h5GuildBoss, 0)
	if next.BuyIndex != 1 || next.BuyGold != 30 || next.State.Count != 1 {
		t.Fatalf("next purchase quote: %+v", next)
	}

	rejoined.send(21305, nil)
	if buff := rejoined.waitFor(21305); buff.Int("code") != 1 {
		t.Fatalf("guild attack buff: %v", buff)
	}
	if assets, err := e.st.RoleAssets(role.RID); err != nil || assets[3] != 10 {
		t.Fatalf("gold after buying a challenge and an attack buff: %v, %v", assets, err)
	}
	rejoined.send(21308, proto.Message{"boss_id": first.BossID})
	if reply := rejoined.waitFor(21308); reply.Int("code") != 1 {
		t.Fatalf("buffed challenge: %v", reply)
	}
	rejoined.waitFor(20027)
	rejoined.send(20062, nil)
	rejoined.waitFor(20062)
	if result := rejoined.waitFor(21309); result.Int("all_dps") <= resumed.State.CombatInfo[0].DPS {
		t.Fatalf("attack buff failed to raise damage: %v", result)
	}
}

func TestH5GuildBossHPScalesAcrossWireLimit(t *testing.T) {
	const full = 5913000000
	const maxHP = 1<<32 - 1
	if got := gbossWire(full, full); got != maxHP {
		t.Fatalf("full boss health on the wire: %d", got)
	}
	if got := gbossWire(full/2, full); got != maxHP/2 {
		t.Fatalf("half boss health on the wire: %d", got)
	}
	if got := gbossWire(1, full); got != 1 {
		t.Fatalf("one point of boss health on the wire: %d", got)
	}
}

func TestNativeGuildSkillUsesExistingStats(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("guild-native", "原版船长")
	if _, err := e.st.CreateGuild(store.Guild{SrvID: role.SrvID, Name: "原版公会", LeaderRID: role.RID}, e.gw.Now().Unix(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: starterH5, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	c.send(proto.H5GuildView, proto.Message{"view": h5GuildRules})
	if reply := c.waitFor(proto.H5GuildView); reply.Int("code") != 0 {
		t.Fatalf("native role received the H5 quote: %v", reply)
	}
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) != 1 {
		t.Fatalf("roster: %v, %v", heroes, err)
	}
	attrs := func() proto.Message {
		c.send(11063, proto.Message{"partner_id": heroes[0].PartnerID})
		return c.waitFor(11063)
	}
	before := attrs()
	e.grantAssets(c, role, map[int64]int64{1: 6000, 10: 10})
	c.send(23701, proto.Message{"skill_id": 10011})
	if reply := c.waitFor(23701); reply.Int("code") != 1 {
		t.Fatalf("native skill purchase: %v", reply)
	}
	if after := attrs(); after.Int("hp") != before.Int("hp") {
		t.Fatalf("native stats changed after training: before %v, after %v", before, after)
	}
}

func TestH5GuildBonusesWorkInFight(t *testing.T) {
	hero := store.Partner{BID: starterH5, Lev: 10, Star: 5, PartnerID: 1}
	slots := []store.FormationSlot{{Pos: 1, PartnerID: hero.PartnerID}}
	team := func(hero store.Partner) fightUnit {
		units, _ := sideUnits(1, "symlf_1", slots, 1, map[int64]store.Partner{hero.PartnerID: hero}, true)
		return units[0]
	}
	before := team(hero)
	hero.GuildSkillLevels = map[int]int{3: 6}
	after := team(hero)
	if after.hp <= before.hp || after.atk <= before.atk || after.speed != before.speed+4 ||
		after.crit != before.crit+5 || after.critRatio != before.critRatio+10 || after.guild.damage != 5 {
		t.Fatalf("fight training stats: before %+v, after %+v", before, after)
	}
	mage := skillRule{Description: "造成200%法术伤害"}
	if got := (guildCombatBonus{resistMagic: 20}).resistDamage(mage); got != 20 {
		t.Fatalf("spell damage resistance: %d", got)
	}
	sim := &fightSim{rng: rand.New(rand.NewSource(1)), round: 1}
	stun := buffRule{Kind: "stun", Chance: 100, Rounds: 1}
	if got := sim.addBuff(&fightUnit{}, &fightUnit{hp: 1, guild: guildCombatBonus{resistControl: 1000}}, stun); got != nil {
		t.Fatalf("full control resistance let a stun through: %v", got)
	}
}
