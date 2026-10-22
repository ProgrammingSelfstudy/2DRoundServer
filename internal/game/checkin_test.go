package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func checkinState(c *client) (day, status int64) {
	c.send(14100, proto.Message{})
	r := c.recv(14100)
	return r.Int("day"), r.Int("status")
}

// signIn signs in and, when it worked, drains the refreshed resources and backpack.
func signIn(c *client) proto.Message {
	c.send(14101, proto.Message{})
	r := c.recv(14101)
	if r.Int("code") == 1 {
		c.recv(10302)
		c.recv(10500)
	}
	return r
}

func giftCount(c *client, id int64) int64 {
	c.send(10500, proto.Message{})
	for _, it := range c.recv(10500).Arr("item_list") {
		if it.Int("base_id") == id {
			return it.Int("quantity")
		}
	}
	return 0
}

func TestCheckin(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 9, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")

	if day, status := checkinState(c); day != 0 || status != 0 {
		t.Fatalf("a new role: %d %d", day, status)
	}
	r := signIn(c)
	if r.Int("code") != 1 || r.Int("day") != 1 || r.Int("status") != 2 || r.Str("msg") == "" {
		t.Fatalf("first sign-in: %v", r)
	}
	// the reward is the daily gift box (99955), one per sign-in
	if n := giftCount(c, 99955); n != 1 {
		t.Fatalf("gift boxes: %d", n)
	}
	if day, status := checkinState(c); day != 1 || status != 2 {
		t.Fatalf("signed today: %d %d", day, status)
	}
	// once a day
	c.send(14101, proto.Message{})
	if r := c.recv(14101); r.Int("code") != 0 || r.Str("msg") != "今天已经签到过了" || r.Int("day") != 1 || r.Int("status") != 2 {
		t.Fatalf("second sign-in today: %v", r)
	}
	if n := giftCount(c, 99955); n != 1 {
		t.Fatalf("a refused sign-in paid: %d", n)
	}

	// the next day: open again, the second sign-in is day 2
	e.advance(24 * time.Hour)
	if day, status := checkinState(c); day != 1 || status != 0 {
		t.Fatalf("next day: %d %d", day, status)
	}
	if r := signIn(c); r.Int("day") != 2 || r.Int("status") != 2 {
		t.Fatalf("second sign-in: %v", r)
	}
	// days skipped do not matter: the third sign-in is day 3, a week later
	e.advance(7 * 24 * time.Hour)
	if r := signIn(c); r.Int("day") != 3 {
		t.Fatalf("third sign-in: %v", r)
	}
	if n := giftCount(c, 99955); n != 3 {
		t.Fatalf("gift boxes after three sign-ins: %d", n)
	}

	// a new month starts again from day 1
	e.setNow(time.Date(2026, 11, 1, 0, 5, 0, 0, dayZone))
	if day, status := checkinState(c); day != 0 || status != 0 {
		t.Fatalf("new month: %d %d", day, status)
	}
	if r := signIn(c); r.Int("day") != 1 {
		t.Fatalf("first sign-in of the month: %v", r)
	}
	if st, _ := e.st.Checkin(role.RID); st.Month != 202611 || st.Count != 1 || st.LastDay != 20261101 {
		t.Fatalf("stored: %+v", st)
	}
}

func TestCheckinEveryDayOfAShortMonth(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 2, 1, 9, 0, 0, 0, dayZone)) // February 2026 has 28 days, the table 29
	c, _ := e.enter("u1", "甲")
	for d := 1; d <= 28; d++ {
		if r := signIn(c); r.Int("code") != 1 || r.Int("day") != int64(d) {
			t.Fatalf("day %d: %v", d, r)
		}
		e.advance(24 * time.Hour)
	}
	// 1 March: a new month, day 1 again
	if r := signIn(c); r.Int("code") != 1 || r.Int("day") != 1 {
		t.Fatalf("March: %v", r)
	}
	if n := giftCount(c, 99955); n != 29 {
		t.Fatalf("gift boxes: %d", n)
	}
}

func TestCheckinRefusesWhenTheMonthsTableIsUsedUp(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 4, 30, 9, 0, 0, 0, dayZone)) // the table has 30 days for April
	c, role := e.enter("u1", "甲")
	// 30 earlier sign-ins this month (made-up day keys, none of them today)
	for i := int64(0); i < 30; i++ {
		if err := e.st.DoCheckin(role.RID, 202604, 100+i, i, nil, nil); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if day, status := checkinState(c); day != 30 || status != 0 {
		t.Fatalf("state: %d %d", day, status)
	}
	c.send(14101, proto.Message{})
	if r := c.recv(14101); r.Int("code") != 0 || r.Str("msg") != "本月签到已完成" || r.Int("day") != 30 {
		t.Fatalf("a used-up month: %v", r)
	}
}

func TestCheckinStoreIsGuarded(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	pay := map[int64]int64{3: 100}
	if err := e.st.DoCheckin(role.RID, 202610, 20261019, 0, pay, nil); err != nil {
		t.Fatal(err)
	}
	// the same day again, and a stale count: both refused and paid nothing
	if err := e.st.DoCheckin(role.RID, 202610, 20261019, 1, pay, nil); err != store.ErrConflict {
		t.Fatalf("same day: %v", err)
	}
	if err := e.st.DoCheckin(role.RID, 202610, 20261020, 0, pay, nil); err != store.ErrConflict {
		t.Fatalf("stale count: %v", err)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[3] != 100 {
		t.Fatalf("paid: %v", got)
	}
}

func TestCheckinBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(14100, proto.Message{})
	c.send(14101, proto.Message{})
	c.expectNothing()
}

func TestCheckinTableIsComplete(t *testing.T) {
	want := []int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	for i, days := range checkinMonths {
		if len(days) != want[i] {
			t.Errorf("month %d: %d days, want %d", i+1, len(days), want[i])
		}
		for d, rewards := range days {
			for _, r := range rewards {
				if _, known := itemTable[r[0]]; !known {
					t.Errorf("month %d day %d pays unknown id %d", i+1, d+1, r[0])
				}
			}
		}
	}
}
