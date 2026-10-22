package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestDramaMilestones(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	take := func(id int64) proto.Message {
		c.send(13009, proto.Message{"id": id})
		return c.recv(13009)
	}
	if r := take(10); r.Int("code") != 0 { // needs dungeon 20200
		t.Fatalf("too early: %v", r)
	}
	if r := take(9999); r.Int("code") != 0 {
		t.Fatalf("unknown: %v", r)
	}
	if r := take(1); r.Int("code") != 0 { // the 4 billion gems entry is refused
		t.Fatalf("test value: %v", r)
	}
	atDungeon(t, e, role.RID, 20200, false)
	r := take(10)
	if r.Int("code") != 1 || r.Int("id") != 10 {
		t.Fatalf("take: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	if got, _ := e.st.RoleItems(role.RID); got[40403] != 1 || got[40203] != 1 {
		t.Fatalf("items: %v", got)
	}
	if r := take(10); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	c.send(13008, proto.Message{})
	l := c.recv(13008).Arr("list")
	if len(l) != 1 || l[0].Int("id") != 10 {
		t.Fatalf("13008: %v", l)
	}
}

func TestMilestonesAreOrderedByTheChain(t *testing.T) {
	// the client's test is max_dun_id >= limit_id, so every limit must be a real dungeon
	for id, m := range milestones {
		if _, ok := dungeonByID[m.Limit]; !ok {
			t.Errorf("milestone %d needs dungeon %d, which does not exist", id, m.Limit)
		}
	}
}
