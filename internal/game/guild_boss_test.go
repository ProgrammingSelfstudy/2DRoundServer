package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGuildBossFight(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "会长")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: role.RID}, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.send(21300, proto.Message{})
	info := c.waitFor(21300)
	if info.Int("fid") != 1 || info.Int("count") != 2 || info.Int("max_id") != 0 || len(info.Arr("info")) != 1 || info.Arr("info")[0].Int("boss_id") != 101 || info.Arr("info")[0].Int("hp") != 1000000 {
		t.Fatalf("21300: %v", info)
	}
	c.send(21308, proto.Message{"boss_id": 999, "formation_type": 1, "pos_info": []proto.Message{}, "hallows_id": 0})
	if r := c.waitFor(21308); r.Int("code") != 0 {
		t.Fatalf("wrong boss: %v", r)
	}
	c.send(11000, proto.Message{})
	hero := c.waitFor(11000).Arr("partners")[0]
	c.send(21308, proto.Message{"boss_id": 101, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero.Int("partner_id")}}, "hallows_id": 0})
	if r := c.waitFor(21308); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	intro := c.waitFor(20027)
	if intro.Int("combat_type") != combatGuildDun || intro.Str("target_role_name") == "" {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(21309)
	dmg := res.Int("all_dps")
	if res.Int("bossid") != 101 || dmg <= 0 || len(res.Arr("award_list")) != 2 {
		t.Fatalf("result: %v", res)
	}
	c.send(21300, proto.Message{})
	info = c.waitFor(21300)
	for info.Int("count") != 1 { // a push from the settling comes first
		info = c.waitFor(21300)
	}
	if info.Arr("info")[0].Int("hp") != 1000000-dmg || info.Arr("combat_info")[0].Int("dps") != dmg {
		t.Fatalf("hp after: %v (damage %d)", info, dmg)
	}
	// the mop-up repeats the damage
	c.send(21317, proto.Message{"boss_id": 101})
	m := c.waitFor(21317)
	if m.Int("code") != 1 || m.Int("all_dps") != dmg {
		t.Fatalf("mop-up: %v", m)
	}
	// no challenges left: a third is refused until one is bought
	c.send(21308, proto.Message{"boss_id": 101, "formation_type": 1, "pos_info": []proto.Message{}, "hallows_id": 0})
	if r := c.waitFor(21308); r.Int("code") != 0 {
		t.Fatalf("out of challenges: %v", r)
	}
	c.send(21312, proto.Message{"type": 1})
	if r := c.waitFor(21312); r.Int("code") != 0 {
		t.Fatalf("no gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(21312, proto.Message{"type": 1})
	if r := c.waitFor(21312); r.Int("code") != 1 || r.Int("count") != 1 || r.Int("buy_count") != 1 {
		t.Fatalf("buy: %v", r)
	}
	// rankings: below the minimum damage nobody is listed
	c.send(21319, proto.Message{"boss_id": 101, "start_num": 1, "end_num": 20})
	r := c.waitFor(21319)
	if dmg >= 2000 && (r.Int("rank") != 1 || len(r.Arr("rank_list")) != 1) || dmg < 2000 && len(r.Arr("rank_list")) != 0 {
		t.Fatalf("damage ranking: %v", r)
	}
	c.send(21318, proto.Message{})
	if g := c.waitFor(21318); g.Str("my_name") != "海贼联盟" || g.Int("my_rank") != 1 || len(g.Arr("rank_guild")) != 1 {
		t.Fatalf("guild ranking: %v", g)
	}
}

func TestGuildBossKillBuffAndReset(t *testing.T) {
	e := newEnv(t, 1000*time.Hour)
	c, role := e.enter("ua", "会长")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: role.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// make the boss nearly dead so one fight kills it
	c.send(21300, proto.Message{})
	c.waitFor(21300)
	st, _, _ := e.st.BossStateOf(gid)
	st.HP = 6000
	if err := e.st.SaveBossState(st); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := e.st.BossHit(gid, st.Day, 101, role.RID, 5500); err != nil { // enough damage to be ranked
		t.Fatal(err)
	}
	// the buff: gems, then the item
	c.send(21305, proto.Message{})
	if r := c.waitFor(21305); r.Int("code") != 0 {
		t.Fatalf("no gems for the buff: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(21305, proto.Message{})
	if r := c.waitFor(21305); r.Int("code") != 1 {
		t.Fatalf("buff: %v", r)
	}
	info := c.waitFor(21300)
	if info.Int("buff_lev") != 1 || info.Int("buff_end_time") <= 0 {
		t.Fatalf("buff in 21300: %v", info)
	}
	c.send(21308, proto.Message{"boss_id": 101, "formation_type": 1, "pos_info": []proto.Message{}, "hallows_id": 0})
	c.waitFor(21308)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(21309)
	if len(res.Arr("award_list")) != 3 { // the challenge reward (2) and the kill reward
		t.Fatalf("kill result: %v", res)
	}
	after, _, _ := e.st.BossStateOf(gid)
	if after.HP != 0 || after.MaxID != 1 {
		t.Fatalf("boss after the kill: %+v", after)
	}
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	var ranked int
	for _, m := range mails {
		if m.Subject == "公会副本排名奖励" && len(m.Attachments) == 2 {
			ranked++
		}
	}
	if ranked != 1 {
		t.Fatalf("ranking mail: %+v", mails)
	}
	gd, _ := e.st.GuildByID(gid)
	if gd.Exp != 100 {
		t.Fatalf("guild exp %d", gd.Exp)
	}
	// tomorrow: the next chapter, or with the rollback the same one
	after.ResetType = 1
	if err := e.st.SaveBossState(after); err != nil {
		t.Fatal(err)
	}
	c.settle()
	e.advance(24 * time.Hour)
	c.send(21300, proto.Message{})
	next := c.waitFor(21300)
	if next.Int("fid") != 1 || next.Arr("info")[0].Int("hp") != 1000000 {
		t.Fatalf("rollback to the same chapter: %v", next)
	}
	c.settle()
	e.advance(24 * time.Hour)
	c.send(21300, proto.Message{})
	if n := c.waitFor(21300); n.Int("fid") != 2 {
		t.Fatalf("normal reset: %v", n)
	}
}

// settle reads everything the server has pushed so far, up to a heartbeat answer.
func (c *client) settle() {
	c.t.Helper()
	c.send(1169, proto.Message{})
	for i := 0; i < 50; i++ {
		if cmd, _ := c.recvNext(); cmd == 1169 {
			return
		}
	}
	c.t.Fatal("no heartbeat answer")
}
