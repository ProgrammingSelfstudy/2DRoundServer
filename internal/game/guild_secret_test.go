package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func secretEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 1000*time.Hour)
	c, role := e.enter("ua", "会长")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: role.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, c, role, gid
}

func TestGuildSecretOpenFightAndSettle(t *testing.T) {
	e, c, role, gid := secretEnv(t)
	c.send(26800, proto.Message{})
	info := c.waitFor(26800)
	if info.Int("bid") != 0 || len(info.Arr("boss_list")) != 5 || info.Arr("boss_list")[0].Int("boss_id") != 91001 || info.Int("count") != 2 {
		t.Fatalf("26800: %v", info)
	}
	c.send(26801, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26801); r.Int("flag") != 0 {
		t.Fatalf("no vitality: %v", r)
	}
	if err := e.st.AddGuildVitality(gid, 2000); err != nil {
		t.Fatal(err)
	}
	c.send(26801, proto.Message{"boss_id": 91002}) // not the line's boss yet
	if r := c.waitFor(26801); r.Int("flag") != 0 {
		t.Fatalf("a higher difficulty: %v", r)
	}
	c.send(26801, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26801); r.Int("flag") != 1 {
		t.Fatalf("open: %v", r)
	}
	c.send(26810, proto.Message{})
	if v := c.waitFor(26810); v.Int("vitality") != 500 {
		t.Fatalf("vitality after paying 1500: %v", v)
	}
	c.send(26800, proto.Message{})
	var open proto.Message
	for open == nil || open.Int("bid") == 0 {
		open = c.waitFor(26800)
	}
	if open.Int("hp") < 9900000 || open.Int("hp") > 11000000 || open.Int("hp") != open.Int("max_hp") || open.Int("end_time") <= 0 {
		t.Fatalf("running crusade: %v", open)
	}
	c.send(26807, proto.Message{})
	if r := c.waitFor(26807); r.Int("flag") != 0 || r.Int("end_time") != open.Int("end_time") {
		t.Fatalf("start notice: %v", r)
	}
	c.send(26807, proto.Message{})
	if r := c.waitFor(26807); r.Int("flag") != 1 {
		t.Fatalf("start notice twice: %v", r)
	}
	// a fight
	c.send(11000, proto.Message{})
	hero := c.waitFor(11000).Arr("partners")[0]
	c.send(26802, proto.Message{"boss_id": 91002, "formation_type": 1, "pos_info": []proto.Message{}, "hallows_id": 0})
	if r := c.waitFor(26802); r.Int("flag") != 0 {
		t.Fatalf("other boss: %v", r)
	}
	c.send(26802, proto.Message{"boss_id": 91001, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero.Int("partner_id")}}, "hallows_id": 0})
	if r := c.waitFor(26802); r.Int("flag") != 1 || r.Int("combat_type") != combatGuildSecret {
		t.Fatalf("challenge: %v", r)
	}
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(26809)
	if res.Int("result") != 1 || len(res.Arr("reward")) != 2 {
		t.Fatalf("result: %v", res)
	}
	st, _ := e.st.SecretStateOf(gid)
	dmg, _ := e.st.SecretDamages(gid, st.Seq)
	if len(dmg) != 1 || dmg[0].Total <= 0 || st.HP != st.MaxHP-dmg[0].Total {
		t.Fatalf("damage %v, state %+v", dmg, st)
	}
	// mop-up repeats it; the third challenge is refused, then bought
	c.send(26805, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26805); r.Int("flag") != 1 {
		t.Fatalf("mop-up: %v", r)
	}
	c.waitFor(26809)
	c.send(26805, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26805); r.Int("flag") != 0 {
		t.Fatalf("out of challenges: %v", r)
	}
	c.send(26803, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26803); r.Int("flag") != 0 {
		t.Fatalf("no gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 200})
	c.send(26803, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26803); r.Int("flag") != 1 || r.Int("count") != 1 {
		t.Fatalf("buy: %v", r)
	}
	// the ranking
	c.send(26806, proto.Message{"boss_id": 91001, "num": 10})
	if r := c.waitFor(26806); len(r.Arr("dps_list")) != 1 || r.Arr("dps_list")[0].Int("rank") != 1 || r.Arr("dps_list")[0].Str("name") != "会长" {
		t.Fatalf("ranking: %v", r)
	}
	// a chest needs the hp down to its stage: make it so
	c.send(26804, proto.Message{"boss_id": 91001, "number": 3})
	if r := c.waitFor(26804); r.Int("flag") != 0 {
		t.Fatalf("chest too early: %v", r)
	}
	st.HP = st.MaxHP * 5 / 10
	if err := e.st.SaveSecretState(st); err != nil {
		t.Fatal(err)
	}
	c.send(26804, proto.Message{"boss_id": 91001, "number": 3})
	if r := c.waitFor(26804); r.Int("flag") != 1 || r.Int("number") != 3 {
		t.Fatalf("chest: %v", r)
	}
	c.send(26804, proto.Message{"boss_id": 91001, "number": 3})
	if r := c.waitFor(26804); r.Int("flag") != 0 {
		t.Fatalf("chest twice: %v", r)
	}
	// the crusade ends with the boss alive: it failed, the line stays, the ranking reward comes by mail
	c.settle()
	e.advance(25 * time.Hour)
	c.send(26808, proto.Message{})
	rep := c.waitFor(26808)
	if rep.Int("flag") != 0 || len(rep.Arr("boss_reward")) == 0 {
		t.Fatalf("end report: %v", rep)
	}
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	var ranked int
	for _, m := range mails {
		if m.Subject == "公会秘境排名奖励" {
			ranked++
		}
	}
	if ranked != 1 {
		t.Fatalf("ranking mails: %d", ranked)
	}
	// no new crusade during the cool-down, one after it
	c.send(26801, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26801); r.Int("flag") != 0 {
		t.Fatalf("cool-down: %v", r)
	}
	c.settle()
	e.advance(24 * time.Hour)
	if err := e.st.AddGuildVitality(gid, 2000); err != nil {
		t.Fatal(err)
	}
	c.send(26801, proto.Message{"boss_id": 91001})
	if r := c.waitFor(26801); r.Int("flag") != 1 {
		t.Fatalf("second crusade: %v", r)
	}
}

func TestGuildSecretKillRaisesTheLine(t *testing.T) {
	e, c, _, gid := secretEnv(t)
	if err := e.st.AddGuildVitality(gid, 2000); err != nil {
		t.Fatal(err)
	}
	c.send(26801, proto.Message{"boss_id": 91001})
	c.waitFor(26801)
	st, _ := e.st.SecretStateOf(gid)
	st.HP, st.MaxHP = 100, 10000000
	if err := e.st.SaveSecretState(st); err != nil {
		t.Fatal(err)
	}
	c.send(26802, proto.Message{"boss_id": 91001, "formation_type": 1, "pos_info": []proto.Message{}, "hallows_id": 0})
	c.waitFor(26802)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	c.waitFor(26809)
	st, _ = e.st.SecretStateOf(gid)
	if st.HP != 0 || st.KillTime == 0 {
		t.Fatalf("not killed: %+v", st)
	}
	c.settle()
	e.advance(25 * time.Hour)
	c.send(26808, proto.Message{})
	if r := c.waitFor(26808); r.Int("flag") != 1 {
		t.Fatalf("report: %v", r)
	}
	c.send(26800, proto.Message{})
	info := c.waitFor(26800)
	if info.Arr("boss_list")[0].Int("boss_id") != 91002 {
		t.Fatalf("line after the win: %v", info.Arr("boss_list"))
	}
}
