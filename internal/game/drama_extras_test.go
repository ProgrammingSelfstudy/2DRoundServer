package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func atDungeon(t *testing.T, e *env, rid, dun int64, cleared bool) {
	t.Helper()
	d := dungeonByID[dun]
	status := dungeonNotCleared
	if cleared {
		status = dungeonCleared
	}
	if err := e.st.SaveDrama(store.Drama{RID: rid, Mode: d.Mode, ChapterID: d.Chapter, DunID: dun, Status: status, MaxDunID: dun}); err != nil {
		t.Fatal(err)
	}
}

func mailTo(t *testing.T, e *env, c *client, rid int64, atts ...store.Attachment) {
	t.Helper()
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, rid)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
}

func TestHookIncome(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// the store keeps real time, the sessions the test clock: count from the role's creation
	start := time.Unix(role.CreatedAt, 0)
	e.setNow(start.Add(90 * time.Minute))
	c.send(13017, proto.Message{})
	r := c.recv(13017)
	items := r.Arr("list")
	if r.Int("hook_time") != 5400 || len(items) != 4 || items[0].Int("bid") != 5 || items[0].Int("num") != 900 || items[2].Int("num") != 18000 {
		t.Fatalf("13017 after 90 minutes: %v", r)
	}
	c.send(13018, proto.Message{})
	if a := c.recv(13018); a.Int("code") != 1 {
		t.Fatalf("claim: %v", a)
	}
	if p := c.recv(13007); p.Int("type") != 1 || p.Int("time") != 5400 || len(p.Arr("items")) != 4 {
		t.Fatalf("13007: %v", p)
	}
	if z := c.recv(13017); z.Int("hook_time") != 0 {
		t.Fatalf("reset: %v", z)
	}
	c.recv(10302)
	c.recv(10500)
	if as, _ := e.st.RoleAssets(role.RID); as[1] != 18000 || as[22] != 2250 {
		t.Fatalf("ledger: %v", as)
	}
	c.send(13018, proto.Message{}) // nothing accumulated yet
	if a := c.recv(13018); a.Int("code") != 0 {
		t.Fatalf("second claim: %v", a)
	}
	// the income stops growing after 12 hours
	e.setNow(start.Add(90*time.Minute + 20*time.Hour))
	c.send(13017, proto.Message{})
	if r := c.recv(13017); r.Int("hook_time") != hookMaxSeconds || r.Arr("list")[0].Int("num") != 10*720 {
		t.Fatalf("cap: %v", r)
	}
}

func TestQuickBattle(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	quick := func() proto.Message { c.send(13004, proto.Message{}); return c.recv(13004) }
	if r := quick(); r.Int("code") != 0 { // 10020 not reached
		t.Fatalf("too early: %v", r)
	}
	atDungeon(t, e, role.RID, 10030, false)
	c.send(13006, proto.Message{})
	if s := c.recv(13006); s.Int("fast_combat_free_num") != 1 || s.Int("fast_combat_num") != 0 || s.Int("fast_combat_max") != 3 || s.Int("auto_num_max") != 10 {
		t.Fatalf("13006: %v", s)
	}
	drain := func() proto.Message {
		p := c.recv(13007)
		st := c.recv(13006)
		c.recv(10302)
		c.recv(10500)
		if p.Int("type") != 2 || p.Int("time") != 7200 {
			t.Fatalf("13007: %v", p)
		}
		return st
	}
	if r := quick(); r.Int("code") != 1 { // the first one is free
		t.Fatalf("free: %v", r)
	}
	if st := drain(); st.Int("fast_combat_num") != 1 || st.Int("fast_combat_free_num") != 0 {
		t.Fatalf("after one: %v", st)
	}
	// 10030's income is 10 exp, 35 hero exp, 210 coin per minute: two hours of it
	if as, _ := e.st.RoleAssets(role.RID); as[1] != 210*120 {
		t.Fatalf("coin: %v", as)
	}
	if r := quick(); r.Int("code") != 0 { // the second costs 100 gems
		t.Fatalf("no gems: %v", r)
	}
	mailTo(t, e, c, role.RID, store.Attachment{Kind: "asset", ItemID: 3, Num: 400})
	for i := 0; i < 2; i++ {
		if r := quick(); r.Int("code") != 1 {
			t.Fatalf("paid %d: %v", i, r)
		}
		drain()
	}
	// 400 gems were mailed, 100 + 200 taken, and every level the exp gained paid its gems
	fresh, _ := e.st.RoleByID(role.RID)
	want := int64(100)
	for lev := 2; lev <= fresh.Lev; lev++ {
		for _, g := range expTable[lev-1].Reward {
			if g[0] == 3 {
				want += g[1]
			}
		}
	}
	if as, _ := e.st.RoleAssets(role.RID); as[3] != want {
		t.Fatalf("gems: %v want %d (level %d)", as, want, fresh.Lev)
	}
	if r := quick(); r.Int("code") != 0 {
		t.Fatalf("fourth: %v", r)
	}
	// next day it is free again
	e.setNow(time.Date(2026, 10, 20, 10, 0, 0, 0, dayZone))
	c.send(13006, proto.Message{})
	if s := c.recv(13006); s.Int("fast_combat_free_num") != 1 || s.Int("fast_combat_num") != 0 {
		t.Fatalf("next day: %v", s)
	}
}

func TestSweep(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	atDungeon(t, e, role.RID, 10040, false)
	sweep := func(dun, num int64) proto.Message {
		c.send(13005, proto.Message{"dun_id": dun, "num": num})
		return c.recv(13005)
	}
	for _, bad := range [][2]int64{{10040, 1}, {10050, 1}, {99, 1}, {10010, 0}, {10010, 6}} { // not cleared / unknown / bad count
		if r := sweep(bad[0], bad[1]); r.Int("code") != 0 {
			t.Fatalf("sweep %v: %v", bad, r)
		}
	}
	r := sweep(10010, 3) // three free ones
	if r.Int("code") != 1 || len(r.Arr("items")) != 4 || r.Arr("items")[0].Int("num") != 10*clearRewardMin*3 {
		t.Fatalf("sweep: %v", r)
	}
	if q := c.recv(13006); q.Int("auto_num") != 3 || q.Int("auto_num_max") != 10 { // the client decides "free" from this
		t.Fatalf("sweep count: %v", q)
	}
	c.recv(10302)
	c.recv(10500)
	if r := sweep(10020, 1); r.Int("code") != 0 { // needs a ticket
		t.Fatalf("no ticket: %v", r)
	}
	e.give(c, role, map[int64]int64{sweepTicketItem: 20})
	if r := sweep(10020, 5); r.Int("code") != 1 {
		t.Fatalf("with tickets: %v", r)
	}
	if q := c.recv(13006); q.Int("auto_num") != 8 {
		t.Fatalf("sweep count: %v", q)
	}
	c.recv(10302)
	c.recv(10500)
	if got, _ := e.st.RoleItems(role.RID); got[sweepTicketItem] != 15 {
		t.Fatalf("tickets: %v", got)
	}
	// 8 done; two more fit in the day's 10, three do not
	if r := sweep(10020, 3); r.Int("code") != 0 {
		t.Fatalf("over the day's limit: %v", r)
	}
	if r := sweep(10020, 2); r.Int("code") != 1 {
		t.Fatalf("last two: %v", r)
	}
	if q := c.recv(13006); q.Int("auto_num") != 10 {
		t.Fatalf("sweep count: %v", q)
	}
}
