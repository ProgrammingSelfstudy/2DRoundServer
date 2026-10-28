package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
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
	if got := orders(a.waitFor(25830), "progress"); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("newest two: %v", got)
	}
	a.send(25830, proto.Message{"start": 2, "num": 2})
	if got := orders(a.waitFor(25830), "progress"); len(got) != 2 || got[0] != 2 || got[1] != 3 {
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
