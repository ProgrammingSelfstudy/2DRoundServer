package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

var (
	atFriday   = time.Date(2026, 10, 23, 13, 0, 0, 0, dayZone) // teams are formed
	atSaturday = time.Date(2026, 10, 24, 10, 0, 0, 0, dayZone) // fights
)

func (e *env) atRole(name string) (*client, *store.Role) {
	e.t.Helper()
	c, role := e.enter(name, name)
	if err := e.st.SetRoleLevel(role.RID, int(atMinLev)); err != nil {
		e.t.Fatal(err)
	}
	return c, role
}

// atTeamOfRobots makes a team of the leader and two robots and signs it up.
func atTeamOfRobots(t *testing.T, c *client, name string) {
	t.Helper()
	c.send(27201, proto.Message{"name": name, "limit_lev": 0, "limit_power": 0, "is_check": 0})
	if r := c.waitFor(27201); r.Int("code") != 1 {
		t.Fatalf("create: %v", r)
	}
	c.send(27228, proto.Message{})
	robots := 0
	for _, m := range c.waitFor(27228).Arr("arena_team_member") {
		if m.Int("rid") >= atRobotRID && robots < 2 {
			c.send(27205, proto.Message{"rid": m.Int("rid"), "srv_id": m.Str("sid")})
			if r := c.waitFor(27205); r.Int("code") != 1 {
				t.Fatalf("invite a robot: %v", r)
			}
			robots++
		}
	}
	if robots != 2 {
		t.Fatalf("robots on the list: %d", robots)
	}
	c.send(27240, proto.Message{})
	if r := c.waitFor(27240); r.Int("code") != 1 {
		t.Fatalf("sign: %v", r)
	}
}

func TestArenateamSeasonStates(t *testing.T) {
	for _, tc := range []struct {
		at    time.Time
		state int64
	}{
		{time.Date(2026, 10, 23, 11, 59, 0, 0, dayZone), 0},
		{time.Date(2026, 10, 23, 12, 0, 0, 0, dayZone), 1},
		{time.Date(2026, 10, 23, 23, 59, 0, 0, dayZone), 1},
		{time.Date(2026, 10, 24, 0, 0, 0, 0, dayZone), 2},
		{time.Date(2026, 10, 25, 22, 59, 0, 0, dayZone), 2},
		{time.Date(2026, 10, 25, 23, 0, 0, 0, dayZone), 4},
		{time.Date(2026, 10, 25, 23, 40, 0, 0, dayZone), 0},
		{time.Date(2026, 10, 26, 9, 0, 0, 0, dayZone), 0},
	} {
		if _, state, _ := atSeason(tc.at); state != tc.state {
			t.Errorf("%v: state %d, want %d", tc.at, state, tc.state)
		}
	}
	k1, _, _ := atSeason(time.Date(2026, 10, 24, 9, 0, 0, 0, dayZone))
	k2, _, _ := atSeason(time.Date(2026, 10, 25, 22, 0, 0, 0, dayZone))
	k3, _, _ := atSeason(time.Date(2026, 10, 30, 12, 0, 0, 0, dayZone))
	if k1 != k2 || k3 != k1+7*86400 {
		t.Errorf("season keys %d %d %d", k1, k2, k3)
	}
}

func TestArenateamSoloWithRobots(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(atFriday)
	c, role := e.enter("at0", "队长")
	c.send(27201, proto.Message{"name": "海贼团"})
	if r := c.waitFor(27201); r.Int("code") != 0 {
		t.Fatalf("created below the level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, int(atMinLev)); err != nil {
		t.Fatal(err)
	}
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("state") != 1 || r.Int("tid") != 0 || len(r.Arr("award_list")) != 5 {
		t.Fatalf("main before a team: %v", r)
	}
	atTeamOfRobots(t, c, "海贼团")
	c.send(27221, proto.Message{})
	d := c.waitFor(27221)
	if len(d.Arr("arena_team_member")) != 3 || d.Int("is_sign") != 1 || d.Int("rid") != role.RID || d.Arr("arena_team_member")[0].Int("power") == 0 {
		t.Fatalf("team details: %v", d)
	}
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("is_sign") != 1 || r.Int("score") != 1000 || r.Int("count") != 3 || r.Int("rank") != 1 {
		t.Fatalf("main after the sign-up: %v", r)
	}
	c.send(27251, proto.Message{})
	if r := c.waitFor(27251); len(r.Arr("rival_list")) != 0 {
		t.Fatalf("rivals before the challenge phase: %v", r)
	}
	c.send(27252, proto.Message{"tid": 1, "srv_id": "symlf_1", "is_auto": 1})
	if r := c.waitFor(27252); r.Int("code") != 0 {
		t.Fatalf("fought before the challenge phase: %v", r)
	}

	e.setNow(atSaturday)
	c.send(27251, proto.Message{})
	rivals := c.waitFor(27251).Arr("rival_list")
	if len(rivals) != 3 || len(rivals[0].Arr("team_members")) != 3 || rivals[0].Int("team_power") == 0 {
		t.Fatalf("rivals: %v", rivals)
	}
	c.send(27252, proto.Message{"tid": 12345, "srv_id": "symlf_1", "is_auto": 1})
	if r := c.waitFor(27252); r.Int("code") != 0 {
		t.Fatalf("fought a team not on the list: %v", r)
	}

	// a skipped challenge
	c.send(27252, proto.Message{"tid": rivals[0].Int("tid"), "srv_id": "symlf_1", "is_auto": 1})
	if r := c.waitFor(27252); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	res := c.waitFor(27253)
	if res.Int("a_score") != 1000 || len(res.Arr("a_team_members")) != 3 || len(res.Arr("b_team_members")) != 3 || len(res.Arr("all_hurt_statistics")) == 0 {
		t.Fatalf("result: %v", res)
	}
	if res.Int("result") == 1 && (res.Int("a_new_score") <= 1000 || res.Int("win_count") != 1) {
		t.Fatalf("a win gave no score: %v", res)
	}
	if res.Int("result") == 2 && (res.Int("a_new_score") != 1000 || res.Int("lose_count") != 1) {
		t.Fatalf("a loss moved the score: %v", res)
	}
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("count") != 2 || r.Int("do_count") != 1 || r.Int("add_time") == 0 {
		t.Fatalf("main after a fight: %v", r)
	}
	c.send(27251, proto.Message{})
	if r := c.waitFor(27251); len(r.Arr("rival_list")) != 3 {
		t.Fatalf("a new list after a fight: %v", r)
	}

	// a played challenge: the first duel is on screen, the skip button ends it
	c.send(27251, proto.Message{})
	rivals = c.waitFor(27251).Arr("rival_list")
	c.send(27252, proto.Message{"tid": rivals[1].Int("tid"), "srv_id": "symlf_1", "is_auto": 0})
	if r := c.waitFor(27252); r.Int("code") != 1 {
		t.Fatalf("played challenge: %v", r)
	}
	c.waitFor(20000)
	if intro := c.waitFor(20027); intro.Int("combat_type") != combatTeam {
		t.Fatalf("intro: %v", intro)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	c.waitFor(27253)

	// boxes need the challenges made
	c.send(27224, proto.Message{"id": 1})
	if r := c.waitFor(27224); r.Int("code") != 0 {
		t.Fatalf("box before the challenges: %v", r)
	}
	for i := 0; i < 3; i++ {
		e.advance(2*time.Hour + time.Minute)
		c.send(27251, proto.Message{})
		rivals = c.waitFor(27251).Arr("rival_list")
		c.send(27252, proto.Message{"tid": rivals[0].Int("tid"), "srv_id": "symlf_1", "is_auto": 1})
		if r := c.waitFor(27252); r.Int("code") != 1 {
			t.Fatalf("challenge %d: %v", i+3, r)
		}
		c.waitFor(27253)
	}
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("do_count") != 5 || r.Arr("award_list")[0].Int("status") != 1 || r.Arr("award_list")[1].Int("status") != 0 {
		t.Fatalf("main after five fights: %v", r)
	}
	c.send(27224, proto.Message{"id": 1})
	if r := c.waitFor(27224); r.Int("code") != 1 || r.Int("id") != 1 {
		t.Fatalf("box: %v", r)
	}
	c.send(27224, proto.Message{"id": 1})
	if r := c.waitFor(27224); r.Int("code") != 0 {
		t.Fatalf("box taken twice: %v", r)
	}

	c.send(27255, proto.Message{})
	logs := c.waitFor(27255).Arr("arena_team_log")
	if len(logs) != 5 || logs[0].Int("id") != 5 || len(logs[0].Arr("a_team_members")) != 3 {
		t.Fatalf("report list: %v", logs)
	}
	c.send(27256, proto.Message{"id": logs[0].Int("id")})
	if r := c.waitFor(27256); len(r.Arr("arena_replay_infos")) == 0 || r.Arr("arena_replay_infos")[0].Int("order") != 1 {
		t.Fatalf("report: %v", r)
	}
	c.send(27223, proto.Message{"start_rank": 1, "end_rank": 100})
	if r := c.waitFor(27223); r.Int("my_rank") != 1 || len(r.Arr("ranks")) != 1 || r.Arr("ranks")[0].Str("team_name") != "海贼团" {
		t.Fatalf("ranking: %v", r)
	}
}

func TestArenateamMembers(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(atFriday)
	a, ra := e.atRole("队长甲")
	b, rb := e.atRole("队员乙")

	a.send(27201, proto.Message{"name": "甲队", "is_check": 1, "limit_lev": 0, "limit_power": 0})
	a.waitFor(27201)
	b.send(27201, proto.Message{"name": "甲队"})
	if r := b.waitFor(27201); r.Int("code") != 0 {
		t.Fatalf("a name taken: %v", r)
	}
	b.send(27200, proto.Message{})
	if r := b.waitFor(27200); len(r.Arr("team_list")) != 1 || r.Arr("team_list")[0].Str("team_name") != "甲队" {
		t.Fatalf("hall: %v", r)
	}
	b.send(27210, proto.Message{"name": "甲"})
	if r := b.waitFor(27210); r.Int("code") != 1 || len(r.Arr("team_list")) != 1 {
		t.Fatalf("search team: %v", r)
	}
	tid := ra.RID * 0
	b.send(27200, proto.Message{})
	tid = b.waitFor(27200).Arr("team_list")[0].Int("tid")

	// a team that checks its members
	b.send(27202, proto.Message{"tid": tid, "srv_id": "symlf_1"})
	if r := b.waitFor(27202); r.Int("code") != 1 {
		t.Fatalf("apply: %v", r)
	}
	if r := a.waitFor(27222); r.Int("type") != 3 {
		t.Fatalf("push to the leader: %v", r)
	}
	b.send(27202, proto.Message{"tid": tid, "srv_id": "symlf_1"})
	if r := b.waitFor(27202); r.Int("code") != 0 {
		t.Fatalf("applied twice: %v", r)
	}
	a.send(27215, proto.Message{})
	if r := a.waitFor(27215); r.Arr("point")[1].Int("state") != 1 {
		t.Fatalf("red dot: %v", r)
	}
	a.send(27203, proto.Message{})
	if r := a.waitFor(27203).Arr("arena_team_member"); len(r) != 1 || r[0].Int("rid") != rb.RID {
		t.Fatalf("apply list: %v", r)
	}
	b.send(27204, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "type": 1})
	if r := b.waitFor(27204); r.Int("code") != 0 {
		t.Fatalf("a member answered: %v", r)
	}
	a.send(27204, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "type": 1})
	if r := a.waitFor(27204); r.Int("code") != 1 {
		t.Fatalf("accept: %v", r)
	}
	if r := b.waitFor(27222); r.Int("type") != 1 {
		t.Fatalf("push to the new member: %v", r)
	}
	b.send(27221, proto.Message{})
	if r := b.waitFor(27221); r.Int("tid") != tid || len(r.Arr("arena_team_member")) != 2 {
		t.Fatalf("details of the member: %v", r)
	}

	// order and hidden member
	a.send(27242, proto.Message{"pos_info": []proto.Message{{"rid": ra.RID, "sid": "symlf_1", "pos": 2, "is_hide": 0}, {"rid": rb.RID, "sid": "symlf_1", "pos": 1, "is_hide": 1}}})
	if r := a.waitFor(27242); r.Int("code") != 1 {
		t.Fatalf("order: %v", r)
	}
	a.send(27243, proto.Message{})
	if r := a.waitFor(27243).Arr("arena_team_member"); r[0].Int("rid") != rb.RID || r[0].Int("is_hide") != 1 || r[1].Int("rid") != ra.RID {
		t.Fatalf("members in order: %v", r)
	}
	b.send(27242, proto.Message{"pos_info": []proto.Message{}})
	if r := b.waitFor(27242); r.Int("code") != 0 {
		t.Fatalf("a member set the order: %v", r)
	}

	// leaving, being asked, the limits
	b.send(27211, proto.Message{})
	if r := b.waitFor(27211); r.Int("code") != 1 {
		t.Fatalf("leave: %v", r)
	}
	a.send(27205, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(27205); r.Int("code") != 1 {
		t.Fatalf("invite: %v", r)
	}
	if r := b.waitFor(27222); r.Int("type") != 4 {
		t.Fatalf("push of the invitation: %v", r)
	}
	b.send(27206, proto.Message{})
	if r := b.waitFor(27206).Arr("team_list"); len(r) != 1 || r[0].Int("tid") != tid {
		t.Fatalf("invitation list: %v", r)
	}
	b.send(27207, proto.Message{"tid": tid, "srv_id": "symlf_1", "type": 1})
	if r := b.waitFor(27207); r.Int("code") != 1 {
		t.Fatalf("accept invitation: %v", r)
	}
	if r := b.waitFor(27222); r.Int("type") != 1 {
		t.Fatalf("push of joining: %v", r)
	}
	a.send(27212, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(27212); r.Int("code") != 1 {
		t.Fatalf("kick: %v", r)
	}
	if r := b.waitFor(27222); r.Int("type") != 2 && r.Int("type") != 5 {
		t.Fatalf("push of the kick: %v", r)
	}
	a.send(27225, proto.Message{"limit_lev": 99, "limit_power": 0, "is_check": 0})
	if r := a.waitFor(27225); r.Int("code") != 1 {
		t.Fatalf("limits: %v", r)
	}
	b.send(27202, proto.Message{"tid": tid, "srv_id": "symlf_1"})
	if r := b.waitFor(27202); r.Int("code") != 0 {
		t.Fatalf("joined below the level limit: %v", r)
	}
	a.send(27225, proto.Message{"limit_lev": 0, "limit_power": 0, "is_check": 0})
	a.waitFor(27225)
	b.send(27202, proto.Message{"tid": tid, "srv_id": "symlf_1"})
	if r := b.waitFor(27202); r.Int("code") != 1 {
		t.Fatalf("joined a team without a check: %v", r)
	}

	// leader moves, names
	a.send(27226, proto.Message{"name": "丙队"})
	if r := a.waitFor(27226); r.Int("code") != 1 {
		t.Fatalf("rename: %v", r)
	}
	b.send(27226, proto.Message{"name": "乙队"})
	if r := b.waitFor(27226); r.Int("code") != 0 {
		t.Fatalf("a member renamed: %v", r)
	}
	a.send(27213, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(27213); r.Int("code") != 1 {
		t.Fatalf("transfer: %v", r)
	}
	a.send(27212, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(27212); r.Int("code") != 0 {
		t.Fatalf("the old leader kicked: %v", r)
	}
	b.send(27211, proto.Message{})
	b.waitFor(27211)
	a.send(27220, proto.Message{})
	if r := a.waitFor(27220); r.Int("tid") != tid || r.Int("team_power") < 0 || len(r.Arr("team_members")) != 1 {
		t.Fatalf("main of the one left: %v", r)
	}
	a.send(27211, proto.Message{})
	a.waitFor(27211)
	b.send(27200, proto.Message{})
	if r := b.waitFor(27200); len(r.Arr("team_list")) != 0 {
		t.Fatalf("an empty team stays: %v", r)
	}
}

func TestArenateamLocksAfterSignAndSettles(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(atFriday)
	c, role := e.atRole("队长")
	atTeamOfRobots(t, c, "锁定队")
	c.send(27211, proto.Message{})
	if r := c.waitFor(27211); r.Int("code") != 0 {
		t.Fatalf("left after the sign-up: %v", r)
	}
	c.send(27241, proto.Message{})
	if r := c.waitFor(27241); r.Int("code") != 1 {
		t.Fatalf("cancel the sign-up before the match: %v", r)
	}
	c.send(27240, proto.Message{})
	if r := c.waitFor(27240); r.Int("code") != 1 {
		t.Fatalf("sign again: %v", r)
	}
	e.setNow(atSaturday)
	c.send(27241, proto.Message{})
	if r := c.waitFor(27241); r.Int("code") != 0 {
		t.Fatalf("cancelled during the match: %v", r)
	}

	e.setNow(time.Date(2026, 10, 25, 23, 10, 0, 0, dayZone))
	c.send(27251, proto.Message{})
	if r := c.waitFor(27251); len(r.Arr("rival_list")) != 0 {
		t.Fatalf("rivals while settling: %v", r)
	}
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("state") != 4 {
		t.Fatalf("settling state: %v", r)
	}

	// the rewards come by mail after 23:40: the rank reward and the boxes not taken
	e.setNow(time.Date(2026, 10, 25, 23, 50, 0, 0, dayZone))
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("state") != 0 {
		t.Fatalf("closed state: %v", r)
	}
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, m := range mails {
		if m.Subject == "组队竞技场排名奖励" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("rank reward mails: %d of %d", found, len(mails))
	}
	c.send(27220, proto.Message{})
	c.waitFor(27220)
	if mails2, _ := e.st.MailsForRole(role); len(mails2) != len(mails) {
		t.Fatalf("the season was settled twice: %d then %d mails", len(mails), len(mails2))
	}

	// the next season starts over
	e.setNow(time.Date(2026, 10, 30, 13, 0, 0, 0, dayZone))
	c.send(27220, proto.Message{})
	if r := c.waitFor(27220); r.Int("state") != 1 || r.Int("is_sign") != 0 || r.Int("tid") == 0 {
		t.Fatalf("new season: %v", r)
	}
}
