package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestDramaPassVideos(t *testing.T) {
	easyDungeons(t)
	e := newEnv(t, time.Minute)
	c, role := e.enter("dp1", "通关人")
	first := dramaChain.Dungeons[0]
	c.send(13015, proto.Message{"dun_id": first.ID})
	if r := c.recv(13015); len(r.Arr("dungeon_replay_log")) != 0 {
		t.Fatalf("before any clear: %v", r)
	}
	c.send(13003, proto.Message{})
	c.recv(13003)
	c.playFight()
	c.recv(13001)
	c.recv(10302)
	c.recv(10500)
	c.send(13015, proto.Message{"dun_id": first.ID})
	r := c.recv(13015)
	log := r.Arr("dungeon_replay_log")
	if len(log) != 3 {
		t.Fatalf("pass videos: %v", r)
	}
	for _, l := range log {
		if l.Int("rid") != role.RID || l.Int("repaly_id") == 0 || l.Str("name") != role.Name {
			t.Fatalf("entry: %v", l)
		}
		if rp, err := e.st.ReplayByID(l.Int("repaly_id"), role.RID); err != nil || rp == nil {
			t.Fatalf("the replay is not kept: %v %v", rp, err)
		}
	}
	// the progress among the players: the only role with a clear is above nobody
	c.send(13020, proto.Message{})
	if r := c.recv(13020); r.Int("val") != 0 {
		t.Fatalf("progress alone: %v", r)
	}
	c2, _ := e.enter("dp2", "新手")
	c2.send(13020, proto.Message{})
	if r := c2.recv(13020); r.Int("val") != 0 {
		t.Fatalf("a new role: %v", r)
	}
	c.send(13020, proto.Message{})
	if r := c.recv(13020); r.Int("val") != 50000 {
		t.Fatalf("progress above one of two: %v", r)
	}
	c.send(13002, proto.Message{})
	if r := c.recv(13002); r.Int("code") != 0 {
		t.Fatalf("13002: %v", r)
	}
}
