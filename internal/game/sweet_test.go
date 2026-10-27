package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestSweet(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: a.SrvID, CampID: sweetCamp, BID: 91049, Status: 1, StartAt: start, EndAt: start + 7*86400, Spec: actSpecJSON(t, false)})
	flag, star := sweetItems[2].Item, sweetItems[0].Item // a red flag (10) and a star (1)
	e.give(ca, a, map[int64]int64{flag: 3, star: 5})
	e.give(cb, b, map[int64]int64{star: 100})

	ca.send(28500, proto.Message{})
	r := ca.waitFor(28500)
	if r.Int("end_time") != start+7*86400 || r.Int("acc_score") != 0 || r.Int("max_acc_score") != sweetCfg.Const["base_max_score"] || r.Int("lev") != 1 {
		t.Fatalf("28500: %v", r)
	}
	put := func(c *client, id, num int64) proto.Message {
		c.send(28502, proto.Message{"id": id, "num": num})
		return c.waitFor(28502)
	}
	if r := put(ca, 12345, 1); r.Int("code") != 0 {
		t.Fatalf("an item that is not one of the three: %v", r)
	}
	if r := put(ca, flag, 4); r.Int("code") != 0 {
		t.Fatalf("more than the role has: %v", r)
	}
	if r := put(ca, flag, 3); r.Int("code") != 1 {
		t.Fatalf("put: %v", r)
	}
	ca.waitFor(10500)
	if r := put(ca, star, 5); r.Int("code") != 1 {
		t.Fatalf("put stars: %v", r)
	}
	ca.waitFor(10500)
	if r := put(cb, star, 100); r.Int("code") != 1 {
		t.Fatalf("b puts: %v", r)
	}
	cb.waitFor(10500)
	ca.send(28500, proto.Message{})
	r = ca.waitFor(28500)
	if r.Int("my_score") != 35 || r.Int("acc_score") != 135 {
		t.Fatalf("scores: %v", r)
	}
	// the rank list of the activity: b first
	ca.send(12900, proto.Message{"type": rankSweet, "start": 1, "num": 10})
	rk := ca.waitFor(12900)
	rows := rk.Arr("rank_list")
	if len(rows) != 2 || rows[0].Int("rid") != b.RID || rows[0].Int("val1") != 100 || rk.Int("my_idx") != 2 || rk.Int("my_val1") != 35 {
		t.Fatalf("rank: %v", rk)
	}
	// nothing is open before the server's total reaches the first reward
	ca.send(28501, proto.Message{"reward_list": []proto.Message{{"id": 1}}})
	if r := ca.waitFor(28501); r.Int("flag") != 0 {
		t.Fatalf("take too early: %v", r)
	}
	// 160 per mille of 100000 is 16000 points
	e.give(cb, b, map[int64]int64{flag: 1600})
	if r := put(cb, flag, 1600); r.Int("code") != 1 {
		t.Fatalf("b puts flags: %v", r)
	}
	cb.waitFor(10500)
	ca.send(28501, proto.Message{"reward_list": []proto.Message{{"id": 1}, {"id": 2}}})
	if r := ca.waitFor(28501); r.Int("flag") != 1 {
		t.Fatalf("take: %v", r)
	}
	ca.send(28501, proto.Message{"reward_list": []proto.Message{{"id": 1}}})
	if r := ca.waitFor(28501); r.Int("flag") != 0 {
		t.Fatalf("take twice: %v", r)
	}
	ca.send(28500, proto.Message{})
	if r := ca.waitFor(28500); len(r.Arr("reward")) != 1 || r.Int("lev") != 2 {
		t.Fatalf("after taking: %v", r)
	}
	// the activity ends: the ranks are mailed once
	e.setNow(actNow.Add(8 * 24 * time.Hour))
	ca.send(28500, proto.Message{})
	ca.waitFor(28500)
	role, _ := e.st.RoleByID(b.RID)
	mails, err := e.st.MailsForRole(role)
	if err != nil || len(mails) == 0 || mails[0].Subject != "甜蜜大作战排名奖励" {
		t.Fatalf("rank mail: %v %v", mails, err)
	}
	ca.send(28500, proto.Message{})
	ca.waitFor(28500)
	if again, _ := e.st.MailsForRole(role); len(again) != len(mails) {
		t.Fatalf("mailed twice: %d -> %d", len(mails), len(again))
	}
}
