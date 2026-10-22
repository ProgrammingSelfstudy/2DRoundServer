package game

import (
	"fmt"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func myCode(c *client) int64 {
	c.send(19800, proto.Message{})
	return c.recv(19800).Int("code")
}

func bindCode(c *client, code int64) proto.Message {
	c.send(19801, proto.Message{"code": code})
	return c.recv(19801)
}

func inviteProgress(c *client) map[int64][2]int64 {
	c.send(19804, proto.Message{})
	out := map[int64][2]int64{}
	for _, e := range c.recv(19804).Arr("list") {
		out[e.Int("id")] = [2]int64{e.Int("num"), e.Int("had")}
	}
	return out
}

func TestInviteCodeIsStableAndUnique(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	cb, _ := e.enter("b", "乙")
	a1, a2, b := myCode(ca), myCode(ca), myCode(cb)
	if a1 != a2 || a1 < 100000 || a1 > 999999 || a1 == b {
		t.Fatalf("codes: %d %d %d", a1, a2, b)
	}
}

func TestInviteBindAndList(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	code := myCode(ca)

	if r := bindCode(cb, code); r.Int("code") != 1 || r.Str("msg") == "" {
		t.Fatalf("bind: %v", r)
	}
	// the inviter is told, and sees b in the list
	push := ca.recv(19803)
	if push.Int("rid") != b.RID || push.Str("name") != "乙" || push.Int("online") != 1 {
		t.Fatalf("push: %v", push)
	}
	ca.send(19802, proto.Message{})
	if l := ca.recv(19802).Arr("list"); !same(ids(l), b.RID) || l[0].Str("srv_id") != "symlf_1" {
		t.Fatalf("list: %v", l)
	}
	// b entered a code once; nobody is invited by b
	if r := bindCode(cb, code); r.Int("code") != 0 || r.Str("msg") != "你已经填写过邀请码了" {
		t.Fatalf("second bind: %v", r)
	}
	cb.send(19802, proto.Message{})
	if l := cb.recv(19802).Arr("list"); len(l) != 0 {
		t.Fatalf("b's list: %v", l)
	}
	_ = a
}

func TestInviteBindRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	code := myCode(ca)
	cc, c := e.enter("c", "丙")

	bad := func(cl *client, code int64, want string) {
		t.Helper()
		if r := bindCode(cl, code); r.Int("code") != 0 || r.Str("msg") != want {
			t.Fatalf("want %q, got %v", want, r)
		}
	}
	bad(cc, 123456, "邀请码不存在")
	bad(cc, myCode(cc), "不能填写自己账号的邀请码")

	// another role of the same account: its code is refused too
	acc, tok := e.account("d")
	d1, _ := e.st.CreateRole(acc.ID, "symlf_1", "丁1", 0, 1, 0)
	d2, _ := e.st.CreateRole(acc.ID, "symlf_1", "丁2", 0, 1, 0)
	cd := e.dial(true)
	cd.send(1164, loginArgs("d", tok))
	cd.recv(1164)
	cd.send(10192, proto.Message{"rid": d2.RID, "srv_id": "symlf_1"})
	cd.recv(10192)
	bad(cd, code, "只有账号的首个角色才可以填写邀请码") // d2 is the account's second role
	_ = d1

	// a banned inviter's code no longer works
	e.st.SetRoleStatus(a.RID, 0)
	bad(cc, code, "邀请码不存在")
	e.st.SetRoleStatus(a.RID, 1)
	_ = c
}

func TestInviteFull(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	code := myCode(ca)
	for i := 0; i < maxInvitees; i++ {
		acc, _ := e.st.FindOrCreateAccount("symlf", fmt.Sprintf("x%d", i))
		r, err := e.st.CreateRole(acc.ID, "symlf_1", fmt.Sprintf("受邀%d", i), 0, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.BindInvite(r.RID, a.RID, maxInvitees); err != nil {
			t.Fatalf("invitee %d: %v", i, err)
		}
	}
	cl, _ := e.enter("late", "迟到")
	if r := bindCode(cl, code); r.Int("code") != 0 || r.Str("msg") != "该邀请码的绑定数量已满" {
		t.Fatalf("a full inviter: %v", r)
	}
}

func TestInviteRewardsFollowTheInvitedRolesPowerAndVip(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	bindCode(cb, myCode(ca))
	ca.recv(19803)

	// with no power or VIP system everything is 0: nothing can be claimed
	for id, v := range inviteProgress(ca) {
		if v != [2]int64{0, 0} {
			t.Fatalf("step %d: %v", id, v)
		}
	}
	ca.send(19805, proto.Message{"id": 1})
	if r := ca.recv(19805); r.Int("code") != 0 || r.Str("msg") != "没有可领取的奖励" || r.Int("id") != 1 {
		t.Fatalf("claim with nothing reached: %v", r)
	}

	// b reaches power 120000 and VIP 3: steps 1 and 2 (power) and 8, 9, 10 (VIP 1, 2, 3)
	e.invite.Metrics = func(rid int64) (int64, int64) {
		if rid == b.RID {
			return 120000, 3
		}
		return 0, 0
	}
	p := inviteProgress(ca)
	for _, id := range []int64{1, 2, 8, 9, 10} {
		if p[id] != [2]int64{1, 0} {
			t.Fatalf("step %d should be reached once: %v", id, p[id])
		}
	}
	for _, id := range []int64{3, 7, 11, 14} {
		if p[id] != [2]int64{0, 0} {
			t.Fatalf("step %d should not be reached: %v", id, p[id])
		}
	}
	// step 1 pays 50 x item 10001 and 5 x item 10002
	ca.send(19805, proto.Message{"id": 1})
	r := ca.recv(19805)
	if r.Int("code") != 1 || r.Int("num") != 1 || r.Int("had") != 1 || r.Int("id") != 1 {
		t.Fatalf("claim: %v", r)
	}
	ca.recv(10302)
	ca.recv(10500)
	if got := giftCount(ca, 10001); got != 50 {
		t.Fatalf("item 10001: %d", got)
	}
	// once per invited role that reached it
	ca.send(19805, proto.Message{"id": 1})
	if r := ca.recv(19805); r.Int("code") != 0 || r.Int("num") != 1 || r.Int("had") != 1 {
		t.Fatalf("second claim: %v", r)
	}
	if got := inviteProgress(ca)[1]; got != [2]int64{1, 1} {
		t.Fatalf("progress after the claim: %v", got)
	}
	// VIP step 8 pays 100 of currency 3 (gold) and one item 10403
	ca.send(19805, proto.Message{"id": 8})
	if r := ca.recv(19805); r.Int("code") != 1 {
		t.Fatalf("claim 8: %v", r)
	}
	if as := ca.recv(10302); as.Str("gold") != "100" {
		t.Fatalf("assets: %v", as)
	}
	ca.recv(10500)
	ca.send(19805, proto.Message{"id": 99})
	if r := ca.recv(19805); r.Int("code") != 0 || r.Str("msg") != "奖励不存在" {
		t.Fatalf("no such step: %v", r)
	}
}

func TestInviteBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	for _, cmd := range []uint16{19800, 19802, 19804} {
		c.send(cmd, proto.Message{})
	}
	c.send(19801, proto.Message{"code": 123456})
	c.send(19805, proto.Message{"id": 1})
	c.expectNothing()
}

func TestInviteStore(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, a := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	if err := e.st.BindInvite(b.RID, a.RID, 5); err != nil {
		t.Fatal(err)
	}
	if err := e.st.BindInvite(b.RID, a.RID, 5); err != store.ErrConflict {
		t.Fatalf("a second bind: %v", err)
	}
	// a claim is allowed as often as the caller says, not more
	pay := map[int64]int64{3: 10}
	if err := e.st.ClaimInvite(a.RID, 1, 2, pay, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ClaimInvite(a.RID, 1, 2, pay, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ClaimInvite(a.RID, 1, 2, pay, nil); err != store.ErrConflict {
		t.Fatalf("over the limit: %v", err)
	}
	if got, _ := e.st.RoleAssets(a.RID); got[3] != 20 {
		t.Fatalf("paid: %v", got)
	}
}

func TestInviteTableIsComplete(t *testing.T) {
	if len(inviteTasks) != 14 {
		t.Fatalf("steps: %d", len(inviteTasks))
	}
	for _, s := range inviteTasks {
		if s.Max != 20 || len(s.Items) == 0 {
			t.Errorf("step %d: %+v", s.ID, s)
		}
		for _, it := range s.Items {
			if _, known := itemTable[it[0]]; !known {
				t.Errorf("step %d pays unknown id %d", s.ID, it[0])
			}
		}
	}
}
