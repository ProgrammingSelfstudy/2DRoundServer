package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// animate.json: the animate action (animate_action 24801~24809) of the client (tools/extract_animate.py): the lantern festival
// lottery and the kitchen.
//
//go:embed data/animate.json
var animateJSON []byte

const (
	animateModule = "animate"
	// lanternCamp is the camp_id the operator gives the lantern festival in the schedule (the client's yuanzhen_adventure,
	// the tab that earns the hero wine); the kitchen runs for the camps of the client's table (51641 and so on).
	lanternCamp = 93018
)

type animatePrize struct {
	Item, Num int64
	Weight    float64
	After     int64
}

type animateRecipe struct {
	ID, Lev, Exp, Limit int64
	Name                string
	Loss, Reward        [][2]int64
}

type animateCamp struct {
	Group   int64
	Recipes []animateRecipe
	Levels  []struct {
		Lev, Exp int64
		Reward   [][2]int64
	}
	Collect struct {
		Award    [][2]int64
		Combat   int64
		BuyLimit int64      `json:"buy_limit"`
		BuyLoss  [][2]int64 `json:"buy_loss"`
	}
}

var animateCfg = func() (out struct {
	Lantern struct {
		Item      int64
		Pool4Cost int64 `json:"pool4_cost"`
		Need      []int64
		Cost      [][]int64
		Pools     [][]animatePrize
	}
	Camps map[string]*animateCamp
}) {
	if err := json.Unmarshal(animateJSON, &out); err != nil || len(out.Camps) == 0 {
		panic("game: bad animate.json")
	}
	return
}()

var animateCamps = func() []int64 {
	var ids []int64
	for k := range animateCfg.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}()

type animatePool struct {
	Count int64
	Drawn []int64 // the indexes (from 1) of the prizes taken; pools 1~3 give each prize once
}

type animateKitchen struct {
	Exp                       int64
	Made                      map[int64]int64
	Claimed                   []int64
	Combat, Buys, Day         int64
	Cooked, Collected, Beaten int64
}

type animateRole struct {
	Start  int64 // the lantern activity's start: a new one begins again
	AllNum int64 // the hero wine spent in pools 1~3
	Pools  [4]animatePool
	Camps  map[int64]*animateKitchen
}

func (k *animateKitchen) level(c *animateCamp) int64 {
	lev := int64(1)
	for _, l := range c.Levels {
		if k.Exp >= l.Exp && l.Lev > lev {
			lev = l.Lev
		}
	}
	return lev
}

// animateOpenCamp is the kitchen camp the operator has opened: the first by id.
func animateOpenCamp(st *store.Store, role *store.Role, now time.Time) (camp, start, end int64) {
	for _, c := range animateCamps {
		if s, e, open := ActivityOpen(st, role.SrvID, c, now); open {
			return c, s, e
		}
	}
	return 0, 0, 0
}

// RegisterAnimate registers the animate action: the lantern festival lottery (24801 the pools, 24802 one pool, 24803 light it:
// one draw) and the kitchen (24804 the page, 24805 the level rewards, 24806 cook, 24807 take a level reward, 24808 the collect
// fight, 24809 buy a fight). The lantern runs while the operator has the activity with camp_id 93018 open, the kitchen for a camp
// of the table. The hero wine (80201) is not paid by any game here: the operator hands it out (an activity aim's reward or mail).
// Ours, not captured: a pool opens when the wine spent in the earlier ones reaches the table's number (the client text and the
// numbers 0 / 100 / 200 / 645 agree), a prize with "must_no_count" N cannot come in the first N draws of its pool, pool 4 never
// ends; the kitchen's collect fight is not played: it takes one of the day's fights and pays the camp's materials at once, a
// bought fight adds one, the exp of a recipe is the table's and the level is the highest whose exp is reached.
func RegisterAnimate(r *gateway.Router, st *store.Store) {
	lan := animateCfg.Lantern
	load := func(role *store.Role) (*animateRole, error) {
		d := &animateRole{}
		if _, err := st.LoadState(role.RID, animateModule, d); err != nil {
			return nil, err
		}
		if d.Camps == nil {
			d.Camps = map[int64]*animateKitchen{}
		}
		return d, nil
	}
	lanternOpen := func(role *store.Role, now time.Time, d *animateRole) bool {
		start, _, open := ActivityOpen(st, role.SrvID, lanternCamp, now)
		if open && d.Start != start {
			*d = animateRole{Start: start, Camps: d.Camps}
		}
		return open
	}
	poolOpen := func(d *animateRole, i int) bool { return d.AllNum >= lan.Need[i] }
	sendWine := func(s *gateway.Session, role *store.Role) error {
		items, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		return s.Send(10350, proto.Message{"holiday_assets": []proto.Message{{"id": lan.Item, "val": items[lan.Item]}}})
	}
	lightState := func(d *animateRole) proto.Message {
		list := []proto.Message{}
		for i := range lan.Pools {
			flag := 0
			if poolOpen(d, i) {
				flag = 1
			}
			list = append(list, proto.Message{"id": i + 1, "flag": flag})
		}
		return proto.Message{"all_num": d.AllNum, "award_list": list}
	}
	poolState := func(d *animateRole, i int) proto.Message {
		list := []proto.Message{}
		for n := range lan.Pools[i] {
			status := 0
			if i < 3 && has(d.Pools[i].Drawn, int64(n+1)) {
				status = 1
			}
			list = append(list, proto.Message{"rand_id": n + 1, "status": status})
		}
		return proto.Message{"flag": 1, "msg": "", "count": d.Pools[i].Count, "rand_list": list}
	}
	r.Handle(24801, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role)
		if err != nil {
			return err
		}
		if !lanternOpen(role, s.Now(), d) {
			return s.Send(24801, proto.Message{"all_num": 0, "award_list": []proto.Message{}})
		}
		if err := sendWine(s, role); err != nil {
			return err
		}
		return s.Send(24801, lightState(d))
	})
	r.Handle(24802, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(24802, proto.Message{"flag": 0, "msg": msg, "count": 0, "rand_list": []proto.Message{}})
		}
		d, err := load(role)
		if err != nil {
			return err
		}
		i := int(req.Int("id")) - 1
		if !lanternOpen(role, s.Now(), d) {
			return refuse("活动未开启")
		}
		if i < 0 || i >= len(lan.Pools) {
			return refuse("花灯不存在")
		}
		if err := sendWine(s, role); err != nil {
			return err
		}
		return s.Send(24802, poolState(d, i))
	})
	r.Handle(24803, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(24803, proto.Message{"flag": 0, "msg": msg}) }
		d, err := load(role)
		if err != nil {
			return err
		}
		i := int(req.Int("id")) - 1
		if !lanternOpen(role, s.Now(), d) {
			return refuse("活动未开启")
		}
		if i < 0 || i >= len(lan.Pools) {
			return refuse("花灯不存在")
		}
		if !poolOpen(d, i) {
			return refuse("花灯尚未开启")
		}
		p := &d.Pools[i]
		cost := lan.Pool4Cost
		var left []int
		for n := range lan.Pools[i] {
			if i == 3 || !has(p.Drawn, int64(n+1)) {
				left = append(left, n)
			}
		}
		if i < 3 {
			if len(left) == 0 {
				return refuse("奖励已全部抽完")
			}
			cost = lan.Cost[i][min(int(p.Count), len(lan.Cost[i])-1)]
		}
		var pick []int
		total := 0.0
		for _, n := range left {
			if pr := lan.Pools[i][n]; p.Count >= pr.After {
				pick = append(pick, n)
				total += pr.Weight
			}
		}
		if len(pick) == 0 {
			pick, total = left, 0
			for _, n := range left {
				total += lan.Pools[i][n].Weight
			}
		}
		x := summonRand() * total
		hit := pick[len(pick)-1]
		for _, n := range pick {
			if x -= lan.Pools[i][n].Weight; x < 0 {
				hit = n
				break
			}
		}
		prize := lan.Pools[i][hit]
		spend, gain := map[int64]int64{}, map[int64]int64{}
		if cost > 0 {
			spend[lan.Item] = cost
		}
		splitGains([]sellGain{{prize.Item, prize.Num}}, 1, map[int64]int64{}, gain)
		if !ItemKnown(prize.Item) {
			gain = map[int64]int64{}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, spend, map[int64]int64{}, gain); err != nil {
			return refuse("英雄酒不足")
		}
		p.Count++
		if i < 3 {
			p.Drawn = append(p.Drawn, int64(hit+1))
			d.AllNum += cost
		}
		if err := st.SaveState(role.RID, animateModule, d); err != nil {
			return err
		}
		if err := s.Send(24803, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := sendWine(s, role); err != nil {
			return err
		}
		if err := s.Send(24802, poolState(d, i)); err != nil {
			return err
		}
		return s.Send(24801, lightState(d))
	})

	// the kitchen
	kitchen := func(s *gateway.Session, role *store.Role) (*animateRole, *animateKitchen, *animateCamp, int64, int64, int64, error) {
		camp, start, end := animateOpenCamp(st, role, s.Now())
		if camp == 0 {
			return nil, nil, nil, 0, 0, 0, nil
		}
		d, err := load(role)
		if err != nil {
			return nil, nil, nil, 0, 0, 0, err
		}
		c := animateCfg.Camps[strconv.FormatInt(camp, 10)]
		k := d.Camps[camp]
		if k == nil {
			k = &animateKitchen{Made: map[int64]int64{}, Combat: c.Collect.Combat, Day: dayKey(s.Now())}
			d.Camps[camp] = k
		}
		if k.Made == nil {
			k.Made = map[int64]int64{}
		}
		if k.Day != dayKey(s.Now()) {
			k.Day, k.Combat, k.Buys = dayKey(s.Now()), c.Collect.Combat, 0
		}
		return d, k, c, camp, start, end, nil
	}
	kitchenState := func(k *animateKitchen, c *animateCamp, camp, start, end int64) proto.Message {
		made := []proto.Message{}
		for _, rc := range c.Recipes {
			made = append(made, proto.Message{"id": rc.ID, "num": k.Made[rc.ID]})
		}
		return proto.Message{"lev": k.level(c), "exp": k.Exp, "camp_id": camp, "group_id": c.Group, "combat_num": k.Combat,
			"start_time": start, "end_time": end,
			"show_list": []proto.Message{{"show_id": 1, "num": k.Cooked}, {"show_id": 2, "num": k.Collected}, {"show_id": 3, "num": k.Beaten}},
			"make_list": made}
	}
	levelState := func(k *animateKitchen, c *animateCamp) proto.Message {
		list := []proto.Message{}
		for _, l := range k.Claimed {
			list = append(list, proto.Message{"id": l})
		}
		return proto.Message{"lev": k.level(c), "reward_list": list}
	}
	push := func(s *gateway.Session, k *animateKitchen, c *animateCamp, camp, start, end int64) error {
		if err := s.Send(24804, kitchenState(k, c, camp, start, end)); err != nil {
			return err
		}
		return s.Send(24805, levelState(k, c))
	}
	// act runs a kitchen request: the common lookup, the refusal and the saving
	act := func(cmd uint16, run func(s *gateway.Session, role *store.Role, req proto.Message, k *animateKitchen, c *animateCamp) (string, error)) {
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			d, k, c, camp, start, end, err := kitchen(s, role)
			if err != nil {
				return err
			}
			if c == nil {
				return s.Send(cmd, proto.Message{"flag": 0, "msg": "活动未开启", "combat_num": 0})
			}
			msg, err := run(s, role, req, k, c)
			if err != nil {
				return err
			}
			if msg != "" {
				return s.Send(cmd, proto.Message{"flag": 0, "msg": msg, "combat_num": k.Combat})
			}
			if err := st.SaveState(role.RID, animateModule, d); err != nil {
				return err
			}
			if err := s.Send(cmd, proto.Message{"flag": 1, "msg": "操作成功", "combat_num": k.Combat}); err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			return push(s, k, c, camp, start, end)
		})
	}
	r.Handle(24804, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, k, c, camp, start, end, err := kitchen(s, role)
		if err != nil {
			return err
		}
		if c == nil {
			return s.Send(24804, proto.Message{"lev": 1, "exp": 0, "camp_id": 0, "group_id": 0, "combat_num": 0, "start_time": 0, "end_time": 0,
				"show_list": []proto.Message{{"show_id": 1, "num": 0}, {"show_id": 2, "num": 0}, {"show_id": 3, "num": 0}}, "make_list": []proto.Message{}})
		}
		if err := st.SaveState(role.RID, animateModule, d); err != nil {
			return err
		}
		return s.Send(24804, kitchenState(k, c, camp, start, end))
	})
	r.Handle(24805, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		_, k, c, _, _, _, err := kitchen(s, role)
		if err != nil {
			return err
		}
		if c == nil {
			return s.Send(24805, proto.Message{"lev": 1, "reward_list": []proto.Message{}})
		}
		return s.Send(24805, levelState(k, c))
	})
	act(24806, func(s *gateway.Session, role *store.Role, req proto.Message, k *animateKitchen, c *animateCamp) (string, error) {
		id := req.Int("id")
		for _, rc := range c.Recipes {
			if rc.ID != id {
				continue
			}
			if k.level(c) < rc.Lev {
				return "制作等级不足", nil
			}
			if k.Made[id] >= rc.Limit {
				return "制作次数已用完", nil
			}
			spendA, spendI, gainA, gainI := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
			splitGains(toGains(rc.Loss), 1, spendA, spendI)
			splitGains(toGains(rc.Reward), 1, gainA, gainI)
			for item := range gainI {
				if !ItemKnown(item) {
					delete(gainI, item)
				}
			}
			if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
				return "材料不足", nil
			}
			k.Made[id]++
			k.Cooked++
			k.Exp += rc.Exp
			return "", nil
		}
		return "菜谱不存在", nil
	})
	act(24807, func(s *gateway.Session, role *store.Role, req proto.Message, k *animateKitchen, c *animateCamp) (string, error) {
		lev := req.Int("id")
		for _, l := range c.Levels {
			if l.Lev != lev {
				continue
			}
			if lev > k.level(c) {
				return "制作等级不足", nil
			}
			if has(k.Claimed, lev) {
				return "已领取", nil
			}
			gainA, gainI := map[int64]int64{}, map[int64]int64{}
			splitGains(toGains(l.Reward), 1, gainA, gainI)
			for item := range gainI {
				if !ItemKnown(item) {
					delete(gainI, item)
				}
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, gainA, gainI); err != nil {
				return "", err
			}
			k.Claimed = append(k.Claimed, lev)
			return "", nil
		}
		return "等级奖励不存在", nil
	})
	act(24808, func(s *gateway.Session, role *store.Role, _ proto.Message, k *animateKitchen, c *animateCamp) (string, error) {
		if k.Combat <= 0 {
			return "挑战次数不足", nil
		}
		gain := map[int64]int64{}
		for _, a := range c.Collect.Award {
			if ItemKnown(a[0]) {
				gain[a[0]] += a[1]
			}
			k.Collected += a[1]
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, gain); err != nil {
			return "", err
		}
		k.Combat--
		k.Beaten++
		return "", nil
	})
	act(24809, func(s *gateway.Session, role *store.Role, _ proto.Message, k *animateKitchen, c *animateCamp) (string, error) {
		if k.Buys >= c.Collect.BuyLimit {
			return "今天的购买次数已用完", nil
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		splitGains(toGains(c.Collect.BuyLoss), 1, spendA, spendI)
		if err := st.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
			return "钻石不足", nil
		}
		k.Buys++
		k.Combat++
		return "", nil
	})
}
