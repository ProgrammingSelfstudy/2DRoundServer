package game

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func storeMail(subject string) store.Mail { return store.Mail{Subject: subject, Content: "x"} }

// wsServer exposes the env's gateway over WebSocket at /ws.
func (e *env) wsServer(o gateway.WSOptions) string {
	e.t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/ws", e.gw.WSHandler(o))
	srv := httptest.NewServer(mux)
	e.t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func (e *env) dialWS(url, origin string) (*client, *websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": {origin}}
	}
	raw, resp, err := websocket.Dial(ctx, url, opts)
	if err != nil {
		return nil, nil, resp, err
	}
	e.t.Cleanup(func() { raw.CloseNow() })
	nc := websocket.NetConn(context.Background(), raw, websocket.MessageBinary)
	schema, _ := proto.LoadSchema()
	return &client{t: e.t, c: nc, br: bufio.NewReader(nc), schema: schema}, raw, resp, nil
}

func (e *env) mustDialWS(url string) *client {
	e.t.Helper()
	c, _, _, err := e.dialWS(url, "")
	if err != nil {
		e.t.Fatal(err)
	}
	c.c.Write([]byte(proto.Handshake))
	return c
}

func TestWebSocketRunsTheWholeLoginSequence(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})
	a, tok := e.account("u1")
	c := e.mustDialWS(url)

	c.send(1164, loginArgs("u1", tok))
	if r := c.recv(1164); r.Int("code") != 1 || len(r.Arr("roles")) != 0 {
		t.Fatalf("role list: %v", r)
	}
	c.send(10191, proto.Message{"sex": 1, "name": "路飞", "career": 2, "playform": "wx"})
	cr := c.recv(10191)
	if cr.Int("code") != 1 || cr.Str("name") != "路飞" {
		t.Fatalf("create: %v", cr)
	}
	c.send(10192, proto.Message{"rid": cr.Int("rid"), "srv_id": "symlf_1"})
	if r := c.recv(10192); r.Int("code") != 1 {
		t.Fatalf("enter: %v", r)
	}
	c.send(10300, proto.Message{})
	if base := c.recv(10301); base.Int("rid") != cr.Int("rid") || base.Str("name") != "路飞" {
		t.Fatalf("10301: %v", base)
	}
	c.recv(10302)
	c.recv(13000)
	c.recv(10304)
	c.send(1169, proto.Message{})
	if hb := c.recv(1169); hb.Int("time") == 0 {
		t.Fatal(hb)
	}

	// server-initiated messages reach a WebSocket client like any other
	m := e.sendMail(storeMail("来自 WebSocket 测试"), cr.Int("rid"))
	if n, err := e.mail.PushMail(m.ID); err != nil || n != 1 {
		t.Fatalf("push: %d %v", n, err)
	}
	if p := c.recv(10803).Arr("mail"); len(p) != 1 || p[0].Str("subject") != "来自 WebSocket 测试" {
		t.Fatalf("pushed mail: %v", p)
	}
	_ = a
}

func TestWebSocketFramesMaySplitAndMerge(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})
	_, tok := e.account("u1")
	c := e.mustDialWS(url)

	// one command cut into 3-byte pieces, each its own WebSocket message
	raw := c.frames(1164, loginArgs("u1", tok))
	for i := 0; i < len(raw); i += 3 {
		c.c.Write(raw[i:min(i+3, len(raw))])
	}
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("split frames: %v", r)
	}
	// two commands in one message
	both := append(c.frames(1169, proto.Message{}), c.frames(1169, proto.Message{})...)
	c.c.Write(both)
	c.recv(1169)
	c.recv(1169)
}

func TestWebSocketWithoutTheHandshakeIsTolerated(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})
	_, tok := e.account("u1")
	c, _, _, err := e.dialWS(url, "")
	if err != nil {
		t.Fatal(err)
	}
	c.send(1164, loginArgs("u1", tok))
	if r := c.recv(1164); r.Int("code") != 1 {
		t.Fatalf("%v", r)
	}
}

func TestWebSocketRefusesWhatIsNotBinaryOrTooBig(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})

	_, raw, _, err := e.dialWS(url, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw.Write(ctx, websocket.MessageText, []byte("hello")) // text is not the protocol
	if _, _, err := raw.Read(ctx); err == nil {
		t.Fatal("a text message must close the connection")
	}

	// a message over the limit is refused; NetConn switches the limit off, so the
	// handler must set it again after creating the adapter
	c2, raw2, _, err := e.dialWS(url, "")
	if err != nil {
		t.Fatal(err)
	}
	raw2.SetReadLimit(1 << 20) // the client side may send big messages
	// valid frames, so only the size can be the reason to refuse: without a limit
	// the gateway would answer them
	beat := c2.frames(1169, proto.Message{})
	var big []byte
	beats := 0
	for len(big) < 200<<10 {
		big = append(big, beat...)
		beats++
	}
	raw2.Write(ctx, websocket.MessageBinary, big)
	// the limit is enforced while the message streams in, so the frames inside the
	// first 64 KiB are answered and then the connection is closed
	answered := 0
	for {
		if _, _, err := raw2.Read(ctx); err != nil {
			break
		}
		answered++
	}
	if answered == 0 || answered >= beats*2/3 {
		t.Fatalf("%d of %d frames answered: the message size limit is not enforced", answered, beats)
	}

	// a message at the limit is fine: a valid frame padded into one message works
	small := e.wsServer(gateway.WSOptions{MaxMessage: 256})
	c, _, _, err := e.dialWS(small, "")
	if err != nil {
		t.Fatal(err)
	}
	c.send(1169, proto.Message{})
	c.recv(1169)
}

func TestWebSocketOriginPolicy(t *testing.T) {
	e := newEnv(t, time.Minute)
	closed := e.wsServer(gateway.WSOptions{})
	open := e.wsServer(gateway.WSOptions{OriginPatterns: []string{"game.example.com"}})

	if _, _, _, err := e.dialWS(closed, ""); err != nil {
		t.Fatalf("no Origin (native and mini-game clients) must be accepted: %v", err)
	}
	_, _, resp, err := e.dialWS(closed, "https://evil.example")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a foreign browser origin must get 403, got %v %v", resp, err)
	}
	if _, _, _, err := e.dialWS(open, "https://game.example.com"); err != nil {
		t.Fatalf("a listed origin must be accepted: %v", err)
	}
	if _, _, resp, err := e.dialWS(open, "https://other.example.com"); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an unlisted origin must be refused: %v", err)
	}
}

func TestWebSocketIsOnlyServedWhereMounted(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})
	http1 := "http" + strings.TrimPrefix(url, "ws")
	if r, _ := http.Get(strings.TrimSuffix(http1, "/ws") + "/elsewhere"); r.StatusCode != 404 {
		t.Fatalf("other paths: %d", r.StatusCode)
	}
	r, _ := http.Get(http1 + "/ws") // a plain request without an upgrade
	if r.StatusCode < 400 || r.StatusCode >= 500 {
		t.Fatalf("a plain GET of /ws must be a client error, got %d", r.StatusCode)
	}
}

func TestWebSocketIdleTimeoutAndShutdown(t *testing.T) {
	e := newEnv(t, 200*time.Millisecond)
	url := e.wsServer(gateway.WSOptions{})
	c := e.mustDialWS(url)
	c.expectClosed() // silent clients are dropped like TCP ones

	e2 := newEnv(t, time.Minute)
	url2 := e2.wsServer(gateway.WSOptions{})
	c2 := e2.mustDialWS(url2)
	c2.send(1169, proto.Message{})
	c2.recv(1169)
	e2.gw.Close() // shutdown ends open WebSocket sessions and refuses new ones
	c2.expectClosed()
	if _, _, resp, err := e2.dialWS(url2, ""); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a closing gateway must answer 503: %v %v", resp, err)
	}
}

func TestWebSocketAndTCPShareOneOnlineTable(t *testing.T) {
	e := newEnv(t, time.Minute)
	url := e.wsServer(gateway.WSOptions{})
	tcp, role := e.enter("u1", "甲") // in over TCP
	if e.gw.Hub.Online(role.RID) == nil {
		t.Fatal("not online over TCP")
	}
	_, tok := e.account("u1")
	ws := e.mustDialWS(url)
	ws.send(1164, loginArgs("u1", tok))
	ws.recv(1164)
	ws.send(10192, proto.Message{"rid": role.RID, "srv_id": "symlf_1"})
	if r := ws.recv(10192); r.Int("code") != 1 {
		t.Fatalf("enter over WebSocket: %v", r)
	}
	// the same role logging in on another transport kicks the first connection
	if kick := tcp.recv(10310); kick.Int("is_show") != 1 {
		t.Fatalf("kick: %v", kick)
	}
	tcp.expectClosed()
	// and the admin kick reaches the WebSocket one
	srv := httptest.NewServer(e.mail.AdminHandler())
	defer srv.Close()
	http.Post(srv.URL+"/kick?rid="+itoa(role.RID), "", nil)
	if kick := ws.recv(10310); kick.Str("msg") == "" {
		t.Fatalf("admin kick: %v", kick)
	}
	ws.expectClosed()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
