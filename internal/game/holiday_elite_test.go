package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestHolidayElite(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("he1", "赐福人")
	c.send(23220, proto.Message{})
	if r := c.waitFor(23220); r.Int("camp_id") != 0 {
		t.Fatalf("no activity open: %v", r)
	}
	c.send(23221, proto.Message{"times": 1, "recruit_type": 1})
	if r := c.waitFor(23221); r.Int("flag") != 0 {
		t.Fatalf("a draw without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 51193, BID: 91193, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(23220, proto.Message{})
	r := c.waitFor(23220)
	if r.Int("camp_id") != 51193 || r.Int("free_time") != 0 || r.Int("times") != 0 {
		t.Fatalf("open: %v", r)
	}
	c.send(23221, proto.Message{"times": 1, "recruit_type": 1})
	if r := c.waitFor(23221); r.Int("flag") != 1 {
		t.Fatalf("free draw: %v", r)
	}
	c.waitFor(23220)
	c.send(23221, proto.Message{"times": 1, "recruit_type": 1})
	if r := c.waitFor(23221); r.Int("flag") != 0 {
		t.Fatalf("a second free draw: %v", r)
	}
	c.send(23221, proto.Message{"times": 10, "recruit_type": 4})
	if r := c.waitFor(23221); r.Int("flag") != 0 {
		t.Fatalf("ten draws without the item: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{37010: 30})
	c.send(23221, proto.Message{"times": 10, "recruit_type": 4})
	if r := c.waitFor(23221); r.Int("flag") != 1 {
		t.Fatalf("ten draws: %v", r)
	}
	r = c.waitFor(23220)
	if r.Int("times") != 11 || r.Int("free_time") != actNow.Unix()+86400 {
		t.Fatalf("state: %v", r)
	}
	c.send(23222, proto.Message{})
	if r := c.waitFor(23222); r.Int("flag") != 0 {
		t.Fatalf("an award not reached: %v", r)
	}
	c.send(23221, proto.Message{"times": 10, "recruit_type": 4})
	c.waitFor(23221)
	c.send(23221, proto.Message{"times": 10, "recruit_type": 4}) // 31 draws: the first award (30)
	c.waitFor(23221)
	c.send(23222, proto.Message{})
	if r := c.waitFor(23222); r.Int("flag") != 1 {
		t.Fatalf("award: %v", r)
	}
}
