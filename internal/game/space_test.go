package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestSpaceCityFollowAndFans(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	a, ra := e.enter("ua", "甲甲")
	b, rb := e.enter("ub", "乙乙")
	a.send(25800, proto.Message{"city_id": 999})
	if r := a.waitFor(25800); r.Int("code") != 0 {
		t.Fatalf("unknown city: %v", r)
	}
	a.send(25800, proto.Message{"city_id": 103})
	if r := a.waitFor(25800); r.Int("code") != 1 || r.Int("city_id") != 103 {
		t.Fatalf("city: %v", r)
	}
	a.send(25801, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(25801); r.Int("code") != 0 {
		t.Fatalf("follow yourself: %v", r)
	}
	a.send(25801, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if r := a.waitFor(25801); r.Int("code") != 1 || r.Int("flag") != 1 {
		t.Fatalf("follow: %v", r)
	}
	b.send(10315, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	if p := b.waitFor(10315); p.Int("city_id") != 103 || p.Int("is_be_fanse") != 1 || p.Int("is_fanse") != 0 || p.Int("room_bbs_set") != 1 {
		t.Fatalf("profile of a: %v", p)
	}
	a.send(10315, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"})
	if p := a.waitFor(10315); p.Int("fans_num") != 1 || p.Int("fans_rank") != 1 || p.Int("is_fanse") != 1 {
		t.Fatalf("profile of b: %v", p)
	}
	b.send(25802, proto.Message{})
	if r := b.waitFor(25802); r.Int("rank") != 1 || len(r.Arr("rank_list")) != 1 || r.Arr("rank_list")[0].Int("fans_num") != 1 {
		t.Fatalf("fans ranking: %v", r)
	}
	a.send(25801, proto.Message{"rid": rb.RID, "srv_id": "symlf_1"}) // again: unfollow
	if r := a.waitFor(25801); r.Int("flag") != 0 {
		t.Fatalf("unfollow: %v", r)
	}
}

func TestSpaceMessageBoard(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	a, ra := e.enter("ua", "甲甲")
	b, rb := e.enter("ub", "乙乙")
	for _, r := range []int64{ra.RID, rb.RID} {
		if err := e.st.SetRoleLevel(r, 40); err != nil {
			t.Fatal(err)
		}
	}
	a.send(25835, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "msg": "你好"})
	if r := a.waitFor(25835); r.Int("result") != 1 {
		t.Fatalf("write: %v", r)
	}
	push := b.waitFor(25841)
	if push.Int("max_num") != 1 || len(push.Arr("room_bbs_info")) != 1 || push.Arr("room_bbs_info")[0].Str("name") != "甲甲" {
		t.Fatalf("push: %v", push)
	}
	a.send(25835, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "msg": "再来"}) // too soon
	if r := a.waitFor(25835); r.Int("result") != 0 {
		t.Fatalf("cool-down: %v", r)
	}
	e.advance(time.Minute)
	a.send(25835, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "msg": "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三四五六七"}) // 37 characters
	if r := a.waitFor(25835); r.Int("result") != 0 {
		t.Fatalf("too long: %v", r)
	}
	// friends only
	b.send(25839, proto.Message{"type": 2})
	if r := b.waitFor(25839); r.Int("result") != 1 || r.Int("type") != 2 {
		t.Fatalf("limit: %v", r)
	}
	a.send(25835, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "msg": "可以吗"})
	if r := a.waitFor(25835); r.Int("result") != 0 {
		t.Fatalf("friends only: %v", r)
	}
	a.send(25837, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "start": 0, "num": 10})
	list := a.waitFor(25837)
	if list.Int("max_num") != 1 || len(list.Arr("room_bbs_info")) != 1 {
		t.Fatalf("list: %v", list)
	}
	id := list.Arr("room_bbs_info")[0].Int("bbs_id")
	// the owner replies
	b.send(25836, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "bbs_id": id, "msg": "收到"})
	if r := b.waitFor(25836); r.Int("result") != 1 {
		t.Fatalf("reply: %v", r)
	}
	a.send(25837, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "start": 0, "num": 10})
	list = a.waitFor(25837)
	rows := list.Arr("room_bbs_info")
	if len(rows) != 2 || rows[0].Int("bbs_type") != 2 || rows[0].Str("reply_name") != "甲甲" || rows[0].Str("name") != "乙乙" {
		t.Fatalf("list after the reply: %v", rows)
	}
	// a stranger cannot delete; the author can
	c, _ := e.enter("uc", "丙丙")
	c.send(25838, proto.Message{"bbs_id": id})
	if r := c.waitFor(25838); r.Int("result") != 0 {
		t.Fatalf("stranger deletes: %v", r)
	}
	a.send(25838, proto.Message{"bbs_id": id})
	if r := a.waitFor(25838); r.Int("result") != 1 {
		t.Fatalf("author deletes: %v", r)
	}
	// low-level roles have no board
	low, rl := e.enter("ud", "新手")
	low.send(25835, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "msg": "hi"})
	if r := low.waitFor(25835); r.Int("result") != 0 {
		t.Fatalf("level 1 writes: %v", r)
	}
	_ = rl
}

func TestSpaceHonorWall(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	a, ra := e.enter("ua", "甲甲")
	a.send(25805, proto.Message{"pos": 1, "id": 5})
	if r := a.waitFor(25805); r.Int("code") != 0 {
		t.Fatalf("not owned: %v", r)
	}
	if err := e.st.GrantBadge(ra.RID, 5, 1); err != nil {
		t.Fatal(err)
	}
	a.send(25805, proto.Message{"pos": 2, "id": 5})
	if r := a.waitFor(25805); r.Int("code") != 1 {
		t.Fatalf("wear: %v", r)
	}
	a.send(25806, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	w := a.waitFor(25806)
	if len(w.Arr("honor_badges")) != 1 || len(w.Arr("use_badges")) != 1 || w.Arr("use_badges")[0].Int("pos") != 2 {
		t.Fatalf("wall: %v", w)
	}
	a.send(25805, proto.Message{"pos": 2, "id": 0})
	if r := a.waitFor(25805); r.Int("code") != 1 {
		t.Fatalf("take off: %v", r)
	}
}
