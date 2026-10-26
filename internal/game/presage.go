package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// presage.json: the presage summon (预知召唤, the pine cone treasure) of the client (tools/extract_presage.py).
//
//go:embed data/presage.json
var presageJSON []byte

const (
	presageModule = "presage"
	presageGold   = 3
	presageEnd    = 1900079999 // the event is always open, like the elite summon beside it
	// presageGoalChance is the chance in percent that a position holds a prize of the shown best list (is_goal).
	presageGoalChance = 25
)

type presageSlot struct {
	ID   int64 `json:"id"`
	Goal int   `json:"goal"`
	Item int64 `json:"item"`
	Num  int64 `json:"num"`
}

type presageConf struct {
	Const map[string]json.RawMessage `json:"const"`
	Prize map[string]struct {
		Item [2]int64 `json:"item"`
		Gold [2]int64 `json:"gold"`
	} `json:"prize"`
	Pools map[string][]presageSlot `json:"pools"`

	coin, price, mustLuxury int64
	free, limit             [3]int64
	refreshCost             int64
	byID                    map[int64]presageSlot
}

var presageCfg = func() *presageConf {
	c := &presageConf{}
	if err := json.Unmarshal(presageJSON, c); err != nil || len(c.Pools["1"]) == 0 || len(c.Pools["2"]) == 0 {
		panic("game: bad presage.json")
	}
	num := func(k string) int64 {
		var v int64
		if err := json.Unmarshal(c.Const[k], &v); err != nil {
			panic("game: bad presage.json " + k)
		}
		return v
	}
	pair := func(k string) [][2]int64 {
		var v [][2]int64
		if err := json.Unmarshal(c.Const[k], &v); err != nil || len(v) == 0 {
			panic("game: bad presage.json " + k)
		}
		return v
	}
	c.coin, c.price, c.mustLuxury = num("item1"), pair("item_price")[0][1], num("must_num_2")
	c.free, c.limit = [3]int64{0, num("free_time1"), num("free_time2")}, [3]int64{0, num("buy_time1"), num("buy_time2")}
	c.refreshCost = pair("refresh")[0][1]
	c.byID = map[int64]presageSlot{}
	for k, pool := range c.Pools {
		kept := pool[:0]
		for _, p := range pool {
			if ItemKnown(p.Item) { // a few prizes of the table are items the client has no row for
				kept = append(kept, p)
				c.byID[p.ID] = p
			}
		}
		c.Pools[k] = kept
	}
	return c
}()

type presageKind struct {
	Count  int64   // refreshes so far, the pity of the luxury pine counts them
	Ref    int64   // refreshes today
	Buy    int64   // results bought in the event
	Opened bool    // the pine cones are peeled
	Bought bool    // the shown result is bought
	Rand   []int64 // the prize position ids of the three pine cones
}

type presageState struct {
	Day   int64
	Kinds [3]presageKind
}

type presageSvc struct{ st *store.Store }

// RegisterPresage registers the presage summon (16690-16694): two kinds of pine cones (lucky and luxury), each hiding
// three prizes. The prizes are dealt in advance and a closed cone shows only whether it is a best one; one peels them
// (free once a day, then the refresh asks one pine coin or diamonds, and the result is dealt again), buys the shown
// three for pine coins topped up with diamonds, and buys pine coins for diamonds. Refreshes reset every day at 0:00.
// Our rules, not captured: the table has no chances, so a position is a best prize with presageGoalChance percent;
// every fortieth luxury result holds at least two of them.
func RegisterPresage(r *gateway.Router, st *store.Store) {
	p := &presageSvc{st: st}
	r.Handle(16690, p.info)
	r.Handle(16691, p.refresh)
	r.Handle(16692, p.buyCoin)
	r.Handle(16693, p.buy)
	r.Handle(16694, p.open)
}

// presageDeal deals the three prizes of a kind; the result at the pity count of a luxury pine holds at least two best ones.
func presageDeal(kind int, count int64, n func(int) int) []int64 {
	pool := presageCfg.Pools[string(rune('0'+kind))]
	var goal, plain []presageSlot
	for _, s := range pool {
		if s.Goal == 1 {
			goal = append(goal, s)
		} else {
			plain = append(plain, s)
		}
	}
	need := 0
	if kind == 2 && presageCfg.mustLuxury > 0 && count%presageCfg.mustLuxury == presageCfg.mustLuxury-1 {
		need = 2
	}
	out := make([]int64, 0, 3)
	taken := map[int64]bool{}
	pick := func(from []presageSlot) {
		for i := 0; i < 20; i++ {
			s := from[n(len(from))]
			if !taken[s.ID] {
				taken[s.ID] = true
				out = append(out, s.ID)
				return
			}
		}
		s := from[n(len(from))]
		out = append(out, s.ID)
	}
	for len(out) < 3 {
		switch {
		case len(out) < need:
			pick(goal)
		case len(goal) > 0 && n(100) < presageGoalChance, len(plain) == 0:
			pick(goal)
		default:
			pick(plain)
		}
	}
	// the cones' places are shuffled so the best ones of a forced result do not come first
	for i := len(out) - 1; i > 0; i-- {
		j := n(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (k *presageKind) lists() []proto.Message {
	out := []proto.Message{}
	for i, id := range k.Rand {
		out = append(out, proto.Message{"pos": i + 1, "id": id})
	}
	return out
}

func (k *presageKind) flag() int {
	if k.Bought {
		return 1
	}
	return 0
}

func (p *presageSvc) with(s *gateway.Session, fn func(role *store.Role, st *presageState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	st := &presageState{}
	if _, err := p.st.LoadState(role.RID, presageModule, st); err != nil {
		return err
	}
	n := rand.New(rand.NewSource(s.Now().UnixNano())).Intn
	day := dayKey(s.Now())
	for kind := 1; kind <= 2; kind++ {
		k := &st.Kinds[kind]
		if st.Day != day || len(k.Rand) != 3 {
			k.Ref, k.Opened, k.Bought = 0, false, false
			k.Rand = presageDeal(kind, k.Count, n)
		}
	}
	st.Day = day
	if err := fn(role, st); err != nil {
		return err
	}
	return p.st.SaveState(role.RID, presageModule, st)
}

func presageReply(s *gateway.Session, cmd uint16, ok bool, msg string, extra proto.Message) error {
	m := proto.Message{"code": 0, "msg": msg}
	if ok {
		m["code"] = 1
	}
	for k, v := range extra {
		m[k] = v
	}
	return s.Send(cmd, m)
}

// info (16690)
func (p *presageSvc) info(s *gateway.Session, _ proto.Message) error {
	return p.with(s, func(_ *store.Role, st *presageState) error {
		data := []proto.Message{}
		for kind := 1; kind <= 2; kind++ {
			k := &st.Kinds[kind]
			data = append(data, proto.Message{"type": kind, "count": k.Count, "rand_lists": k.lists(), "flag": k.flag()})
		}
		status := func(k presageKind) int {
			if k.Opened {
				return 1
			}
			return 0
		}
		left := presageEnd - s.Now().Unix()
		if left < 0 {
			left = 0
		}
		return s.Send(16690, proto.Message{"predict_data": data, "time": left,
			"ref_1": st.Kinds[1].Ref, "ref_2": st.Kinds[2].Ref, "buy_1": st.Kinds[1].Buy, "buy_2": st.Kinds[2].Buy,
			"status_1": status(st.Kinds[1]), "status_2": status(st.Kinds[2])})
	})
}

func presageKindOf(req proto.Message) int {
	if k := int(req.Int("type")); k == 1 || k == 2 {
		return k
	}
	return 0
}

func (p *presageSvc) sendBag(s *gateway.Session, rid int64) error {
	if err := sendAssets(s, p.st, rid); err != nil {
		return err
	}
	return sendBackpack(s, p.st)
}

// open (16694) peels the pine cones: the dealt prizes are shown.
func (p *presageSvc) open(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(_ *store.Role, st *presageState) error {
		kind := presageKindOf(req)
		if kind == 0 {
			return presageReply(s, 16694, false, "松果不存在", nil)
		}
		if st.Kinds[kind].Opened {
			return presageReply(s, 16694, false, "松果已经打开", proto.Message{"type": kind})
		}
		st.Kinds[kind].Opened = true
		return presageReply(s, 16694, true, "", proto.Message{"type": kind})
	})
}

// presagePay pays a price of coins with the coins one has and diamonds for the rest (the price in diamonds is gold).
func presagePay(have, coins, gold int64) (spendItems, spendAssets map[int64]int64) {
	spendItems, spendAssets = map[int64]int64{}, map[int64]int64{}
	if have >= coins {
		spendItems[presageCfg.coin] = coins
		return
	}
	if have > 0 {
		spendItems[presageCfg.coin] = have
		gold -= presageCfg.price * have
	}
	spendAssets[presageGold] = gold
	return
}

// refresh (16691) peels the cones again: the free ones first, then a pine coin, then diamonds.
func (p *presageSvc) refresh(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *presageState) error {
		kind := presageKindOf(req)
		if kind == 0 {
			return presageReply(s, 16691, false, "松果不存在", nil)
		}
		k := &st.Kinds[kind]
		if !k.Opened {
			return presageReply(s, 16691, false, "请先打开松果", nil)
		}
		if k.Ref >= presageCfg.free[kind] {
			items, err := p.st.RoleItems(role.RID)
			if err != nil {
				return err
			}
			spendItems, spendAssets := presagePay(items[presageCfg.coin], presageCfg.refreshCost, presageCfg.price*presageCfg.refreshCost)
			if err := p.st.Exchange(role.RID, spendAssets, spendItems, map[int64]int64{}, map[int64]int64{}); err != nil {
				return presageReply(s, 16691, false, "钻石不足", nil)
			}
			if err := p.sendBag(s, role.RID); err != nil {
				return err
			}
		}
		k.Count++
		k.Ref++
		k.Bought = false
		k.Rand = presageDeal(kind, k.Count, rand.New(rand.NewSource(s.Now().UnixNano())).Intn)
		return presageReply(s, 16691, true, "", proto.Message{"type1": kind, "type": kind, "count": k.Count,
			"rand_lists": k.lists(), "flag": 0, "ref_count": k.Ref})
	})
}

// buy (16693) takes the shown three prizes.
func (p *presageSvc) buy(s *gateway.Session, req proto.Message) error {
	return p.with(s, func(role *store.Role, st *presageState) error {
		kind := presageKindOf(req)
		if kind == 0 {
			return presageReply(s, 16693, false, "松果不存在", nil)
		}
		k := &st.Kinds[kind]
		switch {
		case !k.Opened:
			return presageReply(s, 16693, false, "请先打开松果", nil)
		case k.Bought:
			return presageReply(s, 16693, false, "已经购买过当前宝藏", nil)
		case k.Buy >= presageCfg.limit[kind]:
			return presageReply(s, 16693, false, "活动购买次数已满", nil)
		}
		price := presageCfg.Prize[string(rune('0'+kind))]
		items, err := p.st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		spendItems, spendAssets := presagePay(items[presageCfg.coin], price.Item[1], price.Gold[1])
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		for _, id := range k.Rand {
			g := presageCfg.byID[id]
			splitGains([]sellGain{{g.Item, g.Num}}, 1, gainA, gainI)
		}
		if err := p.st.Exchange(role.RID, spendAssets, spendItems, gainA, gainI); err != nil {
			return presageReply(s, 16693, false, "钻石不足", nil)
		}
		k.Buy++
		k.Bought = true
		if err := p.sendBag(s, role.RID); err != nil {
			return err
		}
		return presageReply(s, 16693, true, "购买成功", proto.Message{"type1": kind, "type": kind, "count": k.Count,
			"rand_lists": k.lists(), "flag": 1, "ref_count": k.Buy})
	})
}

// buyCoin (16692) buys pine coins for diamonds.
func (p *presageSvc) buyCoin(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	num := req.Int("num")
	if num <= 0 || num > 9999 {
		return presageReply(s, 16692, false, "数量不对", nil)
	}
	err := p.st.Exchange(role.RID, map[int64]int64{presageGold: num * presageCfg.price}, map[int64]int64{},
		map[int64]int64{}, map[int64]int64{presageCfg.coin: num})
	if err != nil {
		return presageReply(s, 16692, false, "钻石不足", nil)
	}
	if err := p.sendBag(s, role.RID); err != nil {
		return err
	}
	return presageReply(s, 16692, true, "购买成功", nil)
}
