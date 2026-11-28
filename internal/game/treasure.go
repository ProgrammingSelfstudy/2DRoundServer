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

var trItems = func() map[int64]trItem {
	items := make(map[int64]trItem, len(trCfg.Items))
	for _, item := range trCfg.Items {
		items[item.ID] = item
	}
	return items
}()

// The original table lists a full pass over positions 1–8 for each level bracket.
var trBoards = func() [2][][]trItem {
	var boards [2][][]trItem
	var last [2]int64
	for _, it := range trCfg.Items {
		groups := &boards[it.Type-1]
		if len(*groups) == 0 || it.Pos < last[it.Type-1] {
			*groups = append(*groups, nil)
		}
		last[it.Type-1] = it.Pos
		i := len(*groups) - 1
		(*groups)[i] = append((*groups)[i], it)
	}
	return boards
}()

func trTier(typ, lev int64) int {
	pools := trCfg.Pools[itoa64(typ)]
	for i, p := range pools {
		if lev <= p.Max {
			return i
		}
	}
	return len(pools) - 1
}

func trRoll(w *trWheel, typ, lev int64, rng *rand.Rand) {
	board := trBoards[typ-1][trTier(typ, lev)]
	w.Slots = w.Slots[:0]
	for pos := int64(1); pos <= trCfg.Slots[typ-1]; pos++ {
		var choices []trItem
		for _, it := range board {
			if it.Pos == pos {
				choices = append(choices, it)
			}
		}
		w.Slots = append(w.Slots, trSlot{Pos: pos, ID: choices[rng.Intn(len(choices))].ID})
	}
}

// Select among the actual prizes on this wheel. The level table publishes item
// weights, but omits some variants. Those keep a unit weight. The capture shows
// special (odd) positions graying out and ordinary (even) positions repeating.
func trDraw(w *trWheel, weights map[int64]float64, rng *rand.Rand) (trItem, bool) {
	var total float64
	for _, sl := range w.Slots {
		if sl.Status == 0 {
			total += weights[sl.ID]
		}
	}
	if total == 0 {
		return trItem{}, false
	}
	n := rng.Float64() * total
	for i := range w.Slots {
		sl := &w.Slots[i]
		if sl.Status != 0 {
			continue
		}
		n -= weights[sl.ID]
		if n < 0 {
			if sl.Pos%2 == 1 {
				sl.Status = 1
			}
			return trItems[sl.ID], true
		}
	}
	return trItem{}, false
}

// RegisterTreasure serves both wheels and their lucky gifts.
func RegisterTreasure(r *gateway.Router, st *store.Store) {
	bindAdminGlobal(st, trModule, &trMu, nil)
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
				trRoll(w, int64(i+1), int64(c.role.Lev), rng())
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
			spendA, spendI := map[int64]int64{}, map[int64]int64{}
			for _, p := range o.Expend {
				splitGains([]sellGain{{p[0], p[1]}}, 1, spendA, spendI)
			}
			gainA, gainI := map[int64]int64{}, map[int64]int64{}
			pool := trCfg.Pools[itoa64(typ)][trTier(typ, int64(c.role.Lev))].Award
			weights := make(map[int64]float64, len(w.Slots))
			for _, sl := range w.Slots {
				weights[sl.ID] = 1
				for _, pair := range pool {
					if int64(pair[0]) == trItems[sl.ID].Item {
						weights[sl.ID] = pair[1]
						break
					}
				}
			}
			g := rng()
			var awards1, awards2, positions []proto.Message
			var logPrize trItem
			for i := int64(0); i < o.Count; i++ {
				won, ok := trDraw(w, weights, g)
				if !ok {
					return refuse("请先刷新探宝奖励")
				}
				awards2 = append(awards2, proto.Message{"bid": won.Item, "num": won.Num})
				positions = append(positions, proto.Message{"pos": won.Pos})
				splitGains([]sellGain{{won.Item, won.Num}}, 1, gainA, gainI)
				if i == 0 || won.Pos%2 == 1 {
					logPrize = won
				}
			}
			for _, pair := range o.AddItem {
				awards1 = append(awards1, proto.Message{"bid": pair[0], "num": pair[1]})
				splitGains([]sellGain{{pair[0], pair[1]}}, 1, gainA, gainI)
			}
			w.Count += o.Count
			w.Lucky += o.AddLucky
			err = st.ExchangeAndSave(c.role.RID, spendA, spendI, gainA, gainI, func(tx *store.Store) error {
				if err := tx.SaveState(c.role.RID, trModule, c.d); err != nil {
					return err
				}
				world := &trWorld{}
				if _, err := tx.LoadGlobal(c.role.SrvID, trModule, world); err != nil {
					return err
				}
				log := append([]trLog{{Name: c.role.Name, Bid: logPrize.Item, Num: logPrize.Num}}, world.Log[typ-1]...)
				world.Log[typ-1] = log[:min(len(log), trLogMax)]
				return tx.SaveGlobal(c.role.SrvID, trModule, world)
			})
			if errors.Is(err, store.ErrConflict) {
				return refuse("探宝道具不足")
			}
			if err != nil {
				return err
			}
			if err := s.Send(cmd, proto.Message{"code": 1, "msg": "", "frist_awards": []proto.Message{}, "awards1": awards1, "awards2": awards2,
				"awards3": positions}); err != nil {
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
		if err := st.ExchangeAndSave(c.role.RID, nil, nil, ga, gi, func(tx *store.Store) error { return tx.SaveState(c.role.RID, trModule, c.d) }); err != nil {
			return err
		}
		if err := sendGainList(s, guildGains(trCfg.Lucky[idx].Award)); err != nil {
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
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		if now < w.EndTime {
			splitGains([]sellGain{{trCfg.Refresh[0], trCfg.Refresh[1]}}, 1, spendA, spendI)
		} else {
			w.EndTime = now + trCfg.FreeTime
		}
		trRoll(w, typ, int64(c.role.Lev), rng())
		err = st.ExchangeAndSave(c.role.RID, spendA, spendI, nil, nil, func(tx *store.Store) error { return tx.SaveState(c.role.RID, trModule, c.d) })
		if errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足")
		}
		if err != nil {
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

// Register the persisted state without loading or changing gameplay data.
func init() {
	registerAdminModule(AdminModule{Key: trModule, Title: "幸运/高级探宝", Window: "16637–16643", Scope: "global", View: adminGlobalView, New: adminState[trWorld]})
	registerAdminModule(AdminModule{Key: trModule, Title: "幸运/高级探宝", Window: "16637–16643", Scope: "role", New: adminState[trRole]})
}
