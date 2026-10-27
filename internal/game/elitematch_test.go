package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestElitematchGateAndFight(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(time.Date(2026, 10, 21, 12, 0, 0, 0, dayZone))
	c, role := e.enter("em0", "段位客")
	c.send(24900, proto.Message{})
	if r := c.waitFor(24900); r.Int("lev") != 1 || r.Int("day_combat_count") != 3 || r.Int("state") != phaseNow(e) {
		t.Fatalf("info of a low level role: %v", r)
	}
	c.send(24902, proto.Message{"type": 1})
	if r := c.waitFor(24901); r.Int("is_match") != 0 {
		t.Fatalf("matched below the level: %v", r)
	}
	c.send(24903, proto.Message{})
	if r := c.waitFor(24903); r.Int("code") != 0 {
		t.Fatalf("fight below the level: %v", r)
	}

	if err := e.st.SetRoleLevel(role.RID, int(eliteMinLev)); err != nil {
		t.Fatal(err)
	}
	c.send(24903, proto.Message{})
	if r := c.waitFor(24903); r.Int("code") != 0 {
		t.Fatalf("fight without a match: %v", r)
	}
	c.send(24902, proto.Message{"type": 1})
	m := c.waitFor(24901)
	if m.Int("is_match") != 1 || len(m.Arr("rand_list")) != 1 || m.Arr("rand_list")[0].Int("power") == 0 {
		t.Fatalf("match: %v", m)
	}
	c.send(24903, proto.Message{})
	if r := c.waitFor(24903); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	c.waitFor(20000)
	if intro := c.waitFor(20027); intro.Int("combat_type") != combatElite {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(24906)
	if res.Int("elite_lev") != 1 || len(res.Arr("all_hurt_statistics")) != 1 {
		t.Fatalf("result: %v", res)
	}
	if res.Int("result") == 1 && (res.Int("new_elite_lev") != 2 || res.Int("add_score") != 30 || len(res.Arr("awards")) == 0) {
		t.Fatalf("a win did not promote: %v", res)
	}
	if res.Int("result") == 2 && (res.Int("new_elite_lev") != 1 || res.Int("end_score") != 0) {
		t.Fatalf("a loss moved the level: %v", res)
	}
	info := c.waitFor(24900)
	if info.Int("day_combat_count") != 2 || info.Int("lev") != res.Int("new_elite_lev") {
		t.Fatalf("info after the fight: %v", info)
	}
	c.waitFor(24901)

	c.send(24930, proto.Message{"type": 1})
	if l := c.waitFor(24930).Arr("arena_elite_log"); len(l) != 1 {
		t.Fatalf("log: %v", l)
	}
	c.send(24940, proto.Message{})
	if p := c.waitFor(24940); p.Int("combat_all_count1") != 1 || p.Int("my_elite_lev") != res.Int("new_elite_lev") {
		t.Fatalf("personal: %v", p)
	}
	logID := func() int64 {
		c.send(24930, proto.Message{"type": 1})
		return c.waitFor(24930).Arr("arena_elite_log")[0].Int("id")
	}()
	c.send(24931, proto.Message{"type": 1, "id": logID})
	rep := c.waitFor(24931).Arr("arena_replay_infos")
	if len(rep) != 1 || rep[0].Int("id") == 0 || len(rep[0].Arr("a_plist")) == 0 || len(rep[0].Arr("b_plist")) == 0 || rep[0].Int("ret") != res.Int("result") {
		t.Fatalf("report: %v", rep)
	}
	c.send(24941, proto.Message{"channel": 1})
	if r := c.waitFor(24941); r.Str("msg") == "" {
		t.Fatalf("share: %v", r)
	}
	c.send(24942, proto.Message{"id": role.RID, "share_srv_id": role.SrvID, "period": 0})
	if p := c.waitFor(24942); p.Int("rid") != role.RID || p.Int("combat_all_count1") != 1 {
		t.Fatalf("shared page: %v", p)
	}
}

func TestElitematchClosedBuyRewardAndForms(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(time.Date(2026, 10, 21, 23, 45, 0, 0, dayZone))
	c, role := e.enter("em1", "段位客")
	if err := e.st.SetRoleLevel(role.RID, int(eliteMinLev)); err != nil {
		t.Fatal(err)
	}
	c.send(24905, proto.Message{})
	if r := c.waitFor(24905); r.Int("state") != 0 {
		t.Fatalf("open at 23:45: %v", r)
	}
	c.send(24902, proto.Message{"type": 1})
	if r := c.waitFor(24901); r.Int("is_match") != 0 {
		t.Fatalf("matched while closed: %v", r)
	}
	e.setNow(time.Date(2026, 10, 22, 10, 0, 0, 0, dayZone))
	c.send(24905, proto.Message{})
	if r := c.waitFor(24905); r.Int("state") != 1 {
		t.Fatalf("closed at 10:00: %v", r)
	}

	c.send(24904, proto.Message{})
	if r := c.waitFor(24904); r.Int("code") != 0 {
		t.Fatalf("bought without diamonds: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 500})
	c.send(24904, proto.Message{})
	if r := c.waitFor(24904); r.Int("code") != 1 || r.Int("day_combat_count") != 4 || r.Int("day_buy_count") != 1 {
		t.Fatalf("buy: %v", r)
	}

	c.send(24915, proto.Message{"lev": 2})
	if r := c.waitFor(24915); r.Int("code") != 0 {
		t.Fatalf("reward of a level not reached: %v", r)
	}

	c.send(24920, proto.Message{"type": 1})
	if f := c.waitFor(24920).Arr("formations"); len(f) != 1 || f[0].Int("order") != 1 {
		t.Fatalf("default formation: %v", f)
	}
	c.send(24921, proto.Message{"type": 1, "formations": []proto.Message{{"order": 1, "formation_type": 1, "hallows_id": 0, "pos_info": []proto.Message{{"pos": 1, "id": 7}}}}})
	if r := c.waitFor(24921); r.Int("code") != 1 {
		t.Fatalf("save formation: %v", r)
	}
	c.send(24920, proto.Message{"type": 1})
	if f := c.waitFor(24920).Arr("formations"); len(f) != 1 || len(f[0].Arr("pos_info")) != 1 || f[0].Arr("pos_info")[0].Int("id") != 7 {
		t.Fatalf("saved formation: %v", f)
	}

	c.send(24945, proto.Message{})
	if r := c.waitFor(24945).Arr("manifesto"); len(r) != 3 {
		t.Fatalf("manifesto: %v", r)
	}
	c.send(24946, proto.Message{"manifesto": []proto.Message{{"order": 2, "manifesto_id": 9}}})
	c.waitFor(24946)
	c.send(24945, proto.Message{})
	if r := c.waitFor(24945).Arr("manifesto"); r[1].Int("manifesto_id") != 9 {
		t.Fatalf("saved manifesto: %v", r)
	}
}

func TestEliteSeasons(t *testing.T) {
	// the original server (capture 2026-10-26) opened at 07:21:04 UTC; period 12 started 11 seasons after, on 2026-10-08
	open := int64(1791444064 - 11*21*86400)
	at := func(unix int64) time.Time { return time.Unix(unix, 0) }
	// the capture was taken with 194425 seconds to the end of the season, in its last three days
	now := at(1793258464 - 194425)
	p, phase, start, end, phaseEnd := eliteSeason(now, open)
	if p != 12 || phase != 3 || start != 1791444064 || end != 1793258464 || phaseEnd != end {
		t.Fatalf("capture: %d %d %d %d %d", p, phase, start, end, phaseEnd)
	}
	if p, phase, _, _, pe := eliteSeason(at(1791444064+86400), open); p != 12 || phase != 1 || pe != 1791444064+9*86400 {
		t.Fatalf("first half: %d %d %d", p, phase, pe)
	}
	if p, phase, _, _, pe := eliteSeason(at(1791444064+10*86400), open); p != 12 || phase != 2 || pe != 1791444064+18*86400 {
		t.Fatalf("second half: %d %d %d", p, phase, pe)
	}
	if p, phase, start, _, _ := eliteSeason(at(1793258464), open); p != 13 || phase != 1 || start != 1793258464 {
		t.Fatalf("the next season: %d %d %d", p, phase, start)
	}
	if p, _, _, _, _ := eliteSeason(at(open), open); p != 1 {
		t.Fatalf("the first season: %d", p)
	}
	// the day's window of the capture: 19825 seconds to its end, 30 minutes before the clock time of the opening
	if o, _, end := eliteOpen(now, open); !o || end-now.Unix() != 19825 {
		t.Fatalf("the day's window: open %v, %d seconds left", o, end-now.Unix())
	}
}

func TestEliteSeasonOfAServer(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	openAt := time.Date(2026, 10, 1, 15, 21, 4, 0, dayZone)
	if err := e.st.UpsertServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1, OpenTime: openAt.Unix()}); err != nil {
		t.Fatal(err)
	}
	e.setNow(openAt.Add(25 * 24 * time.Hour))
	c, _ := e.enter("es1", "赛季客")
	c.send(24900, proto.Message{})
	r := c.waitFor(24900)
	// day 26: the second season, its fifth day
	if r.Int("period") != 2 || r.Int("state") != 1 || r.Int("start_time") != openAt.Unix()+21*86400 || r.Int("end_time") != openAt.Unix()+42*86400 || r.Int("zone_id") != 1 {
		t.Fatalf("24900: %v", r)
	}
	if r.Int("state_time") != openAt.Unix()+30*86400-e.clk.Load()/1e9 {
		t.Fatalf("state_time: %v", r)
	}
}

func phaseNow(e *env) int64 {
	srv, _ := e.st.Server("symlf_1")
	at := srv.OpenTime
	if at == 0 {
		at = srv.CreatedAt
	}
	now := time.Unix(0, e.clk.Load())
	if at > now.Unix() {
		at = dayStart(now).Unix()
	}
	_, phase, _, _, _ := eliteSeason(now, at)
	return phase
}

func TestEliteSeasonSettlement(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	openAt := time.Date(2026, 10, 1, 15, 21, 4, 0, dayZone)
	if err := e.st.UpsertServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1, OpenTime: openAt.Unix()}); err != nil {
		t.Fatal(err)
	}
	e.setNow(openAt.Add(36 * time.Hour))
	ca, a := e.enter("ss_a", "甲")
	_, b := e.enter("ss_b", "乙")
	_, low := e.enter("ss_c", "丙")
	put := func(rid, lev, score int64) {
		t.Helper()
		if err := e.st.SaveState(rid, eliteModule, &eliteRole{Lev: lev, Score: score}); err != nil {
			t.Fatal(err)
		}
	}
	put(a.RID, 20, 5000)
	put(b.RID, 22, 6000)
	put(low.RID, 5, 100)            // under the rank the rewards ask
	ca.send(24900, proto.Message{}) // the first look: nothing to pay, only noted
	ca.waitFor(24900)
	count := func(role *store.Role, subject string) int {
		ms, err := e.st.MailsForRole(role)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, m := range ms {
			if m.Subject == subject {
				n++
			}
		}
		return n
	}
	if count(b, "超凡段位赛上半赛季结算奖励") != 0 {
		t.Fatal("paid before the half season ended")
	}
	// the end of the first half (9 days): paid by rank, ranks stay
	e.setNow(openAt.Add(9*24*time.Hour + time.Hour))
	ca.send(24900, proto.Message{})
	ca.waitFor(24900)
	for _, r := range []*store.Role{a, b} {
		if n := count(r, "超凡段位赛上半赛季结算奖励"); n != 1 {
			t.Fatalf("%s: %d mails for the first half", r.Name, n)
		}
	}
	if n := count(low, "超凡段位赛上半赛季结算奖励"); n != 0 {
		t.Fatalf("a low rank was paid: %d", n)
	}
	er := &eliteRole{}
	if _, err := e.st.LoadState(b.RID, eliteModule, er); err != nil || er.Lev != 22 {
		t.Fatalf("a rank moved after a first half: %v %+v", err, er)
	}
	// asking again pays nothing twice
	ca.send(24900, proto.Message{})
	ca.waitFor(24900)
	if n := count(b, "超凡段位赛上半赛季结算奖励"); n != 1 {
		t.Fatalf("paid twice: %d", n)
	}
	// the end of the season (21 days): the second half is paid and the ranks go back
	e.setNow(openAt.Add(21*24*time.Hour + time.Hour))
	ca.send(24900, proto.Message{})
	r := ca.waitFor(24900)
	if count(b, "超凡段位赛下半赛季结算奖励") != 1 || count(a, "超凡段位赛下半赛季结算奖励") != 1 {
		t.Fatal("the second half was not paid")
	}
	if r.Int("period") != 2 || r.Int("lev") != 12 { // 超凡王者 III (20) -> 钻石大师 II (12)
		t.Fatalf("24900 of the new season: %v", r)
	}
	if _, err := e.st.LoadState(b.RID, eliteModule, er); err != nil || er.Lev != 13 || er.Score != eliteCfg.level(13).InitExp {
		t.Fatalf("b after the reset: %v %+v", err, er)
	}
	if _, err := e.st.LoadState(low.RID, eliteModule, er); err != nil || er.Lev != 1 {
		t.Fatalf("low after the reset: %v %+v", err, er)
	}
}
