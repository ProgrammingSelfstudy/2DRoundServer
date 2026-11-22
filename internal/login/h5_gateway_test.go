package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"2droundserver/internal/game"
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

func TestH5LoginThroughConfiguredWebSocket(t *testing.T) {
	s := newTestLogin(t)
	handler := s.Handler()
	srv := s.Cfg.DefaultServer()
	if err := s.Store.EnsureServer(srv); err != nil {
		t.Fatal(err)
	}
	const username = "socketuser"
	native, err := s.Store.CreateAccount(platform, username, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateRole(native.ID, srv.SrvID, "Native", 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, recordFormRequest(handler, "POST", "/index.php/userlogin/yxmmdasba?client=h5&username="+username, ""))

	const wsURL = "ws://gateway.test/ws"
	if err := s.Store.SetClientConfig("h5", "ws_url", wsURL); err != nil {
		t.Fatal(err)
	}
	var startup struct {
		WSURL string `json:"ws_url"`
	}
	w := recordFormRequest(handler, "GET", "/client/config?platform=h5", "")
	if err := json.Unmarshal(w.Body.Bytes(), &startup); err != nil || w.Code != http.StatusOK || startup.WSURL != wsURL {
		t.Fatalf("H5 startup config: %d %q, %v", w.Code, w.Body.String(), err)
	}

	schema, err := proto.LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	gw := &gateway.Server{Schema: schema, Codec: proto.DefaultOptions(), Router: gateway.NewRouter(), Hub: gateway.NewHub(), Log: s.Log}
	(&game.Login{Store: s.Store, SrvID: srv.SrvID, Hub: gw.Hub, Log: s.Log}).Register(gw.Router)
	game.RegisterPartner(gw.Router, s.Store)
	defer gw.Close()
	mux := http.NewServeMux()
	mux.Handle("/ws", gw.WSHandler(gateway.WSOptions{OriginPatterns: []string{"h5.test"}}))

	// Carry the real HTTP upgrade and protocol bytes over an in-memory stream.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ready := make(chan net.Conn, 1)
	ready <- server
	httpServer := &http.Server{Handler: mux}
	defer httpServer.Close()
	go httpServer.Serve(&singleConnListener{ready: ready, addr: server.LocalAddr()})
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, startup.WSURL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
		HTTPHeader: http.Header{"Origin": {"http://h5.test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	exchange := func(cmd uint16, request proto.Message) proto.Message {
		t.Helper()
		payload, err := proto.Encode(gw.Codec, schema.Send[cmd], request)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := (proto.Frame{Command: cmd, Payload: payload}).Encode()
		if err != nil {
			t.Fatal(err)
		}
		if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
		kind, data, err := ws.Read(ctx)
		if err != nil || kind != websocket.MessageBinary {
			t.Fatalf("command %d: read message %v, %v", cmd, kind, err)
		}
		reply, err := proto.ReadFrame(bytes.NewReader(data))
		if err != nil || reply.Command != cmd {
			t.Fatalf("command %d: received command %d, %v", cmd, reply.Command, err)
		}
		msg, err := proto.Decode(gw.Codec, schema.Recv[cmd], reply.Payload)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	if reply := exchange(1164, proto.Message{"args": []proto.Message{
		{"key": "account", "val": username},
		{"key": "token", "val": token},
		{"key": "enter_srv_id", "val": srv.SrvID},
	}}); reply.Int("code") != 1 || len(reply.Arr("roles")) != 0 {
		t.Fatalf("H5 role list: %v", reply)
	}
	created := exchange(10191, proto.Message{"name": "Browser", "sex": 2, "career": 1})
	if created.Int("code") != 1 || created.Int("rid") == 0 {
		t.Fatalf("H5 role creation: %v", created)
	}
	if entered := exchange(10192, proto.Message{"rid": created.Int("rid"), "srv_id": srv.SrvID}); entered.Int("code") != 1 {
		t.Fatalf("H5 role login: %v", entered)
	}
	heroes := exchange(11000, proto.Message{}).Arr("partners")
	if len(heroes) != 1 || heroes[0].Int("bid") != 70501 || heroes[0].Int("star") != 5 {
		t.Fatalf("H5 starter roster: %v", heroes)
	}
}

type singleConnListener struct {
	ready chan net.Conn
	addr  net.Addr
	once  sync.Once
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if conn, ok := <-l.ready; ok {
		return conn, nil
	}
	return nil, net.ErrClosed
}

func (l *singleConnListener) Addr() net.Addr { return l.addr }

func (l *singleConnListener) Close() error {
	l.once.Do(func() { close(l.ready) })
	return nil
}
