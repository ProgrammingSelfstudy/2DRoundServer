package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func campTeam(stage int64, units ...int64) proto.Message {
	var pos []proto.Message
	for i, u := range units {
		pos = append(pos, proto.Message{"pos": i + 1, "id": u})
	}
	return proto.Message{"id": stage, "formation_type": 1, "pos_info": pos}
}

func TestTrainingcampLessons(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("tc", "训练生")
	e.eggGrant(t, role, map[int64]int64{5: 200000000}, nil)
	c.send(27600, proto.Message{})
	if r := c.waitFor(27600); len(r.Arr("ids")) != 0 || r.Int("flag") != 0 {
		t.Fatalf("27600: %v", r)
	}
	// the second lesson is closed until the first is won
	c.send(27602, proto.Message{"id": 2})
	if r := c.waitFor(27602); r.Int("flag") != 0 {
		t.Fatalf("closed: %v", r)
	}
	c.send(27602, proto.Message{"id": 1})
	if r := c.waitFor(27602); r.Int("flag") != 1 || r.Int("is_first") != 1 {
		t.Fatalf("27602: %v", r)
	}
	c.send(27602, proto.Message{"id": 1})
	if r := c.waitFor(27602); r.Int("is_first") != 0 {
		t.Fatalf("27602 again: %v", r)
	}
	// a unit that is not lent by the lesson is not a hero
	c.send(27601, proto.Message{"id": 1, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": 999}}})
	if r := c.waitFor(27601); r.Int("flag") != 0 {
		t.Fatalf("stranger: %v", r)
	}
	c.send(27601, campTeam(1, 96011, 96012, 96013))
	if r := c.waitFor(27601); r.Int("flag") != 1 {
		t.Fatalf("27601: %v", r)
	}
	c.waitFor(20027)
	c.send(20062, proto.Message{}) // skip the fight
	if r := c.waitFor(20006); r.Int("combat_type") != combatCamp {
		t.Fatalf("result: %v", r)
	}
	st := &campState{}
	if _, err := e.st.LoadState(role.RID, campModule, st); err != nil {
		t.Fatal(err)
	}
	if len(st.Done) == 1 {
		if r := c.waitFor(27600); len(r.Arr("ids")) != 1 {
			t.Fatalf("27600 after: %v", r)
		}
	}
}

func TestTrainingcampNeedsRequiredHeroes(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("tc2", "训练生二")
	e.eggGrant(t, role, map[int64]int64{5: 200000000}, nil)
	if err := e.st.SaveState(role.RID, campModule, &campState{Done: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	c.send(27601, campTeam(2, 96021, 96022))
	if r := c.waitFor(27601); r.Int("flag") != 0 || r["msg"] != "必须上阵指定的英雄" {
		t.Fatalf("required: %v", r)
	}
}
