package game

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type env struct {
	t       *testing.T
	st      *store.Store
	gw      *gateway.Server
	addr    string
	logs    *syncBuf
	login   *Login
	friends *FriendService
	chat    *ChatService
	mail    *MailService
	notes   *NoticeService
	online  *OnlineGiftService
	invite  *InviteService
	clk     *atomic.Int64 // the gateway's clock, unix nanoseconds; it only moves with advance
}

// advance moves the gateway's clock forward.
func (e *env) advance(d time.Duration) { e.clk.Add(int64(d)) }

// setNow puts the gateway's clock at t.
func (e *env) setNow(t time.Time) { e.clk.Store(t.UnixNano()) }

func newEnv(t *testing.T, idle time.Duration) *env {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	schema, _ := proto.LoadSchema()
	logs := &syncBuf{}
	lg := log.New(logs, "", 0)
	clk := &atomic.Int64{}
	clk.Store(time.Now().UnixNano())
	gw := &gateway.Server{Schema: schema, Codec: proto.DefaultOptions(), Idle: idle, Log: lg, Now: func() time.Time { return time.Unix(0, clk.Load()) }}
	gw.Hub = gateway.NewHub()
	gw.Router = gateway.NewRouter()
	fs := &FriendService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg}
	fs.Register(gw.Router)
	og := &OnlineGiftService{Store: st, Hub: gw.Hub, Log: lg}
	og.Register(gw.Router)
	(&CheckinService{Store: st}).Register(gw.Router)
	(&GuildService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg}).Register(gw.Router)
	inv := &InviteService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg}
	inv.Register(gw.Router)
	gw.OnDisconnect = func(s *gateway.Session) { fs.Disconnected(s); og.Disconnected(s) }
	cs := &ChatService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg}
	cs.Register(gw.Router)
	l := &Login{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg, MaxRoles: 2, Friends: fs, OnlineGift: og, Chat: cs}
	l.Register(gw.Router)
	ms := &MailService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: lg}
	ms.Register(gw.Router)
	ns := &NoticeService{Store: st, Hub: gw.Hub, Log: lg}
	ns.Register(gw.Router)
	ms.Notices = ns
	RegisterBag(gw.Router, st)
	RegisterUse(gw.Router, st)
	RegisterPartner(gw.Router, st)
	RegisterQuest(gw.Router, st)
	RegisterLooks(gw.Router, st)
	RegisterRoleMisc(gw.Router, st)
	RegisterShop(gw.Router, st)
	RegisterMarket(gw.Router, st)
	RegisterArena(gw.Router, st)
	RegisterStarTower(gw.Router, st)
	RegisterAdventure(gw.Router, st)
	RegisterPlanesafk(gw.Router, st)
	RegisterPlanes(gw.Router, st)
	RegisterEmoguoshi(gw.Router, st)
	RegisterEndless(gw.Router, st)
	RegisterHeaven(gw.Router, st)
	RegisterHallows(gw.Router, st)
	RegisterExpedition(gw.Router, st)
	RegisterMonopoly(gw.Router, st)
	RegisterEscort(gw.Router, st)
	RegisterPracticeTower(gw.Router, st)
	RegisterSmashegg(gw.Router, st)
	RegisterVoyage(gw.Router, st)
	RegisterEncounter(gw.Router, st)
	RegisterTrainingcamp(gw.Router, st)
	RegisterStoneDungeon(gw.Router, st)
	RegisterForge(gw.Router, st)
	RegisterDramaBattle(gw.Router, st)
	RegisterDramaExtras(gw.Router, st)
	RegisterDramaMilestones(gw.Router, st)
	RegisterRank(gw.Router, st)
	RegisterEquipment(gw.Router, st)
	RegisterFormation(gw.Router, st)
	RegisterFeat(gw.Router, st)
	RegisterCompose(gw.Router, st)
	RegisterBattle(gw.Router, st)
	RegisterElfin(gw.Router, st)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go gw.Serve(ln)
	t.Cleanup(func() { gw.Close(); st.Close() })
	return &env{t: t, st: st, gw: gw, addr: ln.Addr().String(), logs: logs, login: l, friends: fs, chat: cs, online: og, invite: inv, clk: clk, mail: ms, notes: ns}
}

func (e *env) account(name string) (*store.Account, string) {
	a, err := e.st.FindOrCreateAccount("symlf", name)
	if err != nil {
		e.t.Fatal(err)
	}
	tok, _ := e.st.IssueToken(a.ID, 3600)
	return a, tok
}

type client struct {
	t      *testing.T
	c      net.Conn
	br     *bufio.Reader
	schema *proto.Schema
	idx    int64
}

func (e *env) dial(handshake bool) *client {
	e.t.Helper()
	c, err := net.Dial("tcp", e.addr)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { c.Close() })
	schema, _ := proto.LoadSchema()
	if handshake {
		c.Write([]byte(proto.Handshake))
	}
	return &client{t: e.t, c: c, br: bufio.NewReader(c), schema: schema}
}

func exempt(cmd uint16) bool {
	return cmd == 10300 || (cmd >= 16601 && cmd <= 16603) || cmd/100 == 111 || cmd/100 == 200
}

// frames returns the bytes the client sends for one command: a correctly signed 1197
// (unless exempt or badSign) and then the command itself.
func (c *client) frames(cmd uint16, msg proto.Message, badSign ...bool) []byte {
	c.t.Helper()
	o := proto.DefaultOptions()
	var out []byte
	if !exempt(cmd) {
		c.idx++
		now := time.Now().Unix()
		sum := md5.Sum([]byte(fmt.Sprintf("%d%d%d__sszg666__", c.idx, now, cmd)))
		sign := hex.EncodeToString(sum[:])
		if len(badSign) > 0 && badSign[0] {
			sign = strings.Repeat("0", 32)
		}
		p, _ := proto.Encode(o, c.schema.Send[1197], proto.Message{"sign": sign, "idx": c.idx, "code": int64(cmd) + (c.idx+5)%16, "time": now})
		b, _ := proto.Frame{Command: 1197, Payload: p}.Encode()
		out = append(out, b...)
	}
	p, err := proto.Encode(o, c.schema.Send[cmd], msg)
	if err != nil {
		c.t.Fatal(err)
	}
	b, _ := proto.Frame{Command: cmd, Payload: p}.Encode()
	return append(out, b...)
}

// send writes the frames of one command in a single write.
func (c *client) send(cmd uint16, msg proto.Message, badSign ...bool) {
	c.t.Helper()
	c.c.Write(c.frames(cmd, msg, badSign...))
}

func (c *client) recv(cmd uint16) proto.Message {
	c.t.Helper()
	c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	fr, err := proto.ReadFrame(c.br)
	for err == nil && (fr.Command == 10403 || fr.Command == 10344 || fr.Command == 16401 || fr.Command == 10501) && fr.Command != cmd {
		fr, err = proto.ReadFrame(c.br) // daily quest progress (10403), level-up (10344), achievement (16401) and equipment bag (10501, sent with every 10500) pushes are only read by tests that ask for them
	}
	if err != nil {
		c.t.Fatalf("waiting for %d: %v", cmd, err)
	}
	if fr.Command != cmd {
		c.t.Fatalf("got command %d, want %d", fr.Command, cmd)
	}
	m, err := proto.Decode(proto.DefaultOptions(), c.schema.Recv[cmd], fr.Payload)
	if err != nil {
		c.t.Fatalf("decode %d: %v", cmd, err)
	}
	return m
}

func (c *client) expectClosed() {
	c.t.Helper()
	c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := proto.ReadFrame(c.br); err != io.EOF && !strings.Contains(fmt.Sprint(err), "EOF") && !strings.Contains(fmt.Sprint(err), "reset") && !strings.Contains(fmt.Sprint(err), "closed") {
		c.t.Fatalf("expected the connection to be closed, got %v", err)
	}
}

func loginArgs(account, token string) proto.Message {
	kv := func(k, v string) proto.Message { return proto.Message{"key": k, "val": v} }
	return proto.Message{"args": []proto.Message{kv("account", account), kv("token", token), kv("enter_srv_id", "symlf_1"), kv("platform", "symlf")}}
}

func TestFullLoginFlow(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("80566037")
	c := e.dial(true)

	c.send(1164, loginArgs("80566037", tok))
	r := c.recv(1164)
	if r.Int("code") != 1 || len(r.Arr("roles")) != 0 {
		t.Fatalf("fresh account: %v", r)
	}

	c.send(10191, proto.Message{"sex": 1, "name": "路飞", "career": 2, "playform": "symlf"})
	cr := c.recv(10191)
	if cr.Int("code") != 1 || cr.Int("rid") == 0 || cr.Str("name") != "路飞" || cr.Str("srv_id") != "symlf_1" || cr.Int("reg_time") == 0 {
		t.Fatalf("create: %v", cr)
	}
	rid := cr.Int("rid")

	// reconnect: the role list now has the role
	c2 := e.dial(true)
	c2.send(1164, loginArgs("80566037", tok))
	rl := c2.recv(1164).Arr("roles")
	if len(rl) != 1 || rl[0].Int("rid") != rid || rl[0].Str("name") != "路飞" || rl[0].Int("sex") != 1 || rl[0].Int("career") != 2 {
		t.Fatalf("role list: %v", rl)
	}

	c2.send(10192, proto.Message{"rid": rid, "srv_id": "symlf_1"})
	lr := c2.recv(10192)
	if lr.Int("code") != 1 || lr.Int("timestamp") == 0 {
		t.Fatalf("login: %v", lr)
	}
	if e.gw.Hub.Online(rid) == nil {
		t.Fatal("role must be online after 10192")
	}

	c2.send(10300, proto.Message{})
	base := c2.recv(10301)
	if base.Int("rid") != rid || base.Str("name") != "路飞" || base.Int("lev") != 1 || base.Int("career") != 2 ||
		base.Int("exp_max") != 120 || base.Int("exp_total_nextlev") != 120 || base.Int("is_first_rename") != 1 || base.Int("reg_time") == 0 {
		t.Fatalf("10301: %v", base)
	}
	if as := c2.recv(10302); as.Int("lev") != 1 || as.Str("gold") != "0" || as.Str("coin") != "0" {
		t.Fatalf("10302: %v", as)
	}
	if dr := c2.recv(13000); dr.Int("mode") != 1 || dr.Int("chapter_id") != 1 || dr.Int("dun_id") != 10010 || dr.Int("max_dun_id") != 0 || dr.Int("is_first") != 1 || dr.Int("status") != 2 {
		// max_dun_id is the highest dungeon cleared: 0 for a new role (capture original-2026-10-22)
		t.Fatalf("13000 must give a new player the first dungeon: %v", dr)
	}
	if enter := c2.recv(10304); enter.Int("play_video") != 0 {
		t.Fatalf("10304 must be pushed after the role data: %v", enter)
	}
	c2.send(1169, proto.Message{})
	if hb := c2.recv(1169); hb.Int("time") == 0 {
		t.Fatalf("heartbeat: %v", hb)
	}
	if strings.Contains(e.logs.String(), "signature mismatch") || strings.Contains(e.logs.String(), "not preceded") {
		t.Fatalf("valid signatures were flagged:\n%s", e.logs.String())
	}
}

func TestNoHandshakeIsTolerated(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(false)
	c.send(1164, loginArgs("u1", tok))
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("%v", r)
	}
}

func TestBadHandshakeClosesConnection(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := net.Dial("tcp", e.addr)
	defer c.Close()
	c.Write([]byte("game_client-----------X")) // right prefix, wrong text
	cl := &client{t: t, c: c, br: bufio.NewReader(c)}
	cl.expectClosed()
}

func TestRoleListRejections(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, tok := e.account("u1")
	cases := []struct {
		name string
		args proto.Message
		code int64
		msg  string
	}{
		{"unknown token", loginArgs("u1", "nope"), 2, "登录凭证无效"},
		{"token of another account", loginArgs("someone_else", tok), 2, "登录凭证无效"},
		{"empty token", loginArgs("u1", ""), 2, "登录凭证无效"},
	}
	for _, tc := range cases {
		c := e.dial(true)
		c.send(1164, tc.args)
		r := c.recv(1164)
		if r.Int("code") != tc.code || !strings.Contains(r.Str("msg"), tc.msg) || len(r.Arr("roles")) != 0 {
			t.Errorf("%s: %v", tc.name, r)
		}
	}
	// banned account: tokens are revoked on ban, and a fresh token must still be refused
	e.st.SetAccountStatus(a.ID, 0, "test")
	tok2, _ := e.st.IssueToken(a.ID, 3600)
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok2))
	if r := c.recv(1164); r.Int("code") != 2 || !strings.Contains(r.Str("msg"), "封禁") {
		t.Fatalf("banned: %v", r)
	}
	// maintenance
	e.st.SetAccountStatus(a.ID, 1, "")
	srv, _ := e.st.Server("symlf_1")
	srv.Status = 0
	e.st.UpsertServer(*srv)
	tok3, _ := e.st.IssueToken(a.ID, 3600)
	c = e.dial(true)
	c.send(1164, loginArgs("u1", tok3))
	if r := c.recv(1164); r.Int("code") != 4 {
		t.Fatalf("maintenance: %v", r)
	}
}

func TestCreateRoleRules(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	_, tok2 := e.account("u2")
	c := e.dial(true)

	// not logged in yet
	c.send(10191, proto.Message{"name": "x"})
	if r := c.recv(10191); r.Int("code") != 0 {
		t.Fatalf("create before login: %v", r)
	}
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)

	for _, bad := range []string{" lead", "trail ", "a b", strings.Repeat("长", 13), "ctl\x07"} {
		c.send(10191, proto.Message{"name": bad})
		if r := c.recv(10191); r.Int("code") != 0 {
			t.Errorf("name %q must be refused: %v", bad, r)
		}
	}
	c.send(10191, proto.Message{"name": strings.Repeat("长", 12)})
	if r := c.recv(10191); r.Int("code") != 1 {
		t.Fatalf("12-rune name: %v", r)
	}
	c.send(10191, proto.Message{"name": "二号"})
	if r := c.recv(10191); r.Int("code") != 1 {
		t.Fatalf("second role: %v", r)
	}
	c.send(10191, proto.Message{"name": "三号"})
	if r := c.recv(10191); r.Int("code") != 0 || !strings.Contains(r.Str("msg"), "上限") {
		t.Fatalf("role limit: %v", r)
	}

	// duplicate name from another account
	c2 := e.dial(true)
	c2.send(1164, loginArgs("u2", tok2))
	c2.recv(1164)
	c2.send(10191, proto.Message{"name": "二号"})
	if r := c2.recv(10191); r.Int("code") != 0 || !strings.Contains(r.Str("msg"), "已被使用") {
		t.Fatalf("duplicate name: %v", r)
	}
}

func TestEnterRules(t *testing.T) {
	e := newEnv(t, time.Minute)
	a1, tok1 := e.account("u1")
	a2, tok2 := e.account("u2")
	r1, _ := e.st.CreateRole(a1.ID, "symlf_1", "甲", 0, 1, 0)
	r2, _ := e.st.CreateRole(a2.ID, "symlf_1", "乙", 0, 1, 0)
	c := e.dial(true)

	c.send(10192, proto.Message{"rid": r1.RID, "srv_id": "symlf_1"}) // before 1164
	if r := c.recv(10192); r.Int("code") != 2 {
		t.Fatalf("enter before login: %v", r)
	}
	c.send(1164, loginArgs("u1", tok1))
	c.recv(1164)

	c.send(10192, proto.Message{"rid": r2.RID, "srv_id": "symlf_1"}) // someone else's role
	if r := c.recv(10192); r.Int("code") != 0 {
		t.Fatalf("foreign role: %v", r)
	}
	c.send(10192, proto.Message{"rid": 99999, "srv_id": "symlf_1"})
	if r := c.recv(10192); r.Int("code") != 0 {
		t.Fatalf("unknown role: %v", r)
	}
	c.send(10192, proto.Message{"rid": r1.RID, "srv_id": "symlf_2"}) // wrong server
	if r := c.recv(10192); r.Int("code") != 0 {
		t.Fatalf("wrong server: %v", r)
	}
	e.st.SetRoleStatus(r1.RID, 0)
	c.send(10192, proto.Message{"rid": r1.RID, "srv_id": "symlf_1"})
	if r := c.recv(10192); r.Int("code") != 0 || !strings.Contains(r.Str("msg"), "封禁") {
		t.Fatalf("banned role: %v", r)
	}
	_ = tok2
}

func TestRelogin10193AndKick(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, tok := e.account("u1")
	r, _ := e.st.CreateRole(a.ID, "symlf_1", "甲", 0, 1, 0)

	first := e.dial(true)
	first.send(1164, loginArgs("u1", tok))
	first.recv(1164)
	first.send(10192, proto.Message{"rid": r.RID, "srv_id": "symlf_1"})
	first.recv(10192)

	second := e.dial(true)
	second.send(1164, loginArgs("u1", tok))
	roles := second.recv(1164).Arr("roles")
	if roles[0].Int("is_online") != 1 {
		t.Fatalf("is_online must be 1 while the role is online: %v", roles)
	}
	second.send(10193, proto.Message{"rid": r.RID, "srv_id": "symlf_1"})
	if rep := second.recv(10193); rep.Int("code") != 1 {
		t.Fatalf("relogin: %v", rep)
	}
	kick := first.recv(10310)
	if kick.Int("is_show") != 1 || kick.Str("msg") == "" {
		t.Fatalf("kick message: %v", kick)
	}
	first.expectClosed()
	if e.gw.Hub.Online(r.RID) == nil {
		t.Fatal("the second session must stay registered")
	}
}

func TestDisconnectUnregistersRole(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, tok := e.account("u1")
	r, _ := e.st.CreateRole(a.ID, "symlf_1", "甲", 0, 1, 0)
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)
	c.send(10192, proto.Message{"rid": r.RID, "srv_id": "symlf_1"})
	c.recv(10192)
	c.c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for e.gw.Hub.Online(r.RID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("role still online after disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnknownCommandsAndBadSignatureAreSurvivable(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	// a command with a send schema but no handler, then a mismatching 1197
	c.send(23304, proto.Message{}) // title composing: not implemented yet
	c.send(1164, loginArgs("u1", tok), true)
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("login after unknown command / bad signature: %v", r)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, "cmd 23304: no handler") || !strings.Contains(logs, "cmd 1164: 1197 signature mismatch") {
		t.Fatalf("expected both events to be logged:\n%s", logs)
	}
}

func TestIdleTimeoutClosesConnection(t *testing.T) {
	e := newEnv(t, 150*time.Millisecond)
	c := e.dial(true)
	c.expectClosed()
}

func TestMalformedPayloadClosesConnection(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	// 10192 needs rid(4)+string; send only 2 bytes
	b, _ := proto.Frame{Command: 10192, Payload: []byte{0, 1}}.Encode()
	c.c.Write(b)
	c.expectClosed()
}

func TestEmptyNameGetsUniquePlaceholder(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	_, tok2 := e.account("u2")
	names := map[string]bool{}
	for i, tk := range []string{tok, tok2} {
		c := e.dial(true)
		c.send(1164, loginArgs(fmt.Sprintf("u%d", i+1), tk))
		c.recv(1164)
		c.send(10191, proto.Message{"sex": 0, "name": "", "career": 1, "playform": "symlf"})
		r := c.recv(10191)
		if r.Int("code") != 1 || !strings.HasPrefix(r.Str("name"), "玩家") || r.Int("rid") == 0 {
			t.Fatalf("empty name must create a placeholder role: %v", r)
		}
		if names[r.Str("name")] {
			t.Fatalf("placeholder names must be unique: %v", r.Str("name"))
		}
		names[r.Str("name")] = true
	}
	// a player who already took "玩家3" must not break the next auto-name
	a, _ := e.st.FindOrCreateAccount("symlf", "u3")
	e.st.CreateRole(a.ID, "symlf_1", "玩家4", 0, 1, 0)
	tok3, _ := e.st.IssueToken(a.ID, 60)
	c := e.dial(true)
	c.send(1164, loginArgs("u3", tok3))
	c.recv(1164)
	c.send(10191, proto.Message{"name": ""})
	r := c.recv(10191)
	if r.Int("code") != 1 || r.Str("name") != "玩家4_1" { // rid 4 would be "玩家4", which is taken
		t.Fatalf("auto-name after a clash: %v", r)
	}
}

func TestRoleDataBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)
	c.send(10300, proto.Message{}) // no role selected yet: nothing may be sent
	c.send(1169, proto.Message{})
	if hb := c.recv(1169); hb.Int("time") == 0 { // the next frame is the heartbeat reply, not role data
		t.Fatal(hb)
	}
}

func TestExpTableMatchesClientData(t *testing.T) {
	if m, tot := expFor(1); m != 120 || tot != 120 {
		t.Fatalf("lev1: %d %d", m, tot)
	}
	if m, tot := expFor(2); m != 360 || tot != 480 {
		t.Fatalf("lev2: %d %d", m, tot)
	}
	// the client's own exp_total for lev 24 and 124 is not the running sum (911300 and
	// 238279300 instead of 472100 and 204591100). The original sends the client's number
	// in 10301 (seen for lev 124, capture original-2026-10-22); exp_max, which decides
	// levelling, is untouched.
	if m, tot := expFor(24); m != 64800 || tot != 911300 {
		t.Fatalf("lev24: %d %d", m, tot)
	}
	if m, tot := expFor(124); m != 6286800 || tot != 238279300 {
		t.Fatalf("lev124: %d %d", m, tot)
	}
	if _, tot := expFor(123); tot != 198304300 { // a normal level keeps the running sum
		t.Fatalf("lev123: %d", tot)
	}
	if m, tot := expFor(0); m != 0 || tot != 0 {
		t.Fatal("out of range must be 0")
	}
	if m, _ := expFor(201); m != 0 {
		t.Fatal("out of range must be 0")
	}
	if len(expTable) != 200 {
		t.Fatalf("levels: %d", len(expTable))
	}
}

func TestEmptyRepliesAnswerAndRealHandlersWin(t *testing.T) {
	e := newEnv(t, time.Minute)
	// the list shrinks as commands get real handlers (tools/gen_empty_replies.py drops them),
	// so only its sanity is checked: a good number of commands, none twice
	if len(emptyReplies) < 100 {
		t.Fatalf("only %d recorded commands", len(emptyReplies))
	}
	seen := map[uint16]bool{}
	for _, r := range emptyReplies {
		if seen[r.Cmd] {
			t.Fatalf("command %d listed twice", r.Cmd)
		}
		seen[r.Cmd] = true
	}
	// a real handler registered first must not be replaced
	called := false
	e.gw.Router.Handle(10924, func(s *gateway.Session, _ proto.Message) error {
		called = true
		return s.Send(10924, proto.Message{})
	})
	RegisterEmpty(e.gw.Router, log.New(io.Discard, "", 0))
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)

	c.send(10922, proto.Message{}) // an empty-reply command
	if r := c.recv(10922); len(r) != 0 && r.Int("status") != 0 {
		t.Fatalf("10922: %v", r)
	}
	c.send(10925, proto.Message{"id": 77}) // a request with a payload: the key is echoed
	if r := c.recv(10925); r.Int("id") != 77 || r.Int("status") != 0 {
		t.Fatalf("10925 must echo id and leave the rest empty: %v", r)
	}
	c.send(10924, proto.Message{})
	c.recv(10924)
	if !called {
		t.Fatal("the real handler must win over the empty reply")
	}
	// every listed command has a receive schema and encodes as an empty message
	schema, _ := proto.LoadSchema()
	for _, er := range emptyReplies {
		f, ok := schema.Recv[er.Cmd]
		if !ok || len(f) == 0 {
			t.Errorf("cmd %d (%s) has no receive schema", er.Cmd, er.Module)
			continue
		}
		if _, err := proto.Encode(proto.DefaultOptions(), f, proto.Message{}); err != nil {
			t.Errorf("cmd %d: %v", er.Cmd, err)
		}
	}
}

func TestCombatTypeEchoesTheRequestedFightType(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)
	for _, ct := range []int64{0, 4, 17} { // 0 = leaving a battle
		c.send(20060, proto.Message{"combat_type": ct})
		if r := c.recv(20060); r.Int("combat_type") != ct || r.Int("type") != 0 {
			t.Fatalf("combat_type %d: %v", ct, r)
		}
	}
}

func TestSpriteTeamsEchoesTheRequestedType(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)
	for _, typ := range []int64{1, 7} {
		c.send(26555, proto.Message{"type": typ})
		r := c.recv(26555)
		if r.Int("type") != typ || len(r.Arr("team_list")) != 0 {
			t.Fatalf("type %d: %v", typ, r)
		}
	}
}

func TestDramaChainDataAndPersistence(t *testing.T) {
	if dramaChain.First != 10010 || len(dramaChain.Dungeons) != 640 {
		t.Fatalf("chain: first=%d n=%d", dramaChain.First, len(dramaChain.Dungeons))
	}
	for i, d := range dramaChain.Dungeons[:len(dramaChain.Dungeons)-1] {
		if d.Next != dramaChain.Dungeons[i+1].ID {
			t.Fatalf("dungeon %d does not lead to the next one in the list", d.ID)
		}
	}
	if last := dramaChain.Dungeons[639]; last.Chapter != 16 {
		t.Fatalf("last dungeon: %+v", last)
	}
	e := newEnv(t, time.Minute)
	a, _ := e.account("u1")
	r, _ := e.st.CreateRole(a.ID, "symlf_1", "甲", 0, 1, 0)
	d, err := dramaState(e.st, r.RID)
	if err != nil || d.DunID != 10010 || d.MaxDunID != 10010 || !d.IsFirst {
		t.Fatalf("default: %+v %v", d, err)
	}
	// saved progress wins over the default
	e.st.SaveDrama(store.Drama{RID: r.RID, Mode: 1, ChapterID: 2, DunID: 10420, Status: 1, MaxDunID: 10420})
	d, _ = dramaState(e.st, r.RID)
	if d.ChapterID != 2 || d.MaxDunID != 10420 || d.IsFirst {
		t.Fatalf("saved: %+v", d)
	}
}

func TestRename10343(t *testing.T) {
	e := newEnv(t, time.Minute)
	acc, tok := e.account("u1")
	r, err := e.st.CreateRoleAutoName(acc.ID, "symlf_1", "玩家", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := e.st.FindOrCreateAccount("symlf", "u2")
	if _, err := e.st.CreateRole(other.ID, "symlf_1", "索隆", 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	enter := func() *client {
		c := e.dial(true)
		c.send(1164, loginArgs("u1", tok))
		c.recv(1164)
		c.send(10192, proto.Message{"rid": r.RID, "srv_id": "symlf_1"})
		c.recv(10192)
		return c
	}
	c := enter()

	bad := func(name string, sex int, wantMsg string) {
		t.Helper()
		c.send(10343, proto.Message{"name": name, "sex": sex})
		got := c.recv(10343)
		if got.Int("code") != 0 || got.Str("msg") != wantMsg {
			t.Fatalf("rename %q sex %d: %v, want code 0 %q", name, sex, got, wantMsg)
		}
	}
	bad("", 1, "角色名不合法")
	bad("一二三四五六七八九十一二三", 1, "角色名不合法") // 13 runes, the limit is 12
	bad("索隆", 1, "角色名已被使用")
	bad("路 飞", 1, "角色名不合法")
	bad("路飞", 2, "请选择性别") // 2 = "not chosen" in the client
	if got, _ := e.st.RoleByID(r.RID); got.Name != r.Name || got.Renamed != 0 {
		t.Fatalf("a failed rename changed the role: %+v", got)
	}

	// first rename: free, sets the sex
	c.send(10343, proto.Message{"name": "路飞", "sex": 1})
	// the starter hero comes first, then the answer (capture original-2026-10-22)
	if h := c.recv(11001).Arr("partners"); len(h) != 1 || h[0].Int("bid") != starterMale {
		t.Fatalf("starter hero push: %v", h)
	}
	c.recv(11211)
	if rb := c.recv(10301); rb.Str("name") != "路飞" || rb.Int("sex") != 1 || rb.Int("is_first_rename") != 0 {
		t.Fatalf("10301 at the first rename: %v", rb)
	}
	if pw := c.recv(10306); pw.Int("power") <= 0 || pw.Int("power") != pw.Int("max_power") {
		t.Fatalf("10306 at the first rename: %v", pw)
	}
	got := c.recv(10343)
	if got.Int("code") != 1 || got.Str("name") != "路飞" || got.Int("sex") != 1 || got.Int("is_first_rename") != 0 {
		t.Fatalf("first rename: %v", got)
	}
	if got.Str("msg") != "" { // the original sends no toast for the free first rename
		t.Fatalf("first rename msg: %q", got.Str("msg"))
	}
	// second rename keeps the sex, even if the request asks for another
	c.send(10343, proto.Message{"name": "草帽", "sex": 0})
	got = c.recv(10343)
	if got.Int("code") != 1 || got.Str("name") != "草帽" || got.Int("sex") != 1 || got.Int("is_first_rename") != 0 || got.Str("msg") != "改名成功" {
		t.Fatalf("second rename: %v", got)
	}
	bad("草帽", 1, "新名字与当前名字相同")

	// persisted: the next login sees the new name and no free rename
	c.c.Close()
	c = enter()
	c.send(10300, proto.Message{})
	base := c.recv(10301)
	if base.Str("name") != "草帽" || base.Int("sex") != 1 || base.Int("is_first_rename") != 0 {
		t.Fatalf("10301 after rename: %v", base)
	}
	// a role that never chose a face gets the first hero's bid, like the original's new roles
	if base.Int("face_id") != 10401 {
		t.Fatalf("10301 face_id: %v", base.Int("face_id"))
	}
}

func TestRenameWithoutRoleIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(10343, proto.Message{"name": "路飞", "sex": 1})
	c.send(1164, loginArgs("u1", tok))
	if r := c.recv(1164); r.Int("code") != 1 { // the next frame is 1164's reply, so 10343 got none
		t.Fatalf("%v", r)
	}
	if !strings.Contains(e.logs.String(), "10343 before any role login") {
		t.Fatalf("log: %s", e.logs.String())
	}
}

func TestOpenDay(t *testing.T) {
	at := time.Date(2026, 10, 1, 23, 30, 0, 0, dayZone)
	for _, c := range []struct {
		now  time.Time
		want int64
	}{
		{at.Add(-time.Second), 0},
		{at, 1},
		{at.Add(20 * time.Minute), 1}, // still 1 Oct, 23:50
		{at.Add(40 * time.Minute), 2}, // 2 Oct 00:10: a new calendar day
		{time.Date(2026, 10, 3, 12, 0, 0, 0, dayZone), 3},
		{time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC).Add(24 * time.Hour), 2}, // UTC date differs from UTC+8
	} {
		if got := openDay(at, c.now); got != c.want {
			t.Errorf("openDay(%v, %v) = %d, want %d", at, c.now, got, c.want)
		}
	}
}

func TestOpenDayReply10906(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	srv, _ := e.st.Server("symlf_1")
	srv.OpenTime = time.Now().Add(-49 * time.Hour).Unix() // two days and an hour ago
	if err := e.st.UpsertServer(*srv); err != nil {
		t.Fatal(err)
	}
	c.send(10906, proto.Message{})
	if r := c.recv(10906); r.Int("open_day") < 3 || r.Int("open_day") > 4 {
		t.Fatalf("open_day for a server opened 49h ago: %v", r)
	}
	srv.OpenTime = time.Now().Add(48 * time.Hour).Unix() // not open yet
	e.st.UpsertServer(*srv)
	c.send(10906, proto.Message{})
	if r := c.recv(10906); r.Int("open_day") != 0 {
		t.Fatalf("open_day before opening: %v", r)
	}
	srv.OpenTime = 0 // unset: counts from the row's creation, which is just now
	e.st.UpsertServer(*srv)
	c.send(10906, proto.Message{})
	if r := c.recv(10906); r.Int("open_day") < 1 || r.Int("open_day") > 2 {
		t.Fatalf("open_day with no open_time: %v", r)
	}
}

// The first rename grants the starter hero once; the heroes a role already owns are
// not doubled, and a later rename pushes nothing but its answer.
func TestFirstRenameGrantsTheStarterOnce(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// "甲" was created with a name already, so it has not used its free rename
	if got, _ := e.st.PartnersOf(role.RID); len(got) != 0 {
		t.Fatalf("heroes before the rename: %v", got)
	}
	c.send(10343, proto.Message{"name": "乙", "sex": 1})
	c.recv(11001)
	c.recv(11211)
	c.recv(10301)
	c.recv(10306)
	if r := c.recv(10343); r.Int("code") != 1 {
		t.Fatalf("10343: %v", r)
	}
	if got, _ := e.st.PartnersOf(role.RID); len(got) != 1 || got[0].BID != starterMale {
		t.Fatalf("heroes after the rename: %v", got)
	}
	// a second rename: only the answer
	c.send(10343, proto.Message{"name": "丙", "sex": 1})
	if r := c.recv(10343); r.Int("code") != 1 || r.Str("msg") != "改名成功" {
		t.Fatalf("second rename: %v", r)
	}
	if got, _ := e.st.PartnersOf(role.RID); len(got) != 1 {
		t.Fatalf("a second rename changed the heroes: %v", got)
	}
}

// enterCity sends 10300 and reads up to 10304 (the main city is up); it then reports
// whether the next frame within a moment is 10342, the naming window.
func enterCityAndSeeName(c *client) (asked bool) {
	c.t.Helper()
	c.send(10300, proto.Message{})
	c.recv(10301)
	c.recv(10302)
	c.recv(13000)
	c.recv(10304)
	c.c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	fr, err := proto.ReadFrame(c.br)
	if err != nil {
		return false // nothing more: not asked
	}
	return fr.Command == 10342
}

// With the switch on, a brand-new role (never named, no starter hero) is asked to name
// itself once its main city is up; with it off, or for a role that has played, it is not.
func TestNewRoleFlowAsksOnlyBrandNewRoles(t *testing.T) {
	// off: nothing is pushed, whatever the role
	e := newEnv(t, time.Minute)
	c, _ := e.enter("off", "甲")
	if enterCityAndSeeName(c) {
		t.Fatal("10342 pushed with the switch off")
	}

	// on: a brand-new role is asked
	e = newEnv(t, time.Minute)
	e.login.NewRoleFlow = true
	c, _ = e.enter("new", "乙")
	if !enterCityAndSeeName(c) {
		t.Fatal("a brand-new role was not asked to name itself")
	}

	// on: a role that already got its starter hero is not
	e = newEnv(t, time.Minute)
	e.login.NewRoleFlow = true
	c, role := e.enter("old", "丙")
	if _, err := e.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		t.Fatal(err)
	}
	if enterCityAndSeeName(c) {
		t.Fatal("a role that has heroes was asked to name itself")
	}

	// on: a role that has already named itself is not
	e = newEnv(t, time.Minute)
	e.login.NewRoleFlow = true
	c, role = e.enter("named", "丁")
	if _, err := e.st.RenameRole(role.RID, "丁丁", 1); err != nil {
		t.Fatal(err)
	}
	if enterCityAndSeeName(c) {
		t.Fatal("a role that was already named was asked again")
	}
}
