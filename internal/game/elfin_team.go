package game

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	elfinPlanNameMax = 7
	elfinOrangeOdds  = 10 // per mille of the permanent summon (group 110): 1 per cent orange, 12.4 purple, 45 blue, 41.6 items
	elfinPurpleOdds  = 124
	elfinBlueOdds    = 450
)

// ---- the sprite summon

func (e *elfinSvc) summonInfo(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *elfinState) error {
		if day := summonDay(s.Now()); st.DayKey != day {
			st.DayKey, st.DayCount = day, 0
		}
		awards, lucky := []proto.Message{}, []proto.Message{}
		for _, id := range st.Claimed {
			awards = append(awards, proto.Message{"award_id": id})
		}
		for _, id := range st.Lucky {
			lucky = append(lucky, proto.Message{"lucky_sprites_bid": id})
		}
		return s.Send(26550, proto.Message{"camp_id": 0, "free_time": st.FreeAt, "times": st.Times, "do_awards": awards, "lucky_ids": lucky, "day_count": st.DayCount})
	})
}

// pullOne makes one pull of the summon: a sprite of a tier, or an item.
func (e *elfinSvc) pullOne(st *elfinState, rng interface{ Intn(int) int }, atLeastPurple bool) (id int64, items [][]int64) {
	roll := rng.Intn(1000)
	tier := int64(0)
	switch {
	case roll < elfinOrangeOdds:
		tier = 113
	case roll < elfinOrangeOdds+elfinPurpleOdds:
		tier = 112
	case roll < elfinOrangeOdds+elfinPurpleOdds+elfinBlueOdds && !atLeastPurple:
		tier = 111
	case atLeastPurple:
		tier = 112
	}
	if tier == 0 {
		return 0, [][]int64{{10040, 10}}
	}
	var pool []int64
	for sid, sp := range elfinCfg.sprite {
		if elfinTier(sid) == tier && sp.Step == 1 {
			pool = append(pool, sid)
		}
	}
	sortIDs(pool)
	if tier == 113 && len(st.Lucky) > 0 && rng.Intn(2) == 0 { // the wish pool makes its sprite likelier
		return st.Lucky[0], nil
	}
	return pool[rng.Intn(len(pool))], nil
}

func sortIDs(ids []int64) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}

// summon (26551): times 1 or 10; recruit_type 1 pays with summon tickets, 2 with gems, 0 uses the day's free pull.
func (e *elfinSvc) summon(s *gateway.Session, req proto.Message) error {
	times, kind := req.Int("times"), req.Int("recruit_type")
	return e.open(s, 26551, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26551, proto.Message{"flag": 0, "msg": msg}) }
		if day := summonDay(s.Now()); st.DayKey != day {
			st.DayKey, st.DayCount = day, 0
		}
		if times != 1 && times != 10 {
			return refuse("召唤次数错误")
		}
		if st.DayCount+times > elfinCfg.lotteryNum("sprite_lottery_daily_limit") {
			return refuse("今日召唤次数已达上限")
		}
		sum := elfinCfg.Lottery.Summon
		var cost [][]int64
		switch {
		case kind == 0 && times == 1:
			if s.Now().Unix() < st.FreeAt {
				return refuse("免费召唤尚未恢复")
			}
		case kind == 1:
			cost = sum.LossItemOnce
			if times == 10 {
				cost = sum.LossItemTen
			}
		case kind == 2:
			cost = sum.LossGoldOnce
			if times == 10 {
				cost = sum.LossGoldTen
			}
		default:
			return refuse("召唤方式错误")
		}
		if cost != nil {
			ok, err := e.pay(s, role, cost, 1)
			if err != nil {
				return err
			}
			if !ok {
				if kind == 1 {
					return refuse("召唤券不足")
				}
				return refuse("钻石不足")
			}
		} else {
			st.FreeAt = s.Now().Unix() + elfinCfg.lotteryNum("c_interval")
		}
		rng := newRng()
		var got []int64
		var items [][]int64
		best := int64(0)
		for i := int64(0); i < times; i++ {
			id, it := e.pullOne(st, rng, false)
			got = append(got, id)
			items = append(items, it...)
			if id != 0 && elfinTier(id) > best {
				best = elfinTier(id)
			}
		}
		if times == 10 && best < 112 { // every ten pulls give a purple sprite at least
			id, _ := e.pullOne(st, rng, true)
			got[len(got)-1] = id
		}
		prize := [][]int64{}
		sprites := []proto.Message{}
		rewards := []proto.Message{}
		for _, id := range got {
			if id != 0 {
				prize = append(prize, []int64{id, 1})
				rewards = append(rewards, proto.Message{"base_id": id, "num": 1})
				sprites = append(sprites, proto.Message{"sprite_bid": id, "quality": elfinQuality(id), "jie": elfinCfg.sprite[id].Step})
			}
		}
		for _, p := range items {
			prize = append(prize, p)
			rewards = append(rewards, proto.Message{"base_id": p[0], "num": p[1]})
		}
		gain := sum.GainOnce
		if times == 10 {
			gain = sum.GainTen
		}
		prize = append(prize, gain...)
		if err := e.give(s, role, st, prize); err != nil {
			return err
		}
		st.Times += times
		st.DayCount += times
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(26553, proto.Message{"group_id": elfinSummonGroup, "times": times, "rewards": rewards, "sprite_bids": sprites}); err != nil {
			return err
		}
		return s.Send(26551, proto.Message{"flag": 1, "msg": ""})
	})
}

func (e *elfinSvc) summonAward(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26552, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		refuse := func(msg string) error { return s.Send(26552, proto.Message{"flag": 0, "msg": msg}) }
		for _, a := range elfinCfg.Lottery.Awards {
			if a.ID != id {
				continue
			}
			for _, c := range st.Claimed {
				if c == id {
					return refuse("已经领取过了")
				}
			}
			if st.Times < a.Times {
				return refuse("召唤次数不足")
			}
			if err := e.give(s, role, st, a.Reward); err != nil {
				return err
			}
			st.Claimed = append(st.Claimed, id)
			if err := e.refresh(s, role); err != nil {
				return err
			}
			return s.Send(26552, proto.Message{"flag": 1, "msg": "领取成功"})
		}
		return refuse("奖励不存在")
	})
}

func (e *elfinSvc) setLucky(s *gateway.Session, req proto.Message) error {
	return e.open(s, 26554, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		var want []int64
		var allowed []any
		_ = allowed
		luckyOK := map[int64]bool{113001: true, 113002: true, 113003: true, 113004: true, 113005: true, 113006: true, 113007: true, 113008: true, 113009: true}
		for _, l := range req.Arr("lucky_ids") {
			id := l.Int("lucky_sprites_bid")
			if !luckyOK[id] {
				return s.Send(26554, proto.Message{"flag": 0, "msg": "该精灵不能放入许愿池"})
			}
			want = append(want, id)
		}
		if int64(len(want)) > elfinCfg.lotteryNum("lucky_sprite_num") {
			return s.Send(26554, proto.Message{"flag": 0, "msg": "许愿池已满"})
		}
		st.Lucky = want
		return s.Send(26554, proto.Message{"flag": 1, "msg": "设置成功"})
	})
}

// ---- sprite teams and plans

func (e *elfinSvc) teamKey(typ, team int64) (string, string) {
	return strconv.FormatInt(typ, 10), strconv.FormatInt(max(team, 1), 10)
}

func (e *elfinSvc) teams(s *gateway.Session, req proto.Message) error {
	typ := req.Int("type")
	if s.Role() == nil { // the client asks right after login, before a role is chosen
		return s.Send(26555, proto.Message{"type": typ, "team_list": []proto.Message{}})
	}
	return e.with(s, func(role *store.Role, st *elfinState) error {
		list := []proto.Message{}
		tk, _ := e.teamKey(typ, 1)
		for n := int64(1); n <= 10; n++ {
			_, k := e.teamKey(typ, n)
			if t, ok := st.Teams[tk][k]; ok {
				list = append(list, proto.Message{"team": n, "sprites": slotMessages(t.Sprites), "plan_id": t.PlanID})
			}
		}
		return s.Send(26555, proto.Message{"type": typ, "team_list": list})
	})
}

func (e *elfinSvc) ensurePlans(st *elfinState) {
	for int64(len(st.Plans)) < elfinCfg.num("free_programmes") {
		n := int64(len(st.Plans) + 1)
		st.Plans = append(st.Plans, elfinPlan{ID: n, Name: fmt.Sprintf("方案%d", n)})
	}
}

func (e *elfinSvc) plans(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *elfinState) error {
		e.ensurePlans(st)
		list := []proto.Message{}
		for _, p := range st.Plans {
			list = append(list, proto.Message{"id": p.ID, "name": p.Name, "plan_sprites": slotMessages(p.Sprites)})
		}
		return s.Send(26556, proto.Message{"plan_list": list})
	})
}

func (e *elfinSvc) planByID(st *elfinState, id int64) *elfinPlan {
	for i := range st.Plans {
		if st.Plans[i].ID == id {
			return &st.Plans[i]
		}
	}
	return nil
}

func (e *elfinSvc) putTeam(st *elfinState, typ, team int64, sprites []elfinSlot, plan int64) {
	tk, k := e.teamKey(typ, team)
	if st.Teams[tk] == nil {
		st.Teams[tk] = map[string]elfinTeam{}
	}
	st.Teams[tk][k] = elfinTeam{Sprites: sprites, PlanID: plan}
}

func (e *elfinSvc) savePlan(s *gateway.Session, req proto.Message) error {
	id, typ, team := req.Int("id"), req.Int("type"), req.Int("team")
	return e.open(s, 26557, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		e.ensurePlans(st)
		p := e.planByID(st, id)
		if p == nil {
			return s.Send(26557, proto.Message{"flag": 0, "msg": "方案不存在"})
		}
		p.Sprites = slotsOf(req.Arr("sprites"))
		e.putTeam(st, typ, team, p.Sprites, id)
		return s.Send(26557, proto.Message{"flag": 1, "msg": "保存成功"})
	})
}

func (e *elfinSvc) renamePlan(s *gateway.Session, req proto.Message) error {
	id, name := req.Int("id"), req.Str("name")
	return e.open(s, 26558, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		e.ensurePlans(st)
		p := e.planByID(st, id)
		switch {
		case p == nil:
			return s.Send(26558, proto.Message{"flag": 0, "msg": "方案不存在"})
		case name == "" || utf8.RuneCountInString(name) > elfinPlanNameMax:
			return s.Send(26558, proto.Message{"flag": 0, "msg": "名称长度不合法"})
		}
		p.Name = name
		return s.Send(26558, proto.Message{"flag": 1, "msg": "修改成功"})
	})
}

func (e *elfinSvc) saveTeam(s *gateway.Session, req proto.Message) error {
	typ, team := req.Int("type"), req.Int("team")
	return e.open(s, 26560, proto.Message{"flag": 0, "type": typ, "team": team}, func(role *store.Role, st *elfinState) error {
		e.putTeam(st, typ, team, slotsOf(req.Arr("sprites")), 0)
		return s.Send(26560, proto.Message{"flag": 1, "msg": "保存成功", "type": typ, "team": team})
	})
}

func (e *elfinSvc) applyPlan(s *gateway.Session, req proto.Message) error {
	typ, team, id := req.Int("type"), req.Int("team"), req.Int("id")
	return e.open(s, 26561, proto.Message{"flag": 0, "type": typ, "team": team}, func(role *store.Role, st *elfinState) error {
		e.ensurePlans(st)
		p := e.planByID(st, id)
		if p == nil {
			return s.Send(26561, proto.Message{"flag": 0, "msg": "方案不存在", "type": typ, "team": team})
		}
		e.putTeam(st, typ, team, p.Sprites, id)
		return s.Send(26561, proto.Message{"flag": 1, "msg": "应用成功", "type": typ, "team": team})
	})
}

func (e *elfinSvc) buyPlan(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.open(s, 26562, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		e.ensurePlans(st)
		refuse := func(msg string) error { return s.Send(26562, proto.Message{"flag": 0, "msg": msg}) }
		if id != int64(len(st.Plans))+1 || id > elfinCfg.num("total_programmes") {
			return refuse("无法开启该方案")
		}
		var cost [][]int64
		var raw []int64
		if err := jsonInts(elfinCfg.Const["programme_add_cost"], &raw); err == nil && len(raw) == 2 {
			cost = [][]int64{raw}
		}
		ok, err := e.pay(s, role, cost, 1)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("钻石不足")
		}
		st.Plans = append(st.Plans, elfinPlan{ID: id, Name: fmt.Sprintf("方案%d", id)})
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(26562, proto.Message{"flag": 1, "msg": "开启成功"})
	})
}

// planTip (26563) warns before an item is used up that a plan or a team of the role needs.
func (e *elfinSvc) planTip(s *gateway.Session, req proto.Message) error {
	bid, num := req.Int("base_id"), req.Int("num")
	return e.with(s, func(role *store.Role, st *elfinState) error {
		uses, name, id := int64(0), "", int64(0)
		count := func(list []elfinSlot) bool {
			for _, p := range list {
				if p.Bid == bid {
					return true
				}
			}
			return false
		}
		for _, p := range st.Plans {
			if count(p.Sprites) {
				uses++
				name, id = p.Name, p.ID
			}
		}
		for _, b := range st.Slots {
			if b == bid {
				uses++
			}
		}
		if uses > 0 && e.bagOf(role.RID)[bid]-num < uses {
			return s.Send(26563, proto.Message{"flag": 1, "type": 1, "id": id, "name": name})
		}
		return s.Send(26563, proto.Message{"flag": 0, "type": 0, "id": 0, "name": ""})
	})
}

// reorder (26564) moves the teams of a kind to new places.
func (e *elfinSvc) reorder(s *gateway.Session, req proto.Message) error {
	typ := req.Int("type")
	return e.open(s, 26564, proto.Message{"flag": 0}, func(role *store.Role, st *elfinState) error {
		tk, _ := e.teamKey(typ, 1)
		old := st.Teams[tk]
		next := map[string]elfinTeam{}
		for _, m := range req.Arr("team_list") {
			_, from := e.teamKey(typ, m.Int("old_team"))
			_, to := e.teamKey(typ, m.Int("team"))
			if t, ok := old[from]; ok {
				next[to] = t
			}
		}
		st.Teams[tk] = next
		return s.Send(26564, proto.Message{"flag": 1, "msg": "调整成功"})
	})
}
