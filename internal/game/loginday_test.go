package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestLoginDay(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("a", "甲")
	statuses := func() map[int64]int64 {
		c.send(21100, proto.Message{})
		r := c.waitFor(21100)
		out := map[int64]int64{}
		for _, d := range r.Arr("status_list") {
			out[d.Int("day")] = d.Int("status")
		}
		if r.Int("type") != 1 || len(out) != 8 {
			t.Fatalf("21100: %v", r)
		}
		return out
	}
	if s := statuses(); s[1] != loginDayOpen || s[2] != loginDayLocked {
		t.Fatalf("day 1 only: %v", s)
	}
	c.send(10922, proto.Message{})
	if l := c.waitFor(10922).Arr("act_list"); len(l) != 1 || l[0].Int("id") != loginDayIcon || l[0].Int("status") != 1 {
		t.Fatalf("10922: %v", l)
	}
	c.send(10925, proto.Message{"id": loginDayIcon})
	if r := c.waitFor(10925); r.Int("id") != loginDayIcon || r.Int("status") != 1 {
		t.Fatalf("10925: %v", r)
	}
	c.send(21101, proto.Message{"day": 2})
	if r := c.waitFor(21101); r.Int("code") != 0 {
		t.Fatalf("day 2 too early: %v", r)
	}
	c.send(21101, proto.Message{"day": 1})
	if r := c.waitFor(21101); r.Int("code") != 1 || r.Int("day") != 1 {
		t.Fatalf("take day 1: %v", r)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[1] < loginDays[0][0][1] {
		t.Fatalf("berries not paid: %v", got)
	}
	c.send(21101, proto.Message{"day": 1})
	if r := c.waitFor(21101); r.Int("code") != 0 {
		t.Fatalf("take twice: %v", r)
	}
	if s := statuses(); s[1] != loginDayTaken {
		t.Fatalf("after taking: %v", s)
	}
	// three days on, day 3 is reached
	if got := loginDayReached(role, time.Unix(role.CreatedAt, 0).Add(48*time.Hour)); got != 3 {
		t.Fatalf("reached after 48h: %d", got)
	}
	if got := loginDayReached(role, time.Unix(role.CreatedAt, 0).Add(1000*time.Hour)); got != 8 {
		t.Fatalf("capped: %d", got)
	}
}
