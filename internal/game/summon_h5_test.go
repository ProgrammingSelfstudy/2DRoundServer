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
	oldRand := summonRand
	summonRand = func() float64 { return 0.99999 }
	t.Cleanup(func() { summonRand = oldRand })
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

func TestH5SummonConvertsLowStarDraws(test *testing.T) {
	for _, tc := range []struct {
		name               string
		cmd                uint16
		kind, group, camp  int64
		ticket, hero, chip int64
		count              int64
		roll               float64
	}{
		{name: "normal diamonds", cmd: 23201, kind: summonExchange, group: 300, hero: 10301, chip: 24700, count: 20},
		{name: "normal free", cmd: 23201, kind: summonFree, group: 300, hero: 10301, chip: 24700, count: 20},
		{name: "elite scroll", cmd: 23231, kind: eliteScrollType, group: eliteGroup, ticket: 10408, hero: 10301, chip: 24700, count: 20, roll: 0.999},
		{name: "limited free", cmd: 23217, kind: timeSummonFree, group: 100, camp: 14012, hero: 10404, chip: 24803, count: 30, roll: 0.06},
		{name: "limited ticket", cmd: 23217, kind: timeSummonItem, group: 100, camp: 14012, ticket: 10407, hero: 10404, chip: 24803, count: 30, roll: 0.06},
	} {
		test.Run(tc.name, func(test *testing.T) {
			env := newDungeonEnv(test)
			env.setNow(actNow)
			cli, role := enterDungeonRole(env, store.PlatformH5, "summoner", "召唤人")
			if tc.camp != 0 {
				env.addActivity(test, store.Activity{SrvID: role.SrvID, CampID: tc.camp, BID: 91193, Status: 1, Spec: actSpecJSON(test, false)})
			}
			var gems, tickets map[int64]int64
			if tc.cmd == 23201 && tc.kind == summonExchange {
				gems = map[int64]int64{3: 220}
			}
			if tc.ticket != 0 {
				tickets = map[int64]int64{tc.ticket: 1}
			}
			if err := env.st.Exchange(role.RID, nil, nil, gems, tickets); err != nil {
				test.Fatal(err)
			}
			seeds := make([]store.PartnerSeed, heroListMax)
			for idx := range seeds {
				seeds[idx] = store.PartnerSeed{BID: starterH5, Star: 5}
			}
			if _, err := env.st.Summon(role.RID, store.SummonResult{Seeds: seeds}); err != nil {
				test.Fatal(err)
			}
			oldRand := summonRand
			roll := tc.roll
			summonRand = func() float64 {
				next := roll
				roll = 0
				return next
			}
			test.Cleanup(func() { summonRand = oldRand })

			var result, bag proto.Message
			var freeUsed bool
			for _, frame := range captureReplies(cli, tc.cmd, proto.Message{"group_id": tc.group, "times": 1, "recruit_type": tc.kind}) {
				switch frame.cmd {
				case 23201:
					result = frame.msg
				case 11001:
					test.Fatalf("shard-only draw pushed heroes: %v", frame.msg)
				case 10500:
					bag = frame.msg
				case 23202:
					freeUsed = frame.msg.Int("free_times") == 0
				case 23216:
					freeUsed = frame.msg.Int("free_time") > actNow.Unix()
				}
			}
			rewards, chips := result.Arr("rewards"), result.Arr("partner_chips")
			if result.Int("group_id") != tc.group || result.Int("times") != 1 || len(result.Arr("partner_bids")) != 0 ||
				len(rewards) != 1 || rewards[0].Int("base_id") != tc.chip || rewards[0].Int("num") != tc.count ||
				len(chips) != 1 || chips[0].Int("partner_bid") != tc.hero || chips[0].Int("chip_bid") != tc.chip || chips[0].Int("chip_num") != tc.count {
				test.Fatalf("unexpected result: %v", result)
			}
			var bagCount int64
			for _, item := range bag.Arr("item_list") {
				if item.Int("base_id") == tc.chip {
					bagCount = item.Int("quantity")
				}
			}
			if bagCount != tc.count {
				test.Fatalf("backpack has %d shards, want %d", bagCount, tc.count)
			}
			if items, err := env.st.RoleItems(role.RID); err != nil || items[tc.chip] != tc.count || items[tc.ticket] != 0 {
				test.Fatalf("shard/ticket balances: %v (%v)", items, err)
			}
			if heroes, err := env.st.PartnersOf(role.RID); err != nil || len(heroes) != len(seeds) {
				test.Fatalf("roster size after shard draw: %d (%v)", len(heroes), err)
			}
			if assets, err := env.st.RoleAssets(role.RID); err != nil || assets[3] != 0 || gems != nil && (assets[22] != 5000 || assets[23] != 20) {
				test.Fatalf("summon charge and currency bonus: %v (%v)", assets, err)
			}
			if tc.kind == summonFree && !freeUsed {
				test.Fatal("shard-only draw did not consume the free summon")
			}
		})
	}
}

func TestH5SummonMixedDrawUsesSlotsOnlyForHeroes(test *testing.T) {
	cli, st, rid := newSummonClient(test, store.PlatformH5)
	seeds := make([]store.PartnerSeed, heroListMax-1)
	for idx := range seeds {
		seeds[idx] = store.PartnerSeed{BID: starterH5, Star: 5}
	}
	if _, err := st.Summon(rid, store.SummonResult{Seeds: seeds}); err != nil {
		test.Fatal(err)
	}
	if err := st.Exchange(rid, nil, nil, nil, map[int64]int64{10403: 10}); err != nil {
		test.Fatal(err)
	}
	oldRand, draws := summonRand, 0
	summonRand = func() float64 {
		draws++
		if draws == 10 {
			return 0.99999
		}
		return 0
	}
	test.Cleanup(func() { summonRand = oldRand })
	cli.send(23201, proto.Message{"group_id": 300, "times": 10, "recruit_type": summonItem})
	result := cli.recv(23201)
	bids := result.Arr("partner_bids")
	if result.Int("times") != 10 || len(bids) != 1 || bids[0].Int("partner_bid") != 50514 || len(result.Arr("partner_chips")) != 9 {
		test.Fatalf("mixed draw result: %v", result)
	}
	if heroes := cli.recv(11001).Arr("partners"); len(heroes) != 1 || heroes[0].Int("bid") != 50514 {
		test.Fatalf("mixed draw roster push: %v", heroes)
	}
	cli.recv(10302)
	cli.recv(10500)
	if heroes, err := st.PartnersOf(rid); err != nil || len(heroes) != heroListMax || heroes[len(heroes)-1].BID != 50514 {
		test.Fatalf("mixed draw roster size: %d (%v)", len(heroes), err)
	}
	if items, err := st.RoleItems(rid); err != nil || items[24700] != 9*20 || items[10403] != 0 {
		test.Fatalf("mixed draw inventory: %v (%v)", items, err)
	}
}
