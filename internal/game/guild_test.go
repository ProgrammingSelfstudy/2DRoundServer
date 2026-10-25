package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// waitFor reads frames until the wanted command comes, skipping pushes the test does not care about.
func (c *client) waitFor(cmd uint16) proto.Message {
	c.t.Helper()
	for i := 0; i < 30; i++ {
		got, m := c.recvNext()
		if got == cmd {
			return m
		}
	}
	c.t.Fatalf("command %d never came", cmd)
	return nil
}

// grant puts currencies in a role's ledger through a claimed mail and drains the replies.
func (e *env) grantAssets(c *client, role *store.Role, assets map[int64]int64) {
	e.t.Helper()
	var atts []store.Attachment
	for id, n := range assets {
		atts = append(atts, store.Attachment{Kind: store.AttachAsset, ItemID: id, Num: n})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.waitFor(10801)
	c.waitFor(10500)
}

func TestGuildLifeCycle(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, ra := e.enter("ua", "会长")
	b, rb := e.enter("ub", "乙乙")
	cc, rc := e.enter("uc", "丙丙")
	for _, r := range []*store.Role{ra, rb, rc} {
		if err := e.st.SetRoleLevel(r.RID, 20); err != nil {
			t.Fatal(err)
		}
	}
	create := func(name string) proto.Message {
		a.send(13500, proto.Message{"name": name, "sign": "海贼王", "apply_type": 0, "apply_lev": 1, "apply_power": 0})
		return a.waitFor(13500)
	}
	if r := create("一"); r.Int("code") != 0 { // 2-6 characters
		t.Fatalf("name too short: %v", r)
	}
	if r := create("海贼联盟"); r.Int("code") != 0 || r.Str("msg") == "" { // VIP 2 needed
		t.Fatalf("no vip: %v", r)
	}
	if err := e.st.SetRoleVIP(ra.RID, 2); err != nil {
		t.Fatal(err)
	}
	if r := create("海贼联盟"); r.Int("code") != 0 { // no gems
		t.Fatalf("no gems: %v", r)
	}
	e.grantAssets(a, ra, map[int64]int64{3: 1000, 1: 100000})
	if r := create("海贼联盟"); r.Int("code") != 1 {
		t.Fatalf("create: %v", r)
	}
	base := a.waitFor(10301)
	if base.Int("gid") == 0 || base.Str("gname") != "海贼联盟" || base.Int("position") != store.PostLeader || base.Int("guild_lev") != 1 {
		t.Fatalf("10301 after creating: %v", base)
	}
	info := a.waitFor(13518)
	if info.Str("name") != "海贼联盟" || info.Int("members_num") != 1 || info.Int("members_max") != 15 || info.Str("leader_name") != "会长" {
		t.Fatalf("13518: %v", info)
	}
	gid := base.Int("gid")
	b.send(13500, proto.Message{"name": "海贼联盟", "sign": "", "apply_type": 0, "apply_lev": 1, "apply_power": 0})
	if r := b.waitFor(13500); r.Int("code") != 0 {
		t.Fatalf("someone else cannot create: %v", r)
	}

	// the list and a free join
	b.send(13501, proto.Message{"page": 1, "flag": 1, "num": 10, "name": ""})
	list := b.waitFor(13501)
	if list.Int("all_count") != 1 || len(list.Arr("guilds")) != 1 || list.Arr("guilds")[0].Str("name") != "海贼联盟" || list.Arr("guilds")[0].Int("is_apply") != 0 {
		t.Fatalf("list: %v", list)
	}
	b.send(13501, proto.Message{"page": 1, "flag": 2, "num": 10, "name": "没有"})
	if l := b.waitFor(13501); l.Int("all_count") != 0 {
		t.Fatalf("search: %v", l)
	}
	join := func(c *client, kind int64) proto.Message {
		c.send(13503, proto.Message{"gid": gid, "gsrv_id": "symlf_1", "type": kind})
		return c.waitFor(13503)
	}
	if r := join(b, 1); r.Int("code") != 1 || r.Int("is_apply") != 0 {
		t.Fatalf("free join: %v", r)
	}
	if m := b.waitFor(10301); m.Int("gid") != gid || m.Int("position") != store.PostMember {
		t.Fatalf("10301 after joining: %v", m)
	}
	a.send(13519, proto.Message{})
	if m := a.waitFor(13519); len(m.Arr("members")) != 2 || m.Arr("members")[0].Int("post") != store.PostLeader {
		t.Fatalf("members: %v", m)
	}

	// approval needed: C asks, A accepts
	a.send(13522, proto.Message{"apply_type": 1, "apply_lev": 10, "apply_power": 0})
	if r := a.waitFor(13522); r.Int("code") != 1 {
		t.Fatalf("apply set: %v", r)
	}
	b.send(13522, proto.Message{"apply_type": 0, "apply_lev": 1, "apply_power": 0})
	if r := b.waitFor(13522); r.Int("code") != 0 { // a member has no say
		t.Fatalf("member sets: %v", r)
	}
	if r := join(cc, 1); r.Int("code") != 1 || r.Int("is_apply") != 1 {
		t.Fatalf("ask: %v", r)
	}
	if r := a.waitFor(13573); r.Int("code") != 1 {
		t.Fatalf("red dot: %v", r)
	}
	a.send(13507, proto.Message{"page": 1, "num": 10})
	if m := a.waitFor(13507); len(m.Arr("guids")) != 1 || m.Arr("guids")[0].Int("rid") != rc.RID {
		t.Fatalf("applicants: %v", m)
	}
	if r := join(cc, 2); r.Int("code") != 1 || r.Int("is_apply") != 0 { // takes it back
		t.Fatalf("cancel: %v", r)
	}
	join(cc, 1)
	a.send(13505, proto.Message{"type": 1, "rid": rc.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(13505); r.Int("code") != 1 {
		t.Fatalf("accept: %v", r)
	}
	if m := cc.waitFor(10301); m.Int("gid") != gid {
		t.Fatalf("C joined: %v", m)
	}

	// posts and kicking
	a.send(13520, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "position": 2})
	if r := a.waitFor(13520); r.Int("code") != 1 {
		t.Fatalf("appoint: %v", r)
	}
	b.send(13513, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	if r := b.waitFor(13513); r.Int("code") != 0 { // not the leader
		t.Fatalf("kick the leader: %v", r)
	}
	b.send(13513, proto.Message{"rid": rc.RID, "srv_id": "symlf_1"})
	if r := b.waitFor(13513); r.Int("code") != 1 {
		t.Fatalf("assistant kicks a member: %v", r)
	}
	if m := cc.waitFor(10301); m.Int("gid") != 0 {
		t.Fatalf("C is out: %v", m)
	}
	if r := join(cc, 1); r.Int("code") != 0 { // 12 hours after being removed
		t.Fatalf("C too soon: %v", r)
	}
	e.advance(13 * time.Hour)

	// donation: once a day, coin buys it, the guild gains exp
	b.send(13524, proto.Message{"type": 1})
	if r := b.waitFor(13524); r.Int("code") != 0 { // no coin
		t.Fatalf("donate without coin: %v", r)
	}
	e.grantAssets(b, rb, map[int64]int64{1: 50000})
	b.send(13524, proto.Message{"type": 1})
	if r := b.waitFor(13524); r.Int("code") != 1 {
		t.Fatalf("donate: %v", r)
	}
	st := b.waitFor(13523)
	if st.Int("donate_exp") != 50 || len(st.Arr("donate_list")) != 1 || st.Arr("donate_list")[0].Int("type") != 1 {
		t.Fatalf("donate state: %v", st)
	}
	b.send(13524, proto.Message{"type": 2})
	if r := b.waitFor(13524); r.Int("code") != 0 {
		t.Fatalf("twice a day: %v", r)
	}
	b.send(13574, proto.Message{"box_id": 1})
	if r := b.waitFor(13574); r.Int("code") != 0 { // 50 of 1000
		t.Fatalf("box too early: %v", r)
	}
	gd, _ := e.st.GuildByID(gid)
	if gd.Exp != 50 {
		t.Fatalf("guild exp %d", gd.Exp)
	}
	if ledger, _ := e.st.RoleAssets(rb.RID); ledger[10] != 100 { // 100 contribution
		t.Fatalf("contribution: %v", ledger)
	}

	// the log
	a.send(13577, proto.Message{})
	if m := a.waitFor(13577); len(m.Arr("guild_log_info_list")) < 4 {
		t.Fatalf("log: %v", m)
	}

	// rename, leaving, disbanding
	a.send(13568, proto.Message{"name": "新海贼团"})
	if r := a.waitFor(13568); r.Int("code") != 1 {
		t.Fatalf("rename: %v", r)
	}
	a.send(13568, proto.Message{"name": "又改名吧"})
	if r := a.waitFor(13568); r.Int("code") != 0 { // cooling down
		t.Fatalf("rename again: %v", r)
	}
	a.send(13514, proto.Message{})
	if r := a.waitFor(13514); r.Int("code") != 0 { // the leader cannot just leave
		t.Fatalf("leader leaves: %v", r)
	}
	b.send(13514, proto.Message{})
	if r := b.waitFor(13514); r.Int("code") != 1 {
		t.Fatalf("leave: %v", r)
	}
	a.send(13516, proto.Message{})
	if r := a.waitFor(13516); r.Int("code") != 1 {
		t.Fatalf("disband: %v", r)
	}
	if _, err := e.st.GuildByID(gid); err == nil {
		t.Fatal("the guild must be gone")
	}
}
