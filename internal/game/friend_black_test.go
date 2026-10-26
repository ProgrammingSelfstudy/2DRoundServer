package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestFriendBlacklist(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	befriend(t, ca, a, cb, b)

	ca.send(13330, proto.Message{})
	if r := ca.recv(13330); len(r.Arr("black_list")) != 0 {
		t.Fatalf("empty list: %v", r)
	}
	ca.send(13332, rid(a))
	if r := ca.recv(13332); r.Int("code") != 0 {
		t.Fatalf("block self: %v", r)
	}
	ca.send(13332, rid(b))
	if r := ca.recv(13332); r.Int("code") != 1 || r.Int("rid") != b.RID {
		t.Fatalf("block: %v", r)
	}
	// the friendship ended, the other side is told
	if r := cb.recv(13308); r.Int("rid") != a.RID {
		t.Fatalf("13308: %v", r)
	}
	ca.send(13300, proto.Message{})
	if r := ca.recv(13300); len(r.Arr("friend_list")) != 0 {
		t.Fatalf("still friends: %v", r)
	}
	ca.send(13332, rid(b))
	if r := ca.recv(13332); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	ca.send(13330, proto.Message{})
	if r := ca.recv(13330); len(r.Arr("black_list")) != 1 || r.Arr("black_list")[0].Str("name") != "乙" {
		t.Fatalf("list: %v", r)
	}
	// neither side can ask the other
	ca.send(13303, rid(b))
	if r := ca.recv(13303); r.Int("code") != 0 {
		t.Fatalf("blocker asks: %v", r)
	}
	cb.send(13303, rid(a))
	if r := cb.recv(13303); r.Int("code") != 0 {
		t.Fatalf("blocked asks: %v", r)
	}
	// taken off, they can again
	ca.send(13333, rid(b))
	if r := ca.recv(13333); r.Int("code") != 1 {
		t.Fatalf("remove: %v", r)
	}
	ca.send(13333, rid(b))
	if r := ca.recv(13333); r.Int("code") != 0 {
		t.Fatalf("remove twice: %v", r)
	}
	cb.send(13303, rid(a))
	if r := cb.recv(13303); r.Int("code") != 1 {
		t.Fatalf("ask after: %v", r)
	}
}

func TestFriendBlacklistDropsPendingRequestsAndRecommend(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	_, c := e.enter("c", "丙")
	cb.send(13303, rid(a))
	cb.recv(13303)
	ca.recv(13304)
	ca.send(13332, rid(b))
	if r := ca.recv(13332); r.Int("code") != 1 {
		t.Fatalf("block: %v", r)
	}
	ca.send(13311, proto.Message{})
	if r := ca.recv(13311); len(r.Arr("friend_req_list")) != 0 {
		t.Fatalf("request kept: %v", r)
	}
	// a sees c only, b sees c only: the two never recommend each other
	ca.send(13320, proto.Message{})
	if l := ca.recv(13320).Arr("recommend_list"); !same(ids(l), c.RID) {
		t.Fatalf("recommend to the blocker: %v", ids(l))
	}
	cb.send(13320, proto.Message{})
	if l := cb.recv(13320).Arr("recommend_list"); !same(ids(l), c.RID) {
		t.Fatalf("recommend to the blocked: %v", ids(l))
	}
	ca.send(13315, rid(b))
	if r := ca.recv(13315); r.Int("code") != 0 {
		t.Fatalf("13315: %v", r)
	}
}
