package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func escortEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ue", "护送者")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	return e, c, role
}

func (e *env) escState(t *testing.T, role *store.Role) *escState {
	st := &escState{}
	if _, err := e.st.LoadState(role.RID, escModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEscortClosedBelowTheLevel(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("ul", "新手")
	c.send(24002, proto.Message{})
	if r := c.waitFor(24002); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("24002: %v", r)
	}
}

func TestEscortHireFinishCollect(t *testing.T) {
	e, c, role := escortEnv(t)
	c.send(24006, proto.Message{})
	my := c.waitFor(24006)
	if my.Int("status") != 0 || len(my.Arr("datas")) != 6 {
		t.Fatalf("24006: %v", my)
	}
	c.send(24004, proto.Message{})
	if r := c.waitFor(24004); r.Int("result") != 0 {
		t.Fatalf("collect before hiring: %v", r)
	}
	// raise the quality for diamonds: all the way to orange
	e.grantAssets(c, role, map[int64]int64{3: 500})
	c.send(24001, proto.Message{"type": 2})
	if r := c.waitFor(24001); r.Int("result") != 1 {
		t.Fatalf("24001: %v", r)
	}
	c.send(24002, proto.Message{})
	if r := c.waitFor(24002); r.Int("result") != 1 {
		t.Fatalf("24002: %v", r)
	}
	my = c.waitFor(24006)
	if my.Int("status") != 1 || my.Int("quality") != 4 || my.Int("end_time") <= time.Now().Unix() {
		t.Fatalf("hired: %v", my)
	}
	c.send(24002, proto.Message{})
	if r := c.waitFor(24002); r.Int("result") != 0 {
		t.Fatalf("hired twice: %v", r)
	}
	c.send(24004, proto.Message{})
	if r := c.waitFor(24004); r.Int("result") != 0 {
		t.Fatalf("collect while out: %v", r)
	}
	c.send(24003, proto.Message{})
	if r := c.waitFor(24003); r.Int("result") != 1 {
		t.Fatalf("24003: %v", r)
	}
	c.waitFor(24006)
	c.send(24004, proto.Message{})
	if r := c.waitFor(24004); r.Int("result") != 1 {
		t.Fatalf("24004: %v", r)
	}
	my = c.waitFor(24006)
	if my.Int("status") != 0 {
		t.Fatalf("after collecting: %v", my)
	}
	if st := e.escState(t, role); st.Cnt[cntEscort] != 1 {
		t.Fatalf("hired once: %+v", st.Cnt)
	}
}

func TestEscortPlunderAndLogs(t *testing.T) {
	e, a, ra := escortEnv(t)
	b, rb := e.enter("ub", "被掠者")
	if err := e.st.SetRoleLevel(rb.RID, 40); err != nil {
		t.Fatal(err)
	}
	b.send(11000, proto.Message{})
	b.waitFor(11000)
	b.send(24002, proto.Message{})
	b.waitFor(24002)
	b.waitFor(24006)

	a.send(24000, proto.Message{})
	a.waitFor(24006)
	list := a.waitFor(24000).Arr("plunders")
	if len(list) != 1 || list[0].Int("rid") != rb.RID {
		t.Fatalf("24000: %v", list)
	}
	a.send(24010, proto.Message{"rid": rb.RID, "srv_id": rb.SrvID})
	if d := a.waitFor(24010); d.Int("rid") != rb.RID || len(d.Arr("p_list")) == 0 {
		t.Fatalf("24010: %v", d)
	}
	a.send(24011, proto.Message{"rid": ra.RID, "srv_id": ra.SrvID})
	if r := a.waitFor(24011); r.Int("result") != 0 {
		t.Fatalf("plundering oneself: %v", r)
	}
	a.send(24011, proto.Message{"rid": rb.RID, "srv_id": rb.SrvID})
	if r := a.waitFor(24011); r.Int("result") != 1 {
		t.Fatalf("24011: %v", r)
	}
	a.send(20062, proto.Message{})
	a.waitFor(20062)
	res := a.waitFor(24012)
	won := res.Int("result") == 1
	if won != (len(res.Arr("item_rewards")) > 0) {
		t.Fatalf("24012: %v", res)
	}
	a.waitFor(24006)
	a.send(24013, proto.Message{"type": 2})
	if l := a.waitFor(24013).Arr("logs"); len(l) != 1 || l[0].Int("rid") != rb.RID || l[0].Int("ret") != res.Int("result") {
		t.Fatalf("atk log: %v", l)
	}
	if st := e.escState(t, rb); won != (st.Lost == 1) || won != (len(st.Logs) == 1) {
		t.Fatalf("victim: won=%v %+v", won, st)
	}
	if !won {
		return
	}
	// a second plunder of the same beast is refused
	a.send(24011, proto.Message{"rid": rb.RID, "srv_id": rb.SrvID})
	if r := a.waitFor(24011); r.Int("result") != 0 {
		t.Fatalf("plundered twice: %v", r)
	}
	// the victim sees the plunder and asks its guild: it has none
	b.send(24013, proto.Message{"type": 1})
	logs := b.waitFor(24013).Arr("logs")
	if len(logs) != 1 || logs[0].Int("rid") != ra.RID || logs[0].Int("ret") != 1 {
		t.Fatalf("def log: %v", logs)
	}
	id := logs[0].Int("id")
	b.send(24017, proto.Message{"id": id})
	if r := b.waitFor(24017); r.Int("code") != 0 {
		t.Fatalf("help without a guild: %v", r)
	}
	// and takes revenge
	b.send(24014, proto.Message{"rid": rb.RID, "srv_id": rb.SrvID, "id": id, "type": 1})
	if r := b.waitFor(24014); r.Int("rid") != ra.RID || r.Int("id") != id {
		t.Fatalf("24014: %v", r)
	}
	b.send(24015, proto.Message{"rid": ra.RID, "srv_id": ra.SrvID, "id": id})
	if r := b.waitFor(24015); r.Int("result") != 1 {
		t.Fatalf("24015: %v", r)
	}
	b.send(20062, proto.Message{})
	b.waitFor(20062)
	rev := b.waitFor(24012)
	if rev.Int("result") == 1 && len(rev.Arr("item_rewards")) == 0 {
		t.Fatalf("revenge without a reward: %v", rev)
	}
	if upd := b.waitFor(24018); upd.Int("id") != id || (rev.Int("result") == 1) != (upd.Int("atk_ret") == 1) {
		t.Fatalf("24018: %v", upd)
	}
}
