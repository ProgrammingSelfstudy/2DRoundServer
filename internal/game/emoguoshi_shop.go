package game

import (
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// ---- the shop

func (e *emoSvc) shopList(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error {
		var ids []int64
		for k, n := range st.Bought {
			if n > 0 {
				id, _ := strconv.ParseInt(k, 10, 64)
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"id": id, "times": st.Bought[strconv.FormatInt(id, 10)]})
		}
		return s.Send(31108, proto.Message{"list": list})
	})
}

func (e *emoSvc) jifen(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error { return s.Send(31109, proto.Message{"jifen": st.Jifen}) })
}

// buy (31106): the price is in 恶魔之种; what has been spent in the shop in all counts for the goods and boxes that ask
// for points. The limit counts the cycle's purchases.
func (e *emoSvc) buy(s *gateway.Session, req proto.Message) error {
	id, num := req.Int("id"), max(1, req.Int("num"))
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31106, proto.Message{"result": 0, "msg": msg}) }
		g, ok := emoCfg.Shop[strconv.FormatInt(id, 10)]
		key := strconv.FormatInt(id, 10)
		switch {
		case !ok:
			return refuse("商品不存在")
		case int64(role.Lev) < g.NeedLevel:
			return refuse("等级不足")
		case st.Jifen < g.NeedJifen:
			return refuse("积分不足")
		case st.Bought[key]+num > g.Limit:
			return refuse("购买次数已达上限")
		}
		paid, err := e.pay(role, [][]int64{{g.PayType, g.Price}}, num)
		if err != nil {
			return err
		}
		if !paid {
			return refuse("恶魔之种不足")
		}
		if err := e.give(role, [][]int64{{g.ItemID, g.ItemNum * num}}); err != nil {
			return err
		}
		st.Bought[key] += num
		st.Jifen += g.Price * num
		st.tick(5)
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(31107, proto.Message{"id": id, "times": st.Bought[key]}); err != nil {
			return err
		}
		if err := s.Send(31109, proto.Message{"jifen": st.Jifen}); err != nil {
			return err
		}
		return s.Send(31106, proto.Message{"result": 1, "msg": "购买成功"})
	})
}

// ---- the lottery boxes

// boxLevels are the levels the four boxes ask for (the shop's own steps: 50 / 70 / 90 / 110).
var emoBoxLevels = map[int64]int64{10001: 50, 10002: 70, 10003: 90, 10004: 110}

func (e *emoSvc) unlockedBoxes(role *store.Role, st *emoState) int64 {
	var n int64
	for _, b := range emoCfg.Boxes {
		if int64(role.Lev) >= emoBoxLevels[b.Bid] && st.Jifen >= b.NeedJifen {
			n++
		}
	}
	return n
}

func (e *emoSvc) drawTimes(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error {
		return s.Send(31116, proto.Message{"times": max(0, emoCfg.num("emo_choujaing_times")-st.DrawToday)})
	})
}

// pullGroup picks the group of one pull of a box: the weights of the table, a group that hit stays away for
// `not_get_n` pulls, one that is due by `must_get_n` (or `first_get_n` for the first time) is forced, and one that
// has `first_not_get_n` does not show in the first pulls.
func (e *emoSvc) pullGroup(st *emoState, bid int64, rng interface{ Intn(int) int }, tenForced *emoDo) emoDo {
	bk := strconv.FormatInt(bid, 10)
	groups := emoCfg.Do[bk]
	total := st.Total[bk]
	var pool []emoDo
	var sum int64
	for _, g := range groups {
		gk := bk + ":" + strconv.FormatInt(g.GroupID, 10)
		since := st.Since[gk]
		hits := st.Hits[gk]
		if (g.MustGetN > 0 && since+1 >= g.MustGetN) || (hits == 0 && g.FirstGetN > 0 && total+1 >= g.FirstGetN) {
			return g
		}
		if since < g.NotGetN && hits > 0 {
			continue
		}
		if hits == 0 && total < g.FirstNotGetN {
			continue
		}
		pool = append(pool, g)
		sum += g.Weight
	}
	if sum == 0 {
		return groups[0]
	}
	n := int64(rng.Intn(int(sum)))
	for _, g := range pool {
		if n < g.Weight {
			return g
		}
		n -= g.Weight
	}
	return pool[0]
}

// draw (31115): id the box, type 1 once and anything else ten times, up_id the wished entry of the rare groups.
func (e *emoSvc) draw(s *gateway.Session, req proto.Message) error {
	bid, kind, up := req.Int("id"), req.Int("type"), req.Int("up_id")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31115, proto.Message{"result": 0, "msg": msg}) }
		box, ok := emoCfg.Boxes[strconv.FormatInt(bid, 10)]
		switch {
		case !ok:
			return refuse("果盒不存在")
		case int64(role.Lev) < emoBoxLevels[bid]:
			return refuse("等级不足")
		case st.Jifen < box.NeedJifen:
			return refuse("积分不足")
		}
		times := int64(1)
		cost1, cost2, add := box.OneNeedItems1, box.OneNeedItems2, box.OneAddItems
		if kind != 1 {
			times, cost1, cost2, add = 10, box.TenNeedItems1, box.TenNeedItems2, box.TenAddItems
		}
		viaTickets, err := e.pay(role, cost2, 1) // tickets first, gems otherwise
		if err != nil {
			return err
		}
		if !viaTickets {
			if st.DrawToday+times > emoCfg.num("emo_choujaing_times") {
				return refuse("今日钻石抽取次数已达上限")
			}
			paid, err := e.pay(role, cost1, 1)
			if err != nil {
				return err
			}
			if !paid {
				return refuse("材料不足")
			}
			st.DrawToday += times
		}
		rng := newRng()
		bk := strconv.FormatInt(bid, 10)
		var prize [][]int64
		for i := int64(0); i < times; i++ {
			g := e.pullGroup(st, bid, rng, nil)
			for _, o := range emoCfg.Do[bk] {
				gk := bk + ":" + strconv.FormatInt(o.GroupID, 10)
				if o.GroupID == g.GroupID {
					st.Since[gk], st.Hits[gk] = 0, st.Hits[gk]+1
				} else {
					st.Since[gk]++
				}
			}
			st.Total[bk]++
			items := emoCfg.Groups[strconv.FormatInt(g.GroupID, 10)]
			if len(items) == 0 {
				continue
			}
			var sum int64
			w := make([]int64, len(items))
			for j, it := range items {
				w[j] = it.Weight
				if it.ID == up {
					w[j] += max(it.UpAddWeight, it.Weight*emoCfg.num("emo_xuyuan"))
				}
				sum += w[j]
			}
			n := int64(rng.Intn(int(sum)))
			for j, it := range items {
				if n < w[j] {
					prize = append(prize, []int64{it.ItemID, it.ItemNum})
					break
				}
				n -= w[j]
			}
		}
		prize = append(prize, add...)
		if err := e.give(role, prize); err != nil {
			return err
		}
		st.tick(5 * times)
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(31115, proto.Message{"result": 1, "msg": ""})
	})
}

// ---- the fruit gift

func (e *emoSvc) giftRows(st *emoState) []emoGift {
	if int(st.Layer) > len(emoCfg.Gift) || st.Layer < 1 {
		return emoCfg.Gift[0]
	}
	return emoCfg.Gift[st.Layer-1]
}

func (e *emoSvc) giftInfo(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error {
		list := []proto.Message{}
		for i := range e.giftRows(st) {
			k := strconv.Itoa(i+1) + ":1"
			k2 := strconv.Itoa(i+1) + ":2"
			list = append(list, proto.Message{"index": i + 1, "tag1": b2i(st.Tag1[k]), "tag2": b2i(st.Tag1[k2])})
		}
		open := int64(0)
		if int64(role.Lev) >= emoCfg.num("emo_zhanling_jiesuo") {
			open = 1
		}
		return s.Send(31120, proto.Message{"open_tag": open, "end_time": st.GiftStart + emoCycle, "open_id": st.Layer, "unlock_tag": b2i(st.GiftOpen), "charge_tag": 0,
			"value": st.GiftValue, "list": list})
	})
}

func (e *emoSvc) giftUnlock(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31123, proto.Message{"result": 0, "msg": msg}) }
		rows := e.giftRows(st)
		switch {
		case st.GiftOpen:
			return refuse("已经解锁")
		case int64(role.Lev) < emoCfg.num("emo_zhanling_jiesuo"):
			return refuse("等级不足")
		}
		paid, err := e.pay(role, rows[0].UnlockItems, 1)
		if err != nil {
			return err
		}
		if !paid {
			return refuse("钻石不足")
		}
		st.GiftOpen = true
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(31123, proto.Message{"result": 1, "msg": "解锁成功"})
	})
}

// giftTake (31122): type 1 the basic reward of a step (after unlocking it with gems), type 2 the advanced one, which
// needs a paid pass that does not exist here.
func (e *emoSvc) giftTake(s *gateway.Session, req proto.Message) error {
	index, typ := req.Int("index"), req.Int("type")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31122, proto.Message{"result": 0, "msg": msg}) }
		rows := e.giftRows(st)
		if index < 1 || int(index) > len(rows) || (typ != 1 && typ != 2) {
			return refuse("奖励不存在")
		}
		row := rows[index-1]
		key := strconv.FormatInt(index, 10) + ":" + strconv.FormatInt(typ, 10)
		switch {
		case typ == 1 && !st.GiftOpen:
			return refuse("请先解锁果实赠礼")
		case typ == 2:
			return refuse("需要进阶赠礼")
		case st.Tag1[key]:
			return refuse("已经领取")
		case st.GiftValue < row.Process:
			return refuse("活跃度不足")
		}
		st.Tag1[key] = true
		if err := e.give(role, row.Items1); err != nil {
			return err
		}
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(31122, proto.Message{"result": 1, "msg": "领取成功"})
	})
}
