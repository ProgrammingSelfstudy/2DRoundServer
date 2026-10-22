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
	atDungeon(t, e, role.RID, 20200, true) // the limit is the highest dungeon *cleared*
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

// Reward 1 is 200,000,000 captain exp and nothing else: the original paid exactly
// that (capture original-2026-10-22: 12742 with one asset, diamonds unchanged), not
// the four entries, 4,000,000,000 gems among them, of the client's table.
func TestMilestoneOneIsCaptainExpOnly(t *testing.T) {
	if m := milestones[1]; len(m.Items) != 1 || m.Items[0] != [2]int64{5, 200_000_000} {
		t.Fatalf("reward 1: %v", m.Items)
	}
	for id, m := range milestones {
		for _, g := range m.Items {
			if g[1] > maxMilestoneAmount {
				t.Errorf("milestone %d pays %d of %d, above the limit", id, g[1], g[0])
			}
		}
	}
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// a new role has cleared nothing (max_dun_id 0 in the original), so it cannot take it yet
	c.send(13009, proto.Message{"id": 1})
	if r := c.recv(13009); r.Int("code") != 0 {
		t.Fatalf("before the first win: %v", r)
	}
	atDungeon(t, e, role.RID, 10010, true)
	c.send(13009, proto.Message{"id": 1})
	gain := c.recv(12742)
	list := gain.Arr("asset_list")
	if len(list) != 1 || list[0].Int("bid") != 5 || list[0].Str("num") != "200000000" || list[0].Int("is_bind") != 1 || gain.Int("source") != 0 {
		t.Fatalf("12742: %v", gain)
	}
	if r := c.recv(13009); r.Int("code") != 1 || r.Int("id") != 1 || r.Str("msg") != "" {
		t.Fatalf("13009: %v", r)
	}
	// the capture's role went to level 124 on the same exp
	if got, err := e.st.RoleByID(role.RID); err != nil || got.Lev != 124 {
		t.Fatalf("level after the claim: %v %v", got, err)
	}
}
