package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSandybeachBossFight(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("sb", "沙滩人")
	c.send(25404, proto.Message{})
	if r := c.waitFor(25404); r.Int("code") != 0 {
		t.Fatalf("fight while closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: sbCamp, BID: 91050, Status: 1, StartAt: start, EndAt: start + 7*86400, Spec: actSpecJSON(t, false)})
	c.send(25401, proto.Message{})
	r := c.waitFor(25401)
	if r.Int("order") != 1 || r.Int("count") != 5 || r.Int("buy_count") != 0 || r.Int("hp_per") != sbHPScale || r.Int("rank") != 0 {
		t.Fatalf("25401: %v", r)
	}
	c.send(25404, proto.Message{})
	if r := c.waitFor(25404); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	c.waitFor(20000)
	if intro := c.waitFor(20027); intro.Int("combat_type") != combatSB {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(25405)
	if res.Int("all_dps") <= 0 || res.Int("target_role_name") == 0 && res.Str("target_role_name") == "" {
		t.Fatalf("25405: %v", res)
	}
	c.send(25401, proto.Message{})
	r = c.waitFor(25401)
	killed := res.Int("result") == 1
	switch {
	case r.Int("count") != 4:
		t.Fatalf("a try was not used: %v", r)
	case killed && r.Int("order") != 2:
		t.Fatalf("the boss died but the layer did not move: %v", r)
	case !killed && (r.Int("order") != 1 || r.Int("hp_per") >= sbHPScale):
		t.Fatalf("the hp was not kept: %v", r)
	case r.Int("score") != res.Int("dps_score")+res.Int("kill_score"):
		t.Fatalf("score %d against %v", r.Int("score"), res)
	case r.Int("score") > 0 && r.Int("rank") != 1:
		t.Fatalf("not ranked: %v", r)
	}
	// buying a try: 50 diamonds the first time, then 100
	e.grantAssets(c, role, map[int64]int64{3: 1000})
	buy := func() proto.Message {
		c.send(25402, proto.Message{})
		return c.waitFor(25402)
	}
	if b := buy(); b.Int("code") != 1 || b.Int("count") != 5 || b.Int("buy_count") != 1 {
		t.Fatalf("buy: %v", b)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[3] != 950 {
		t.Fatalf("diamonds after the first buy: %v", got)
	}
	// the next ones cost 100, 100, 200, 200, and five a day is all there is
	for i, want := range []int64{1, 1, 1, 1, 0} {
		if b := buy(); b.Int("code") != want {
			t.Fatalf("buy %d: %v", i+2, b)
		}
	}
	// the score rewards need their score
	c.send(25403, proto.Message{})
	if a := c.waitFor(25403); a.Int("code") != 0 {
		t.Fatalf("a reward beyond the score: %v", a)
	}
	d := &sbRole{}
	if _, err := e.st.LoadState(role.RID, sbModule, d); err != nil {
		t.Fatal(err)
	}
	for o := int64(1); o <= 20; o++ {
		d.Layers[o] = &sbLayer{Killed: true, Best: 1000}
	}
	d.Left = 0
	if err := e.st.SaveState(role.RID, sbModule, d); err != nil {
		t.Fatal(err)
	}
	score := d.score()
	c.send(25403, proto.Message{})
	a := c.waitFor(25403)
	want := 0
	for _, rw := range sbCfg.Rewards {
		if score >= rw.Num {
			want++
		}
	}
	if a.Int("code") != 1 || len(a.Arr("award_info")) != want || want == 0 {
		t.Fatalf("rewards at %d: %v (want %d)", score, a, want)
	}
	c.send(25403, proto.Message{})
	if a := c.waitFor(25403); a.Int("code") != 0 {
		t.Fatalf("rewards twice: %v", a)
	}
	// the rank list and the mail at the end
	c.send(12900, proto.Message{"type": rankSandybeach, "start": 1, "num": 10})
	if rk := c.waitFor(12900); len(rk.Arr("rank_list")) != 1 {
		t.Fatalf("rank: %v", rk)
	}
	e.setNow(actNow.Add(8 * 24 * time.Hour))
	c.send(25400, proto.Message{})
	c.waitFor(25400)
	rl, _ := e.st.RoleByID(role.RID)
	if mails, err := e.st.MailsForRole(rl); err != nil || len(mails) == 0 || mails[0].Subject != "沙滩Boss排名奖励" {
		t.Fatalf("rank mail: %v %v", mails, err)
	}
}
