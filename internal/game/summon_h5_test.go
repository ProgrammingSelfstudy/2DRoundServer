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

// summonTestListener lets the real gateway serve one in-process connection.
type summonTestListener struct {
	conn     net.Conn
	accepted bool
}

func (l *summonTestListener) Accept() (net.Conn, error) {
	if l.accepted {
		return nil, net.ErrClosed
	}
	l.accepted = true
	return l.conn, nil
}

func (l *summonTestListener) Addr() net.Addr { return l.conn.LocalAddr() }
func (l *summonTestListener) Close() error   { return l.conn.Close() }

func newSummonClient(t *testing.T, platform string) (*client, *store.Store, int64) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	const serverID = "symlf_1"
	if err := st.EnsureServer(store.Server{SrvID: serverID, Name: "summon test", Platform: "symlf", ZoneID: 1, Status: 1}); err != nil {
		t.Fatal(err)
	}
	account, err := st.FindOrCreateAccount(platform, "summoner")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.IssueToken(account.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(account.ID, serverID, "Summoner", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := proto.LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	gw := &gateway.Server{Schema: schema, Codec: proto.DefaultOptions(), Hub: gateway.NewHub(), Router: gateway.NewRouter(), Log: log.New(io.Discard, "", 0)}
	(&Login{Store: st, SrvID: serverID, Hub: gw.Hub, Log: gw.Log}).Register(gw.Router)
	RegisterSummon(gw.Router, st)
	serverConn, conn := net.Pipe()
	if err := gw.Serve(&summonTestListener{conn: serverConn}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	t.Cleanup(func() { conn.Close() })
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	c := &client{t: t, c: conn, br: bufio.NewReader(conn), schema: schema}
	c.send(1164, loginArgs(account.Account, token))
	if reply := c.recv(1164); reply.Int("code") != 1 {
		t.Fatalf("login: %v", reply)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": serverID})
	if reply := c.recv(10192); reply.Int("code") != 1 {
		t.Fatalf("enter: %v", reply)
	}
	return c, st, role.RID
}

// The heartbeat catches any unexpected push, including a summon result on refusal.
func summonRefusal(c *client, req proto.Message, want uint16) proto.Message {
	c.t.Helper()
	frames := append(c.frames(23201, req), c.frames(1169, nil)...)
	if _, err := c.c.Write(frames); err != nil {
		c.t.Fatal(err)
	}
	cmd, msg := c.recvNext()
	if want == 0 {
		if cmd != 1169 {
			c.t.Fatalf("silent summon refusal sent %d: %v", cmd, msg)
		}
		return nil
	}
	if cmd != want {
		c.t.Fatalf("summon refusal sent %d, want %d: %v", cmd, want, msg)
	}
	if cmd, msg := c.recvNext(); cmd != 1169 {
		c.t.Fatalf("extra frame after summon refusal: %d %v", cmd, msg)
	}
	return msg
}

func TestSummonRefusalsByPlatform(t *testing.T) {
	for _, tc := range []struct {
		platform      string
		itemNotice    uint16
		generalNotice uint16
	}{
		{store.PlatformH5, 12745, 12741},
		{"symlf", 0, 0},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			c, st, rid := newSummonClient(t, tc.platform)
			const ticket = 10403
			const gems = 219
			if err := st.Exchange(rid, nil, nil, map[int64]int64{3: gems}, map[int64]int64{ticket: 1}); err != nil {
				t.Fatal(err)
			}
			checkState := func(wantHeroes int, wantTickets int64) {
				t.Helper()
				if heroes, err := st.PartnersOf(rid); err != nil || len(heroes) != wantHeroes {
					t.Fatalf("heroes after refusal: %d, want %d (%v)", len(heroes), wantHeroes, err)
				}
				if items, err := st.RoleItems(rid); err != nil || items[ticket] != wantTickets {
					t.Fatalf("tickets after refusal: %d, want %d (%v)", items[ticket], wantTickets, err)
				}
				if assets, err := st.RoleAssets(rid); err != nil || assets[3] != gems || assets[22] != 0 || assets[23] != 0 {
					t.Fatalf("assets after refusal: %v (%v)", assets, err)
				}
			}
			ticketDraw := proto.Message{"group_id": 300, "times": 10, "recruit_type": summonItem}
			if msg := summonRefusal(c, ticketDraw, tc.itemNotice); tc.itemNotice != 0 && (msg.Int("bid") != ticket || msg.Int("need_num") != 10 || msg.Int("num") != 1) {
				t.Fatalf("ticket shortage: %v", msg)
			}
			gemDraw := proto.Message{"group_id": 300, "times": 1, "recruit_type": summonExchange}
			if msg := summonRefusal(c, gemDraw, tc.generalNotice); tc.generalNotice != 0 && msg.Str("msg") != "召唤所需资源不足" {
				t.Fatalf("currency shortage: %v", msg)
			}
			checkState(0, 1)

			// The ten-pull can be paid for, but there are only nine open slots.
			if err := st.Exchange(rid, nil, nil, nil, map[int64]int64{ticket: 9}); err != nil {
				t.Fatal(err)
			}
			seeds := make([]store.PartnerSeed, heroListMax-9)
			for i := range seeds {
				seeds[i] = store.PartnerSeed{BID: starterH5, Star: 5}
			}
			if _, err := st.Summon(rid, store.SummonResult{Seeds: seeds}); err != nil {
				t.Fatal(err)
			}
			if msg := summonRefusal(c, ticketDraw, tc.generalNotice); tc.generalNotice != 0 && msg.Str("msg") != "英雄背包已满, 不能召唤英雄" {
				t.Fatalf("hero capacity: %v", msg)
			}
			checkState(len(seeds), 10)
		})
	}
}
