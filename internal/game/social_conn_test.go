package game

import (
	"bufio"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type socialEnv struct {
	t      *testing.T
	st     *store.Store
	schema *proto.Schema
	dial   func() (net.Conn, error)
}

func newSocialEnv(t *testing.T) *socialEnv {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8817, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	schema, err := proto.LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	gw := &gateway.Server{Schema: schema, Codec: proto.DefaultOptions(), Idle: time.Minute,
		Log: log.New(io.Discard, "", 0), Hub: gateway.NewHub(), Router: gateway.NewRouter()}
	friends := &FriendService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: gw.Log}
	friends.Register(gw.Router)
	chat := &ChatService{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: gw.Log}
	chat.Register(gw.Router)
	(&Login{Store: st, SrvID: "symlf_1", Hub: gw.Hub, Log: gw.Log, MaxRoles: 2, Friends: friends, Chat: chat}).Register(gw.Router)
	return &socialEnv{t: t, st: st, schema: schema, dial: serveTestPipe(t, gw, true)}
}

func (env *socialEnv) role(platform, username, name string) (*store.Account, *store.Role) {
	env.t.Helper()
	account, err := env.st.CreateAccount(platform, username, "", "")
	if err != nil {
		env.t.Fatal(err)
	}
	role, err := env.st.CreateRole(account.ID, "symlf_1", name, 0, 1, 0)
	if err != nil {
		env.t.Fatal(err)
	}
	return account, role
}

func (env *socialEnv) connect(account *store.Account, role *store.Role) *client {
	env.t.Helper()
	token, err := env.st.IssueToken(account.ID, 3600)
	if err != nil {
		env.t.Fatal(err)
	}
	conn, err := env.dial()
	if err != nil {
		env.t.Fatal(err)
	}
	env.t.Cleanup(func() { conn.Close() })
	if _, err := io.WriteString(conn, proto.Handshake); err != nil {
		env.t.Fatal(err)
	}
	cli := &client{t: env.t, c: conn, br: bufio.NewReader(conn), schema: env.schema}
	cli.send(1164, loginArgs(account.Account, token))
	if result := cli.recv(1164); result.Int("code") != 1 {
		env.t.Fatalf("login: %v", result)
	}
	cli.send(10192, rid(role))
	if result := cli.recv(10192); result.Int("code") != 1 {
		env.t.Fatalf("enter: %v", result)
	}
	return cli
}

func (env *socialEnv) enter(platform, username, name string) (*client, *store.Role) {
	env.t.Helper()
	account, role := env.role(platform, username, name)
	return env.connect(account, role), role
}
