package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func questList(c *client) map[int64]proto.Message {
	c.send(10400, proto.Message{})
	out := map[int64]proto.Message{}
	for _, q := range c.recv(10400).Arr("quest_list") {
		out[q.Int("id")] = q
	}
	return out
}

func TestDailyQuests(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	c.send(10300, proto.Message{})
	intoCity(c)
	qs := questList(c)
	if len(qs) != 12 {
		t.Fatalf("quests: %d", len(qs))
	}
	// the login counted: 90000 is finished, the others are open
	if qs[90000].Int("finish") != 1 || qs[90003].Int("finish") != 0 {
		t.Fatalf("after login: %v %v", qs[90000], qs[90003])
	}
	p := qs[90003].Arr("progress")[0]
	if p.Int("target_val") != 3 || p.Int("value") != 0 {
		t.Fatalf("summon quest: %v", p)
	}

	// take the login reward: 20 gems and 5 activity points
	c.send(10402, proto.Message{"id": 90000})
	if r := c.recv(10402); r.Int("flag") != 1 || r.Int("id") != 90000 {
		t.Fatalf("claim: %v", r)
	}
	if q := c.recv(10403).Arr("quest_list"); len(q) != 1 || q[0].Int("finish") != 2 {
		t.Fatalf("10403: %v", q)
	}
	c.recv(10302)
	c.recv(10500)
	if as, _ := e.st.RoleAssets(role.RID); as[3] != 20 || as[9] != 5 {
		t.Fatalf("reward: %v", as)
	}
	// no second time, no unfinished one, no unknown one
	for _, id := range []int64{90000, 90003, 12345} {
		c.send(10402, proto.Message{"id": id})
		if r := c.recv(10402); r.Int("flag") != 0 {
			t.Fatalf("claim %d: %v", id, r)
		}
	}

	// summons count: two draws, then ten more finish the quest (the value stops at 3)
	old := summonRand
	summonRand = func() float64 { return 0 }
	t.Cleanup(func() { summonRand = old })
	e.give(c, role, map[int64]int64{10401: 3})
	for _, times := range []int64{1, 1, 1} {
		c.send(23201, proto.Message{"group_id": 100, "times": times, "recruit_type": 4})
		c.recv(23201)
		c.recv(11001)
		if q := c.recv(10403).Arr("quest_list"); len(q) != 1 || q[0].Int("id") != 90003 {
			t.Fatalf("progress push: %v", q)
		}
		c.recv(10302)
		c.recv(10500)
	}
	qs = questList(c)
	if qs[90003].Int("finish") != 1 || qs[90003].Arr("progress")[0].Int("value") != 3 {
		t.Fatalf("summon quest after 3 draws: %v", qs[90003])
	}

	// the next day starts again from nothing, with the login counted anew
	e.setNow(time.Date(2026, 10, 20, 10, 0, 0, 0, dayZone))
	qs = questList(c)
	if qs[90003].Int("finish") != 0 || qs[90003].Arr("progress")[0].Int("value") != 0 || qs[90000].Int("finish") != 0 {
		t.Fatalf("next day: %v %v", qs[90003], qs[90000])
	}
}

func TestFriendGiftCountsForItsQuest(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	befriend(t, ca, a, cb, b)
	giftReq(ca, 0, b)
	ca.recv(10302)
	q := ca.recv(10403).Arr("quest_list")
	if len(q) != 1 || q[0].Int("id") != 90001 || q[0].Arr("progress")[0].Int("value") != 1 {
		t.Fatalf("10403 after a gift: %v", q)
	}
}

// The client's task window submits a finished quest with 10406 (TaskController.requestSubmitTask),
// not 10402.
func TestDailyQuestClaimWith10406(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	c.send(10300, proto.Message{})
	intoCity(c)
	questList(c)
	c.send(10406, proto.Message{"id": 90000})
	if r := c.recv(10406); r.Int("flag") != 1 || r.Int("id") != 90000 {
		t.Fatalf("claim: %v", r)
	}
	if q := c.recv(10403).Arr("quest_list"); len(q) != 1 || q[0].Int("finish") != 2 {
		t.Fatalf("10403: %v", q)
	}
	c.recv(10302)
	c.recv(10500)
	if as, _ := e.st.RoleAssets(role.RID); as[3] != 20 || as[9] != 5 {
		t.Fatalf("reward: %v", as)
	}
	c.send(10406, proto.Message{"id": 90000})
	if r := c.recv(10406); r.Int("flag") != 0 {
		t.Fatalf("second claim: %v", r)
	}
}
