package game

import (
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// dayZone decides where a calendar day starts for "open day" counting. The
// original rule is unknown, so this is our policy: China time, UTC+8.
var dayZone = time.FixedZone("UTC+8", 8*3600)

// openDay is the number of the day the server is on: 1 on the day it opened, 2 on
// the next calendar day, and so on; 0 before it opens. The client compares it with
// configured days (role_controller.lua handle10906, e.g. guild and first-charge
// unlock checks); whether the original counts from 0 or 1 is not confirmed.
func openDay(openAt, now time.Time) int64 {
	if now.Before(openAt) {
		return 0
	}
	a, b := openAt.In(dayZone), now.In(dayZone)
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, dayZone)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, dayZone)
	return int64(db.Sub(da)/(24*time.Hour)) + 1
}

// serverOpenDay answers 10906 from the server row's open_time; a row without one
// (open_time 0) counts from when it was created.
func (l *Login) serverOpenDay(s *gateway.Session, _ proto.Message) error {
	srv, err := l.Store.Server(l.SrvID)
	if err != nil {
		return err
	}
	at := srv.OpenTime
	if at == 0 {
		at = srv.CreatedAt
	}
	return s.Send(10906, proto.Message{"open_day": openDay(time.Unix(at, 0), s.Now())})
}
