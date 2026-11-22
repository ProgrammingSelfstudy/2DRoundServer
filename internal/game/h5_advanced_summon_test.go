package game

import (
	"slices"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestH5AdvancedHeroOdds(t *testing.T) {
	counts := map[int64]int{}
	for i := 0; i < 2520; i++ {
		bid, next := h5Advanced.pick(11, (float64(i)+0.5)/2520)
		counts[bid]++
		hero := partnerTable[bid]
		if hero.InitStar != 5 || hero.Camp == 7 || (next == 0) != slices.Contains(h5Advanced.rareHeroes, bid) {
			t.Fatalf("invalid ordinary draw %d, next misses %d", bid, next)
		}
	}
	if len(counts) != 65 || len(h5Advanced.rareHeroes) != 18 || h5Advanced.Pity != 30 {
		t.Fatalf("pool size %d, rare %d, guarantee %d", len(counts), len(h5Advanced.rareHeroes), h5Advanced.Pity)
	}
	tierCounts := map[int]int{}
	for _, weight := range counts {
		tierCounts[weight]++
	}
	if tierCounts[63] != 26 || tierCounts[36] != 21 || tierCounts[7] != 18 {
		t.Fatalf("individual weights: %v", tierCounts)
	}
	for i, want := range h5Advanced.rareHeroes {
		bid, next := h5Advanced.pick(29, (float64(i)+0.5)/18)
		if bid != want || next != 0 {
			t.Fatalf("guarantee %d: got %d, next %d", i, bid, next)
		}
	}
}

func TestH5AdvancedGuaranteeAcrossPaymentsAndLogin(t *testing.T) {
	original := summonRand
	summonRand = func() float64 { return 0 } // every regular roll misses rare
	// The gateway cleanup must finish before restoring the shared random source.
	t.Cleanup(func() { summonRand = original })
	e := newEnv(t, time.Minute)
	account, err := e.st.FindOrCreateAccount(store.PlatformH5, "h5pool")
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.IssueToken(account.ID, 3600)
	if err != nil {
		t.Fatal(err)
	}
	role, err := e.st.CreateRole(account.ID, "symlf_1", "甲", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	login := func() *client {
		c := e.dial(true)
		c.send(1164, loginArgs(account.Account, token))
		c.recv(1164)
		c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
		c.recv(10192)
		return c
	}
	c := login()
	state := func(want int64) {
		t.Helper()
		c.send(23200, proto.Message{})
		if n := c.recv(23200).Int("must_five_num"); n != want {
			t.Fatalf("remaining %d, want %d", n, want)
		}
	}
	draw := func(times, kind, want int64) []proto.Message {
		t.Helper()
		c.send(23201, proto.Message{"group_id": 300, "recruit_type": kind, "times": times})
		r := c.recv(23201)
		if r.Int("must_five_num") != want || int64(len(r.Arr("partner_bids"))) != times {
			t.Fatalf("draw size %d, expected remaining %d: %v", times, want, r)
		}
		c.recv(11001)
		c.recv(10302)
		c.recv(10500)
		if kind == summonFree {
			c.recv(23202)
		}
		return r.Arr("partner_bids")
	}
	state(30)
	draw(1, summonFree, 29)
	c.send(23201, proto.Message{"group_id": 300, "recruit_type": summonItem, "times": 10})
	c.expectNothing()
	state(29)
	e.give(c, role, map[int64]int64{summonTable[300].ItemOnce[0][0]: 36})
	draw(10, summonItem, 19)
	draw(10, summonItem, 9)
	for n := int64(8); n >= 3; n-- {
		draw(1, summonItem, n)
	}
	c.c.Close()
	c = login()
	state(3)
	bids := draw(10, summonItem, 23)
	for i, b := range bids {
		if b.Int("init_star") != 5 || slices.Contains(h5Advanced.rareHeroes, b.Int("partner_bid")) != (i == 2) {
			t.Fatalf("draw %d of ten: %v", i+1, b)
		}
	}
	state(23)
}
