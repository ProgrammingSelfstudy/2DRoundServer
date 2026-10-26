package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestFortuneBagRound(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("fb", "福袋人")
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 17473, BID: fbBID, Status: 1, StartAt: start, EndAt: start + 7*86400, Spec: actSpecJSON(t, false)})
	e.give(c, role, map[int64]int64{37013: 3500})

	c.send(28300, proto.Message{})
	if r := c.waitFor(28300); r.Int("round") != 1 || r.Int("gold_time") != 5 || r.Int("original_gold_time") != 5 || r.Int("optional_id") != 0 {
		t.Fatalf("28300: %v", r)
	}
	// a card needs the ultimate prize to be chosen first
	c.send(28302, proto.Message{"pos": 1})
	if r := c.waitFor(28302); r.Int("code") != 0 {
		t.Fatalf("flip before choosing: %v", r)
	}
	c.send(28305, proto.Message{"type_id": 99999})
	if r := c.waitFor(28305); r.Int("code") != 0 {
		t.Fatalf("a prize that does not exist: %v", r)
	}
	c.send(28305, proto.Message{"type_id": 10001})
	if r := c.waitFor(28305); r.Int("code") != 1 {
		t.Fatalf("choose: %v", r)
	}
	if r := c.waitFor(28300); r.Int("optional_id") != 10001 {
		t.Fatalf("after choosing: %v", r)
	}
	// turn the cards until the ultimate prize: it comes at the 35th card at the latest, never before the 21st
	var last proto.Message
	for pos := int64(1); pos <= fbCells; pos++ {
		c.send(28302, proto.Message{"pos": pos})
		if r := c.waitFor(28302); r.Int("code") != 1 {
			t.Fatalf("flip %d: %v", pos, r)
		}
		last = c.waitFor(28300)
		if last.Int("next_round") == 1 {
			if pos <= fbFirst {
				t.Fatalf("the ultimate prize came at card %d", pos)
			}
			break
		}
	}
	if last.Int("next_round") != 1 {
		t.Fatalf("no ultimate prize in a round: %v", last)
	}
	c.send(28301, proto.Message{})
	if r := c.waitFor(28301); len(r.Arr("award_list")) == 0 {
		t.Fatalf("28301: %v", r)
	}
	c.send(28303, proto.Message{})
	var got int64
	for _, a := range c.waitFor(28303).Arr("award_list") {
		if a.Int("type_id") == 10001 {
			got = a.Int("get_count")
		}
	}
	if got != 1 {
		t.Fatalf("the prize taken once: %d", got)
	}
	c.send(28308, proto.Message{})
	if r := c.waitFor(28308); r.Int("code") != 1 {
		t.Fatalf("next round: %v", r)
	}
	if r := c.waitFor(28300); r.Int("round") != 2 || r.Int("optional_id") != 0 || len(r.Arr("award_list")) != 0 {
		t.Fatalf("round 2: %v", r)
	}
	c.send(28308, proto.Message{})
	if r := c.waitFor(28308); r.Int("code") != 0 {
		t.Fatalf("next round twice: %v", r)
	}
}

func TestFortuneBagDiamondsAndClosed(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("fb2", "福袋人二")
	c.send(28302, proto.Message{"pos": 1})
	if r := c.waitFor(28302); r.Int("code") != 0 {
		t.Fatalf("closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 17553, BID: fbBID, Status: 1, StartAt: start, EndAt: start + 86400, Spec: actSpecJSON(t, false)})
	e.grantAssets(c, role, map[int64]int64{3: 2000 * 5})
	c.send(28305, proto.Message{"type_id": 10001})
	c.waitFor(28305)
	c.waitFor(28300)
	// no whistles: the diamonds at the discount pay five cards, then the full price is more than there is
	for pos := int64(1); pos <= 5; pos++ {
		c.send(28302, proto.Message{"pos": pos})
		if r := c.waitFor(28302); r.Int("code") != 1 {
			t.Fatalf("flip %d: %v", pos, r)
		}
		if r := c.waitFor(28300); r.Int("gold_time") != 5-pos {
			t.Fatalf("discount left: %v", r)
		}
	}
	c.send(28302, proto.Message{"pos": 6})
	if r := c.waitFor(28302); r.Int("code") != 0 {
		t.Fatalf("the full price without diamonds: %v", r)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] >= 5000 {
		t.Fatalf("diamonds left: %d", a[3])
	}
}
