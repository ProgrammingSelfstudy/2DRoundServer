package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestElitematchGateAndFight(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(time.Date(2026, 10, 21, 12, 0, 0, 0, dayZone))
	c, role := e.enter("em0", "段位客")
	c.send(24900, proto.Message{})
	if r := c.waitFor(24900); r.Int("lev") != 1 || r.Int("day_combat_count") != 3 || r.Int("state") != 1 {
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
