package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	elfinModule        = "elfin"
	elfinSecPerPoint   = 11 // seconds a hatching point takes (the client's text: "11秒减一点孵化点")
	elfinHatches       = 6
	elfinTreeSlots     = 4
	elfinHatchOpen     = 1
	elfinHatchPending  = 2 // the condition is met but the ground still has to be bought
	elfinStateIdle     = 0
	elfinStateHatching = 1
	elfinStateDone     = 2
	elfinSummonGroup   = 110
	elfinHeroExp       = 22
)

type elfinSlot struct{ Pos, Bid int64 }

type elfinHatchState struct {
	ID, State, Egg, End, Lev int64
	Unlocked                 bool
}

type elfinTeam struct {
	Sprites []elfinSlot
	PlanID  int64
}

type elfinPlan struct {
	ID      int64
	Name    string
	Sprites []elfinSlot
}

type elfinState struct {
	Hatches   []elfinHatchState
	Bought    map[string]int64
	BoughtDay int64
	Lev       int64
	Break     int64
	Slots     []int64
	Seen      map[string]bool
	Plans     []elfinPlan
	Teams     map[string]map[string]elfinTeam
	Times     int64 // summon points
	FreeAt    int64
	DayKey    int64
	DayCount  int64
	Claimed   []int64
	Lucky     []int64
}

type elfinSvc struct{ st *store.Store }

// RegisterElfin registers the sprite ("elfin") system: 26500-26512 the hatching grounds and the ancient tree, 26513 /
// 26514 the tree's slots, 26508 / 26509 compose and the book, 26550-26554 the sprite summon, 26555-26564 the sprite
// teams and plans. It opens at level 80 (the client's rule). The summon is the permanent one (group 110) of the
// client's holiday summon tables. Hatching takes 11 seconds a point, eggs drop by the odds their text gives, the
// summon odds are the table's; the rest (what the "other items" are, the compose of eggs, what 26506 does) is ours.
func RegisterElfin(r *gateway.Router, db *store.Store) {
	e := &elfinSvc{st: db}
	r.Handle(26500, e.hatchList)
	r.Handle(26502, e.upgradeGround)
	r.Handle(26535, e.unlockGround)
	r.Handle(26503, e.startHatch)
	r.Handle(26504, e.speedUp)
	r.Handle(26505, e.collect)
	r.Handle(26506, e.finishNow)
	r.Handle(26507, e.buy)
	r.Handle(26508, e.compose)
	r.Handle(26509, e.book)
	r.Handle(26510, e.tree)
	r.Handle(26511, e.treeLevelUp)
	r.Handle(26512, e.treeBreak)
	r.Handle(26513, e.placeSprite)
	r.Handle(26514, e.setSlots)
	r.Handle(26550, e.summonInfo)
	r.Handle(26551, e.summon)
	r.Handle(26552, e.summonAward)
	r.Handle(26554, e.setLucky)
	r.Handle(26555, e.teams)
	r.Handle(26556, e.plans)
	r.Handle(26557, e.savePlan)
	r.Handle(26558, e.renamePlan)
	r.Handle(26560, e.saveTeam)
	r.Handle(26561, e.applyPlan)
	r.Handle(26562, e.buyPlan)
	r.Handle(26563, e.planTip)
	r.Handle(26564, e.reorder)
}

func (e *elfinSvc) with(s *gateway.Session, fn func(role *store.Role, st *elfinState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil { // the level changes behind the session's back
		role = fresh
	}
	st := &elfinState{}
	if _, err := e.st.LoadState(role.RID, elfinModule, st); err != nil {
		return err
	}
	if len(st.Hatches) == 0 {
		for i := int64(1); i <= elfinHatches; i++ {
			st.Hatches = append(st.Hatches, elfinHatchState{ID: i})
		}
		st.Lev = 1
	}
	if len(st.Slots) != elfinTreeSlots {
		st.Slots = make([]int64, elfinTreeSlots)
	}
	if st.Bought == nil {
		st.Bought = map[string]int64{}
	}
	if st.Seen == nil {
		st.Seen = map[string]bool{}
	}
	if st.Teams == nil {
		st.Teams = map[string]map[string]elfinTeam{}
	}
	if day := summonDay(s.Now()); st.BoughtDay != day {
		st.BoughtDay, st.Bought = day, map[string]int64{}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, elfinModule, st)
}

// open runs fn for a role that has the system open (level 80); others get msg on cmd.
func (e *elfinSvc) open(s *gateway.Session, cmd uint16, fail proto.Message, fn func(role *store.Role, st *elfinState) error) error {
	return e.with(s, func(role *store.Role, st *elfinState) error {
		if need := elfinCfg.num("sprite_unlocked_lv"); int64(role.Lev) < need {
			fail["msg"] = fmt.Sprintf("精灵系统将在%d级开放", need)
			return s.Send(cmd, fail)
		}
		return fn(role, st)
	})
}

func slotsOf(list []proto.Message) []elfinSlot {
	var out []elfinSlot
	for _, p := range list {
		out = append(out, elfinSlot{p.Int("pos"), p.Int("item_bid")})
	}
	return out
}

func slotMessages(list []elfinSlot) []proto.Message {
	out := []proto.Message{}
	for _, p := range list {
		out = append(out, proto.Message{"pos": p.Pos, "item_bid": p.Bid})
	}
	return out
}

// owned is how many of an item the role has.
func (e *elfinSvc) owned(rid int64) (map[int64]int64, error) { return e.st.RoleItems(rid) }

func (e *elfinSvc) bagOf(rid int64) map[int64]int64 {
	m, _ := e.st.RoleItems(rid)
	return m
}

// pay takes a price (asset and item pairs); false when it cannot be paid.
func (e *elfinSvc) pay(s *gateway.Session, role *store.Role, list [][]int64, times int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	var l [][2]int64
	for _, p := range list {
		if len(p) > 1 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	splitGains(guildGains(l), times, sa, si)
	err := e.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	return err == nil, err
}

func (e *elfinSvc) give(s *gateway.Session, role *store.Role, st *elfinState, list [][]int64) error {
	var l [][2]int64
	for _, p := range list {
		if len(p) > 1 && p[1] > 0 {
			l = append(l, [2]int64{p[0], p[1]})
			if _, ok := elfinCfg.sprite[p[0]]; ok {
				st.Seen[strconv.FormatInt(p[0], 10)] = true
			}
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l), 1, ga, gi)
	return e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi)
}

func (e *elfinSvc) refresh(s *gateway.Session, role *store.Role) error {
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, e.st); err != nil {
		return err
	}
	return sendSpriteBag(s, e.st)
}

// ---- the hatching grounds

func (e *elfinSvc) placedPower(st *elfinState) int64 {
	var p int64
	for _, b := range st.Slots {
		p += elfinCfg.sprite[b].Power
	}
	return p
}

func (e *elfinSvc) hasAny(rid int64, st *elfinState, ids []any) bool {
	bag := e.bagOf(rid)
	for _, v := range ids {
		f, _ := v.(float64)
		id := int64(f)
		if bag[id] > 0 || st.Seen[strconv.FormatInt(id, 10)] {
			return true
		}
	}
	return false
}

// groundOpen says 0 (locked), 1 (open) or 2 (can be bought open) for a ground.
func (e *elfinSvc) groundOpen(role *store.Role, st *elfinState, h elfinHatchState) int64 {
	cfg := elfinCfg.Hatch[h.ID-1]
	met := false
	if len(cfg.HatchCond) >= 2 {
		kind, _ := cfg.HatchCond[0].(string)
		val, _ := cfg.HatchCond[1].(float64)
		switch kind {
		case "lev":
			met = int64(role.Lev) >= int64(val)
		case "sprite_tree_lv":
			met = st.Lev >= int64(val)
		case "sprite_power":
			met = e.placedPower(st) >= int64(val)
		case "has_any_sprite":
			list, _ := cfg.HatchCond[1].([]any)
			met = e.hasAny(role.RID, st, list)
		}
	}
	switch {
	case h.Unlocked:
		return elfinHatchOpen
	case met && len(cfg.Expend) == 0:
		return elfinHatchOpen
	case met:
		return elfinHatchPending
	}
	return 0
}

func (e *elfinSvc) hatchMessage(role *store.Role, st *elfinState, h elfinHatchState) proto.Message {
	return proto.Message{"id": h.ID, "is_open": e.groundOpen(role, st, h), "state": h.State, "do_id": h.Egg, "all_end_time": h.End}
}

// settleHatch turns a finished hatch into the "done" state.
func (e *elfinSvc) settleHatch(s *gateway.Session, st *elfinState) {
	for i := range st.Hatches {
		if h := &st.Hatches[i]; h.State == elfinStateHatching && s.Now().Unix() >= h.End {
			h.State = elfinStateDone
		}
	}
}

func (e *elfinSvc) pushHatch(s *gateway.Session, role *store.Role, st *elfinState, ids ...int64) error {
	list := []proto.Message{}
	for _, h := range st.Hatches {
		for _, id := range ids {
			if h.ID == id {
				list = append(list, e.hatchMessage(role, st, h))
			}
		}
	}
	return s.Send(26501, proto.Message{"sprite_hatch": list})
}

func (e *elfinSvc) buyInfo(st *elfinState) []proto.Message {
	out := []proto.Message{}
	var ids []int64
	for k := range st.Bought {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		out = append(out, proto.Message{"item_bid": id, "buy_num": st.Bought[strconv.FormatInt(id, 10)]})
	}
	return out
}

func (e *elfinSvc) hatchList(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *elfinState) error {
		e.settleHatch(s, st)
		list := []proto.Message{}
		for _, h := range st.Hatches {
			list = append(list, e.hatchMessage(role, st, h))
		}
		return s.Send(26500, proto.Message{"sprite_hatchs": list, "info": e.buyInfo(st)})
	})
}

func (e *elfinSvc) ground(st *elfinState, id int64) *elfinHatchState {
	if id < 1 || id > int64(len(st.Hatches)) {
		return nil
	}
	return &st.Hatches[id-1]
}

// upgradeGround (26502): a ground's level, bought with gems; the higher levels want a VIP level.
func (e *elfinSvc) upgradeGround(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26502, proto.Message{"result": 0, "id": id, "lev": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string, lev int64) error {
			return s.Send(26502, proto.Message{"result": 0, "msg": msg, "id": id, "lev": lev})
		}
		h := e.ground(st, id)
		if h == nil || e.groundOpen(role, st, *h) != elfinHatchOpen {
			return refuse("该幽地尚未开启", 0)
		}
		next, ok := elfinCfg.HatchLev[strconv.FormatInt(h.Lev+1, 10)]
		switch {
		case !ok:
			return refuse("已达最高等级", h.Lev)
		case int64(role.VIPLev) < next.LimitVIP:
			return refuse(fmt.Sprintf("需要VIP%d", next.LimitVIP), h.Lev)
		}
		okPay, err := e.pay(s, role, next.Expend, 1)
		if err != nil {
			return err
		}
		if !okPay {
			return refuse("钻石不足", h.Lev)
		}
		h.Lev++
		if err := e.give(s, role, st, next.Award); err != nil {
			return err
		}
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(26502, proto.Message{"result": 1, "msg": "升级成功", "id": id, "lev": h.Lev})
	})
}

func (e *elfinSvc) unlockGround(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26535, proto.Message{"flag": 0, "id": id}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26535, proto.Message{"flag": 0, "msg": msg, "id": id}) }
		h := e.ground(st, id)
		if h == nil || e.groundOpen(role, st, *h) != elfinHatchPending {
			return refuse("无法开启该幽地")
		}
		ok, err := e.pay(s, role, elfinCfg.Hatch[id-1].Expend, 1)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("钻石不足")
		}
		h.Unlocked = true
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := e.pushHatch(s, role, st, id); err != nil {
			return err
		}
		return s.Send(26535, proto.Message{"flag": 1, "msg": "开启成功", "id": id})
	})
}

func (e *elfinSvc) startHatch(s *gateway.Session, req proto.Message) error {
	id, egg := req.Int("id"), req.Int("item_bid")
	return e.open(s, 26503, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26503, proto.Message{"result": 0, "msg": msg, "id": id}) }
		h := e.ground(st, id)
		cfg, isEgg := elfinCfg.Egg[strconv.FormatInt(egg, 10)]
		switch {
		case h == nil || e.groundOpen(role, st, *h) != elfinHatchOpen:
			return refuse("该幽地尚未开启")
		case h.State != elfinStateIdle:
			return refuse("该幽地正在孵化")
		case !isEgg:
			return refuse("无法孵化该物品")
		}
		if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{egg: 1}, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("蛋不足")
		} else if err != nil {
			return err
		}
		h.State, h.Egg, h.End = elfinStateHatching, egg, s.Now().Unix()+cfg.NeedPoint*elfinSecPerPoint
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := e.pushHatch(s, role, st, id); err != nil {
			return err
		}
		return s.Send(26503, proto.Message{"result": 1, "msg": "开始孵化", "id": id})
	})
}

// speedUp (26504): a speed-up item takes points off the hatch.
func (e *elfinSvc) speedUp(s *gateway.Session, req proto.Message) error {
	id, bid, num := req.Int("id"), req.Int("item_bid"), max(1, req.Int("item_num"))
	return e.open(s, 26504, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26504, proto.Message{"result": 0, "msg": msg, "id": id}) }
		h := e.ground(st, id)
		item, ok := elfinCfg.Smash[strconv.FormatInt(bid, 10)]
		if h == nil || !ok || h.State != elfinStateHatching {
			return refuse("无法使用")
		}
		if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{bid: num}, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("道具不足")
		} else if err != nil {
			return err
		}
		h.End -= item.DelPoint * elfinSecPerPoint * num
		e.settleHatch(s, st)
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := e.pushHatch(s, role, st, id); err != nil {
			return err
		}
		return s.Send(26504, proto.Message{"result": 1, "msg": "使用成功", "id": id})
	})
}

// hatchSprite picks the sprite an egg hatches by the odds in its text: eggs 10601 and 10602 give blue, purple and
// orange sprites 95/4/1 and 75/22/3 per cent, 10603 purple and orange 92.8/7.2.
func hatchSprite(egg int64, rng interface{ Intn(int) int }) int64 {
	odds := map[int64][3]int{10601: {950, 40, 10}, 10602: {750, 220, 30}, 10603: {0, 928, 72}}[egg]
	roll := rng.Intn(1000)
	tier := int64(113)
	switch {
	case roll < odds[0]:
		tier = 111
	case roll < odds[0]+odds[1]:
		tier = 112
	}
	var pool []int64
	for id, sp := range elfinCfg.sprite {
		if elfinTier(id) == tier && sp.Step == 1 {
			pool = append(pool, id)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
	return pool[rng.Intn(len(pool))]
}

func (e *elfinSvc) finish(s *gateway.Session, role *store.Role, st *elfinState, h *elfinHatchState, cmd uint16) error {
	cfg := elfinCfg.Egg[strconv.FormatInt(h.Egg, 10)]
	got := hatchSprite(h.Egg, newRng())
	prize := append([][]int64{{got, 1}}, cfg.Award...)
	if err := e.give(s, role, st, prize); err != nil {
		return err
	}
	h.State, h.Egg, h.End = elfinStateIdle, 0, 0
	if err := e.refresh(s, role); err != nil {
		return err
	}
	if err := e.pushHatch(s, role, st, h.ID); err != nil {
		return err
	}
	awards := []proto.Message{}
	for _, p := range prize {
		awards = append(awards, proto.Message{"item_bid": p[0], "item_num": p[1]})
	}
	return s.Send(cmd, proto.Message{"result": 1, "msg": "", "id": h.ID, "awards": awards})
}

func (e *elfinSvc) collect(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26505, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *elfinState) error {
		e.settleHatch(s, st)
		h := e.ground(st, id)
		if h == nil || h.State != elfinStateDone {
			return s.Send(26505, proto.Message{"result": 0, "msg": "孵化尚未完成", "id": id})
		}
		return e.finish(s, role, st, h, 26505)
	})
}

// finishNow (26506) finishes a hatch at once for the gems the egg's table lists.
func (e *elfinSvc) finishNow(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26506, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *elfinState) error {
		e.settleHatch(s, st)
		h := e.ground(st, id)
		refuse := func(msg string) error { return s.Send(26506, proto.Message{"result": 0, "msg": msg, "id": id}) }
		if h == nil || (h.State != elfinStateHatching && h.State != elfinStateDone) {
			return refuse("没有可以完成的孵化")
		}
		if h.State == elfinStateHatching {
			ok, err := e.pay(s, role, elfinCfg.Egg[strconv.FormatInt(h.Egg, 10)].Expend, 1)
			if err != nil {
				return err
			}
			if !ok {
				return refuse("钻石不足")
			}
		}
		return e.finish(s, role, st, h, 26506)
	})
}

// buy (26507): type 1 buys eggs, 2 buys speed-up items; the day's limit is the table's.
func (e *elfinSvc) buy(s *gateway.Session, req proto.Message) error {
	typ, bid, num := req.Int("type"), req.Int("item_id"), max(1, req.Int("num"))
	return e.open(s, 26507, proto.Message{"result": 0, "item_id": bid}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error {
			return s.Send(26507, proto.Message{"result": 0, "msg": msg, "item_id": bid, "count": 0})
		}
		var price [][]int64
		var limit, can int64
		key := strconv.FormatInt(bid, 10)
		if egg, ok := elfinCfg.Egg[key]; ok && typ != 2 {
			price, limit, can = egg.Price, egg.LimitNum, egg.CanBuy
		} else if item, ok := elfinCfg.Smash[key]; ok {
			price, limit, can = item.Price, item.LimitNum, item.CanBuy
		} else {
			return refuse("该物品不能购买")
		}
		switch {
		case can != 1:
			return refuse("该物品不能购买")
		case limit > 0 && st.Bought[key]+num > limit:
			return refuse("今日购买次数已达上限")
		}
		ok, err := e.pay(s, role, price, num)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("资源不足")
		}
		if err := e.give(s, role, st, [][]int64{{bid, num}}); err != nil {
			return err
		}
		st.Bought[key] += num
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(26507, proto.Message{"result": 1, "msg": "购买成功", "item_id": bid, "count": st.Bought[key]})
	})
}

// ---- compose, book

// compose (26508) makes the next step of a sprite or egg from the recipe; with a pos it works on the sprite placed
// there and puts the result in its place.
func (e *elfinSvc) compose(s *gateway.Session, req proto.Message) error {
	bid, num, pos := req.Int("item_bid"), max(1, req.Int("num")), req.Int("pos")
	return e.open(s, 26508, proto.Message{"result": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26508, proto.Message{"result": 0, "msg": msg}) }
		rec, ok := elfinCfg.Compose[strconv.FormatInt(bid, 10)]
		if !ok {
			return refuse("无法合成")
		}
		if pos != 0 {
			if pos < 1 || pos > elfinTreeSlots || st.Slots[pos-1] != bid {
				return refuse("该位置没有对应的精灵")
			}
			num = 1
		}
		sa, si := map[int64]int64{}, map[int64]int64{}
		var l [][2]int64
		for _, p := range rec.Expend {
			l = append(l, [2]int64{p[0], p[1]})
		}
		splitGains(guildGains(l), num, sa, si)
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains([][2]int64{{rec.Award, 1}}), num, ga, gi)
		if err := e.st.Exchange(role.RID, sa, si, ga, gi); errors.Is(err, store.ErrConflict) {
			return refuse("材料不足")
		} else if err != nil {
			return err
		}
		if _, isSprite := elfinCfg.sprite[rec.Award]; isSprite {
			st.Seen[strconv.FormatInt(rec.Award, 10)] = true
		}
		if pos != 0 {
			st.Slots[pos-1] = rec.Award
		}
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(26508, proto.Message{"result": 1, "msg": "合成成功"})
	})
}

// book (26509) lists the sprites the role ever had (the client's book).
func (e *elfinSvc) book(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *elfinState) error {
		seen := map[int64]bool{}
		for k := range st.Seen {
			id, _ := strconv.ParseInt(k, 10, 64)
			seen[id] = true
		}
		for id, n := range e.bagOf(role.RID) {
			if _, ok := elfinCfg.sprite[id]; ok && n > 0 {
				seen[id] = true
			}
		}
		var ids []int64
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out := []proto.Message{}
		for _, id := range ids {
			out = append(out, proto.Message{"item_bid": id})
		}
		return s.Send(26509, proto.Message{"awards": out})
	})
}

// ---- the ancient tree

func (e *elfinSvc) treeAttrs(st *elfinState) map[string]int64 {
	t := elfinCfg.TreeAttr
	step, _ := elfinCfg.step(st.Break)
	extra := map[string]int64{}
	for _, a := range step.AllAttr {
		if len(a) == 2 {
			var name string
			var v int64
			_ = json.Unmarshal(a[0], &name)
			_ = json.Unmarshal(a[1], &v)
			extra[name] = v
		}
	}
	grow := func(base, add, stepAdd int64, name string) int64 {
		return base + extra[name] + st.Lev*add*stepAdd/1000000
	}
	out := map[string]int64{
		"atk":    grow(t["atk"], t["add_atk"], step.AddAtk, "atk"),
		"hp_max": grow(t["hp_max"], t["add_hp"], step.AddHP, "hp_max"),
		"def":    grow(t["def"], t["add_def"], step.AddDef, "def"),
		"speed":  grow(t["speed"], t["add_speed"], step.AddSpeed, "speed"),
	}
	for _, b := range st.Slots { // the sprites on the altar add theirs
		for k, v := range elfinCfg.sprite[b].Altar {
			out[k] += v
		}
	}
	for _, k := range []string{"hit_rate", "dodge_rate", "crit_rate", "crit_ratio", "hit_magic", "dodge_magic"} {
		out[k] = t[k]
	}
	return out
}

func (e *elfinSvc) treeMessage(st *elfinState) proto.Message {
	a := e.treeAttrs(st)
	power := heroPower(a["atk"], a["hp_max"], a["def"], a["speed"]) + e.placedPower(st)
	sprites := []proto.Message{}
	for i, b := range st.Slots {
		sprites = append(sprites, proto.Message{"pos": i + 1, "item_bid": b})
	}
	return proto.Message{"lev": st.Lev, "break_lev": st.Break, "atk": a["atk"], "def_p": a["def"], "def_s": a["def"], "hp_max": a["hp_max"], "speed": a["speed"],
		"hit_rate": a["hit_rate"], "dodge_rate": a["dodge_rate"], "crit_rate": a["crit_rate"], "crit_ratio": a["crit_ratio"], "hit_magic": a["hit_magic"],
		"dodge_magic": a["dodge_magic"], "def": a["def"], "sprites": sprites, "power": power}
}

func (e *elfinSvc) tree(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *elfinState) error { return s.Send(26510, e.treeMessage(st)) })
}

func (e *elfinSvc) pushTree(s *gateway.Session, st *elfinState) error {
	return s.Send(26510, e.treeMessage(st))
}

func (e *elfinSvc) treeLevelUp(s *gateway.Session, _ proto.Message) error {
	return e.open(s, 26511, proto.Message{"result": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26511, proto.Message{"result": 0, "msg": msg}) }
		step, _ := elfinCfg.step(st.Break)
		switch {
		case st.Lev >= step.LevMax:
			return refuse("需要先进阶")
		case int(st.Lev) > len(elfinCfg.TreeUp):
			return refuse("已达最高等级")
		}
		ok, err := e.pay(s, role, elfinCfg.TreeUp[st.Lev-1].Expend, 1)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("材料不足")
		}
		st.Lev++
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(26511, proto.Message{"result": 1, "msg": "升级成功"}); err != nil {
			return err
		}
		return e.pushTree(s, st)
	})
}

// treeBreak (26512) takes the tree to the next step once it is at its level cap and the role is high enough.
func (e *elfinSvc) treeBreak(s *gateway.Session, _ proto.Message) error {
	return e.open(s, 26512, proto.Message{"result": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26512, proto.Message{"result": 0, "msg": msg}) }
		step, _ := elfinCfg.step(st.Break)
		if _, more := elfinCfg.step(st.Break + 1); !more {
			return refuse("已达最高阶")
		}
		if st.Lev < step.LevMax {
			return refuse("等级未达到进阶要求")
		}
		for _, c := range step.StepCond {
			var kind string
			var v int64
			if len(c) == 2 && json.Unmarshal(c[0], &kind) == nil && json.Unmarshal(c[1], &v) == nil && kind == "role_lev" && int64(role.Lev) < v {
				return refuse(fmt.Sprintf("需要角色等级达到%d级", v))
			}
		}
		ok, err := e.pay(s, role, step.Expend, 1)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("材料不足")
		}
		st.Break++
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(26512, proto.Message{"result": 1, "msg": "进阶成功"}); err != nil {
			return err
		}
		return e.pushTree(s, st)
	})
}

// validSlots checks a loadout: unlocked places, sprites the role owns, one of each kind.
func (e *elfinSvc) validSlots(rid int64, st *elfinState, want []int64) string {
	step, _ := elfinCfg.step(st.Break)
	bag := e.bagOf(rid)
	used := map[int64]int64{}
	kinds := map[int64]bool{}
	for i, b := range want {
		if b == 0 {
			continue
		}
		sp, ok := elfinCfg.sprite[b]
		switch {
		case int64(i) >= step.SkillNum:
			return "该位置尚未解锁"
		case !ok:
			return "无法放置该物品"
		case kinds[sp.Type]:
			return "同类型的精灵只能放置一个"
		}
		kinds[sp.Type] = true
		used[b]++
		if used[b] > bag[b] {
			return "没有该精灵"
		}
	}
	return ""
}

func (e *elfinSvc) placeSprite(s *gateway.Session, req proto.Message) error {
	pos, bid := req.Int("pos"), req.Int("item_bid")
	return e.open(s, 26513, proto.Message{"result": 0}, func(role *store.Role, st *elfinState) error {
		if pos < 1 || pos > elfinTreeSlots {
			return s.Send(26513, proto.Message{"result": 0, "msg": "位置错误"})
		}
		want := append([]int64(nil), st.Slots...)
		want[pos-1] = bid
		if msg := e.validSlots(role.RID, st, want); msg != "" {
			return s.Send(26513, proto.Message{"result": 0, "msg": msg})
		}
		st.Slots = want
		if err := s.Send(26513, proto.Message{"result": 1, "msg": ""}); err != nil {
			return err
		}
		return e.pushTree(s, st)
	})
}

func (e *elfinSvc) setSlots(s *gateway.Session, req proto.Message) error {
	return e.open(s, 26514, proto.Message{"result": 0, "sprites": []proto.Message{}}, func(role *store.Role, st *elfinState) error {
		want := make([]int64, elfinTreeSlots)
		for _, p := range req.Arr("sprites") {
			if pos := p.Int("pos"); pos >= 1 && pos <= elfinTreeSlots {
				want[pos-1] = p.Int("item_bid")
			}
		}
		if msg := e.validSlots(role.RID, st, want); msg != "" {
			return s.Send(26514, proto.Message{"result": 0, "msg": msg, "sprites": []proto.Message{}})
		}
		st.Slots = want
		sprites := []proto.Message{}
		for i, b := range want {
			sprites = append(sprites, proto.Message{"pos": i + 1, "item_bid": b})
		}
		if err := s.Send(26514, proto.Message{"result": 1, "msg": "保存成功", "sprites": sprites}); err != nil {
			return err
		}
		return e.pushTree(s, st)
	})
}
