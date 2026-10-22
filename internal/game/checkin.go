package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// checkin.json is the client's sign-in table (tools/extract_checkin.py): for each
// month 1-12 the reward of each sign-in day, a list of {id, amount}.
//
//go:embed data/checkin.json
var checkinJSON []byte

var checkinMonths = func() [][][][2]int64 {
	var f struct {
		Months [][][][2]int64 `json:"months"`
	}
	if err := json.Unmarshal(checkinJSON, &f); err != nil || len(f.Months) != 12 {
		panic("game: bad checkin.json")
	}
	return f.Months
}()

// Sign-in status the client understands (sign_panel.lua): 0 not signed in today,
// 2 signed in today. (1 is "signed in, an extra reward waits for a recharge";
// there is no recharge, so it is never used.)
const (
	checkinOpen = 0
	checkinDone = 2
)

// CheckinService implements the daily sign-in (14100 state, 14101 sign in). The
// n-th sign-in of a month pays the n-th reward of that month's table, whichever
// days the player signs in on. A day and a month are calendar ones in China time
// (our policy: the original rule is unknown).
type CheckinService struct {
	Store *store.Store
}

func (c *CheckinService) Register(r *gateway.Router) {
	r.Handle(14100, c.state)
	r.Handle(14101, c.signIn)
}

func monthKey(t time.Time) int64 {
	y, m, _ := t.In(dayZone).Date()
	return int64(y*100 + int(m))
}

// signedThisMonth is how many sign-ins the role has this month, and whether one was today.
func (c *CheckinService) signedThisMonth(rid int64, now time.Time) (count int64, today bool, err error) {
	st, err := c.Store.Checkin(rid)
	if err != nil {
		return 0, false, err
	}
	if st.Month != monthKey(now) {
		return 0, false, nil
	}
	return st.Count, st.LastDay == dayKey(now), nil
}

func (c *CheckinService) state(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	count, today, err := c.signedThisMonth(me.RID, s.Now())
	if err != nil {
		return err
	}
	status := checkinOpen
	if today {
		status = checkinDone
	}
	return s.Send(14100, proto.Message{"day": count, "status": status})
}

func (c *CheckinService) signIn(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	now := s.Now()
	count, today, err := c.signedThisMonth(me.RID, now)
	if err != nil {
		return err
	}
	reply := func(ok bool, msg string) error {
		status := checkinOpen
		if today || ok {
			status = checkinDone
		}
		day := count
		if ok {
			day = count + 1
		}
		return s.Send(14101, proto.Message{"code": b2i(ok), "msg": msg, "day": day, "status": status})
	}
	days := checkinMonths[int(now.In(dayZone).Month())-1]
	switch {
	case today:
		return reply(false, "今天已经签到过了")
	case int(count) >= len(days):
		return reply(false, "本月签到已完成")
	}
	gains := make([]sellGain, 0, len(days[count]))
	for _, r := range days[count] {
		gains = append(gains, sellGain{ID: r[0], Amount: r[1]})
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	splitGains(gains, 1, assets, items)
	switch err := c.Store.DoCheckin(me.RID, monthKey(now), dayKey(now), count, assets, items); {
	case errors.Is(err, store.ErrConflict):
		return reply(false, "今天已经签到过了") // signed in from another session in between
	case err != nil:
		return err
	}
	if err := reply(true, "签到成功"); err != nil {
		return err
	}
	if err := sendAssets(s, c.Store, me.RID); err != nil {
		return err
	}
	return sendBackpack(s, c.Store)
}
