package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGuildWar(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	ca, a := e.enter("gwa", "甲会长")
	cb, b := e.enter("gwb", "乙会长")
	for _, r := range []*store.Role{a, b} {
		if err := e.st.SetRoleLevel(r.RID, 30); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.GrantStarterPartners(r.RID, starterSeeds(r.Sex)); err != nil {
			t.Fatal(err)
		}
	}
	ga, err := e.st.CreateGuild(store.Guild{SrvID: a.SrvID, Name: "甲盟", LeaderRID: a.RID, Lev: 3}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateGuild(store.Guild{SrvID: b.SrvID, Name: "乙盟", LeaderRID: b.RID, Lev: 3}, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	// a Monday: the war is at 12:00~20:00
	day := dayStart(actNow)
	for weekday(day) != 1 {
		day = day.AddDate(0, 0, 1)
	}
	e.setNow(day.Add(5 * time.Hour)) // after the matching, before the start
	ca.send(24204, proto.Message{})
	r := ca.waitFor(24204)
	if r.Int("status") != gwShowing || r.Int("flag") != 1 || r.Int("end_time") != day.Add(12*time.Hour).Unix() {
		t.Fatalf("24204 showing: %v", r)
	}
	ca.send(24205, proto.Message{})
	if r := ca.waitFor(24205); len(r.Arr("match_info")) != 1 || r.Arr("match_info")[0].Str("guild_name1") == r.Arr("match_info")[0].Str("guild_name2") {
		t.Fatalf("24205: %v", r)
	}
	ca.send(24202, proto.Message{"pos": 1, "hp": 1, "flag": 0})
	if r := ca.waitFor(24202); r.Int("code") != 0 {
		t.Fatalf("attack before the start: %v", r)
	}
	e.setNow(day.Add(13 * time.Hour))
	ca.send(24204, proto.Message{})
	if r := ca.waitFor(24204); r.Int("status") != gwProcessing || r.Int("end_time") != day.Add(20*time.Hour).Unix() {
		t.Fatalf("24204 processing: %v", r)
	}
	ca.send(24200, proto.Message{})
	r = ca.waitFor(24200)
	if len(r.Arr("defense")) != gwPositions || r.Int("hp1") != 150 || r.Int("hp2") != 150 || r.Int("count") != 0 || r.Str("gname2") == "" {
		t.Fatalf("24200: %v", r)
	}
	ca.send(24201, proto.Message{"pos": 1})
	if r := ca.waitFor(24201); r.Int("pos") != 1 || r.Int("hp") != 3 || len(r.Arr("defense")) == 0 {
		t.Fatalf("24201: %v", r)
	}
	// the difficulty cannot be above the stars left; a ruin cannot be taken while it stands
	ca.send(24202, proto.Message{"pos": 1, "hp": 4, "flag": 0})
	if r := ca.waitFor(24202); r.Int("code") != 0 {
		t.Fatalf("difficulty: %v", r)
	}
	ca.send(24202, proto.Message{"pos": 1, "hp": 0, "flag": 1})
	if r := ca.waitFor(24202); r.Int("code") != 0 {
		t.Fatalf("a ruin that is not one: %v", r)
	}
	fight := func(c *client, diff int64) proto.Message {
		c.send(24202, proto.Message{"pos": 1, "hp": diff, "flag": 0})
		if r := c.waitFor(24202); r.Int("code") != 1 {
			t.Fatalf("attack: %v", r)
		}
		c.waitFor(20027)
		c.send(20062, proto.Message{})
		c.waitFor(20062)
		return c.waitFor(24203)
	}
	res := fight(ca, 1)
	won := res.Int("result") == 1
	ca.send(24200, proto.Message{})
	r = ca.waitFor(24200)
	if r.Int("count") != 1 || (won && r.Int("hp2") != 149) || (!won && r.Int("hp2") != 150) {
		t.Fatalf("after the fight (won %v): %v", won, r)
	}
	ca.send(24212, proto.Message{})
	if r := ca.waitFor(24212); len(r.Arr("guild_war_log")) != 1 || r.Arr("guild_war_log")[0].Int("flag1") != 1 {
		t.Fatalf("24212: %v", r)
	}
	ca.send(24213, proto.Message{})
	if r := ca.waitFor(24213); len(r.Arr("ranks")) != 1 || r.Arr("ranks")[0].Int("war_score") <= 0 {
		t.Fatalf("24213: %v", r)
	}
	// b sees the attack in its log as one against its guild
	cb.send(24212, proto.Message{})
	if r := cb.waitFor(24212); len(r.Arr("guild_war_log")) != 1 || r.Arr("guild_war_log")[0].Int("flag1") != 0 {
		t.Fatalf("b's 24212: %v", r)
	}
	// two attempts a war
	fight(ca, 1)
	ca.send(24202, proto.Message{"pos": 2, "hp": 1, "flag": 0})
	if r := ca.waitFor(24202); r.Int("code") != 0 {
		t.Fatalf("a third attempt: %v", r)
	}
	// the war ends at 20:00
	e.setNow(day.Add(21 * time.Hour))
	ca.send(24200, proto.Message{})
	r = ca.waitFor(24200)
	if r.Int("result") == gwFighting {
		t.Fatalf("settled: %v", r)
	}
	ca.send(24220, proto.Message{})
	r = ca.waitFor(24220)
	if len(r.Arr("guild_war_box")) != 1 || r.Arr("guild_war_box")[0].Int("rid") != 0 || r.Int("status") != 1 {
		t.Fatalf("24220: %v", r)
	}
	before, _ := e.st.RoleAssets(a.RID)
	ca.send(24221, proto.Message{"order": 1})
	if r := ca.waitFor(24221); r.Int("result") != 1 {
		t.Fatalf("open: %v", r)
	}
	if after, _ := e.st.RoleAssets(a.RID); after[guildLabel(10)] <= before[guildLabel(10)] {
		t.Fatalf("contribution %v -> %v", before, after)
	}
	ca.send(24221, proto.Message{"order": 1})
	if r := ca.waitFor(24221); r.Int("result") != 0 {
		t.Fatalf("twice: %v", r)
	}
	// the mails of the settlement
	if ms, _ := e.st.MailsForRole(a); len(ms) < 2 {
		t.Fatalf("mails: %v", ms)
	}
	_ = ga
}
