package game

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type socialListener struct {
	conns chan net.Conn
	ready chan struct{}
	once  sync.Once
}

func (listener *socialListener) Accept() (net.Conn, error) {
	listener.once.Do(func() { close(listener.ready) })
	conn, ok := <-listener.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (listener *socialListener) Addr() net.Addr { return &net.UnixAddr{Name: "social", Net: "pipe"} }
func (listener *socialListener) Close() error   { close(listener.conns); return nil }

type socialPipe struct {
	net.Conn
	writes chan []byte
	done   chan struct{}
	once   sync.Once
}

func (pipe *socialPipe) Write(data []byte) (int, error) {
	select {
	case <-pipe.done:
		return 0, net.ErrClosed
	default:
	}
	select {
	case pipe.writes <- bytes.Clone(data):
		return len(data), nil
	default:
		return 0, io.ErrShortWrite
	}
}

func (pipe *socialPipe) Close() error {
	pipe.once.Do(func() { close(pipe.done) })
	return pipe.Conn.Close()
}

func socialPipePair() (net.Conn, net.Conn) {
	clientConn, serverConn := net.Pipe()
	pipe := &socialPipe{Conn: serverConn, writes: make(chan []byte, 128), done: make(chan struct{})}
	go func() {
		defer pipe.Close()
		for {
			select {
			case <-pipe.done:
				return
			case data := <-pipe.writes:
				if _, err := pipe.Conn.Write(data); err != nil {
					return
				}
			}
		}
	}()
	return clientConn, pipe
}

type socialEnv struct {
	t        *testing.T
	st       *store.Store
	schema   *proto.Schema
	listener *socialListener
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
	listener := &socialListener{conns: make(chan net.Conn), ready: make(chan struct{})}
	stopped := make(chan error, 1)
	go func() { stopped <- gw.Serve(listener) }()
	<-listener.ready
	t.Cleanup(func() {
		gw.Close()
		if err := <-stopped; err != nil {
			t.Error(err)
		}
	})
	return &socialEnv{t: t, st: st, schema: schema, listener: listener}
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
	conn, peer := socialPipePair()
	env.t.Cleanup(func() { conn.Close() })
	env.listener.conns <- peer
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
