package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func rid(r *store.Role) proto.Message { return proto.Message{"rid": r.RID, "srv_id": "symlf_1"} }

func ids(list []proto.Message) []int64 {
	out := []int64{}
	for _, e := range list {
		out = append(out, e.Int("rid"))
	}
	return out
}

func same(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// befriend makes a and b friends through the game's own commands and drains the
// pushes that follow.
func befriend(t *testing.T, ca *client, a *store.Role, cb *client, b *store.Role) {
	t.Helper()
	ca.send(13303, rid(b))
	if r := ca.recv(13303); r.Int("code") != 1 {
		t.Fatalf("add: %v", r)
	}
	cb.recv(13304)
	acc := rid(a)
	acc["agreed"] = 1
	cb.send(13305, acc)
	cb.recv(13310)
	if r := cb.recv(13305); r.Int("code") != 1 {
		t.Fatalf("accept: %v", r)
	}
	ca.recv(13310)
}

func TestFriendRequestAndAccept(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")

	ca.send(13303, rid(b))
	if r := ca.recv(13303); r.Int("code") != 1 || r.Str("msg") == "" {
		t.Fatalf("add: %v", r)
	}
	// the other side is told, then asks for the list
	if p := cb.recv(13304); p.Int("rid") != a.RID || p.Str("name") != "甲" || p.Str("srv_id") != "symlf_1" {
		t.Fatalf("push: %v", p)
	}
	cb.send(13311, proto.Message{})
	if l := cb.recv(13311).Arr("friend_req_list"); !same(ids(l), a.RID) || l[0].Str("name") != "甲" {
		t.Fatalf("requests of b: %v", l)
	}
	ca.send(13311, proto.Message{})
	if l := ca.recv(13311).Arr("friend_req_list"); len(l) != 0 {
		t.Fatalf("a asked, so a has no request: %v", l)
	}

	// accepting makes both sides friends and tells both
	acc := rid(a)
	acc["agreed"] = 1
	cb.send(13305, acc)
	if p := cb.recv(13310); p.Int("rid") != a.RID || p.Int("is_online") != 1 || p.Str("name") != "甲" {
		t.Fatalf("b's new friend: %v", p)
	}
	if r := cb.recv(13305); r.Int("code") != 1 || r.Int("rid") != a.RID {
		t.Fatalf("accept: %v", r)
	}
	if p := ca.recv(13310); p.Int("rid") != b.RID || p.Int("is_online") != 1 {
		t.Fatalf("a's new friend: %v", p)
	}
	for _, c := range []struct {
		c    *client
		want int64
	}{{ca, b.RID}, {cb, a.RID}} {
		c.c.send(13300, proto.Message{})
		l := c.c.recv(13300).Arr("friend_list")
		if !same(ids(l), c.want) || l[0].Int("is_online") != 1 || l[0].Int("lev") != 1 {
			t.Fatalf("friend list: %v", l)
		}
	}
	cb.send(13311, proto.Message{})
	if l := cb.recv(13311).Arr("friend_req_list"); len(l) != 0 {
		t.Fatalf("the request must be gone: %v", l)
	}
}

func TestFriendRequestRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	_, banned := e.enter("c", "丙")
	e.st.SetRoleStatus(banned.RID, 0)

	bad := func(c *client, target proto.Message, msg string) {
		t.Helper()
		c.send(13303, target)
		if r := c.recv(13303); r.Int("code") != 0 || r.Str("msg") != msg {
			t.Fatalf("want %q, got %v", msg, r)
		}
	}
	bad(ca, rid(a), "不能添加自己为好友")
	bad(ca, proto.Message{"rid": 999999, "srv_id": "symlf_1"}, "玩家不存在")
	bad(ca, rid(banned), "玩家不存在")
	bad(ca, proto.Message{"rid": b.RID, "srv_id": "other_9"}, "暂不支持跨服好友")

	ca.send(13303, rid(b))
	ca.recv(13303)
	cb.recv(13304)
	bad(ca, rid(b), "已发送过申请，请等待对方处理")
	bad(cb, rid(a), "对方已向你发出申请，请在申请列表中同意")

	// declining one request, then asking again works
	no := rid(a)
	no["agreed"] = 0
	cb.send(13305, no)
	if r := cb.recv(13305); r.Int("code") != 0 || r.Str("msg") != "已拒绝申请" {
		t.Fatalf("decline: %v", r)
	}
	cb.send(13311, proto.Message{})
	if l := cb.recv(13311).Arr("friend_req_list"); len(l) != 0 {
		t.Fatalf("declined request still listed: %v", l)
	}
	ca.send(13303, rid(b))
	if r := ca.recv(13303); r.Int("code") != 1 {
		t.Fatalf("ask again: %v", r)
	}
	cb.recv(13304)

	// accepting a request that does not exist
	ghost := proto.Message{"rid": 999999, "srv_id": "symlf_1", "agreed": 1}
	cb.send(13305, ghost)
	if r := cb.recv(13305); r.Int("code") != 0 || r.Str("msg") != "该申请已失效" {
		t.Fatalf("ghost accept: %v", r)
	}
	// already friends
	acc := rid(a)
	acc["agreed"] = 1
	cb.send(13305, acc)
	cb.recv(13310)
	cb.recv(13305)
	ca.recv(13310)
	bad(ca, rid(b), "对方已经是你的好友")
	bad(cb, rid(a), "对方已经是你的好友")
}

func TestFriendDelete(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	befriend(t, ca, a, cb, b)

	ca.send(13307, rid(b))
	if r := ca.recv(13307); r.Int("code") != 1 || r.Int("rid") != b.RID || r.Str("srv_id") != "symlf_1" {
		t.Fatalf("delete: %v", r)
	}
	if p := cb.recv(13308); p.Int("rid") != a.RID {
		t.Fatalf("b must be told: %v", p)
	}
	for _, c := range []*client{ca, cb} {
		c.send(13300, proto.Message{})
		if l := c.recv(13300).Arr("friend_list"); len(l) != 0 {
			t.Fatalf("friend list after delete: %v", l)
		}
	}
	// not friends any more: deleting again fails, and so does deleting a stranger
	ca.send(13307, rid(b))
	if r := ca.recv(13307); r.Int("code") != 0 || r.Str("msg") != "对方不是你的好友" {
		t.Fatalf("second delete: %v", r)
	}
	// they can become friends again
	befriend(t, ca, a, cb, b)
}

func TestFriendOnlineAndOfflineNotices(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	befriend(t, ca, a, cb, b)

	// a drops: b is told, and the logout time is stored
	ca.c.Close()
	off := cb.recv(13301)
	if off.Int("rid") != a.RID || off.Int("is_online") != 0 || off.Int("login_out_time") == 0 {
		t.Fatalf("offline notice: %v", off)
	}
	cb.send(13300, proto.Message{})
	if l := cb.recv(13300).Arr("friend_list"); l[0].Int("is_online") != 0 || l[0].Int("login_out_time") == 0 {
		t.Fatalf("offline friend in the list: %v", l)
	}

	// a comes back
	ca2, _ := e.dialSameRole("a", a)
	on := cb.recv(13301)
	if on.Int("rid") != a.RID || on.Int("is_online") != 1 || on.Int("login_time") == 0 {
		t.Fatalf("online notice: %v", on)
	}

	// logging in again while online replaces the old session: no offline notice for that
	ca3, _ := e.dialSameRole("a", a)
	if on := cb.recv(13301); on.Int("is_online") != 1 {
		t.Fatalf("second login: %v", on)
	}
	ca2.recv(10310) // the old session is told it was replaced, then dropped
	ca2.expectClosed()
	time.Sleep(150 * time.Millisecond) // time for a stale offline notice to show up, if there were one
	cb.expectNothing()
	_ = ca3
}

func TestFriendSearchAndRecommend(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "路飞")
	cb, b := e.enter("b", "路人甲")
	_, c := e.enter("c", "索隆")
	_, d := e.enter("d", "香吉士")
	e.st.SetRoleStatus(d.RID, 0) // banned roles are never offered
	_ = cb

	ca.send(13314, proto.Message{"name": "路"})
	if l := ca.recv(13314).Arr("role_list"); !same(ids(l), a.RID, b.RID) { // the client drops itself
		t.Fatalf("search 路: %v", ids(l))
	}
	ca.send(13314, proto.Message{"name": "香"})
	if l := ca.recv(13314).Arr("role_list"); len(l) != 0 {
		t.Fatalf("a banned role was found: %v", l)
	}
	ca.send(13314, proto.Message{"name": ""})
	if l := ca.recv(13314).Arr("role_list"); len(l) != 0 {
		t.Fatalf("empty search: %v", l)
	}
	ca.send(13314, proto.Message{"name": "%"}) // a wildcard is a plain character
	if l := ca.recv(13314).Arr("role_list"); len(l) != 0 {
		t.Fatalf("wildcard search: %v", l)
	}

	ca.send(13320, proto.Message{})
	if l := ca.recv(13320).Arr("recommend_list"); len(l) != 2 {
		t.Fatalf("recommend: %v", ids(l))
	}
	// an asked role and a friend drop out of the recommendations
	ca.send(13303, rid(b))
	ca.recv(13303)
	ca.send(13320, proto.Message{})
	if l := ca.recv(13320).Arr("recommend_list"); !same(ids(l), c.RID) {
		t.Fatalf("recommend after asking b: %v", ids(l))
	}
}

func TestFriendBatchAndRefuseAll(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	cc, c := e.enter("c", "丙")

	// a asks b and c at once (and a bad id: it is skipped)
	ca.send(13306, proto.Message{"role_ids": []proto.Message{rid(b), rid(c), {"rid": 999999, "srv_id": "symlf_1"}}})
	if r := ca.recv(13306); r.Int("code") != 1 {
		t.Fatalf("batch add: %v", r)
	}
	cb.recv(13304)
	cc.recv(13304)
	ca.send(13306, proto.Message{"role_ids": []proto.Message{rid(b)}}) // already asked: nothing to send
	if r := ca.recv(13306); r.Int("code") != 0 {
		t.Fatalf("batch add of a pending request: %v", r)
	}

	// c refuses all; b accepts all
	cc.send(13312, proto.Message{})
	if r := cc.recv(13312); r.Int("code") != 1 {
		t.Fatalf("refuse all: %v", r)
	}
	cc.send(13311, proto.Message{})
	if l := cc.recv(13311).Arr("friend_req_list"); len(l) != 0 {
		t.Fatalf("refused: %v", l)
	}
	cb.send(13334, proto.Message{"role_ids": []proto.Message{rid(a), {"rid": 999999, "srv_id": "symlf_1"}}})
	cb.recv(13310)
	if r := cb.recv(13334).Arr("role_ids"); !same(ids(r), a.RID) {
		t.Fatalf("accept all: %v", ids(r))
	}
	ca.recv(13310)
	ca.send(13300, proto.Message{})
	if l := ca.recv(13300).Arr("friend_list"); !same(ids(l), b.RID) {
		t.Fatalf("a's friends: %v", ids(l))
	}
}

func TestFriendCommandsBeforeRoleLoginAreIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	for _, cmd := range []uint16{13300, 13311, 13320, 13312} {
		c.send(cmd, proto.Message{})
	}
	c.send(13303, proto.Message{"rid": 1, "srv_id": "symlf_1"})
	c.send(13314, proto.Message{"name": "x"})
	c.expectNothing()
}

func TestFriendStoreLimit(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, a := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	_, c := e.enter("c", "丙")
	for _, x := range []*store.Role{b, c} {
		if err := e.st.RequestFriend(x.RID, a.RID); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.AcceptFriendRequest(b.RID, a.RID, 1); err != nil {
		t.Fatal(err)
	}
	// a is at the limit now: c's request is refused and stays pending
	if err := e.st.AcceptFriendRequest(c.RID, a.RID, 1); err != store.ErrFriendsFull {
		t.Fatalf("err = %v", err)
	}
	if n, _ := e.st.CountFriends(a.RID); n != 1 {
		t.Fatalf("friends of a: %d", n)
	}
	if has, _ := e.st.HasFriendRequest(c.RID, a.RID); !has {
		t.Fatal("a failed accept must keep the request")
	}
}

func TestLogoutTimeIsStoredWhenTheGatewayShutsDown(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("a", "甲")
	if got, _ := e.st.RoleByID(role.RID); got.LastLogoutAt != 0 {
		t.Fatalf("online role has a logout time: %+v", got)
	}
	e.gw.Close() // waits for every session to end, which runs the disconnect hooks
	if got, _ := e.st.RoleByID(role.RID); got.LastLogoutAt == 0 || got.LastLogoutAt < got.LastLoginAt {
		t.Fatalf("logout time after a shutdown: %+v", got)
	}
}

func TestStaleSessionsAreMarkedOffline(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, a := e.enter("a", "甲") // logged in, never logged out: the gateway "died" with it online
	_, b := e.enter("b", "乙")
	if err := e.st.TouchRoleLogout(b.RID); err != nil { // b logged out properly
		t.Fatal(err)
	}
	before, _ := e.st.RoleByID(b.RID)

	n, err := e.st.MarkStaleSessionsOffline()
	if err != nil || n != 1 {
		t.Fatalf("fixed %d roles (%v), want only the one that never logged out", n, err)
	}
	if got, _ := e.st.RoleByID(a.RID); got.LastLogoutAt == 0 || got.LastLogoutAt < got.LastLoginAt {
		t.Fatalf("the stale role: %+v", got)
	}
	if got, _ := e.st.RoleByID(b.RID); got.LastLogoutAt != before.LastLogoutAt {
		t.Fatalf("a proper logout was changed: %+v -> %+v", before, got)
	}
	if n, _ := e.st.MarkStaleSessionsOffline(); n != 0 { // nothing left to fix
		t.Fatalf("second run fixed %d", n)
	}
}
