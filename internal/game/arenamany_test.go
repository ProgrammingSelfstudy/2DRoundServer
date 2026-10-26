package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

var amDay = time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone) // a Tuesday

func (e *env) amRole(name string) (*client, *store.Role) {
	e.t.Helper()
	c, role := e.enter(name, name)
	if err := e.st.SetRoleLevel(role.RID, int(amMinLev)); err != nil {
		e.t.Fatal(err)
	}
	return c, role
}

func (e *env) amBoard(t *testing.T, srv string) *amBoard {
	b := &amBoard{}
	if _, err := e.st.LoadGlobal(srv, amModule, b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestArenamanyLevelAndMain(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(amDay)
	c, role := e.enter("am0", "多人甲")
	c.send(29016, proto.Message{})
	if r := c.waitFor(29016); r.Int("code") != 0 {
		t.Fatalf("matched below the level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, int(amMinLev)); err != nil {
		t.Fatal(err)
	}
	c.send(29000, proto.Message{})
	r := c.waitFor(29000)
	if r.Int("state") != 1 || r.Int("count") != 2 || r.Int("buy_count") != 0 || len(r.Arr("award_list")) != 11 || len(r.Arr("team_members")) != 1 {
		t.Fatalf("main: %v", r)
	}
	me := r.Arr("team_members")[0]
	if me.Int("rid") != role.RID || me.Int("score") != 1000 || me.Int("score_lev") != 1 || me.Int("rank") != 0 || me.Int("power") == 0 {
		t.Fatalf("member: %v", me)
	}
	if end := r.Int("end_time"); end <= amDay.Unix() || end > amDay.Unix()+7*86400 {
		t.Fatalf("end time %d", end)
	}
	// diamonds buy challenges, two a day
	c.send(29030, proto.Message{})
	if r := c.waitFor(29030); r.Int("code") != 0 {
		t.Fatalf("bought without diamonds: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{presageGold: 1000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2; i++ {
		c.send(29030, proto.Message{})
		if r := c.waitFor(29030); r.Int("code") != 1 || r.Int("buy_count") != i || r.Int("count") != 2+i {
			t.Fatalf("buy %d: %v", i, r)
		}
	}
	c.send(29030, proto.Message{})
	if r := c.waitFor(29030); r.Int("code") != 0 {
		t.Fatalf("a third buy: %v", r)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[presageGold] != 1000-300 {
		t.Fatalf("diamonds %v", a)
	}
}

func TestArenamanyPartyWithRobotAndRole(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(amDay)
	c1, r1 := e.amRole("amp1")
	c2, r2 := e.amRole("amp2")
	c1.send(29008, proto.Message{})
	var robot proto.Message
	for _, m := range c1.waitFor(29008).Arr("team_members") {
		if m.Int("rid") >= arenaRobotBase && robot == nil {
			robot = m
		}
		if m.Int("rid") == r1.RID {
			t.Fatal("recommended myself")
		}
	}
	if robot == nil {
		t.Fatal("no robot on the recommended list")
	}
	c1.send(29002, proto.Message{"rid": robot.Int("rid"), "srv_id": robot.Str("sid")})
	if r := c1.waitFor(29002); r.Int("code") != 1 {
		t.Fatalf("invite a robot: %v", r)
	}
	if r := c1.waitFor(29000); len(r.Arr("team_members")) != 2 || r.Arr("team_members")[0].Int("is_leader") != 1 {
		t.Fatalf("party after the robot: %v", r)
	}
	c1.send(29002, proto.Message{"rid": r2.RID, "srv_id": r2.SrvID})
	if r := c1.waitFor(29002); r.Int("code") != 0 {
		t.Fatalf("a second teammate: %v", r)
	}
	c1.send(29006, proto.Message{})
	if r := c1.waitFor(29006); r.Int("code") != 1 {
		t.Fatalf("leave: %v", r)
	}
	if r := c1.waitFor(29000); len(r.Arr("team_members")) != 1 {
		t.Fatalf("alone again: %v", r)
	}

	// a role invites a role
	c1.send(29002, proto.Message{"rid": r2.RID, "srv_id": r2.SrvID})
	if r := c1.waitFor(29002); r.Int("code") != 1 {
		t.Fatalf("invite: %v", r)
	}
	c1.send(29010, proto.Message{})
	if r := c1.waitFor(29010); len(r.Arr("invite_list")) != 1 || r.Arr("invite_list")[0].Int("rid") != r2.RID {
		t.Fatalf("sent invites: %v", r)
	}
	c2.send(29003, proto.Message{})
	var inv []proto.Message
	for {
		r := c2.waitFor(29003) // the push of the invitation comes first, then the answer
		if inv = r.Arr("team_members"); len(inv) == 1 {
			break
		}
	}
	if inv[0].Int("rid") != r1.RID {
		t.Fatalf("invitations: %v", inv)
	}
	c2.send(29004, proto.Message{"rid": r1.RID, "srv_id": r1.SrvID, "type": 1})
	if r := c2.waitFor(29004); r.Int("code") != 1 {
		t.Fatalf("accept: %v", r)
	}
	if r := c1.waitFor(29000); len(r.Arr("team_members")) != 2 {
		t.Fatalf("leader is told: %v", r)
	}
	c2.send(29000, proto.Message{}) // answered after the handler above has saved
	c2.waitFor(29000)
	b := e.amBoard(t, r1.SrvID)
	if b.Roles[r1.RID].Partner != r2.RID || !b.Roles[r1.RID].Leader || b.Roles[r2.RID].Partner != r1.RID || b.Roles[r2.RID].Leader {
		t.Fatalf("board: %+v %+v", b.Roles[r1.RID], b.Roles[r2.RID])
	}
	c1.send(29007, proto.Message{"rid": r2.RID, "srv_id": r2.SrvID})
	if r := c1.waitFor(29007); r.Int("code") != 1 {
		t.Fatalf("kick: %v", r)
	}
	c1.send(29000, proto.Message{})
	c1.waitFor(29000)
	c1.send(29000, proto.Message{})
	c1.waitFor(29000)
	if e.amBoard(t, r1.SrvID).Roles[r2.RID].Partner != 0 {
		t.Fatal("the kicked role still has a teammate")
	}
}

func TestArenamanyMatchAndFight(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(amDay)
	c, role := e.amRole("amf1")
	c.send(29016, proto.Message{})
	r := c.waitFor(29016)
	if r.Int("code") != 1 || len(r.Arr("atk_team_members")) != 3 || len(r.Arr("def_team_members")) != 3 || r.Int("is_enter") != 0 || r.Int("end_time") <= amDay.Unix() {
		t.Fatalf("match: %v", r)
	}
	if first := r.Arr("atk_team_members")[0]; first.Int("rid") != role.RID || len(first.Arr("partner_infos")) == 0 {
		t.Fatalf("my side: %v", first)
	}
	for i := int64(1); i <= amCfg.num("team_refresh_num"); i++ {
		c.send(29017, proto.Message{})
		if r := c.waitFor(29017); r.Int("code") != 1 {
			t.Fatalf("rematch %d: %v", i, r)
		}
		if r := c.waitFor(29016); r.Int("ref_count") != i {
			t.Fatalf("rematch message %d: %v", i, r)
		}
	}
	c.send(29017, proto.Message{})
	if r := c.waitFor(29017); r.Int("code") != 0 {
		t.Fatalf("a rematch past the limit: %v", r)
	}
	c.send(29018, proto.Message{})
	c.waitFor(29018)
	c.send(29016, proto.Message{})
	r = c.waitFor(29016)
	if r.Int("is_enter") != 1 {
		t.Fatalf("entered: %v", r)
	}
	atk := r.Arr("atk_team_members")
	var order []proto.Message
	for i := len(atk) - 1; i >= 0; i-- { // the reverse order
		order = append(order, proto.Message{"rid": atk[i].Int("rid"), "sid": atk[i].Str("sid"), "pos": len(atk) - i})
	}
	c.send(29022, proto.Message{"pos_info": order})
	c.waitFor(29022)
	c.send(29019, proto.Message{"pos_info": order})
	if r := c.waitFor(29019); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	c.send(20062, proto.Message{})
	if r := c.waitFor(20062); r.Int("code") != 1 {
		t.Fatalf("skip: %v", r)
	}
	res := c.waitFor(29020)
	if res.Int("score") != 1000 || res.Int("win_count")+res.Int("lose_count") < 2 || res.Int("win_count") > 2 || res.Int("lose_count") > 2 ||
		len(res.Arr("all_hurt_statistics")) < 2 || res.Arr("all_hurt_statistics")[0].Int("replay_id") == 0 {
		t.Fatalf("result: %v", res)
	}
	if res.Int("result") == 1 && (res.Int("new_score") <= 1000 || res.Int("win_count") != 2 || res.Int("new_rank") != 1) {
		t.Fatalf("a win: %v", res)
	}
	if res.Int("result") == 2 && (res.Int("new_score") != 1000 || res.Int("lose_count") != 2) {
		t.Fatalf("a loss: %v", res)
	}
	c.send(29000, proto.Message{})
	if m := c.waitFor(29000); m.Int("count") != 1 {
		t.Fatalf("a challenge was not used: %v", m)
	}
	c.send(29019, proto.Message{"pos_info": order})
	if r := c.waitFor(29019); r.Int("code") != 0 {
		t.Fatalf("fought without a match: %v", r)
	}
	c.send(29026, proto.Message{})
	logs := c.waitFor(29026).Arr("holiday_arena_team_log")
	if len(logs) != 1 || len(logs[0].Arr("atk_face")) != 3 || logs[0].Int("ret") != res.Int("result") {
		t.Fatalf("reports: %v", logs)
	}
	c.send(29027, proto.Message{"id": logs[0].Int("id")})
	if infos := c.waitFor(29027).Arr("replay_infos"); len(infos) != int(res.Int("win_count")+res.Int("lose_count")) || infos[0].Int("id") == 0 {
		t.Fatalf("replay infos: %v", infos)
	}
	c.send(29025, proto.Message{"start_num": 1, "end_num": 3})
	if rk := c.waitFor(29025); rk.Int("rank") != 1 || len(rk.Arr("team_members")) != 1 || rk.Arr("team_members")[0].Int("rid") != role.RID {
		t.Fatalf("rank: %v", rk)
	}
	// two challenges a day; the third one is refused until a day passes
	for i := 0; i < 1; i++ {
		c.send(29016, proto.Message{})
		if r := c.waitFor(29016); r.Int("code") != 1 {
			t.Fatalf("match %d: %v", i, r)
		}
		c.send(29019, proto.Message{"pos_info": order})
		if r := c.waitFor(29019); r.Int("code") != 1 {
			t.Fatalf("fight %d: %v", i, r)
		}
		c.send(20062, proto.Message{})
		c.waitFor(20062)
		c.waitFor(29020)
	}
	c.send(29016, proto.Message{})
	if r := c.waitFor(29016); r.Int("code") != 0 {
		t.Fatalf("matched with no challenges left: %v", r)
	}
	e.setNow(amDay.Add(24 * time.Hour))
	c.send(29000, proto.Message{})
	if m := c.waitFor(29000); m.Int("count") != 2 {
		t.Fatalf("a new day: %v", m)
	}
}

func TestArenamanyBoxAndSeasonMail(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(amDay)
	c, role := e.amRole("amb1")
	c.send(29000, proto.Message{})
	c.waitFor(29000)
	c.send(29031, proto.Message{"id": 2})
	if r := c.waitFor(29031); r.Int("code") != 0 {
		t.Fatalf("box without the level: %v", r)
	}
	b := e.amBoard(t, role.SrvID)
	b.Roles[role.RID].Score, b.Roles[role.RID].Do = 1100, 1
	if err := e.st.SaveGlobal(role.SrvID, amModule, b); err != nil {
		t.Fatal(err)
	}
	c.send(29031, proto.Message{"id": 2})
	r := c.waitFor(29031)
	if r.Int("code") != 1 {
		t.Fatalf("box: %v", r)
	}
	for _, a := range r.Arr("award_list") {
		if a.Int("award_id") == 2 && a.Int("status") != 2 {
			t.Fatalf("box state: %v", r)
		}
	}
	c.send(29031, proto.Message{"id": 2})
	if r := c.waitFor(29031); r.Int("code") != 0 {
		t.Fatalf("box twice: %v", r)
	}
	mails, _ := e.st.MailsForRole(role)
	e.setNow(amDay.Add(7 * 24 * time.Hour)) // the season turns
	c.send(29000, proto.Message{})
	m := c.waitFor(29000)
	if m.Int("count") != 2 {
		t.Fatalf("main in the new season: %v", m)
	}
	after, _ := e.st.MailsForRole(role)
	if len(after) != len(mails)+2 { // the rank reward and the level boxes not taken
		t.Fatalf("rank mail: %d -> %d", len(mails), len(after))
	}
	if me := m.Arr("team_members")[0]; me.Int("score") != 1000 || me.Int("rank") != 0 {
		t.Fatalf("the score is not new: %v", me)
	}
}

func TestArenamanyDelta(t *testing.T) {
	if amDelta(1000, 1000, true) < 1 || amDelta(1000, 1500, true) <= amDelta(1000, 1000, true) {
		t.Fatalf("win gains %d %d", amDelta(1000, 1000, true), amDelta(1000, 1500, true))
	}
	if amDelta(1500, 1000, false) <= amDelta(1000, 1500, false) {
		t.Fatalf("loss costs %d %d", amDelta(1500, 1000, false), amDelta(1000, 1500, false))
	}
	if amLev(999) != 1 || amLev(1030) != 2 || amLev(99999) != 12 {
		t.Fatalf("levels %d %d %d", amLev(999), amLev(1030), amLev(99999))
	}
}
