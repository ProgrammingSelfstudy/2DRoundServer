package gateway

import (
	"bufio"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type rig struct {
	t    *testing.T
	srv  *Server
	addr string
	logs *syncBuf
	gone chan *Session
}

func newRig(t *testing.T, idle time.Duration) *rig { return newRigWith(t, idle, nil) }

// newRigWith lets the test set fields of the Server before it starts serving.
func newRigWith(t *testing.T, idle time.Duration, configure func(*Server)) *rig {
	t.Helper()
	schema, err := proto.LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	r := &rig{t: t, logs: logs, gone: make(chan *Session, 8)}
	r.srv = &Server{Schema: schema, Codec: proto.DefaultOptions(), Idle: idle, Log: log.New(logs, "", 0), OnDisconnect: func(s *Session) { r.gone <- s }}
	r.srv.Router = NewRouter()
	// 1169 heartbeat: reply with the server time.
	r.srv.Router.Handle(1169, func(s *Session, _ proto.Message) error {
		return s.Send(1169, proto.Message{"time": int64(42)})
	})
	// 1164 is only used to ask the gateway to close the connection.
	r.srv.Router.Handle(1164, func(s *Session, _ proto.Message) error { return ErrClose })
	// 1165 stands for a successful login: the session gets an account
	r.srv.Router.Handle(1165, func(s *Session, _ proto.Message) error { s.SetAccount(&store.Account{ID: 1}); return nil })
	if configure != nil {
		configure(r.srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r.addr = ln.Addr().String()
	go r.srv.Serve(ln)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *rig) dial() net.Conn {
	c, err := net.Dial("tcp", r.addr)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	return c
}

func (r *rig) frame(cmd uint16, msg proto.Message) []byte {
	r.t.Helper()
	payload, err := proto.Encode(r.srv.Codec, r.srv.Schema.Send[cmd], msg)
	if err != nil {
		r.t.Fatal(err)
	}
	b, err := proto.Frame{Command: cmd, Payload: payload}.Encode()
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func (r *rig) sig(idx, tm int64, cmd uint16) []byte {
	return r.frame(1197, proto.Message{"idx": idx, "time": tm, "code": expectedCode(cmd, idx), "sign": expectedSign(idx, tm, cmd)})
}

func write(t *testing.T, c net.Conn, parts ...[]byte) {
	t.Helper()
	for _, p := range parts {
		if _, err := c.Write(p); err != nil {
			t.Fatal(err)
		}
	}
}

// expectPong reads one reply frame and checks it is the 1169 answer.
func (r *rig) expectPong(c net.Conn) {
	r.t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr, err := proto.ReadFrame(bufio.NewReader(c))
	if err != nil {
		r.t.Fatal(err)
	}
	if fr.Command != 1169 {
		r.t.Fatalf("got cmd %d, want 1169", fr.Command)
	}
	msg, err := proto.Decode(r.srv.Codec, r.srv.Schema.Recv[1169], fr.Payload)
	if err != nil || msg.Int("time") != 42 {
		r.t.Fatalf("bad reply %v %v", msg, err)
	}
}

// expectClosed waits for the server to close the connection.
func (r *rig) expectClosed(c net.Conn) {
	r.t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err != io.EOF && !strings.Contains(err.Error(), "reset") {
		r.t.Fatalf("connection not closed: %v", err)
	}
}

func (r *rig) waitGone() {
	r.t.Helper()
	select {
	case <-r.gone:
	case <-time.After(2 * time.Second):
		r.t.Fatal("OnDisconnect not called")
	}
}

func TestHandshakeThenFrame(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, []byte(proto.Handshake), r.frame(1169, nil))
	r.expectPong(c)
}

func TestHandshakeIsOptional(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, r.frame(1169, nil))
	r.expectPong(c)
}

func TestBadHandshakeCloses(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, []byte("game_clientXXXXXXXXXXXX"))
	r.expectClosed(c)
	r.waitGone()
	if !strings.Contains(r.logs.String(), "bad handshake") {
		t.Fatalf("log: %s", r.logs.String())
	}
}

func TestFrameSplitAcrossWrites(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	b := r.frame(1169, nil)
	for i := range b { // one byte per write, plus two frames glued together afterwards
		write(t, c, b[i:i+1])
		time.Sleep(time.Millisecond)
	}
	r.expectPong(c)
	write(t, c, append(r.frame(1169, nil), r.frame(1169, nil)...))
	br := bufio.NewReader(c)
	for i := 0; i < 2; i++ {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if fr, err := proto.ReadFrame(br); err != nil || fr.Command != 1169 {
			t.Fatalf("reply %d: %v %v", i, fr.Command, err)
		}
	}
}

func TestOversizedFrameCloses(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	// flags=0, body length 0xFFFFFF, cmd 1169: far above MaxPayload
	write(t, c, []byte{0, 0xFF, 0xFF, 0xFF, 0x04, 0x91})
	r.expectClosed(c)
	r.waitGone()
}

func TestIdleTimeoutCloses(t *testing.T) {
	r := newRig(t, 150*time.Millisecond)
	c := r.dial()
	write(t, c, r.frame(1169, nil))
	r.expectPong(c)
	r.expectClosed(c) // nothing more is sent, so the idle deadline fires
	r.waitGone()
}

func TestHeartbeatKeepsConnectionAlive(t *testing.T) {
	r := newRig(t, 300*time.Millisecond)
	c := r.dial()
	br := bufio.NewReader(c)
	for i := 0; i < 4; i++ {
		write(t, c, r.frame(1169, nil))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := proto.ReadFrame(br); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		time.Sleep(150 * time.Millisecond) // total 600ms, longer than Idle
	}
}

func TestErrCloseEndsConnection(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, r.frame(1164, nil))
	r.expectClosed(c)
	r.waitGone()
	if strings.Contains(r.logs.String(), "close connection") {
		t.Fatalf("ErrClose must not be logged as an error: %s", r.logs.String())
	}
}

func TestUnknownCommandKeepsConnection(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, r.frame(1169, nil)) // known handler first
	r.expectPong(c)
	// 65000 has no schema and no handler: ignored, connection stays up
	b, _ := proto.Frame{Command: 65000, Payload: []byte{1, 2, 3}}.Encode()
	write(t, c, b, r.frame(1169, nil))
	r.expectPong(c)
	if !strings.Contains(r.logs.String(), "cmd 65000: no send schema") {
		t.Fatalf("log: %s", r.logs.String())
	}
}

func TestSignatureChecks(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	br := bufio.NewReader(c)
	// 10300 is exempt from the 1197 prefix, so it never complains
	r.srv.Router.Handle(10300, func(s *Session, _ proto.Message) error { return s.Send(1169, proto.Message{"time": int64(42)}) })
	write(t, c, r.frame(10300, nil))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := proto.ReadFrame(br); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.logs.String(), "1197") {
		t.Fatalf("exempt command logged: %s", r.logs.String())
	}
	// A non-exempt command (1164 would close the connection, so use another).
	r.srv.Router.Handle(12000, func(s *Session, _ proto.Message) error { return s.Send(1169, proto.Message{"time": int64(42)}) })
	send := func(pre []byte) {
		if pre != nil {
			write(t, c, pre)
		}
		write(t, c, r.frame(12000, nil))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := proto.ReadFrame(br); err != nil {
			t.Fatal(err)
		}
	}
	send(r.sig(7, 1700000000, 12000))
	if strings.Contains(r.logs.String(), "cmd 12000") {
		t.Fatalf("valid signature complained: %s", r.logs.String())
	}
	send(nil)
	if !strings.Contains(r.logs.String(), "cmd 12000: not preceded by 1197") {
		t.Fatalf("missing 1197 not logged: %s", r.logs.String())
	}
	send(r.sig(7, 1700000000, 12001)) // signed for another command
	if !strings.Contains(r.logs.String(), "cmd 12000: 1197 signature mismatch") {
		t.Fatalf("mismatch not logged: %s", r.logs.String())
	}
}

func TestSignHelpers(t *testing.T) {
	for _, cmd := range []uint16{10300, 16601, 16602, 16603, 11100, 11199, 20000, 20099} {
		if !signExempt(cmd) {
			t.Errorf("%d should be exempt", cmd)
		}
	}
	for _, cmd := range []uint16{10191, 10192, 11200, 11099, 20100, 1164} {
		if signExempt(cmd) {
			t.Errorf("%d should not be exempt", cmd)
		}
	}
	if a, b := expectedSign(1, 2, 3), expectedSign(1, 2, 4); a == b || len(a) != 32 {
		t.Errorf("sign should depend on cmd and be 32 hex chars: %s %s", a, b)
	}
	if got := expectedCode(100, 11); got != 100+(11+5)%16 {
		t.Errorf("code = %d", got)
	}
}

func TestServerCloseDropsSessions(t *testing.T) {
	r := newRig(t, 5*time.Second)
	c := r.dial()
	write(t, c, r.frame(1169, nil))
	r.expectPong(c)
	r.srv.Close()
	r.expectClosed(c)
}

func TestHub(t *testing.T) {
	h := NewHub()
	a, b := &Session{}, &Session{}
	if prev := h.Register(1, a); prev != nil {
		t.Fatal("first register has no previous")
	}
	if prev := h.Register(1, a); prev != nil {
		t.Fatal("re-registering the same session is not a kick")
	}
	if prev := h.Register(1, b); prev != a {
		t.Fatal("second session should replace the first")
	}
	h.Unregister(1, a) // stale session must not remove the new binding
	if h.Online(1) != b || h.Count() != 1 {
		t.Fatal("stale Unregister removed the live session")
	}
	n := 0
	h.Each(func(rid int64, s *Session) { n++; h.Unregister(rid, s) }) // Each must tolerate Hub calls
	if n != 1 || h.Count() != 0 || h.Online(1) != nil {
		t.Fatalf("n=%d count=%d", n, h.Count())
	}
}

func TestPerIPConnectionLimit(t *testing.T) {
	r := newRigWith(t, 5*time.Second, func(s *Server) { s.MaxConnsPerIP = 2 })
	a, b := r.dial(), r.dial()
	write(t, a, r.frame(1169, nil))
	r.expectPong(a)
	write(t, b, r.frame(1169, nil))
	r.expectPong(b)

	// the third connection from the same address is refused: closed without an answer
	c := r.dial()
	write(t, c, r.frame(1169, nil))
	r.expectClosed(c)
	if !strings.Contains(r.logs.String(), "refused") {
		t.Fatalf("log: %s", r.logs.String())
	}
	// the two that were let in keep working
	write(t, a, r.frame(1169, nil))
	r.expectPong(a)

	// a place frees up when a connection ends, and not before
	a.Close()
	r.waitGone()
	var d net.Conn
	for i := 0; i < 50; i++ { // the server notices the close a moment later
		d = r.dial()
		write(t, d, r.frame(1169, nil))
		d.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := proto.ReadFrame(bufio.NewReader(d)); err == nil {
			return
		}
		d.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no place was freed after a connection closed")
}

func TestUnlimitedWhenNoCap(t *testing.T) {
	r := newRig(t, 5*time.Second)
	for i := 0; i < 8; i++ {
		c := r.dial()
		write(t, c, r.frame(1169, nil))
		r.expectPong(c)
	}
}

func TestLoginTimeoutDropsSilentConnectionsOnly(t *testing.T) {
	r := newRigWith(t, 5*time.Second, func(s *Server) { s.LoginTimeout = 200 * time.Millisecond })
	silent := r.dial() // connects and says nothing
	start := time.Now()
	r.expectClosed(silent)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the silent connection lived %v, want about 200ms", d)
	}

	// a connection that logs in is not held to the short limit any more
	c := r.dial()
	write(t, c, r.frame(1165, nil))
	time.Sleep(500 * time.Millisecond) // more than LoginTimeout
	write(t, c, r.frame(1169, nil))
	r.expectPong(c)
}

func TestLoginTimeoutNeverExceedsIdle(t *testing.T) {
	// a LoginTimeout longer than Idle changes nothing: Idle still applies
	r := newRigWith(t, 150*time.Millisecond, func(s *Server) { s.LoginTimeout = time.Minute })
	c := r.dial()
	r.expectClosed(c)
}
