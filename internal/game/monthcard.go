package game

import (
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	monthCardModule = "monthcard"
	monthCardDays   = 30 // month_card*_cont_day
)

// monthCards are the two month cards of the welfare window: the money (yuan) that activates them and the diamonds of each day,
// from the client's charge constants (month_card1: 98 yuan, 200 a day, 6000 over the 30 days; month_card2: 30 yuan, 120 a day).
var monthCards = [2]struct{ Sun, Daily int64 }{{98, 200}, {30, 120}}

type monthCardState struct {
	Base  [2]int64 // cents paid when the card was activated the last time: the next activation counts from there
	Start [2]int64 // day number of the first day, 0 = never activated
	Last  [2]int64 // day number of the last day taken
	Days  [2]int64 // days taken in this run
}

// view is the card as the window shows it: acc the yuan paid towards the next activation, state 0 not active, 1 the day's gift
// waits, 2 it was taken.
func (d *monthCardState) view(i int, rc *store.RoleCharge, today int64) (acc, state, days, end int64) {
	c := monthCards[i]
	run := d.Start[i] != 0 && today < d.Start[i]+monthCardDays
	if !run {
		return min(max(0, (rc.TotalCents-d.Base[i])/100), c.Sun), 0, d.Days[i], 0
	}
	state = 1
	if d.Last[i] == today {
		state = 2
	}
	return c.Sun, state, d.Days[i], (d.Start[i] + monthCardDays) // the day number; the caller turns it to a time
}

// dayNo counts the days since 1970 in China time, so that days can be added.
func dayNo(t time.Time) int64 { return (t.Unix() + 8*3600) / 86400 }

// RegisterMonthCard registers the month cards: 16705 the window, 16706 take a card's day, 16707 whether anything waits (the VIP
// window's red dot) and 16708 take every card's day.
func RegisterMonthCard(r *gateway.Router, st *store.Store) {
	type ctx struct {
		role  *store.Role
		rc    *store.RoleCharge
		d     *monthCardState
		today int64
	}
	load := func(s *gateway.Session) (*ctx, error) {
		role := s.Role()
		if role == nil {
			return nil, nil
		}
		rc, err := st.RoleCharge(role.RID)
		if err != nil {
			return nil, err
		}
		d := &monthCardState{}
		if _, err := st.LoadState(role.RID, monthCardModule, d); err != nil {
			return nil, err
		}
		c := &ctx{role: role, rc: rc, d: d, today: dayNo(s.Now())}
		// a card is active from the moment the paid money reaches its sum; the run starts that day
		for i := range monthCards {
			run := d.Start[i] != 0 && c.today < d.Start[i]+monthCardDays
			if !run && (rc.TotalCents-d.Base[i])/100 >= monthCards[i].Sun {
				d.Base[i], d.Start[i], d.Last[i], d.Days[i] = rc.TotalCents, c.today, 0, 0
				if err := st.SaveState(role.RID, monthCardModule, d); err != nil {
					return nil, err
				}
			}
		}
		return c, nil
	}
	endTime := func(day int64) int64 {
		if day == 0 {
			return 0
		}
		return day*86400 - 8*3600 // midnight of that day, China time
	}
	window := func(c *ctx) proto.Message {
		m := proto.Message{}
		for i := range monthCards {
			acc, state, days, end := c.d.view(i, c.rc, c.today)
			n := "card" + itoa64(int64(i+1))
			m[n+"_acc"], m[n+"_is_reward"], m[n+"_days"], m[n+"_end_time"] = acc, state, days, endTime(end)
		}
		return m
	}
	take := func(s *gateway.Session, c *ctx, i int) (string, error) {
		_, state, _, _ := c.d.view(i, c.rc, c.today)
		switch state {
		case 0:
			return "还没有激活月卡", nil
		case 2:
			return "今天已经领取过了", nil
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{3, monthCards[i].Daily}}, 1, ga, gi)
		if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return "", err
		}
		c.d.Last[i] = c.today
		c.d.Days[i]++
		return "", st.SaveState(c.role.RID, monthCardModule, c.d)
	}
	r.Handle(16705, func(s *gateway.Session, _ proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		return s.Send(16705, window(c))
	})
	r.Handle(16706, func(s *gateway.Session, req proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		t := req.Int("card_type")
		if t < 1 || t > 2 {
			return s.Send(16706, proto.Message{"code": 0, "msg": "月卡不存在", "card_type": t})
		}
		msg, err := take(s, c, int(t-1))
		if err != nil {
			return err
		}
		if msg != "" {
			return s.Send(16706, proto.Message{"code": 0, "msg": msg, "card_type": t})
		}
		if err := s.Send(16706, proto.Message{"code": 1, "msg": "领取成功", "card_type": t}); err != nil {
			return err
		}
		if err := s.Send(16705, window(c)); err != nil {
			return err
		}
		return sendAssets(s, st, c.role.RID)
	})
	r.Handle(16707, func(s *gateway.Session, _ proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		status := int64(0)
		for i := range monthCards {
			if _, state, _, _ := c.d.view(i, c.rc, c.today); state == 1 {
				status = 1
			} else if state == 2 && status == 0 {
				status = 2
			}
		}
		return s.Send(16707, proto.Message{"status": status})
	})
	r.Handle(16708, func(s *gateway.Session, _ proto.Message) error {
		c, err := load(s)
		if c == nil {
			return err
		}
		got := false
		for i := range monthCards {
			if msg, err := take(s, c, i); err != nil {
				return err
			} else if msg == "" {
				got = true
			}
		}
		if !got {
			return s.Send(16708, proto.Message{"code": 0, "msg": "没有可以领取的月卡奖励"})
		}
		if err := s.Send(16708, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(16705, window(c)); err != nil {
			return err
		}
		return sendAssets(s, st, c.role.RID)
	})
}
