package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestExpTasks(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("a", "甲")
	c.send(25810, proto.Message{})
	if r := c.waitFor(25810); len(r.Arr("feat_list")) != 0 {
		t.Fatalf("below the open level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, expTaskOpenLev); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: 300}); err != nil {
		t.Fatal(err)
	}
	find := func(r proto.Message, id int64) proto.Message {
		for _, f := range r.Arr("feat_list") {
			if f.Int("id") == id {
				return f
			}
		}
		return nil
	}
	c.send(25810, proto.Message{})
	r := c.waitFor(25810)
	if f := find(r, 40000); f == nil || f.Int("finish") != questDone {
		t.Fatalf("tower 300: %v", r)
	}
	if f := find(r, 40010); f == nil || f.Int("finish") != questOpen || f.Arr("progress")[0].Int("value") != 300 {
		t.Fatalf("tower 600: %v", f)
	}
	if find(r, 77000) != nil { // needs level 100
		t.Fatal("a trial above the level is listed")
	}
	c.send(25812, proto.Message{"id": 40010})
	if r := c.waitFor(25812); r.Int("code") != 0 {
		t.Fatalf("take an unfinished: %v", r)
	}
	c.send(25812, proto.Message{"id": 40000})
	if r := c.waitFor(25812); r.Int("code") != 1 || r.Int("id") != 40000 || r.Int("finish_time") == 0 {
		t.Fatalf("take: %v", r)
	}
	c.send(25812, proto.Message{"id": 40000})
	if r := c.waitFor(25812); r.Int("code") != 0 {
		t.Fatalf("take twice: %v", r)
	}
	c.send(25810, proto.Message{})
	r = c.waitFor(25810)
	if find(r, 40000) != nil || len(r.Arr("finish_list")) != 1 || r.Arr("finish_list")[0].Int("id") != 40000 {
		t.Fatalf("after taking: %v", r)
	}
}
