package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestLoginDay(t *testing.T) {
	e := newGameEnv(t, time.Minute)
	account, token, role := e.welfareRole("symlf", "a", "甲")
	c := enterWelfareRole(e.welfareDialer(), account, token, role)
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
	if l := c.waitFor(10922).Arr("act_list"); len(l) != len(growthIconIDs)+1 || l[0].Int("id") != loginDayIcon || l[0].Int("status") != 1 {
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
	c.recv(10302)
	c.recv(10500)
	c.recv(10501)
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

func TestGrowthActivityIconsFollowModulePeriods(t *testing.T) {
	e := newGameEnv(t, time.Minute)
	e.connect = serveTestPipe(t, e.gw, true)
	account, token, role := e.welfareRole("symlf", "growth", "成长图标")
	created := time.Date(2026, time.January, 25, 0, 0, 0, 0, dayZone)
	if err := e.st.SetRoleCreatedAt(role.RID, created.Unix()); err != nil {
		t.Fatal(err)
	}
	e.setNow(created)
	c := enterWelfareRole(func() *client { return e.dial(true) }, account, token, role)
	checkIcon := func(msg proto.Message, id, status int64) {
		t.Helper()
		if msg.Int("id") != id || msg.Int("status") != status || msg.Arr("int_args") == nil || len(msg.Arr("int_args")) != 0 || msg.Arr("ext_args") == nil || len(msg.Arr("ext_args")) != 0 {
			t.Fatalf("icon %d wanted %d, got %v", id, status, msg)
		}
	}
	check := func(seven, war int64, wanted map[int64]int64) {
		t.Helper()
		for cmd, period := range map[uint16]int64{13604: seven, 25300: war} {
			c.send(cmd, proto.Message{})
			if msg := c.waitFor(cmd); msg.Int("period") != period {
				t.Fatalf("%d expected period %d, got %v", cmd, period, msg)
			}
		}
		for _, cmd := range []uint16{10922, 10924} {
			c.send(cmd, proto.Message{})
			list := c.waitFor(cmd).Arr("act_list")
			if len(list) != len(growthIconIDs)+1 {
				t.Fatalf("%d: %v", cmd, list)
			}
			byID := make(map[int64]proto.Message)
			for _, msg := range list {
				byID[msg.Int("id")] = msg
			}
			checkIcon(byID[loginDayIcon], loginDayIcon, 1)
			for _, id := range growthIconIDs {
				checkIcon(byID[id], id, wanted[id])
			}
		}
		for _, id := range growthIconIDs {
			c.send(10925, proto.Message{"id": id})
			checkIcon(c.waitFor(10925), id, wanted[id])
		}
	}
	rollover := func(boundary time.Time, wanted map[int64]int64) {
		t.Helper()
		e.setNow(boundary)
		stop, done := make(chan struct{}), make(chan struct{})
		defer func() { close(stop); <-done }()
		go func() {
			defer close(done)
			first := true
			WatchActivities(e.st, e.gw.Hub, time.Millisecond, func() time.Time {
				if first {
					first = false
					return boundary.Add(-time.Second)
				}
				return boundary
			}, stop)
		}()
		for range growthIconIDs {
			msg := c.waitFor(10923)
			id := msg.Int("id")
			status, ok := wanted[id]
			if !ok {
				t.Fatalf("unexpected or repeated rollover: %v", msg)
			}
			checkIcon(msg, id, status)
			delete(wanted, id)
		}
	}
	diary := map[int64]int64{503: 1, 604: 1, 605: 1, 1503: 1, 602: 1, 2503: 1, 3503: 1, 4503: 1, 617: 1}
	box := map[int64]int64{503: 1, 604: 1, 605: 1, 603: 1, 2503: 1, 3503: 1, 4503: 1, 617: 1}
	check(2, 9, diary)
	rollover(created.AddDate(0, 0, 7), map[int64]int64{503: 1, 604: 1, 605: 1, 1503: 0, 602: 0, 603: 1, 2503: 1, 3503: 1, 4503: 1, 617: 1})
	check(3, 10, box)
	rollover(time.Date(2026, time.March, 1, 0, 0, 0, 0, dayZone), map[int64]int64{503: 1, 604: 1, 605: 1, 1503: 0, 602: 0, 603: 1, 2503: 1, 3503: 1, 4503: 1, 617: 0})
	delete(box, 617)
	check(9, 0, box)
	e.setNow(created.AddDate(0, 0, 70))
	check(0, 0, nil)
}

func TestLoginDayUnlocksAtChinaMidnight(t *testing.T) {
	created := time.Date(2026, time.November, 1, 23, 59, 58, 0, dayZone)
	role := &store.Role{CreatedAt: created.Unix()}
	if got := loginDayReached(role, created.Add(time.Second).UTC()); got != 1 {
		t.Fatalf("one second before China midnight: reached day %d", got)
	}
	if got := loginDayReached(role, created.Add(2*time.Second).UTC()); got != 2 {
		t.Fatalf("at China midnight: reached day %d", got)
	}
}
