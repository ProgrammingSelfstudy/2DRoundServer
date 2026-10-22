package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// online_gifts.json is the client's online-gift table (tools/extract_online_gifts.py):
// steps in time order, each with the online seconds it needs and its reward.
//
//go:embed data/online_gifts.json
var onlineGiftsJSON []byte

type onlineStep struct {
	Index int        `json:"index"`
	Time  int64      `json:"time"`
	Items [][2]int64 `json:"items"`
}

var onlineSteps = func() []onlineStep {
	var f struct {
		Steps []onlineStep `json:"steps"`
	}
	if err := json.Unmarshal(onlineGiftsJSON, &f); err != nil || len(f.Steps) == 0 {
		panic("game: bad online_gifts.json")
	}
	return f.Steps
}()

// splitGains turns a reward list ({id, amount} pairs, amount scaled by times)
// into currency amounts (ids of type 7 in the item table) and item counts.
func splitGains(list []sellGain, times int64, assets, items map[int64]int64) {
	for _, g := range list {
		if t, ok := itemTable[g.ID]; ok && t.Type == 7 {
			assets[g.ID] += g.Amount * times
		} else {
			items[g.ID] += g.Amount * times
		}
	}
}

// dayKey is the calendar day of t in the game's day zone as YYYYMMDD, and
// dayStart the moment that day began.
func dayKey(t time.Time) int64 {
	y, m, d := t.In(dayZone).Date()
	return int64(y*10000 + int(m)*100 + d)
}

// dayTime is noon of the day YYYYMMDD in the game's day zone.
func dayTime(day int64) time.Time {
	return time.Date(int(day/10000), time.Month(day/100%100), int(day%100), 12, 0, 0, 0, dayZone)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.In(dayZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, dayZone)
}

// OnlineGiftService implements the online gifts (10926 progress, 10927 claim).
// The client shows the day's online seconds and the steps already claimed. What
// counts as "a day" is our policy (calendar day, China time): the original rule
// is unknown.
type OnlineGiftService struct {
	Store *store.Store
	Hub   *gateway.Hub
	Log   *log.Logger

	mu    sync.Mutex
	since map[int64]time.Time // online roles: when their current stretch of time began
}

func (o *OnlineGiftService) Register(r *gateway.Router) {
	r.Handle(10926, o.progress)
	r.Handle(10927, o.claim)
}

// flush adds the stretch since the last flush to today's online time (the part
// before midnight belongs to the previous day and is dropped) and starts a new one.
func (o *OnlineGiftService) flush(rid int64, now time.Time) error {
	o.mu.Lock()
	start, ok := o.since[rid]
	if ok {
		o.since[rid] = now
	}
	o.mu.Unlock()
	if !ok {
		return nil
	}
	if ds := dayStart(now); start.Before(ds) {
		start = ds
	}
	if n := int64(now.Sub(start) / time.Second); n > 0 {
		return o.Store.AddOnlineSeconds(rid, dayKey(now), n)
	}
	return nil
}

// Entered starts counting for a role that has logged in. A role replaced by a
// newer login of itself keeps what it has earned so far.
func (o *OnlineGiftService) Entered(rid int64, now time.Time) {
	if err := o.flush(rid, now); err != nil {
		o.Log.Printf("onlinegift: role %d: %v", rid, err)
	}
	o.mu.Lock()
	if o.since == nil {
		o.since = map[int64]time.Time{}
	}
	o.since[rid] = now
	o.mu.Unlock()
}

// Disconnected is the gateway's OnDisconnect hook; a role that was replaced by a
// newer login of itself is still counting.
func (o *OnlineGiftService) Disconnected(s *gateway.Session) {
	r := s.Role()
	if r == nil || o.Hub.Online(r.RID) != nil {
		return
	}
	now := s.Now()
	if err := o.flush(r.RID, now); err != nil {
		o.Log.Printf("onlinegift: role %d: %v", r.RID, err)
	}
	o.mu.Lock()
	delete(o.since, r.RID)
	o.mu.Unlock()
}

func (o *OnlineGiftService) seconds(rid int64, now time.Time) (int64, error) {
	if err := o.flush(rid, now); err != nil {
		return 0, err
	}
	return o.Store.OnlineSeconds(rid, dayKey(now))
}

func (o *OnlineGiftService) progress(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	now := s.Now()
	secs, err := o.seconds(me.RID, now)
	if err != nil {
		return err
	}
	claimed, err := o.Store.OnlineGiftClaims(me.RID, dayKey(now))
	if err != nil {
		return err
	}
	list := make([]proto.Message, 0, len(claimed))
	for _, t := range claimed {
		list = append(list, proto.Message{"time": t})
	}
	return s.Send(10926, proto.Message{"time": secs, "list": list})
}

func (o *OnlineGiftService) claim(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	// The request's "time" is the step's number (1-12, the client sends the table index);
	// the reply's and the progress list's "time" is the step's required seconds.
	want := req.Int("time")
	var step *onlineStep
	for i := range onlineSteps {
		if int64(onlineSteps[i].Index) == want {
			step = &onlineSteps[i]
		}
	}
	reply := func(ok bool, msg string) error {
		secs := want
		if step != nil {
			secs = step.Time
		}
		return s.Send(10927, proto.Message{"code": b2i(ok), "msg": msg, "time": secs})
	}
	if step == nil {
		return reply(false, "奖励不存在")
	}
	now := s.Now()
	secs, err := o.seconds(me.RID, now)
	if err != nil {
		return err
	}
	if secs < step.Time {
		return reply(false, "在线时间不足")
	}
	gains := make([]sellGain, 0, len(step.Items))
	for _, it := range step.Items {
		gains = append(gains, sellGain{ID: it[0], Amount: it[1]})
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	splitGains(gains, 1, assets, items)
	switch err := o.Store.ClaimOnlineGift(me.RID, dayKey(now), step.Time, assets, items); {
	case errors.Is(err, store.ErrConflict):
		return reply(false, "已经领取过了")
	case err != nil:
		return err
	}
	if err := reply(true, "领取成功"); err != nil {
		return err
	}
	if err := sendAssets(s, o.Store, me.RID); err != nil {
		return err
	}
	return sendBackpack(s, o.Store)
}
