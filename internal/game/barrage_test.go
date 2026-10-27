package game

import (
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestBarrage(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	a, _ := e.enter("bar1", "弹幕甲")
	b, roleB := e.enter("bar2", "弹幕乙")

	// sending before the barrage is open is refused
	a.send(12732, proto.Message{"type": barrageChampion, "msg": "第一"})
	if r := a.waitFor(12732); r.Int("code") != 0 {
		t.Fatalf("not entered: %v", r)
	}
	a.send(12730, proto.Message{"type": barrageChampion, "is_push": 1})
	b.send(12730, proto.Message{"type": barrageChampion, "is_push": 1})

	a.send(12732, proto.Message{"type": 999, "msg": "x"})
	if r := a.waitFor(12732); r.Int("code") != 0 {
		t.Fatalf("unknown type: %v", r)
	}
	a.send(12732, proto.Message{"type": barrageChampion, "msg": strings.Repeat("长", barrageMaxRunes+1)})
	if r := a.waitFor(12732); r.Int("code") != 0 {
		t.Fatalf("too long: %v", r)
	}
	a.send(12732, proto.Message{"type": barrageChampion, "msg": "加油"})
	// the push comes before the reply
	for name, c := range map[string]*client{"sender": a, "other": b} {
		r := c.waitFor(12733)
		l := r.Arr("list")
		if r.Int("type") != barrageChampion || len(l) != 1 || l[0].Str("msg") != "加油" || l[0].Str("name") != "弹幕甲" {
			t.Fatalf("%s push: %v", name, r)
		}
	}
	if r := a.waitFor(12732); r.Int("code") != 1 {
		t.Fatalf("send: %v", r)
	}
	// too fast
	a.send(12732, proto.Message{"type": barrageChampion, "msg": "再来"})
	if r := a.waitFor(12732); r.Int("code") != 0 {
		t.Fatalf("too fast: %v", r)
	}

	// the other type is a separate room; a newcomer gets the recent messages; leaving stops the pushes
	e.advance(barrageGap)
	b.send(12731, proto.Message{})
	a.send(12732, proto.Message{"type": barrageChampion, "msg": "再见"})
	a.waitFor(12733)
	a.waitFor(12732)
	b.send(12732, proto.Message{"type": barrageChampion, "msg": "我走了"})
	if r := b.waitFor(12732); r.Int("code") != 0 {
		t.Fatalf("left: %v", r)
	}
	b.send(12730, proto.Message{"type": barrageChampion, "is_push": 1})
	if r := b.waitFor(12733); len(r.Arr("list")) != 2 {
		t.Fatalf("history: %v", r)
	}
	b.send(12730, proto.Message{"type": barrageCrossChampion, "is_push": 1})
	b.send(12729, proto.Message{})
	_ = roleB
}
