package store

import (
	"testing"
	"time"
)

func TestAnalyticsReport(t *testing.T) {
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	if err := s.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	acc, _ := s.FindOrCreateAccount("symlf", "ua")
	a, err := s.CreateRole(acc.ID, "symlf_1", "甲", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	acc2, _ := s.FindOrCreateAccount("symlf", "ub")
	b, err := s.CreateRole(acc2.ID, "symlf_1", "乙", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	day0 := DayNo(now.Unix())

	now = now.Add(24 * time.Hour) // day 1: only the first role comes back
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.RecordEvent("login", 0, acc.ID, 0))
	must(s.RecordEvent("enter", a.RID, acc.ID, 0))
	must(s.RecordEvent("enter", a.RID, acc.ID, 0)) // a second entry of the same role is one active role
	must(s.RecordEvent("logout", a.RID, acc.ID, 600))
	must(s.SampleOnline(3))
	must(s.SampleOnline(5)) // same minute: the larger count stays
	s.RecordCmd(a.RID, 10300)
	s.RecordCmd(a.RID, 10300)
	s.RecordCmd(a.RID, 13003)
	s.RecordCmd(b.RID, 10300)
	must(s.FlushCmds())
	s.RecordCmd(a.RID, 10300)
	must(s.FlushCmds()) // counts add up across flushes
	must(s.FlushCmds()) // nothing left is not an error

	o, err := s.CreatePayOrder(PayOrder{OrderNo: "P1", RID: a.RID, SrvID: "symlf_1", ChargeID: 8, AmountCents: 64800})
	must(err)
	if _, err := s.CreatePayOrder(PayOrder{OrderNo: "P2", RID: a.RID, SrvID: "symlf_1", ChargeID: 8, AmountCents: 64800}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmPayOrder(o.OrderNo, "op", "", 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	now = now.Add(24 * time.Hour) // today is day 2
	r, err := s.Analytics(5)
	must(err)
	if len(r.Days) != 5 || r.Days[4].Day != r.To {
		t.Fatalf("window: %d days, last %q, to %q", len(r.Days), r.Days[4].Day, r.To)
	}
	byDay := map[string]AnalyticsDay{}
	for _, d := range r.Days {
		byDay[d.Day] = d
	}
	d0, d1 := byDay[dayDate(day0)], byDay[dayDate(day0+1)]
	if d0.NewRoles != 2 || d0.NewAccounts != 2 || d0.Active != 0 {
		t.Fatalf("day 0: %+v", d0)
	}
	if d1.Active != 1 || d1.LoginAccounts != 1 || d1.Sessions != 1 || d1.AvgSessionSec != 600 || d1.PeakOnline != 5 {
		t.Fatalf("day 1: %+v", d1)
	}
	if d1.OrdersCreated != 2 || d1.OrdersPaid != 1 || d1.PayingRoles != 1 || d1.PaidCents != 64800 {
		t.Fatalf("day 1 payments: %+v", d1)
	}
	if r.EventsFrom != dayDate(day0+1) {
		t.Fatalf("events from %q", r.EventsFrom)
	}

	var ret AnalyticsRetention
	for _, x := range r.Retention {
		if x.Day == dayDate(day0) {
			ret = x
		}
	}
	// day 0 began before any event was recorded; day 1 did not, so D1 is known: one of the two roles came back
	if ret.Cohort != 2 || ret.D1 == nil || *ret.D1 != 1 || ret.D3 != nil || ret.D7 != nil {
		t.Fatalf("retention of day 0: %+v", ret)
	}

	if len(r.Cmds) != 2 || r.Cmds[0].Cmd != 10300 || r.Cmds[0].Hits != 4 || r.Cmds[0].Roles != 2 || r.Cmds[1].Cmd != 13003 || r.Cmds[1].Hits != 1 {
		t.Fatalf("commands: %+v", r.Cmds)
	}
}

func TestAnalyticsEmptyAndBounds(t *testing.T) {
	s := open(t)
	r, err := s.Analytics(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Days) != maxAnalyticsDays || r.EventsFrom != "" || len(r.Cmds) != 0 {
		t.Fatalf("empty report: %d days, events from %q, %d commands", len(r.Days), r.EventsFrom, len(r.Cmds))
	}
	for _, x := range r.Retention {
		if x.D1 != nil || x.D3 != nil || x.D7 != nil {
			t.Fatalf("retention without events: %+v", x)
		}
	}
	if r, err = s.Analytics(0); err != nil || len(r.Days) != 1 {
		t.Fatalf("0 days gives %v, %v", r, err)
	}
}

func TestOnlineSamplesArePruned(t *testing.T) {
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	if err := s.SampleOnline(1); err != nil {
		t.Fatal(err)
	}
	now = now.Add((sampleKeepDays + 1) * 24 * time.Hour)
	if err := s.SampleOnline(2); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM online_samples`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d samples kept, %v", n, err)
	}
}
