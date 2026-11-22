package game

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type welfareListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func (listener *welfareListener) Accept() (net.Conn, error) {
	select {
	case conn := <-listener.connections:
		return conn, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *welfareListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *welfareListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8816}
}

func (listener *welfareListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	select {
	case listener.connections <- serverConn:
		return clientConn, nil
	case <-listener.closed:
		clientConn.Close()
		serverConn.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		clientConn.Close()
		serverConn.Close()
		return nil, ctx.Err()
	}
}

func (e *env) welfareDialer() func() *client {
	e.t.Helper()
	listener := &welfareListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: e.gw.WSHandler(gateway.WSOptions{})},
	}
	server.Start()
	e.t.Cleanup(server.Close)
	server.Client().Transport.(*http.Transport).DialContext = listener.dial
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	return func() *client {
		e.t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		raw, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: server.Client()})
		if err != nil {
			e.t.Fatal(err)
		}
		e.t.Cleanup(func() { raw.CloseNow() })
		conn := websocket.NetConn(context.Background(), raw, websocket.MessageBinary)
		conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write([]byte(proto.Handshake)); err != nil {
			e.t.Fatal(err)
		}
		conn.SetWriteDeadline(time.Time{})
		return &client{t: e.t, c: conn, br: bufio.NewReader(conn), schema: e.gw.Schema}
	}
}

func (e *env) welfareRole(platform, accountName, roleName string) (*store.Account, string, *store.Role) {
	e.t.Helper()
	account, err := e.st.FindOrCreateAccount(platform, accountName)
	if err != nil {
		e.t.Fatal(err)
	}
	token, err := e.st.IssueToken(account.ID, 3600)
	if err != nil {
		e.t.Fatal(err)
	}
	role, err := e.st.CreateRole(account.ID, "symlf_1", roleName, 0, 1, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return account, token, role
}

func enterWelfareRole(dial func() *client, account *store.Account, token string, role *store.Role) *client {
	c := dial()
	c.t.Helper()
	c.send(1164, loginArgs(account.Account, token))
	if r := c.waitFor(1164); r.Int("code") != 1 {
		c.t.Fatalf("%s login: %v", account.Platform, r)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if r := c.waitFor(10192); r.Int("code") != 1 {
		c.t.Fatalf("%s enter: %v", account.Platform, r)
	}
	return c
}

func TestH5MailWelfareAcrossReconnect(t *testing.T) {
	e := newGameEnv(t, time.Minute)
	account, token, role := e.welfareRole(store.PlatformH5, "h5-welfare", "福利测试")
	e.setNow(time.Unix(role.CreatedAt, 0).Add(48 * time.Hour))
	dial := e.welfareDialer()
	c := enterWelfareRole(dial, account, token, role)
	call := func(cmd uint16, req proto.Message) proto.Message {
		c.send(cmd, req)
		return c.waitFor(cmd)
	}
	wantCode := func(cmd uint16, req proto.Message, code int64) {
		t.Helper()
		if reply := call(cmd, req); reply.Int("code") != code {
			t.Fatalf("%d: %v, want code %d", cmd, reply, code)
		}
	}
	mail := e.sendMail(store.Mail{Subject: "福利邮件", Content: "奖励已送达", Attachments: []store.Attachment{
		{Kind: store.AttachAsset, ItemID: 3, Num: 17}, {Kind: store.AttachItem, ItemID: 10403, Num: 2},
	}}, role.RID)
	second := e.sendMail(store.Mail{Subject: "第二封", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 3, Num: 3}}}, role.RID)
	addr := proto.Message{"id": mail.ID, "srv_id": role.SrvID}
	if rows := call(10800, proto.Message{}).Arr("mail"); len(rows) != 2 {
		t.Fatalf("inbox: %v", rows)
	}
	wantCode(10805, addr, 1)
	if r := call(10806, addr); r.Str("content") != "奖励已送达" || len(r.Arr("assets")) != 1 || len(r.Arr("items")) != 1 {
		t.Fatalf("detail: %v", r)
	}
	wantCode(10801, addr, 1)
	if a := c.waitFor(10302); a.Str("gold") != "17" {
		t.Fatalf("single mail assets: %v", a)
	}
	c.waitFor(10500)
	c.recv(10501)
	wantCode(10801, addr, 0)
	if ids := call(10802, proto.Message{}).Arr("ids"); len(ids) != 1 || ids[0].Int("id") != second.ID {
		t.Fatalf("claim all: %v", ids)
	}
	c.waitFor(10500)
	c.recv(10501)
	if ids := call(10804, proto.Message{"ids": []proto.Message{addr}}).Arr("ids"); len(ids) != 1 || ids[0].Int("id") != mail.ID {
		t.Fatalf("delete mail: %v", ids)
	}
	notice := e.notice(store.Notice{Title: "福利公告", Content: "活动详情", Status: 1})
	if list := call(10950, proto.Message{}).Arr("board_list"); len(list) != 1 || list[0].Int("flag") != 0 {
		t.Fatalf("unread notice: %v", list)
	}
	wantCode(10952, proto.Message{"id": notice.ID}, 1)
	if list := c.waitFor(10950).Arr("board_list"); len(list) != 1 || list[0].Int("flag") != 1 {
		t.Fatalf("read notice update: %v", list)
	}
	if r := call(14101, proto.Message{}); r.Int("code") != 1 || r.Int("day") != 1 {
		t.Fatalf("check-in: %v", r)
	}
	d1, d3 := proto.Message{"day": 1}, proto.Message{"day": 3}
	wantCode(21101, proto.Message{"day": 4}, 0)
	wantCode(21101, d1, 1)
	c.waitFor(10500)
	c.recv(10501)
	wantCode(21101, d3, 1)
	c.waitFor(10500)
	c.recv(10501)
	if err := e.st.CreateRedeemCode(store.RedeemCode{Code: "WELFARE", Attachments: []store.Attachment{
		{Kind: store.AttachAsset, ItemID: 3, Num: 5}, {Kind: store.AttachItem, ItemID: 10403, Num: 3},
	}}); err != nil {
		t.Fatal(err)
	}
	code := proto.Message{"card_id": " welfare "}
	wantCode(10945, code, 1)
	if err := c.c.Close(); err != nil {
		t.Fatal(err)
	}
	c = enterWelfareRole(dial, account, token, role)
	if list := call(10800, proto.Message{}).Arr("mail"); len(list) != 1 || list[0].Int("id") != second.ID || list[0].Int("status") != 2 {
		t.Fatalf("inbox after reconnect: %v", list)
	}
	wantCode(10801, addr, 0)
	if r := call(10802, proto.Message{}); len(r.Arr("ids")) != 0 {
		t.Fatalf("claim all paid again: %v", r)
	}
	if list := call(10950, proto.Message{}).Arr("board_list"); len(list) != 1 || list[0].Int("flag") != 1 {
		t.Fatalf("notice after reconnect: %v", list)
	}
	if r := call(14100, proto.Message{}); r.Int("status") != 1 || r.Int("day") != 1 {
		t.Fatalf("check-in after reconnect: %v", r)
	}
	wantCode(14101, proto.Message{}, 0)
	if days := call(21100, proto.Message{}).Arr("status_list"); len(days) != 8 || days[0].Int("status") != loginDayTaken ||
		days[1].Int("status") != loginDayOpen || days[2].Int("status") != loginDayTaken || days[3].Int("status") != loginDayLocked {
		t.Fatalf("login days after reconnect: %v", days)
	}
	wantCode(21101, d1, 0)
	wantCode(21101, d3, 0)
	wantCode(10945, code, 0)
	if heroes := call(11000, proto.Message{}).Arr("partners"); len(heroes) != 1 || heroes[0].Int("bid") != starterH5 || heroes[0].Int("star") != 5 {
		t.Fatalf("H5 roster after welfare claims: %v", heroes)
	}
	// The first roster request grants the starter and pushes its power. Read
	// that push before writing the next request over the unbuffered pipe.
	c.recv(10306)
	bag := call(10500, proto.Message{}).Arr("item_list")
	counts := map[int64]int64{}
	for _, item := range bag {
		counts[item.Int("base_id")] = item.Int("quantity")
	}
	if counts[99955] != 1 || counts[10001] != 10_000 || counts[10403] != 5 {
		t.Fatalf("backpack after reconnect: %v", counts)
	}
	c.recv(10501)
	c.send(10300, proto.Message{})
	if a := c.waitFor(10302); a.Str("gold") != "25" || a.Str("coin") != "1000000" {
		t.Fatalf("assets after reconnect: %v", a)
	}
}

func TestLoginDayLastClaimClosesCityIcon(t *testing.T) {
	for _, platform := range []string{store.PlatformH5, "symlf"} {
		t.Run(platform, func(t *testing.T) {
			e := newGameEnv(t, time.Minute)
			account, token, role := e.welfareRole(platform, "last-day", "末日测试")
			created := time.Unix(role.CreatedAt, 0)
			e.setNow(created.Add(7 * 24 * time.Hour))
			if err := e.st.SaveState(role.RID, store.LoginDayStateKey, store.LoginDayState{Taken: []int64{1, 2, 3, 4, 5, 6, 7}}); err != nil {
				t.Fatal(err)
			}
			c := enterWelfareRole(e.welfareDialer(), account, token, role)
			icon := proto.Message{"id": loginDayIcon}
			c.send(10925, icon)
			if r := c.recv(10925); r.Int("id") != loginDayIcon || r.Int("status") != 1 {
				t.Fatalf("icon before final claim: %v", r)
			}
			day := proto.Message{"day": 8}
			c.send(21101, day)
			if r := c.recv(21101); r.Int("code") != 1 || r.Int("day") != 8 {
				t.Fatalf("last claim: %v", r)
			}
			c.recv(10302)
			c.recv(10500)
			c.recv(10501)
			if platform == store.PlatformH5 {
				if r := c.recv(10923); r.Int("id") != loginDayIcon || r.Int("status") != 0 {
					t.Fatalf("10923 icon state: %v", r)
				}
			}
			request := c.frames(10925, icon)
			written := make(chan error, 1)
			go func() {
				_, err := c.c.Write(request)
				written <- err
			}()
			if r := c.recv(10925); r.Int("id") != loginDayIcon || r.Int("status") != 0 {
				t.Fatalf("icon after final claim: %v", r)
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			e.setNow(created.Add(6 * 24 * time.Hour))
			c.send(21101, day)
			if r := c.recv(21101); r.Int("code") != 0 || r.Str("msg") != "奖励已经领取" || r.Int("day") != 8 {
				t.Fatalf("retry after clock rewind: %v", r)
			}
			if got, err := e.st.RoleAssets(role.RID); err != nil || got[22] != 2_000_000 {
				t.Fatalf("final reward did not pay exactly once: %v (%v)", got, err)
			}
		})
	}
}
