package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// returnaction.json: the return action of the client (tools/extract_returnaction.py): a welcome gift, a lottery pool of 100
// entries, daily tasks, 7 days of sign-in, a shop and a red packet that a returning player's friends can grab.
//
//go:embed data/returnaction.json
var returnActionJSON []byte

const (
	retModule   = "returnaction"
	retCamp     = 101 // ReturnActionConstants.ReturnActionType.privilege: the camp_id the operator gives the activity in the schedule
	retMinLev   = 40  // data_privilege.open_cond: level 40 and 16 days away
	retAwayDays = 16
	retRedLev   = 10 // data_constant.red_packet_cond: level 10
	retRedList  = 50 // packets the list shows
	retDayLimit = 100
)

type retTask struct {
	ID, Target int64
	Title      string
	Desc       string
	Award      [][2]int64
	Label      string
}

type retEntry struct {
	ID, Must int64
	TypeID   int64 `json:"type_id"`
	MustNot  int64 `json:"must_not"`
	Show     int64
	Rewards  [][2]int64
}

type retShopRow struct {
	ID       int64
	Award    [][2]int64
	Expend   [][2]int64
	LimitAll int64 `json:"limit_all"`
	LimitDay int64 `json:"limit_day"`
}

var retCfg = func() (c struct {
	Const     map[string]json.RawMessage
	Privilege struct {
		HolidayDay int64 `json:"holiday_day"`
	}
	Gift   [][2]int64
	Signin []struct {
		Day     int64
		Rewards [][2]int64
	}
	Tasks  []retTask
	Summon []retEntry
	Shop   []retShopRow
}) {
	if err := json.Unmarshal(returnActionJSON, &c); err != nil || len(c.Signin) == 0 || len(c.Tasks) == 0 || len(c.Summon) == 0 || len(c.Shop) == 0 {
		panic("game: bad returnaction.json")
	}
	return
}()

func retNum(name string) int64 {
	var n int64
	_ = json.Unmarshal(retCfg.Const[name], &n)
	return n
}

// retPair is the first [item, amount] pair of a constant that holds a list of them.
func retPair(name string) [2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(retCfg.Const[name], &l)
	if len(l) == 0 {
		return [2]int64{}
	}
	return l[0]
}

// retRole is what a role has done in the activity. A returning role is one that came back after the activity opened.
type retRole struct {
	Start    int64 // the activity's start this belongs to
	Since    int64 // when the role came back; 0 when it did not
	Gift     bool
	Drawn    []int64 // the ids of the pool entries taken
	FreeUsed bool
	Day      int64
	Prog     map[int64]int64 // the tasks of the day
	Claimed  []int64
	Signed   []int64
	Shop     map[int64]int64 // bought, all time
	ShopDay  map[int64]int64 // bought today
	Got      int64           // red packets grabbed today
}

type retGrab struct {
	RID   int64  `json:"rid"`
	SrvID string `json:"srv"`
	Name  string `json:"name"`
	Lev   int64  `json:"lev"`
	Face  int64  `json:"face"`
	Time  int64  `json:"time"`
	Red   int64  `json:"red"`
	Num   int64  `json:"num"`
}

type retPacket struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Face  int64     `json:"face"`
	Sent  int64     `json:"sent"`
	Left  int64     `json:"left"`
	Grabs []retGrab `json:"grabs"`
}

type retWorld struct {
	Start   int64       `json:"start"`
	NextID  int64       `json:"next"`
	Packets []retPacket `json:"packets"`
	Feed    []retGrab   `json:"feed"`
}

type retCtx struct {
	role                  *store.Role
	d                     *retRole
	w                     *retWorld
	now                   time.Time
	start, end            int64
	open, returning       bool
	openDay, until        int64
	holidayDay, packetTTL int64
}

func (d *retRole) drawnOf(typeID int64) (n int64) {
	for _, e := range retCfg.Summon {
		if e.TypeID == typeID && has(d.Drawn, e.ID) {
			n++
		}
	}
	return
}

func retLoad(st *store.Store, role *store.Role, now time.Time) (*retCtx, error) {
	if fresh, err := st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	c := &retCtx{role: role, now: now, d: &retRole{}, w: &retWorld{}, holidayDay: retCfg.Privilege.HolidayDay, packetTTL: retNum("effect_time") * 3600}
	if _, err := st.LoadState(role.RID, retModule, c.d); err != nil {
		return nil, err
	}
	c.start, c.end, c.open = ActivityOpen(st, role.SrvID, retCamp, now)
	if !c.open {
		return c, nil
	}
	if _, err := st.LoadGlobal(role.SrvID, retModule, c.w); err != nil {
		return nil, err
	}
	if c.w.Start != c.start {
		*c.w = retWorld{Start: c.start}
	}
	if c.d.Start != c.start {
		*c.d = retRole{Start: c.start}
	}
	if c.d.Day != dayKey(now) {
		c.d.Day, c.d.Prog, c.d.Claimed, c.d.ShopDay, c.d.Got = dayKey(now), map[int64]int64{}, nil, map[int64]int64{}, 0
	}
	if c.d.Prog == nil {
		c.d.Prog = map[int64]int64{}
	}
	if c.d.Shop == nil {
		c.d.Shop = map[int64]int64{}
	}
	if c.d.ShopDay == nil {
		c.d.ShopDay = map[int64]int64{}
	}
	if c.d.Since > 0 {
		from := dayStart(time.Unix(c.d.Since, 0))
		c.openDay = (dayStart(now).Unix()-from.Unix())/86400 + 1
		c.until = from.AddDate(0, 0, int(c.holidayDay)).Unix()
		if c.end > 0 && c.end < c.until {
			c.until = c.end
		}
		c.returning = c.openDay <= c.holidayDay && now.Unix() < c.until
	}
	return c, nil
}

func retSave(st *store.Store, c *retCtx) error {
	if !c.open {
		return nil
	}
	if err := st.SaveGlobal(c.role.SrvID, retModule, c.w); err != nil {
		return err
	}
	return st.SaveState(c.role.RID, retModule, c.d)
}

// ReturnEntered is called when a role logs in: a role of level 40 that was away for 16 days while the activity is open is a
// returning player for 14 days from now. role is the row as it was before this login touched it.
func ReturnEntered(st *store.Store, role *store.Role, now time.Time) {
	start, _, open := ActivityOpen(st, role.SrvID, retCamp, now)
	if !open || role.Lev < retMinLev {
		return
	}
	prev := role.LastLogoutAt
	if prev == 0 {
		prev = role.LastLoginAt
	}
	if prev == 0 || now.Unix()-prev < retAwayDays*86400 {
		return
	}
	d := &retRole{}
	if _, err := st.LoadState(role.RID, retModule, d); err != nil || (d.Start == start && d.Since > 0) {
		return
	}
	*d = retRole{Start: start, Since: now.Unix()}
	_ = st.SaveState(role.RID, retModule, d)
}

func retWith(s *gateway.Session, st *store.Store, fn func(c *retCtx) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	c, err := retLoad(st, role, s.Now())
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	return retSave(st, c)
}

func retTaskMsg(c *retCtx, t retTask) proto.Message {
	v := min(c.d.Prog[t.ID], t.Target)
	finish := int64(0)
	switch {
	case has(c.d.Claimed, t.ID):
		finish = 2
	case v >= t.Target:
		finish = 1
	}
	return proto.Message{"id": t.ID, "finish": finish, "target_val": t.Target, "value": v, "ref_time": dayStart(c.now).AddDate(0, 0, 1).Unix()}
}

// returnEvent counts a game event for the tasks of the day and tells a returning role when one is ready (27906).
func returnEvent(s *gateway.Session, st *store.Store, role *store.Role, label string, n int64) error {
	if role == nil || n <= 0 {
		return nil
	}
	if label == evtGuildBoss {
		label = "evt_guild_dun_fight"
	}
	wanted := false
	for _, t := range retCfg.Tasks {
		wanted = wanted || t.Label == label
	}
	if !wanted {
		return nil
	}
	c, err := retLoad(st, role, s.Now())
	if err != nil || !c.returning {
		return err
	}
	var ready []proto.Message
	for _, t := range retCfg.Tasks {
		if t.Label != label || has(c.d.Claimed, t.ID) || c.d.Prog[t.ID] >= t.Target {
			continue
		}
		c.d.Prog[t.ID] = min(c.d.Prog[t.ID]+n, t.Target)
		ready = append(ready, retTaskMsg(c, t))
	}
	if len(ready) == 0 {
		return nil
	}
	if err := retSave(st, c); err != nil {
		return err
	}
	for _, m := range ready {
		if err := s.Send(27906, m); err != nil {
			return err
		}
	}
	return nil
}

func retGain(s *gateway.Session, st *store.Store, rid int64, list [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	var known []sellGain
	for _, g := range guildGains(list) {
		if ItemKnown(g.ID) {
			known = append(known, g)
		}
	}
	splitGains(known, 1, ga, gi)
	if err := st.Exchange(rid, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, st, rid); err != nil {
		return err
	}
	return sendBackpack(s, st)
}

// retRedbagMsg is the count push (27912); code 2 tells the main screen to show the packet.
func retRedbagMsg(c *retCtx) proto.Message {
	code := int64(0)
	for _, p := range c.w.Packets {
		if c.packetState(p) == 1 {
			code = 2
		}
	}
	return proto.Message{"code": code, "msg": "", "get_num": c.d.Got, "max_num": retNum("max_day_get_num")}
}

// packetState is 1 for a packet the role can open, 2 for one it opened and 0 for one that is empty or over.
func (c *retCtx) packetState(p retPacket) int64 {
	for _, g := range p.Grabs {
		if g.RID == c.role.RID {
			return 2
		}
	}
	if int64(len(p.Grabs)) >= retNum("max_get_num") || p.Sent+c.packetTTL <= c.now.Unix() {
		return 0
	}
	return 1
}

func retGrabMsg(g retGrab) proto.Message {
	return proto.Message{"r_rid": g.RID, "r_srvid": g.SrvID, "red_id": g.Red, "name": g.Name, "lev": g.Lev, "face_id": g.Face, "avatar_bid": 0, "time": g.Time,
		"item": []proto.Message{{"item_id": 1, "num": g.Num}}}
}

// returnRedbagUse is 10515 of the red packet item: each one sent becomes a packet in the server's list. Not told to others: they
// see it the next time they open the list.
func returnRedbagUse(s *gateway.Session, st *store.Store, role *store.Role, n int64, reply func(int, string) error) error {
	c, err := retLoad(st, role, s.Now())
	if err != nil {
		return err
	}
	switch {
	case !c.open:
		return reply(0, "活动未开启")
	case n <= 0 || n > maxUseAtOnce:
		return reply(0, "使用数量不正确")
	}
	item := retNum("red_item_id")
	if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{item: n}, map[int64]int64{}, map[int64]int64{}); err != nil {
		if err == store.ErrConflict {
			return reply(0, "物品数量不足")
		}
		return err
	}
	face := int64(c.role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	for i := int64(0); i < n; i++ {
		c.w.NextID++
		c.w.Packets = append(c.w.Packets, retPacket{ID: c.w.NextID, Name: c.role.Name, Face: face, Sent: c.now.Unix(), Left: retPair("red_item_value")[1]})
	}
	// packets past their time are dropped after a week: the list shows only the live ones
	keep := c.w.Packets[:0]
	for _, p := range c.w.Packets {
		if p.Sent+7*24*3600 > c.now.Unix() {
			keep = append(keep, p)
		}
	}
	c.w.Packets = keep
	if err := retSave(st, c); err != nil {
		return err
	}
	if err := reply(1, "使用成功"); err != nil {
		return err
	}
	if err := sendBackpack(s, st); err != nil {
		return err
	}
	return s.Send(27912, retRedbagMsg(c))
}

// RegisterReturnAction registers the return action (27900~27915). The operator opens the activity with camp_id 101 in the schedule;
// a role of level 40 that logs in after 16 days away is a returning player for 14 days (ReturnEntered). Ours, not captured: the
// rule that finds the returning players is the client's text (level 40, away 15 days) with the table's numbers (16 days, 14 days),
// the draw (one entry of the 100 is taken each time, "must" N makes it come at the Nth draw and "must_not" N keeps it from the
// first N-1, so 100 draws take everything), the type of 27904 (1 the free draw or one for the ticket, 2 one for tickets with
// the lack paid in diamonds at 1:1, 3 ten of them likewise), the tasks and the shop limits renewing every day, the red packet
// item making a packet of the table's 100000 coins that eight others share. The time bottles that the shop takes have no source
// here (the friend duels with returning players that give them are not counted): the operator hands them out. The tickets are
// not recalled in coins at the end of the activity.
func RegisterReturnAction(r *gateway.Router, st *store.Store) {
	r.Handle(27900, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			flag := int64(1)
			if c.returning && !c.d.Gift {
				flag = 0
			}
			return s.Send(27900, proto.Message{"flag": flag, "endtime": c.until})
		})
	})
	r.Handle(27901, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			refuse := func(msg string) error { return s.Send(27901, proto.Message{"code": 0, "msg": msg}) }
			switch {
			case !c.returning:
				return refuse("你不是回归玩家")
			case c.d.Gift:
				return refuse("已经领取过了")
			}
			c.d.Gift = true
			if err := retGain(s, st, c.role.RID, retCfg.Gift); err != nil {
				return err
			}
			if err := s.Send(27901, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
				return err
			}
			return s.Send(27900, proto.Message{"flag": 1, "endtime": c.until})
		})
	})
	r.Handle(27902, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			if !c.returning {
				return s.Send(27902, proto.Message{"period": 1, "open_day": 0, "is_open": 0})
			}
			return s.Send(27902, proto.Message{"period": 1, "open_day": c.openDay, "is_open": 1})
		})
	})

	// the pool
	page := func(c *retCtx) proto.Message {
		free := int64(0)
		if !c.d.FreeUsed {
			free = retNum("free_draw_time")
		}
		var list []proto.Message
		seen := map[int64]bool{}
		for _, e := range retCfg.Summon {
			if seen[e.TypeID] {
				continue
			}
			seen[e.TypeID] = true
			limit := int64(0)
			for _, x := range retCfg.Summon {
				if x.TypeID == e.TypeID {
					limit++
				}
			}
			list = append(list, proto.Message{"type_id": e.TypeID, "get_count": c.d.drawnOf(e.TypeID), "limit_count": limit, "is_show": e.Show})
		}
		return proto.Message{"endtime": c.until, "starttime": c.d.Since, "draw_time": len(c.d.Drawn), "limit_draw_time": len(retCfg.Summon), "free_time": free, "award_list": list}
	}
	r.Handle(27903, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			if !c.returning {
				return s.Send(27903, proto.Message{"endtime": 0, "starttime": 0, "draw_time": 0, "limit_draw_time": len(retCfg.Summon), "free_time": 0, "award_list": []proto.Message{}})
			}
			return s.Send(27903, page(c))
		})
	})
	r.Handle(27904, func(s *gateway.Session, req proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			refuse := func(msg string) error { return s.Send(27904, proto.Message{"code": 0, "msg": msg}) }
			typ := req.Int("type")
			times := int64(1)
			if typ == 3 {
				times = 10
			}
			switch {
			case !c.returning:
				return refuse("你不是回归玩家")
			case typ < 1 || typ > 3:
				return refuse("操作不合法")
			case int64(len(c.d.Drawn)) >= int64(len(retCfg.Summon)):
				return refuse("奖励已全部抽完")
			}
			times = min(times, int64(len(retCfg.Summon)-len(c.d.Drawn)))
			spendAssets, spendItems := map[int64]int64{}, map[int64]int64{}
			cost := retPair("draw_item_cost")
			need := cost[1] * times
			free := typ == 1 && !c.d.FreeUsed
			if free {
				need = 0
			}
			if need > 0 {
				ticket, _ := st.RoleItems(c.role.RID)
				have := ticket[cost[0]]
				use := min(have, need)
				if use > 0 {
					spendItems[cost[0]] = use
				}
				if short := need - use; short > 0 {
					if typ == 1 {
						return refuse("英雄祈愿券不足")
					}
					value := retPair("draw_item_value")
					spendAssets[value[0]] = short * value[1]
				}
			}
			if err := st.Exchange(c.role.RID, spendAssets, spendItems, map[int64]int64{}, map[int64]int64{}); err != nil {
				if err == store.ErrConflict {
					return refuse("钻石不足")
				}
				return err
			}
			if free {
				c.d.FreeUsed = true
			}
			var prize [][2]int64
			for i := int64(0); i < times; i++ {
				e := retDraw(c.d.Drawn)
				c.d.Drawn = append(c.d.Drawn, e.ID)
				prize = append(prize, e.Rewards...)
			}
			if err := retGain(s, st, c.role.RID, prize); err != nil {
				return err
			}
			if err := s.Send(27904, proto.Message{"code": 1, "msg": "祈愿成功"}); err != nil {
				return err
			}
			return s.Send(27903, page(c))
		})
	})

	// the tasks
	r.Handle(27905, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			list := []proto.Message{}
			if c.returning {
				for _, t := range retCfg.Tasks {
					list = append(list, retTaskMsg(c, t))
				}
			}
			return s.Send(27905, proto.Message{"quest_list": list, "endtime": c.until, "period": 1})
		})
	})
	r.Handle(27907, func(s *gateway.Session, req proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			id := req.Int("id")
			refuse := func(msg string) error { return s.Send(27907, proto.Message{"code": 0, "msg": msg, "id": id}) }
			if !c.returning {
				return refuse("你不是回归玩家")
			}
			for _, t := range retCfg.Tasks {
				if t.ID != id {
					continue
				}
				switch {
				case has(c.d.Claimed, id):
					return refuse("奖励已领取")
				case c.d.Prog[id] < t.Target:
					return refuse("任务未完成")
				}
				c.d.Claimed = append(c.d.Claimed, id)
				if err := retGain(s, st, c.role.RID, t.Award); err != nil {
					return err
				}
				return s.Send(27907, proto.Message{"code": 1, "msg": "领取成功", "id": id})
			}
			return refuse("任务不存在")
		})
	})

	// the sign-in
	r.Handle(27908, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			list := []proto.Message{}
			if c.returning {
				for _, d := range retCfg.Signin {
					status := int64(0)
					switch {
					case has(c.d.Signed, d.Day):
						status = 2
					case d.Day <= c.openDay:
						status = 1
					}
					list = append(list, proto.Message{"day": d.Day, "status": status})
				}
			}
			return s.Send(27908, proto.Message{"endtime": c.until, "status_list": list})
		})
	})
	r.Handle(27909, func(s *gateway.Session, req proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			day := req.Int("day")
			refuse := func(msg string) error { return s.Send(27909, proto.Message{"code": 0, "msg": msg, "day": day}) }
			if !c.returning {
				return refuse("你不是回归玩家")
			}
			for _, d := range retCfg.Signin {
				if d.Day != day {
					continue
				}
				switch {
				case has(c.d.Signed, day):
					return refuse("已经签到过了")
				case day > c.openDay:
					return refuse("还没到签到时间")
				}
				c.d.Signed = append(c.d.Signed, day)
				if err := retGain(s, st, c.role.RID, d.Rewards); err != nil {
					return err
				}
				return s.Send(27909, proto.Message{"code": 1, "msg": "签到成功", "day": day})
			}
			return refuse("签到日不存在")
		})
	})

	// the red packets
	r.Handle(27910, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			list := []proto.Message{}
			for i := len(c.w.Packets) - 1; i >= 0 && len(list) < retRedList; i-- {
				p := c.w.Packets[i]
				if p.Sent+c.packetTTL <= c.now.Unix() {
					continue
				}
				list = append(list, proto.Message{"red_packet_id": p.ID, "msg_id": 1, "name": p.Name, "status": c.packetState(p)})
			}
			return s.Send(27910, proto.Message{"red_packet_list": list, "get_num": c.d.Got, "max_num": retNum("max_day_get_num")})
		})
	})
	r.Handle(27911, func(s *gateway.Session, req proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			id := req.Int("red_packet_id")
			refuse := func(msg string) error { return s.Send(27911, proto.Message{"code": 0, "msg": msg}) }
			if !c.open {
				return refuse("活动未开启")
			}
			if c.role.Lev < retRedLev {
				return refuse("等级不足")
			}
			var p *retPacket
			for i := range c.w.Packets {
				if c.w.Packets[i].ID == id {
					p = &c.w.Packets[i]
				}
			}
			if p == nil || p.Sent+c.packetTTL <= c.now.Unix() {
				return refuse("红包已过期")
			}
			view := func() error {
				grabs := []proto.Message{}
				for _, g := range p.Grabs {
					grabs = append(grabs, retGrabMsg(g))
				}
				return s.Send(27911, proto.Message{"code": 1, "msg": "", "get_red_packet_list": grabs, "red_packet_id": p.ID, "get_num": len(p.Grabs), "max_num": retNum("max_get_num"),
					"end_time": p.Sent + c.packetTTL, "send_name": p.Name, "send_face_id": p.Face, "send_avatar_bid": 0})
			}
			state := c.packetState(*p)
			switch {
			case state == 2:
				return view()
			case state == 0:
				return refuse("红包已被领完")
			case c.d.Got >= retNum("max_day_get_num"):
				return refuse("今日领取次数已达上限")
			}
			slots := retNum("max_get_num") - int64(len(p.Grabs))
			share := p.Left
			if slots > 1 {
				avg := p.Left / slots
				share = max(1, min(avg/2+rand.Int63n(max(avg, 1)+1), p.Left-(slots-1)))
			}
			p.Left -= share
			face := int64(c.role.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			g := retGrab{RID: c.role.RID, SrvID: c.role.SrvID, Name: c.role.Name, Lev: int64(c.role.Lev), Face: face, Time: c.now.Unix(), Red: p.ID, Num: share}
			p.Grabs = append(p.Grabs, g)
			c.d.Got++
			c.w.Feed = append([]retGrab{g}, c.w.Feed...)
			if limit := int(retNum("broadcast_num")); len(c.w.Feed) > limit {
				c.w.Feed = c.w.Feed[:limit]
			}
			if err := retGain(s, st, c.role.RID, [][2]int64{{retPair("red_item_value")[0], share}}); err != nil {
				return err
			}
			if err := view(); err != nil {
				return err
			}
			return s.Send(27912, retRedbagMsg(c))
		})
	})
	r.Handle(27913, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			list := []proto.Message{}
			for _, g := range c.w.Feed {
				list = append(list, retGrabMsg(g))
			}
			return s.Send(27913, proto.Message{"get_red_packet_list": list})
		})
	})

	// the shop
	shopInfo := func(c *retCtx) proto.Message {
		list := []proto.Message{}
		for _, row := range retCfg.Shop {
			list = append(list, proto.Message{"id": row.ID, "day_num": c.d.ShopDay[row.ID], "all_num": c.d.Shop[row.ID]})
		}
		return proto.Message{"endtime": c.until, "get_count": 0, "limit_count": retNum("exchange_item_day_limit_num"), "buy_info": list}
	}
	r.Handle(27914, func(s *gateway.Session, _ proto.Message) error {
		return retWith(s, st, func(c *retCtx) error { return s.Send(27914, shopInfo(c)) })
	})
	r.Handle(27915, func(s *gateway.Session, req proto.Message) error {
		return retWith(s, st, func(c *retCtx) error {
			id := req.Int("id")
			refuse := func(msg string) error { return s.Send(27915, proto.Message{"code": 0, "msg": msg, "id": id}) }
			if !c.open || c.role.Lev < retRedLev {
				return refuse("暂时不能兑换")
			}
			for _, row := range retCfg.Shop {
				if row.ID != id {
					continue
				}
				switch {
				case row.LimitAll > 0 && c.d.Shop[id] >= row.LimitAll:
					return refuse("兑换次数已达上限")
				case row.LimitDay > 0 && c.d.ShopDay[id] >= row.LimitDay:
					return refuse("今日兑换次数已达上限")
				}
				spendAssets, spendItems := map[int64]int64{}, map[int64]int64{}
				var cost []sellGain
				for _, e := range row.Expend {
					cost = append(cost, sellGain{guildLabel(e[0]), e[1]})
				}
				splitGains(cost, 1, spendAssets, spendItems)
				if err := st.Exchange(c.role.RID, spendAssets, spendItems, map[int64]int64{}, map[int64]int64{}); err != nil {
					if err == store.ErrConflict {
						return refuse("时光漂流瓶不足")
					}
					return err
				}
				c.d.Shop[id]++
				c.d.ShopDay[id]++
				if err := retGain(s, st, c.role.RID, row.Award); err != nil {
					return err
				}
				if err := s.Send(27915, proto.Message{"code": 1, "msg": "兑换成功", "id": id}); err != nil {
					return err
				}
				return s.Send(27914, shopInfo(c))
			}
			return refuse("商品不存在")
		})
	})
}

// retDraw takes one entry of the pool that is not taken yet: the one that must come at this draw, else a random one among those
// that may (their must_not has been passed), else any.
func retDraw(drawn []int64) retEntry {
	n := int64(len(drawn)) + 1
	var left, may []retEntry
	for _, e := range retCfg.Summon {
		if has(drawn, e.ID) {
			continue
		}
		if e.Must == n {
			return e
		}
		left = append(left, e)
		if e.MustNot <= n {
			may = append(may, e)
		}
	}
	if len(may) == 0 {
		may = left
	}
	sort.Slice(may, func(i, j int) bool { return may[i].ID < may[j].ID })
	return may[rand.Intn(len(may))]
}
