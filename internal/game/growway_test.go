package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGrowWay(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	a, ra := e.enter("ua", "甲甲")
	b, rb := e.enter("ub", "乙乙")
	// a follower is the first line of the followed role's growth way
	b.send(25801, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	if r := b.waitFor(25801); r.Int("flag") != 1 {
		t.Fatalf("follow: %v", r)
	}
	a.send(25830, proto.Message{"start": 0, "num": 10})
	r := a.waitFor(25830)
	if r.Int("max_num") != 1 || len(r.Arr("progress")) != 1 {
		t.Fatalf("first line: %v", r)
	}
	line := r.Arr("progress")[0]
	args := line.Arr("arge")
	if line.Int("id") != 120403 || line.Int("order") != 1 || len(args) != 2 || args[0].Str("val") != "乙乙" || args[1].Str("val") != "1" {
		t.Fatalf("line: %v", line)
	}
	// more lines; the pages are by order
	for i := 0; i < 4; i++ {
		if err := RecordGrowth(e.st, ra.RID, 115002, time.Now().Unix(), "路飞"); err != nil {
			t.Fatal(err)
		}
	}
	orders := func(m proto.Message, key string) []int64 {
		var out []int64
		for _, l := range m.Arr(key) {
			out = append(out, l.Int("order"))
		}
		return out
	}
	a.send(25830, proto.Message{"start": 0, "num": 2})
	if got := orders(a.waitFor(25830), "progress"); len(got) != 2 || got[0] != 5 || got[1] != 4 { // newest first (capture original-2026-10-27)
		t.Fatalf("newest two: %v", got)
	}
	a.send(25830, proto.Message{"start": 2, "num": 2})
	if got := orders(a.waitFor(25830), "progress"); len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Fatalf("from 2: %v", got)
	}
	// another role reads it by id
	b.send(25832, proto.Message{"rid": ra.RID, "srv_id": "symlf_1", "start": 0, "num": 3})
	r = b.waitFor(25832)
	if r.Int("max_num") != 5 || len(r.Arr("room_grow_info")) != 3 || r.Int("rid") != ra.RID {
		t.Fatalf("other's: %v", r)
	}
	// the one who followed has no lines
	b.send(25830, proto.Message{"start": 0, "num": 10})
	if r := b.waitFor(25830); r.Int("max_num") != 0 || len(r.Arr("progress")) != 0 {
		t.Fatalf("own: %v", r)
	}
	_ = rb
}

func TestGrowEvents(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	_, ra := e.enter("ua", "甲甲")
	_, rb := e.enter("ub", "乙乙")
	ids := func(rid int64) []int64 {
		d := &growDoc{}
		if _, err := e.st.LoadState(rid, growModule, d); err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, g := range d.Entries {
			out = append(out, g.ID)
		}
		return out
	}
	has := func(rid, id int64) bool {
		for _, x := range ids(rid) {
			if x == id {
				return true
			}
		}
		return false
	}
	// hasAny: one of the n variants of an event (the original picks one at random)
	hasAny := func(rid, base int64, n int) bool {
		for i := 0; i < n; i++ {
			if has(rid, base+int64(i)) {
				return true
			}
		}
		return false
	}
	// the first friend
	if err := e.st.RequestFriend(ra.RID, rb.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(ra.RID, rb.RID, 100); err != nil {
		t.Fatal(err)
	}
	growFriend(e.st, ra.RID, rb.Name, 1)
	if !hasAny(ra.RID, 103001, 4) {
		t.Fatalf("first friend: %v", ids(ra.RID))
	}
	// a guild
	growGuild(e.st, rb.RID, "海贼团", ra.RID, false, 2)
	growGuild(e.st, ra.RID, "海贼团", ra.RID, true, 2)
	if !hasAny(rb.RID, 109001, 4) || !hasAny(ra.RID, 110001, 4) {
		t.Fatalf("guild: %v %v", ids(rb.RID), ids(ra.RID))
	}
	// worshipped: 1st, then nothing until the 10th
	growWorshipped(e.st, ra.RID, "乙乙", 1, 3)
	growWorshipped(e.st, ra.RID, "乙乙", 2, 3)
	growWorshipped(e.st, ra.RID, "乙乙", 100, 3)
	if !hasAny(ra.RID, 107001, 3) || !has(ra.RID, 107003) {
		t.Fatalf("worship: %v", ids(ra.RID))
	}
	// stars: the first 6-star hero once, the 5th 10-star hero
	if _, err := e.st.GrantStarterPartners(rb.RID, []store.PartnerSeed{{BID: 10502, Star: 6}, {BID: 10502, Star: 10}, {BID: 10502, Star: 10},
		{BID: 10502, Star: 10}, {BID: 10502, Star: 10}, {BID: 10502, Star: 10}}); err != nil {
		t.Fatal(err)
	}
	growStar(e.st, rb.RID, "火拳", 6, 4)
	growStar(e.st, rb.RID, "火拳", 6, 5)
	growStar(e.st, rb.RID, "火拳", 10, 6)
	n6 := 0
	for _, id := range ids(rb.RID) {
		if id >= 102001 && id <= 102004 {
			n6++
		}
	}
	if n6 != 1 || !hasAny(rb.RID, 121101, 2) {
		t.Fatalf("stars: %v", ids(rb.RID))
	}
	growChampion(e.st, rb.RID, 1, 7)
	growChampion(e.st, rb.RID, 20, 7)
	growChampion(e.st, rb.RID, 90, 7)
	if !has(rb.RID, 118201) || !has(rb.RID, 118001) {
		t.Fatalf("champion: %v", ids(rb.RID))
	}
}
