package game

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// voyage.json: the voyage orders of the client (tools/extract_voyage.py).
//
//go:embed data/voyage.json
var voyageJSON []byte

const (
	voyageModule  = "voyage"
	voyageDiamond = 3

	voyageUnget    = 1
	voyageUnderway = 2
	voyageFinish   = 3
)

type voyageConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Orders []struct {
		ID      int64      `json:"id"`
		Limit   int        `json:"limit"`
		Time    int64      `json:"time"`
		Quality int        `json:"quality"`
		Award   [][2]int64 `json:"award"`
		Cond    []int64    `json:"cond"`
		Expend  [][2]int64 `json:"expend"`
	} `json:"orders"`
	Conds   map[string][]json.RawMessage     `json:"conds"`
	Quick   []struct{ Min, Max, Gold int64 } `json:"quick"`
	Refresh []struct {
		Vip, All, Free int
		Expend         [][2]int64
	} `json:"refresh"`
}

var voyageCfg = func() *voyageConf {
	c := &voyageConf{}
	if err := json.Unmarshal(voyageJSON, c); err != nil || len(c.Orders) == 0 || len(c.Refresh) == 0 {
		panic("game: bad voyage.json")
	}
	return c
}()

func (c *voyageConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *voyageConf) order(bid int64) (i int, ok bool) {
	for i := range c.Orders {
		if c.Orders[i].ID == bid {
			return i, true
		}
	}
	return 0, false
}

type voyageOrder struct {
	ID     int64
	Bid    int64
	Status int
	End    int64
	Assign []int64
}

type voyageState struct {
	Day    int64
	Free   int64 // free refreshes used today
	Coin   int64 // refreshes for diamonds used today
	NextID int64
	Fresh  bool // the first refresh of the role is done
	Orders []voyageOrder
}

type voyageSvc struct{ st *store.Store }

// RegisterVoyage registers the voyage (23800-23806): up to six shipping orders a day, each taken by sending heroes that
// meet its star or camp conditions and paid with coins, finished after a time (or at once for diamonds) and then
// collected; the orders are refreshed with a free refresh, an item or diamonds. Our rules, not captured.
func RegisterVoyage(r *gateway.Router, db *store.Store) {
	v := &voyageSvc{st: db}
	r.Handle(23800, v.info)
	r.Handle(23802, v.receive)
	r.Handle(23803, v.finish)
	r.Handle(23804, v.refresh)
	r.Handle(23805, v.activity)
	r.Handle(23806, v.quick)
}

// deal fills the order list with new orders up to the limit, none twice; the first time the ones of the client's
// "first refresh" are among them.
func (st *voyageState) deal(n func(int) int, max int, first bool) {
	have := map[int64]bool{}
	for _, o := range st.Orders {
		have[o.Bid] = true
	}
	add := func(bid int64) {
		have[bid] = true
		st.NextID++
		st.Orders = append(st.Orders, voyageOrder{ID: st.NextID, Bid: bid, Status: voyageUnget})
	}
	if first {
		var fixed []int64
		_ = json.Unmarshal(voyageCfg.Const["first_refresh"], &fixed)
		for _, bid := range fixed {
			if _, ok := voyageCfg.order(bid); ok && !have[bid] && len(st.Orders) < max {
				add(bid)
			}
		}
	}
	for len(st.Orders) < max && len(have) < len(voyageCfg.Orders) {
		bid := voyageCfg.Orders[n(len(voyageCfg.Orders))].ID
		if !have[bid] {
			add(bid)
		}
	}
}

// settle turns the orders whose time is up into finished ones.
func (st *voyageState) settle(now int64) {
	for i := range st.Orders {
		if o := &st.Orders[i]; o.Status == voyageUnderway && o.End <= now {
			o.Status = voyageFinish
		}
	}
}

func (st *voyageState) find(id int64) int {
	for i, o := range st.Orders {
		if o.ID == id {
			return i
		}
	}
	return -1
}

func (v *voyageSvc) with(s *gateway.Session, fn func(role *store.Role, st *voyageState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := v.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &voyageState{}
	if _, err := v.st.LoadState(role.RID, voyageModule, st); err != nil {
		return err
	}
	if day := dayKey(s.Now()); st.Day != day {
		st.Day, st.Free, st.Coin = day, 0, 0
		st.deal(newRng().Intn, int(voyageCfg.num("max_quest")), !st.Fresh)
		st.Fresh = true
	}
	st.settle(s.Now().Unix())
	if err := fn(role, st); err != nil {
		return err
	}
	return v.st.SaveState(role.RID, voyageModule, st)
}

func voyageReply(s *gateway.Session, cmd uint16, ok bool, msg string, extra proto.Message) error {
	m := proto.Message{"flag": int64(0), "msg": msg}
	if ok {
		m["flag"] = int64(1)
	}
	for k, x := range extra {
		m[k] = x
	}
	return s.Send(cmd, m)
}

func (o voyageOrder) message() proto.Message {
	ids := []proto.Message{}
	for _, p := range o.Assign {
		ids = append(ids, proto.Message{"partner_id": p})
	}
	return proto.Message{"order_id": o.ID, "order_bid": o.Bid, "status": int64(o.Status), "end_time": o.End, "assign_ids": ids}
}

func (v *voyageSvc) sendInfo(s *gateway.Session, st *voyageState) error {
	list := []proto.Message{}
	for _, o := range st.Orders {
		list = append(list, o.message())
	}
	return s.Send(23800, proto.Message{"order_list": list, "free_times": st.Free, "coin_times": st.Coin})
}

func (v *voyageSvc) info(s *gateway.Session, _ proto.Message) error {
	return v.with(s, func(_ *store.Role, st *voyageState) error { return v.sendInfo(s, st) })
}

// activity (23805): the discount of the information activity is never on.
func (v *voyageSvc) activity(s *gateway.Session, _ proto.Message) error {
	return s.Send(23805, proto.Message{"flag": int64(0)})
}

// meets checks the heroes against the conditions of the order: a number of heroes of at least a star or of a camp.
func (v *voyageSvc) meets(cond []int64, heroes []store.Partner) bool {
	for _, id := range cond {
		c, ok := voyageCfg.Conds[fmt.Sprint(id)]
		if !ok || len(c) < 3 {
			continue
		}
		var kind string
		var val, need int64
		_ = json.Unmarshal(c[0], &kind)
		_ = json.Unmarshal(c[1], &val)
		_ = json.Unmarshal(c[2], &need)
		var n int64
		for _, h := range heroes {
			if kind == "partner_star" && int64(h.Star) >= val || kind == "partner_camp_type" && int64(partnerTable[h.BID].Camp) == val {
				n++
			}
		}
		if n < need {
			return false
		}
	}
	return true
}

// receive (23802): take an order with the heroes sent.
func (v *voyageSvc) receive(s *gateway.Session, req proto.Message) error {
	return v.with(s, func(role *store.Role, st *voyageState) error {
		i := st.find(req.Int("order_id"))
		if i < 0 || st.Orders[i].Status != voyageUnget {
			return voyageReply(s, 23802, false, "订单不存在", nil)
		}
		o := &st.Orders[i]
		oc, _ := voyageCfg.order(o.Bid)
		conf := voyageCfg.Orders[oc]
		busy := map[int64]bool{}
		for _, x := range st.Orders {
			for _, p := range x.Assign {
				busy[p] = true
			}
		}
		own, err := v.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		byID := map[int64]store.Partner{}
		for _, p := range own {
			byID[p.PartnerID] = p
		}
		var sent []store.Partner
		var ids []int64
		for _, a := range req.Arr("assign_ids") {
			p, ok := byID[a.Int("partner_id")]
			if !ok || busy[p.PartnerID] {
				return voyageReply(s, 23802, false, "英雄不可用", nil)
			}
			busy[p.PartnerID] = true
			sent, ids = append(sent, p), append(ids, p.PartnerID)
		}
		if len(sent) == 0 || len(sent) > conf.Limit {
			return voyageReply(s, 23802, false, "派遣英雄数量不对", nil)
		}
		if !v.meets(conf.Cond, sent) {
			return voyageReply(s, 23802, false, "不满足派遣条件", nil)
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, e := range conf.Expend {
			splitGains([]sellGain{{e[0], e[1]}}, 1, assets, items)
		}
		if err := v.st.Exchange(role.RID, assets, items, map[int64]int64{}, map[int64]int64{}); err != nil {
			return voyageReply(s, 23802, false, "材料不足", nil)
		}
		o.Status, o.End, o.Assign = voyageUnderway, s.Now().Unix()+conf.Time, ids
		if err := sendAssets(s, v.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, v.st); err != nil {
			return err
		}
		if err := voyageReply(s, 23802, true, "接取成功", nil); err != nil {
			return err
		}
		return s.Send(23801, o.message())
	})
}

// pay hands out the award of the finished order at index i and removes it.
func (v *voyageSvc) pay(role *store.Role, st *voyageState, i int) error {
	oc, _ := voyageCfg.order(st.Orders[i].Bid)
	assets, items := map[int64]int64{}, map[int64]int64{}
	for _, a := range voyageCfg.Orders[oc].Award {
		splitGains([]sellGain{{a[0], a[1]}}, 1, assets, items)
	}
	if err := v.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
		return err
	}
	st.Orders = append(st.Orders[:i], st.Orders[i+1:]...)
	return nil
}

// finish (23803): type 1 speeds an order under way up for diamonds (by the time left), type 0 collects a finished one.
func (v *voyageSvc) finish(s *gateway.Session, req proto.Message) error {
	return v.with(s, func(role *store.Role, st *voyageState) error {
		id := req.Int("order_id")
		i := st.find(id)
		if i < 0 {
			return voyageReply(s, 23803, false, "订单不存在", nil)
		}
		if req.Int("type") == 1 {
			if st.Orders[i].Status != voyageUnderway {
				return voyageReply(s, 23803, false, "订单不在进行中", nil)
			}
			left := st.Orders[i].End - s.Now().Unix()
			var gold int64
			for _, q := range voyageCfg.Quick {
				if left >= q.Min && left <= q.Max {
					gold = q.Gold
				}
			}
			if err := v.st.Exchange(role.RID, map[int64]int64{voyageDiamond: gold}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
				return voyageReply(s, 23803, false, "钻石不足", nil)
			}
			st.Orders[i].Status = voyageFinish
		} else if st.Orders[i].Status != voyageFinish {
			return voyageReply(s, 23803, false, "订单还没有完成", nil)
		}
		if err := v.pay(role, st, i); err != nil {
			return err
		}
		if err := sendAssets(s, v.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, v.st); err != nil {
			return err
		}
		return voyageReply(s, 23803, true, "领取成功", proto.Message{"order_id": id})
	})
}

// quick (23806): collect every finished order.
func (v *voyageSvc) quick(s *gateway.Session, _ proto.Message) error {
	return v.with(s, func(role *store.Role, st *voyageState) error {
		done := []proto.Message{}
		for i := len(st.Orders) - 1; i >= 0; i-- {
			if st.Orders[i].Status != voyageFinish {
				continue
			}
			done = append(done, proto.Message{"order_id": st.Orders[i].ID})
			if err := v.pay(role, st, i); err != nil {
				return err
			}
		}
		if len(done) == 0 {
			return voyageReply(s, 23806, false, "没有可领取的订单", proto.Message{"order_list": []proto.Message{}})
		}
		if err := sendAssets(s, v.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, v.st); err != nil {
			return err
		}
		return voyageReply(s, 23806, true, "领取成功", proto.Message{"order_list": done})
	})
}

// refresh (23804): new orders in place of the ones not taken. The free ones first, then the item of the client, then
// diamonds up to the limit of the VIP level.
func (v *voyageSvc) refresh(s *gateway.Session, _ proto.Message) error {
	return v.with(s, func(role *store.Role, st *voyageState) error {
		lim := voyageCfg.Refresh[0]
		for _, r := range voyageCfg.Refresh {
			if r.Vip <= role.VIPLev {
				lim = r
			}
		}
		none := map[int64]int64{}
		var paid bool
		if st.Free < int64(lim.Free) {
			st.Free++
			paid = true
		}
		if !paid {
			var cost [][2]int64
			_ = json.Unmarshal(voyageCfg.Const["refresh_cost"], &cost)
			if len(cost) > 0 {
				paid = v.st.Exchange(role.RID, none, map[int64]int64{cost[0][0]: cost[0][1]}, none, none) == nil
			}
		}
		if !paid && st.Coin < int64(lim.All) && len(lim.Expend) > 0 {
			paid = v.st.Exchange(role.RID, map[int64]int64{lim.Expend[0][0]: lim.Expend[0][1]}, none, none, none) == nil
			if paid {
				st.Coin++
			}
		}
		if !paid {
			return voyageReply(s, 23804, false, "刷新次数不足", nil)
		}
		keep := st.Orders[:0:0]
		for _, o := range st.Orders {
			if o.Status != voyageUnget {
				keep = append(keep, o)
			}
		}
		st.Orders = keep
		st.deal(newRng().Intn, int(voyageCfg.num("max_quest")), false)
		if err := sendAssets(s, v.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, v.st); err != nil {
			return err
		}
		if err := voyageReply(s, 23804, true, "刷新成功", nil); err != nil {
			return err
		}
		return v.sendInfo(s, st)
	})
}
