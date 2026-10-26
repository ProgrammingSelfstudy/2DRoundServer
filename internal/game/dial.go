package game

import (
	_ "embed"
	"encoding/json"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// dial.json: the dial (转盘) of the client (tools/extract_dial.py).
//
//go:embed data/dial.json
var dialJSON []byte

const (
	dialModule = "dial"
	dialCamp   = 93022 // ActionRankCommonType.dial: the camp_id the operator gives the activity in the schedule
	dialLogMax = 30
)

type dialSlot struct {
	Order int64 `json:"order"`
	ID    int64 `json:"id"`
	Num   int64 `json:"num"`
	Type  int64 `json:"type"` // 1, 2, 3: the first, second and third prize (a share of the pool); 9: the hero card
}

type dialAward struct {
	ID    int64      `json:"id"`
	Score int64      `json:"score"`
	Award [][2]int64 `json:"award"`
}

var dialCfg = func() (c struct {
	Const  map[string]int64 `json:"const"`
	Slots  []dialSlot       `json:"slots"`
	Awards []dialAward      `json:"awards"`
}) {
	if err := json.Unmarshal(dialJSON, &c); err != nil || len(c.Slots) != 12 {
		panic("game: bad dial.json")
	}
	return
}()

// dialWeights is the chance in ten thousandths of each slot (their sum is 10085: the listed chances add up to 100.85%, so they are used as weights) (by order). The client text lists the chances of the kinds (the
// hero card 2.85%, the pool diamonds 6.75%, the advanced summon tickets 5%, the rune essence 41.15%, the god-refining
// stones 12.1%, the 5-star shards 33%) but not which slot is which; the slots are matched to the kinds by their items
// and the pool prizes share their 6.75% as 0.75 / 1.5 / 4.5 (first, second, third) — ours, not captured.
var dialWeights = [13]int64{
	1:  285, // the hero card
	2:  150, // gift boxes: the summon ticket group, 5% in all
	3:  150,
	4:  75,   // the first prize
	5:  1000, // 神技精石 ×1000
	6:  200,
	7:  150,  // the second prize
	8:  3300, // the 5-star shards
	9:  3115, // 神技精石 ×10
	10: 450,  // the third prize
	11: 605,  // 原初意志 (the god-refining stones group is 12.1% with the next)
	12: 605,  // 强化训练
}

// dialShare is the part of the pool the first, second and third prize pay (the client text: 50%, 20%, 10%).
var dialShare = map[int64]int64{1: 50, 2: 20, 3: 10}

type dialLog struct {
	Name string `json:"name"`
	BID  int64  `json:"bid"`
	Num  int64  `json:"num"`
}

type dialRole struct {
	Start   int64 // the window the state is of; a new activity starts from nothing
	Score   int64
	FreeDay int64
	Claimed []int64
	Pending []dialLog // what the last draw gave, until the client asks for it (16672)
	Log     []dialLog
}

type dialWorld struct {
	Start int64
	Pool  int64
	Log   []dialLog
}

var dialMu sync.Mutex

func dialPick() int {
	total := int64(0)
	for _, w := range dialWeights {
		total += w
	}
	r := int64(summonRand() * float64(total))
	for i := 1; i <= 12; i++ {
		if r -= dialWeights[i]; r < 0 {
			return i
		}
	}
	return 12
}

func dialPush(l []dialLog, e dialLog) []dialLog {
	l = append([]dialLog{e}, l...)
	if len(l) > dialLogMax {
		l = l[:dialLogMax]
	}
	return l
}

// RegisterDial registers the dial (16670-16676): spend the treasure key (37003) to turn it, one free turn a day; every
// turn adds 10 diamonds to the pool of the server and a point; the three top prizes pay a share of the pool; points
// give awards. It runs while the operator has the activity with camp_id 93022 open in the schedule. The diamond price
// of a turn in the table is 22 billion, so a turn is paid with keys only.
func RegisterDial(r *gateway.Router, st *store.Store) {
	load := func(role *store.Role, now time.Time) (*dialRole, *dialWorld, int64, bool, error) {
		start, _, open := ActivityOpen(st, role.SrvID, dialCamp, now)
		d, w := &dialRole{}, &dialWorld{}
		if _, err := st.LoadState(role.RID, dialModule, d); err != nil {
			return nil, nil, 0, false, err
		}
		if _, err := st.LoadGlobal(role.SrvID, dialModule, w); err != nil {
			return nil, nil, 0, false, err
		}
		if d.Start != start {
			*d = dialRole{Start: start}
		}
		if w.Start != start || w.Pool < dialCfg.Const["min_reward"] {
			if w.Start != start {
				*w = dialWorld{Start: start}
			}
			w.Pool = max(w.Pool, dialCfg.Const["min_reward"])
		}
		return d, w, start, open, nil
	}
	info := func(role *store.Role, d *dialRole, w *dialWorld, now time.Time) proto.Message {
		list := []proto.Message{}
		for _, a := range dialCfg.Awards {
			status := int64(0)
			switch {
			case has(d.Claimed, a.ID):
				status = 2
			case d.Score >= a.Score:
				status = 1
			}
			list = append(list, proto.Message{"id": a.ID, "status": status})
		}
		free := int64(1)
		if d.FreeDay != dayKey(now) {
			free = 0 // is_free 0 means the free turn is still there
		}
		return proto.Message{"num": d.Score, "gold": w.Pool, "is_free": free, "holiday_lev": int64(role.Lev), "award_list": list}
	}
	r.Handle(16670, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		dialMu.Lock()
		defer dialMu.Unlock()
		d, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		return s.Send(16670, info(role, d, w, s.Now()))
	})
	r.Handle(16671, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(16671, proto.Message{"result": 0, "msg": msg, "id": 0, "flag": req.Int("flag")})
		}
		count := req.Int("count")
		if count != 1 && count != dialCfg.Const["continue_time"] {
			return refuse("次数不对")
		}
		dialMu.Lock()
		defer dialMu.Unlock()
		d, w, _, open, err := load(role, s.Now())
		if err != nil {
			return err
		}
		if !open {
			return refuse("活动未开启")
		}
		day := dayKey(s.Now())
		spend := count
		if count == 1 && d.FreeDay != day {
			spend = 0
			d.FreeDay = day
		}
		if spend > 0 {
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{dialCfg.Const["lottery_ticket"]: spend}, map[int64]int64{}, map[int64]int64{}); err != nil {
				return refuse("道具不足")
			}
		}
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		var got []dialLog
		last := 0
		for i := int64(0); i < count; i++ {
			w.Pool += dialCfg.Const["bouns_add"]
			d.Score++
			slot := dialPick()
			last = slot
			it := dialCfg.Slots[slot-1]
			id, num := it.ID, it.Num
			if pct, ok := dialShare[it.Type]; ok {
				id, num = 3, w.Pool*pct/100
				w.Pool = max(w.Pool-num, dialCfg.Const["min_reward"])
			}
			if ItemKnown(id) {
				splitGains([]sellGain{{id, num}}, 1, gainA, gainI)
			}
			e := dialLog{Name: role.Name, BID: id, Num: num}
			got = append(got, e)
			d.Log = dialPush(d.Log, e)
			if it.Type != 0 { // the pool prizes and the hero card are told to everybody
				w.Log = dialPush(w.Log, e)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, gainA, gainI); err != nil {
			return err
		}
		d.Pending = got
		if err := st.SaveState(role.RID, dialModule, d); err != nil {
			return err
		}
		if err := st.SaveGlobal(role.SrvID, dialModule, w); err != nil {
			return err
		}
		if err := s.Send(16671, proto.Message{"result": 1, "msg": "抽取成功", "id": last, "flag": req.Int("flag")}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(16676, proto.Message{"gold": w.Pool}); err != nil {
			return err
		}
		if req.Int("flag") == 1 { // without the animation the client does not ask for the result
			d.Pending = nil
			if err := st.SaveState(role.RID, dialModule, d); err != nil {
				return err
			}
			awards := []proto.Message{}
			for _, e := range got {
				awards = append(awards, proto.Message{"bid": e.BID, "num": e.Num})
			}
			if err := s.Send(16672, proto.Message{"count": count, "awards2": awards}); err != nil {
				return err
			}
		}
		return s.Send(16670, info(role, d, w, s.Now()))
	})
	r.Handle(16672, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		dialMu.Lock()
		defer dialMu.Unlock()
		d, _, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		awards := []proto.Message{}
		for _, e := range d.Pending {
			awards = append(awards, proto.Message{"bid": e.BID, "num": e.Num})
		}
		n := len(d.Pending)
		d.Pending = nil
		if n > 0 {
			if err := st.SaveState(role.RID, dialModule, d); err != nil {
				return err
			}
		}
		return s.Send(16672, proto.Message{"count": n, "awards2": awards})
	})
	r.Handle(16673, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(16673, proto.Message{"result": 0, "msg": msg}) }
		dialMu.Lock()
		defer dialMu.Unlock()
		d, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		var a *dialAward
		for i := range dialCfg.Awards {
			if dialCfg.Awards[i].ID == req.Int("id") {
				a = &dialCfg.Awards[i]
			}
		}
		switch {
		case a == nil:
			return refuse("奖励不存在")
		case has(d.Claimed, a.ID):
			return refuse("已经领取过了")
		case d.Score < a.Score:
			return refuse("积分不够")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, x := range a.Award {
			if ItemKnown(x[0]) {
				splitGains([]sellGain{{x[0], x[1]}}, 1, ga, gi)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Claimed = append(d.Claimed, a.ID)
		if err := st.SaveState(role.RID, dialModule, d); err != nil {
			return err
		}
		if err := s.Send(16673, proto.Message{"result": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(16670, info(role, d, w, s.Now()))
	})
	r.Handle(16674, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		dialMu.Lock()
		defer dialMu.Unlock()
		d, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		src := d.Log
		if req.Int("type") == 2 {
			src = w.Log
		}
		list := []proto.Message{}
		for _, e := range src {
			list = append(list, proto.Message{"role_name": e.Name, "bid": e.BID, "num": e.Num})
		}
		return s.Send(16674, proto.Message{"type": req.Int("type"), "log_list": list})
	})
}
