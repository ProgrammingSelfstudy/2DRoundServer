package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestOpenGiftsAll(t *testing.T) {
	g := giftInfo{Items: []sellGain{{1, 100}, {10450, 5}}}
	assets, items := openGifts(g, 3, func(int) int { t.Fatal("rand 0 must not draw"); return 0 })
	if assets[1] != 300 || items[10450] != 15 || len(assets)+len(items) != 2 {
		t.Fatalf("assets %v items %v", assets, items)
	}
}

func TestOpenGiftsRandom(t *testing.T) {
	g := giftInfo{Rand: 2, Items: []sellGain{{10450, 1}, {10451, 2}, {10452, 3}}}
	// always taking the first of what is left: entries 0 and 1 of each box
	assets, items := openGifts(g, 2, func(int) int { return 0 })
	if len(assets) != 0 || items[10450] != 2 || items[10451] != 4 || items[10452] != 0 {
		t.Fatalf("assets %v items %v", assets, items)
	}
	// rand larger than the list gives the whole list once
	_, items = openGifts(giftInfo{Rand: 9, Items: g.Items}, 1, func(int) int { return 0 })
	if len(items) != 3 {
		t.Fatalf("items %v", items)
	}
}

func TestUseGiftBox(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// 99955 is the daily sign-in box; 10020 is an item without a gift table
	e.give(c, role, map[int64]int64{99955: 3, 10020: 1})

	use := func(id, n int64) proto.Message {
		c.send(10515, proto.Message{"id": id, "quantity": n, "args": []proto.Message{}})
		return c.recv(10515)
	}
	for _, bad := range [][2]int64{{99955, 0}, {99955, 4}, {10020, 1}, {12345678, 1}} {
		if r := use(bad[0], bad[1]); r.Int("flag") != 0 || r.Str("msg") == "" {
			t.Fatalf("use %v should fail: %v", bad, r)
		}
	}
	if got, _ := e.st.RoleItems(role.RID); got[99955] != 3 {
		t.Fatalf("a refused use must change nothing: %v", got)
	}

	r := use(99955, 2)
	if r.Int("flag") != 1 || r.Int("base_id") != 99955 {
		t.Fatalf("use: %v", r)
	}
	if as := c.recv(10302); as.Str("coin") != "100000000" { // 2 boxes x 50,000,000 (asset 1)
		t.Fatalf("coin after opening: %v", as)
	}
	c.recv(10500)
	got, _ := e.st.RoleItems(role.RID)
	if got[99955] != 1 || got[10450] != 100000 || got[10103] != 20 {
		t.Fatalf("ledger after opening: %v", got)
	}
}
