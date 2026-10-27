package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// treasure.json: the lucky treasure hunt (幸运探宝 and 高级探宝, 16637~16643; the log 16641 is only pushed) of the client (tools/extract_treasure.py).
//
//go:embed data/treasure.json
var treasureJSON []byte

const (
	trModule = "treasure"
	trLogMax = 20
)

type trItem struct{ ID, Type, Pos, Item, Num int64 }

var trCfg = func() (c struct {
	FreeTime int64 `json:"free_time"`
	Refresh  [2]int64
	Slots    [2]int64
	Options  []struct {
		Type, Type2, Count int64
		Expend             [][2]int64
		AddItem            [][2]int64 `json:"add_item"`
		AddLucky           int64      `json:"add_lucky"`
		Lev, Vip           int64
	}
	Lucky []struct {
		ID, Type, Val int64
		Award         [][2]int64
	}
	Items []trItem
	Pools map[string][]struct {
		Min, Max int64
		Award    [][2]float64
	}
}) {
	if err := json.Unmarshal(treasureJSON, &c); err != nil || len(c.Items) == 0 || len(c.Options) != 4 || len(c.Pools) == 0 {
		panic("game: bad treasure.json")
	}
	return
}()

type trSlot struct{ Pos, ID, Status int64 }

type trWheel struct {
	Lucky   int64
	Count   int64
	Claimed []int64
	EndTime int64 // when the next refresh is free
	Day     int64
	Slots   []trSlot
}

type trRole struct{ Wheels [2]trWheel }

type trLog struct {
	Name string
	Bid  int64
	Num  int64
}

type trWorld struct{ Log [2][]trLog }

var trMu sync.Mutex

func trItemByID(id int64) trItem {
	for _, it := range trCfg.Items {
		if it.ID == id {
			return it
		}
	}
	return trItem{}
}

// trRoll fills a wheel: one item for each position out of the ones the table lists for it.
func trRoll(w *trWheel, typ int64, rng *rand.Rand) {
	w.Slots = w.Slots[:0]
	for pos := int64(1); pos <= trCfg.Slots[typ-1]; pos++ {
		var pool []trItem
		for _, it := range trCfg.Items {
			if it.Type == typ && it.Pos == pos {
				pool = append(pool, it)
			}
		}
		if len(pool) > 0 {
			w.Slots = append(w.Slots, trSlot{Pos: pos, ID: pool[rng.Intn(len(pool))].ID})
		}
	}
}

// trPool is the multi draw pool of a wheel for a level.
func trPool(typ, lev int64) [][2]float64 {
	pools := trCfg.Pools[itoa64(typ)]
	for _, p := range pools {
		if lev >= p.Min && lev <= p.Max {
			return p.Award
		}
	}
	return pools[len(pools)-1].Award
}

func trPick(pool [][2]float64, rng *rand.Rand) int64 {
	var total float64
	for _, p := range pool {
		total += p[1]
	}
	n := rng.Float64() * total
	for _, p := range pool {
		if n < p[1] {
			return int64(p[0])
		}
		n -= p[1]
	}
	return int64(pool[len(pool)-1][0])
}

// RegisterTreasure registers the lucky treasure hunt, always open: two wheels of 8 items (lucky from level 10, advanced
// from 30); one draw lands on a position of the wheel, the multi draws also pull from a pool by level; every draw adds
// lucky value, which opens the five lucky gifts; a refresh puts new items on the wheel (free every 3 hours, else 30
// diamonds); the wheel is new every day. Where the client has no table (which position a draw lands on, what a multi draw
// pulls in addition) the rule is ours: not captured.
func RegisterTreasure(r *gateway.Router, st *store.Store) {
	rng := newRng
	type ctx struct {
		role *store.Role
		d    *trRole
	}
	load := func(s *gateway.Session) (*ctx, error) {
		role := s.Role()
		if role == nil {
			return nil, nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		c := &ctx{role: role, d: &trRole{}}
		if _, err := st.LoadState(role.RID, trModule, c.d); err != nil {
			return nil, err
		}
		day := dayKey(s.Now())
		for i := range c.d.Wheels {
			w := &c.d.Wheels[i]
			if w.Day != day || len(w.Slots) == 0 {
				trRoll(w, int64(i+1), rng())
				w.Day = day
			}
		}
		return c, nil
	}
	wheelMsg := func(c *ctx, typ int64) proto.Message {
		w := c.d.Wheels[typ-1]
		slots := []proto.Message{}
		for _, sl := range w.Slots {
			slots = append(slots, proto.Message{"pos": sl.Pos, "id": sl.ID, "status": sl.Status})
		}
		claimed := []proto.Message{}
		for _, id := range w.Claimed {
			claimed = append(claimed, proto.Message{"lucky": id})
		}
		return proto.Message{"type": typ, "count": w.Count, "lucky": w.Lucky, "frist_type": 0, "lucky_award": claimed, "end_time": w.EndTime, "rand_lists": slots}
	}
	logOf := func(srv string, typ int64) ([]proto.Message, error) {
		w := &trWorld{}
		if _, err := st.LoadGlobal(srv, trModule, w); err != nil {
			return nil, err
		}
		out := []proto.Message{}
		for _, l := range w.Log[typ-1] {
			out = append(out, proto.Message{"role_name": l.Name, "bid": l.Bid, "num": l.Num})
		}
		return out, nil
	}
	r.Handle(16637, func(s *gateway.Session, _ proto.Message) error {
		trMu.Lock()
		defer trMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		if err := st.SaveState(c.role.RID, trModule, c.d); err != nil {
			return err
		}
		list := []proto.Message{}
		for typ := int64(1); typ <= 2; typ++ {
			m := wheelMsg(c, typ)
			if m["log_list"], err = logOf(c.role.SrvID, typ); err != nil {
				return err
			}
			list = append(list, m)
		}
		return s.Send(16637, proto.Message{"dial_data": list})
	})
	// draw is 16638 and 16643: the same rules, the answer goes to the wheel or to the window of the prizes
	draw := func(cmd uint16) gateway.Handler {
		return func(s *gateway.Session, req proto.Message) error {
			trMu.Lock()
			defer trMu.Unlock()
			c, err := load(s)
			if c == nil {
				return err
			}
			typ, typ2 := req.Int("type"), req.Int("type2")
			refuse := func(msg string) error { return s.Send(cmd, proto.Message{"code": 0, "msg": msg}) }
			opt := -1
			for i, o := range trCfg.Options {
				if o.Type == typ && o.Type2 == typ2 {
					opt = i
				}
			}
			if opt < 0 {
				return refuse("探宝方式不存在")
			}
			o := trCfg.Options[opt]
			switch {
			case int64(c.role.Lev) < o.Lev:
				return refuse("人物等级" + itoa64(o.Lev) + "级开启")
			case int64(c.role.VIPLev) < o.Vip:
				return refuse("VIP" + itoa64(o.Vip) + "开启")
			}
			w := &c.d.Wheels[typ-1]
			var free []int
			for i, sl := range w.Slots {
				if sl.Status == 0 {
					free = append(free, i)
				}
			}
			if len(free) == 0 {
				return refuse("请先刷新探宝奖励")
			}
			spendA, spendI := map[int64]int64{}, map[int64]int64{}
			for _, p := range o.Expend {
				splitGains([]sellGain{{p[0], p[1]}}, 1, spendA, spendI)
			}
			gainA, gainI := map[int64]int64{}, map[int64]int64{}
			g := rng()
			// the first draw lands on a position of the wheel, the others of a multi draw come from the pool
			slot := &w.Slots[free[g.Intn(len(free))]]
			won := trItemByID(slot.ID)
			slot.Status = 1
			awards1 := []proto.Message{{"bid": won.Item, "num": won.Num}}
			splitGains([]sellGain{{won.Item, won.Num}}, 1, gainA, gainI)
			picked := map[int64]int64{}
			var order []int64
			for i := int64(1); i < o.Count; i++ {
				bid := trPick(trPool(typ, int64(c.role.Lev)), g)
				if picked[bid] == 0 {
					order = append(order, bid)
				}
				picked[bid]++
			}
			awards2 := []proto.Message{}
			for _, bid := range order {
				awards2 = append(awards2, proto.Message{"bid": bid, "num": picked[bid]})
				splitGains([]sellGain{{bid, picked[bid]}}, 1, gainA, gainI)
			}
			for _, p := range o.AddItem {
				awards2 = append(awards2, proto.Message{"bid": p[0], "num": p[1]})
				splitGains([]sellGain{{p[0], p[1]}}, 1, gainA, gainI)
			}
			if err := st.Exchange(c.role.RID, spendA, spendI, gainA, gainI); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return refuse("探宝道具不足")
				}
				return err
			}
			w.Count += o.Count
			w.Lucky += o.AddLucky
			if err := st.SaveState(c.role.RID, trModule, c.d); err != nil {
				return err
			}
			world := &trWorld{}
			if _, err := st.LoadGlobal(c.role.SrvID, trModule, world); err != nil {
				return err
			}
			world.Log[typ-1] = append([]trLog{{Name: c.role.Name, Bid: won.Item, Num: won.Num}}, world.Log[typ-1]...)
			if len(world.Log[typ-1]) > trLogMax {
				world.Log[typ-1] = world.Log[typ-1][:trLogMax]
			}
			if err := st.SaveGlobal(c.role.SrvID, trModule, world); err != nil {
				return err
			}
			if err := s.Send(cmd, proto.Message{"code": 1, "msg": "", "frist_awards": []proto.Message{}, "awards1": awards1, "awards2": awards2,
				"awards3": []proto.Message{{"pos": slot.Pos}}}); err != nil {
				return err
			}
			if err := s.Send(16639, wheelMsg(c, typ)); err != nil {
				return err
			}
			if log, err := logOf(c.role.SrvID, typ); err != nil {
				return err
			} else if err := s.Send(16641, proto.Message{"type": typ, "log_list": log}); err != nil {
				return err
			}
			if err := sendAssets(s, st, c.role.RID); err != nil {
				return err
			}
			return sendBackpack(s, st)
		}
	}
	r.Handle(16638, draw(16638))
	r.Handle(16643, draw(16643))
	r.Handle(16640, func(s *gateway.Session, req proto.Message) error {
		trMu.Lock()
		defer trMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		typ, id := req.Int("type"), req.Int("id")
		refuse := func(msg string) error { return s.Send(16640, proto.Message{"code": 0, "msg": msg, "type": typ}) }
		if typ < 1 || typ > 2 {
			return refuse("探宝类型错误")
		}
		w := &c.d.Wheels[typ-1]
		idx := -1
		for i, l := range trCfg.Lucky {
			if l.ID == id && l.Type == typ {
				idx = i
			}
		}
		switch {
		case idx < 0:
			return refuse("幸运奖励不存在")
		case has(w.Claimed, id):
			return refuse("已经领取过了")
		case w.Lucky < trCfg.Lucky[idx].Val:
			return refuse("幸运值不足")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, p := range trCfg.Lucky[idx].Award {
			splitGains([]sellGain{{p[0], p[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		w.Claimed = append(w.Claimed, id)
		// all five taken at the top value: the lucky value starts again
		all := true
		for _, l := range trCfg.Lucky {
			if l.Type == typ && !has(w.Claimed, l.ID) {
				all = false
			}
		}
		if all {
			w.Lucky, w.Claimed = 0, nil
		}
		if err := st.SaveState(c.role.RID, trModule, c.d); err != nil {
			return err
		}
		if err := s.Send(16640, proto.Message{"code": 1, "msg": "领取成功", "type": typ}); err != nil {
			return err
		}
		if err := s.Send(16639, wheelMsg(c, typ)); err != nil {
			return err
		}
		if err := sendAssets(s, st, c.role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
	r.Handle(16642, func(s *gateway.Session, req proto.Message) error {
		trMu.Lock()
		defer trMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		typ := req.Int("type")
		refuse := func(msg string) error { return s.Send(16642, proto.Message{"code": 0, "msg": msg, "type": typ}) }
		if typ < 1 || typ > 2 {
			return refuse("探宝类型错误")
		}
		w := &c.d.Wheels[typ-1]
		now := s.Now().Unix()
		if now < w.EndTime {
			// not free yet: the diamonds
			spendA, spendI := map[int64]int64{}, map[int64]int64{}
			splitGains([]sellGain{{trCfg.Refresh[0], trCfg.Refresh[1]}}, 1, spendA, spendI)
			if err := st.Exchange(c.role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return refuse("钻石不足")
				}
				return err
			}
		} else {
			w.EndTime = now + trCfg.FreeTime
		}
		trRoll(w, typ, rng())
		if err := st.SaveState(c.role.RID, trModule, c.d); err != nil {
			return err
		}
		if err := s.Send(16642, proto.Message{"code": 1, "msg": "", "type": typ}); err != nil {
			return err
		}
		if err := s.Send(16639, wheelMsg(c, typ)); err != nil {
			return err
		}
		return sendAssets(s, st, c.role.RID)
	})
}
