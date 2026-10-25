package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestEncounterStory(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("en", "奇遇人")
	e.eggGrant(t, role, map[int64]int64{5: 200000000}, nil)
	// 娜美 (20402) has a story without level or power asked
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 20402, Star: 4}}); err != nil || !ok {
		t.Fatal(err, ok)
	}
	e.setNow(time.Date(2026, 10, 25, 12, 30, 0, 0, dayZone))
	c.send(27100, proto.Message{})
	r := c.waitFor(27100)
	if r.Int("id") != 1 || r.Int("page") != 1 {
		t.Fatalf("27100: %v", r)
	}
	c.send(27103, proto.Message{"id": 1, "page": 3})
	c.send(27104, proto.Message{"choice": 9})
	if r := c.waitFor(27104); r["msg"] != "没有可选择的物语" {
		t.Fatalf("bad choice: %v", r)
	}
	c.send(27104, proto.Message{"choice": 2})
	if r := c.waitFor(27104); r["msg"] != "选择成功" {
		t.Fatalf("choice: %v", r)
	}
	c.send(27100, proto.Message{})
	if r := c.waitFor(27100); r.Int("id") != 1 || r.Int("page") != 3 {
		t.Fatalf("progress: %v", r)
	}
	c.send(27101, proto.Message{})
	c.waitFor(27101)
	c.send(27102, proto.Message{})
	if r := c.waitFor(27102); len(r.Arr("id_list")) != 1 || r.Arr("id_list")[0].Int("id") != 1 {
		t.Fatalf("library: %v", r)
	}
	// nothing more in the same window, and the only story is read
	e.setNow(time.Date(2026, 10, 25, 13, 0, 0, 0, dayZone))
	c.send(27100, proto.Message{})
	if r := c.waitFor(27100); r.Int("id") != 0 {
		t.Fatalf("again: %v", r)
	}
	e.setNow(time.Date(2026, 10, 26, 0, 10, 0, 0, dayZone))
	c.send(27100, proto.Message{})
	if r := c.waitFor(27100); r.Int("id") != 0 {
		t.Fatalf("read story back: %v", r)
	}
}

func TestEncounterNeedsLevelAndWindow(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("en2", "新人")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 20402, Star: 4}}); err != nil || !ok {
		t.Fatal(err, ok)
	}
	e.setNow(time.Date(2026, 10, 25, 12, 30, 0, 0, dayZone))
	c.send(27100, proto.Message{})
	if r := c.waitFor(27100); r.Int("id") != 0 {
		t.Fatalf("low level: %v", r)
	}
	e.eggGrant(t, role, map[int64]int64{5: 200000000}, nil)
	e.setNow(time.Date(2026, 10, 25, 15, 0, 0, 0, dayZone))
	c.send(27100, proto.Message{})
	if r := c.waitFor(27100); r.Int("id") != 0 {
		t.Fatalf("outside the windows: %v", r)
	}
}
