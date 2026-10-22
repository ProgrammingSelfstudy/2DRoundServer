package game

import (
	"math"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestPickHeroFollowsChances(t *testing.T) {
	for id, g := range summonTable {
		total := 0.0
		for _, p := range g.Pool {
			if _, ok := partnerTable[int64(p[0])]; !ok {
				t.Fatalf("group %d: hero %v is not in the hero table", id, p[0])
			}
			total += p[1]
		}
		if math.Abs(total-100) > 0.5 {
			t.Fatalf("group %d chances add up to %v", id, total)
		}
		if first := pickHero(g, 0); first != int64(g.Pool[0][0]) {
			t.Fatalf("group %d: r=0 gave %d", id, first)
		}
		if last := pickHero(g, 0.9999999); last != int64(g.Pool[len(g.Pool)-1][0]) {
			t.Fatalf("group %d: r~1 gave %d", id, last)
		}
	}
}

func TestSummon(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	first := int64(summonTable[100].Pool[0][0])

	// the state: group 100 has its free summon
	c.send(23200, proto.Message{})
	st := c.recv(23200).Arr("recruit_list")
	// the original's shape: groups 100 / 200 / 300 offer 10 and 1 draws (10 listed first), 400 one; the
	// free draw (kv key 4) is on the single draw of groups 100 and 300
	freeOf := func(g proto.Message, times int64) int64 {
		for _, d := range g.Arr("draw_list") {
			if d.Int("times") == times {
				return d.Arr("kv_list")[0].Int("val")
			}
		}
		return -1
	}
	if len(st) != 4 || st[0].Int("group_id") != 100 || len(st[0].Arr("draw_list")) != 2 || st[0].Arr("draw_list")[0].Int("times") != 10 ||
		freeOf(st[0], 1) != 1 || freeOf(st[0], 10) != 0 || freeOf(st[1], 1) != 0 || freeOf(st[2], 1) != 1 || len(st[3].Arr("draw_list")) != 1 {
		t.Fatalf("state: %v", st)
	}

	summon := func(group, times, kind int64) proto.Message {
		c.send(23201, proto.Message{"group_id": group, "times": times, "recruit_type": kind})
		return c.recvOrNone(23201)
	}
	drain := func() { // 11001, 10302, 10500 follow a success
		c.recv(11001)
		c.recv(10302)
		c.recv(10500)
	}
	// the free summon works once a day
	r := summon(100, 1, 1)
	if r == nil || r.Int("flag") != 0 || len(r.Arr("partner_bids")) != 1 || r.Arr("partner_bids")[0].Int("partner_bid") != first {
		t.Fatalf("free: %v", r)
	}
	if hs := c.recv(11001).Arr("partners"); len(hs) != 1 || hs[0].Int("bid") != first {
		t.Fatalf("11001: %v", hs)
	}
	c.recv(10302)
	c.recv(10500)
	// the free draw is followed by the group's state: used up, back at 05:00 the next China day
	if f := c.recv(23202); f.Int("group_id") != 100 || f.Int("free_times") != 0 || f.Int("free_cd_end") != nextFreeReset(time.Unix(0, e.clk.Load())).Unix() {
		t.Fatalf("23202: %v", f)
	}
	if r := summon(100, 1, 1); r != nil {
		t.Fatalf("a second free summon: %v", r)
	}
	// with the item (10401) once, then ten at a time needs ten of it
	if r := summon(100, 1, 4); r != nil {
		t.Fatalf("no item: %v", r)
	}
	e.give(c, role, map[int64]int64{10401: 11})
	if r := summon(100, 1, 4); r == nil {
		t.Fatalf("item summon: %v", r)
	}
	drain()
	if r := summon(100, 10, 4); r == nil || r.Int("flag") != 0 || len(r.Arr("partner_bids")) != 10 {
		t.Fatalf("ten: %v", r)
	}
	drain()
	hs, _ := e.st.PartnersOf(role.RID)
	if len(hs) != 1+1+10 {
		t.Fatalf("heroes: %d", len(hs))
	}
	if got, _ := e.st.RoleItems(role.RID); got[10401] != 0 {
		t.Fatalf("item left: %v", got)
	}
	// every draw pays summon points (asset 23) back
	if as, _ := e.st.RoleAssets(role.RID); as[23] != 1+1+10 {
		t.Fatalf("points: %v", as)
	}
	// a draw count the group does not offer, an unknown group
	if r := summon(100, 5, 4); r != nil {
		t.Fatalf("five: %v", r)
	}
	if r := summon(999, 1, 4); r != nil {
		t.Fatalf("group: %v", r)
	}
}

func TestSummonForGemsAndCap(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	summon := func(group, times, kind int64) proto.Message {
		c.send(23201, proto.Message{"group_id": group, "times": times, "recruit_type": kind})
		return c.recvOrNone(23201)
	}
	if r := summon(300, 1, 3); r != nil { // 220 of asset 3 needed
		t.Fatalf("no gems: %v", r)
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 500}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
	if r := summon(300, 1, 3); r == nil {
		t.Fatalf("gems: %v", r)
	}
	c.recv(11001)
	as := c.recv(10302)
	c.recv(10500)
	_ = as
	got, _ := e.st.RoleAssets(role.RID)
	if got[3] != 280 || got[22] != 5000 {
		t.Fatalf("gems 500-220, bonus 5000 of asset 22: %v", got)
	}
	// the list is full at heroListMax heroes (one was just summoned)
	seeds := make([]store.PartnerSeed, heroListMax-1)
	for i := range seeds {
		seeds[i] = store.PartnerSeed{BID: 10101, Star: 1}
	}
	if _, err := e.st.Summon(role.RID, store.SummonResult{Seeds: seeds}); err != nil {
		t.Fatal(err)
	}
	if r := summon(300, 1, 3); r != nil {
		t.Fatalf("full list: %v", r)
	}
}

// recvOrNone returns the next frame if it is cmd, or nil when none arrives soon
// (a refused summon is not answered).
func (c *client) recvOrNone(cmd uint16) proto.Message {
	c.t.Helper()
	c.c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	fr, err := proto.ReadFrame(c.br)
	for err == nil && fr.Command == 10501 && cmd != 10501 { // the equipment bag follows every 10500
		fr, err = proto.ReadFrame(c.br)
	}
	if err != nil {
		return nil
	}
	if fr.Command != cmd {
		c.t.Fatalf("got command %d, want %d or nothing", fr.Command, cmd)
	}
	m, err := proto.Decode(proto.DefaultOptions(), c.schema.Recv[cmd], fr.Payload)
	if err != nil {
		c.t.Fatal(err)
	}
	return m
}

// The pairs below are the 23201 of the original server (capture 2026-10-21): the
// rewards entry of each draw is the card item of the hero drawn.
func TestHeroCardItemMatchesTheOriginalServer(t *testing.T) {
	for _, c := range []struct {
		bid  int64
		star int
		item int64
	}{
		{20511, 5, 25410}, {20301, 3, 25200}, {10303, 3, 24202}, {20305, 3, 25204}, {10304, 3, 24203}, {30303, 3, 26202},
		{40301, 3, 27200}, {50403, 4, 28302}, {30403, 4, 26302}, {20303, 3, 25202}, {10101, 1, 24000},
	} {
		if got, ok := heroCardItem(c.bid, c.star); !ok || got != c.item {
			t.Errorf("hero %d star %d: card %d (%v), the original sent %d", c.bid, c.star, got, ok, c.item)
		}
	}
	// every hero the summon pools can give has a card, so no drawn hero is left out of the page
	missing := 0
	for _, g := range summonTable {
		for _, e := range g.Pool {
			bid := int64(e[0]) // a pool entry is [hero base id, chance]
			if _, ok := heroCardItem(bid, partnerTable[bid].InitStar); !ok {
				missing++
			}
		}
	}
	if missing != 0 {
		t.Fatalf("%d pool entries have no card item", missing)
	}
}

func TestSummonRewardsListTheHeroCards(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	e.give(c, role, map[int64]int64{10401: 20})
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	c.send(23201, proto.Message{"group_id": 100, "recruit_type": 4, "times": 10})
	r := c.recv(23201)
	bids, rewards := r.Arr("partner_bids"), r.Arr("rewards")
	if len(bids) != 10 || len(rewards) != 10 {
		t.Fatalf("10 draws should list 10 heroes and 10 cards: %v", r)
	}
	for i, b := range bids {
		want, _ := heroCardItem(b.Int("partner_bid"), int(b.Int("init_star")))
		if rewards[i].Int("base_id") != want || rewards[i].Int("num") != 1 {
			t.Fatalf("draw %d: hero %v, reward %v, want card %d", i, b, rewards[i], want)
		}
	}
	// the points are still paid, quietly
	as, _ := e.st.RoleAssets(role.RID)
	var want int64
	for _, h := range summonTable[100].HeroGain {
		if h[0] == 23 {
			want += h[1] * 10
		}
	}
	if want == 0 || as[23] != want {
		t.Fatalf("points: %v, want %d", as, want)
	}
}

func TestFreeSummonDayTurnsAtFive(t *testing.T) {
	zone := dayZone
	at := func(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, zone) }
	if summonDay(at(4, 59)) == summonDay(at(5, 0)) || summonDay(at(5, 0)) != summonDay(at(23, 59)) {
		t.Fatal("the free day must turn at 05:00")
	}
	if got, want := nextFreeReset(at(13, 49)), time.Date(2026, 10, 22, 5, 0, 0, 0, zone); !got.Equal(want) {
		t.Fatalf("next reset %v, want %v", got, want)
	}
	if got, want := nextFreeReset(at(3, 0)), time.Date(2026, 10, 21, 5, 0, 0, 0, zone); !got.Equal(want) {
		t.Fatalf("before 05:00 the reset is today: %v, want %v", got, want)
	}
	// the capture: free_cd_end 1792616400 was sent at 2026-10-21 13:49 China time
	if got := nextFreeReset(time.Unix(1792561742, 0)).Unix(); got != 1792616400 {
		t.Fatalf("the original sent 1792616400, we say %d", got)
	}
}

func TestAdvancedSummonHasAFreeDraw(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	c.send(23201, proto.Message{"group_id": 300, "recruit_type": 1, "times": 1})
	if r := c.recv(23201); len(r.Arr("partner_bids")) != 1 || len(r.Arr("rewards")) != 1 {
		t.Fatalf("free advanced draw: %v", r)
	}
	c.recv(11001)
	c.recv(10302)
	c.recv(10500)
	if f := c.recv(23202); f.Int("group_id") != 300 || f.Int("free_times") != 0 || f.Int("free_cd_end") == 0 {
		t.Fatalf("23202: %v", f)
	}
	c.send(23201, proto.Message{"group_id": 300, "recruit_type": 1, "times": 1})
	if r := c.recvOrNone(23201); r != nil {
		t.Fatalf("a second free advanced draw: %v", r)
	}
	// the friendship summon has none
	c.send(23201, proto.Message{"group_id": 200, "recruit_type": 1, "times": 1})
	if r := c.recvOrNone(23201); r != nil {
		t.Fatalf("friendship summon is never free: %v", r)
	}
}
