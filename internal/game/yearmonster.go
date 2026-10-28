package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// yearmonster.json: the year monster action of the client (actionyearmonster 28200~28224, tools/extract_yearmonster.py): the
// map 100001, its event kinds, the three monsters, the damage bands and rewards.
//
//go:embed data/yearmonster.json
var yearMonsterJSON []byte

const (
	ymModule  = "yearmonster"
	ymCamp    = 93032 // ActionRankCommonType.year_monster_fight: the camp_id the operator gives the activity in the schedule
	ymForm    = 32    // PartnerConst.Fun_Form.YearMonster
	combatYM  = 39    // BattleConst.Fight_Type.YearMonsterWar
	ymMaxVal  = 1800  // the last of the red bag steps, which the client takes as the full bar
	ymCakeVal = 10    // the progress one cake gives: ours
	ymBossHP  = 1_000_000_000_000
	ymSmallHP = 60000 * 3 // the small monster: ours, the client has the unit but no power
	ymSmallPW = 60000
	ymSmallLv = 80
	ymRedRain = 5  // red bag tiles on the map while the rain lasts
	ymGoldHrs = 48 // the gold monster is there for the last two days of the activity
	ymRankMax = 50
	ymTouch   = 3 // how near a role must be to an event it acts on
)

// the event kinds of the map (ActionyearmonsterConstants.Evt_Type)
const (
	ymEvtNormal   = 0
	ymEvtMonster  = 1
	ymEvtStart    = 2
	ymEvtBoard    = 3
	ymEvtGoods    = 4
	ymEvtDialog   = 5
	ymEvtTribute  = 9
	ymEvtFirework = 10
	ymEvtRedBag   = 11
	ymEvtTimer    = 12
	ymEvtGold     = 13
	ymEvtChar     = 8
)

type ymEvent struct {
	Type   int64
	Name   string
	IsWalk int64 `json:"is_walk"`
	Reward [][2]int64
	Ext    []json.RawMessage
	Skills []int64
}

var ymCfg = func() (c struct {
	Const  map[string]json.RawMessage
	Events map[string]ymEvent
	Harm   map[string][][2]int64
	Ranks  []struct {
		Min, Max int64
		Reward   [][2]int64
	}
	Exchange []struct {
		ID, Time   int64
		Loss, Gain [][2]int64
	}
	Redbag []int64
	Units  struct{ Timer, Gold, Small monsterDef }
	Map    plMap
}) {
	if err := json.Unmarshal(yearMonsterJSON, &c); err != nil || len(c.Events) == 0 || len(c.Exchange) == 0 || len(c.Map.Block) == 0 {
		panic("game: bad yearmonster.json")
	}
	return
}()

func ymNum(name string) int64 {
	var n int64
	_ = json.Unmarshal(ymCfg.Const[name], &n)
	return n
}

func ymPair(name string) [2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(ymCfg.Const[name], &l)
	if len(l) == 0 {
		return [2]int64{}
	}
	return l[0]
}

func ymEvt(id int64) ymEvent { return ymCfg.Events[strconv.FormatInt(id, 10)] }

// ymCells are the road cells of the map.
var ymCells = func() []int64 {
	var out []int64
	for y := int64(1); y <= int64(len(ymCfg.Map.Block)); y++ {
		for x := int64(1); x <= int64(len(ymCfg.Map.Block[y-1])); x++ {
			if ymCfg.Map.walkable(x, y) {
				out = append(out, plIndex(x, y))
			}
		}
	}
	return out
}()

type ymFace struct{ Order, Face int64 }

type ymRole struct {
	Start   int64
	Pos     int64
	Taken   []string // the tiles taken (by their keys)
	Day     int64
	Reds    int64 // red bags taken today
	Round   int64 // the timer monster's round the fight counts belong to
	GoldWin int64 // the gold monster's window the counts belong to
	Left    [2]int64
	Buys    [2]int64
	Exch    map[int64]int64
	Faces   []ymFace
}

type ymRed struct {
	RID   int64  `json:"rid"`
	SrvID string `json:"srv"`
	Name  string `json:"name"`
	Face  int64  `json:"face"`
	Time  int64  `json:"time"`
	Items [][2]int64
}

type ymSeen struct {
	Name string `json:"name"`
	Look int64  `json:"look"`
	At   int64  `json:"at"`
	Face []ymFace
}

type ymWorld struct {
	Start, End int64
	Val        int64 // the cakes handed in, as progress; the full bar wakes the timer monster
	Round      int64
	TimerEnd   int64
	Passed     []int64 // the red bag steps passed in this fill
	RedLeft    int64
	RedAll     int64
	RedSeq     int64
	RedLog     []ymRed
	Timer      map[int64]int64 // damage per role, of the round
	Gold       map[int64]int64
	GoldDone   bool
	Seen       map[int64]ymSeen
}

type ymTile struct {
	Key        string
	Index, Evt int64
}

type ymCtx struct {
	role                *store.Role
	d                   *ymRole
	w                   *ymWorld
	now                 time.Time
	open                bool
	goldFrom, goldUntil int64
}

func (c *ymCtx) timerAwake() bool { return c.w.TimerEnd > c.now.Unix() }
func (c *ymCtx) goldAwake() bool {
	return c.goldFrom > 0 && c.now.Unix() >= c.goldFrom && c.now.Unix() < c.goldUntil
}

// ymLayout is the map of the moment: the fixed events and the ones that come back on their hours (monsters at 0, 6, 12, 18, random
// rewards at 0, 8, 16, the sweets every 4 hours, cakes and fireworks every 2), all on road cells, the same for everybody.
func ymLayout(c *ymCtx) []ymTile {
	now := c.now.In(dayZone)
	day, hour := dayKey(c.now), int64(now.Hour())
	used := map[int64]bool{}
	var out []ymTile
	seed := c.w.Start*1000003 + 7
	place := func(kind string, slot int64, evts []int64) {
		rng := rand.New(rand.NewSource(seed + slot*131 + int64(len(kind))*977 + int64(kind[0])*31))
		free := make([]int64, 0, len(ymCells))
		for _, cell := range ymCells {
			if !used[cell] {
				free = append(free, cell)
			}
		}
		rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
		for i, evt := range evts {
			if i >= len(free) {
				return
			}
			used[free[i]] = true
			out = append(out, ymTile{Key: fmt.Sprintf("%s%d.%d", kind, slot, i), Index: free[i], Evt: evt})
		}
	}
	start := ymCells[0]
	used[start] = true
	out = append(out, ymTile{Key: "start", Index: start, Evt: 10001})
	timerEvt, goldEvt := int64(19001), int64(19051)
	if c.timerAwake() {
		timerEvt = 19003
	}
	if c.goldAwake() {
		goldEvt = 19053
	}
	place("fix", 0, []int64{10101, 10201, 10202, timerEvt, goldEvt})
	place("m", day*10+hour/6, []int64{10801, 10801, 10801})
	place("g", day*10+hour/8, []int64{10401, 10401, 10401})
	place("c", day*100+hour/4, []int64{10301, 10302, 10303, 10304})
	place("t", day*100+hour/2, []int64{10501, 10501, 10501, 10501, 10501, 10601, 10601, 10601})
	if c.w.RedLeft > 0 {
		place("r", c.w.RedSeq, []int64{10701, 10701, 10701, 10701, 10701})
	}
	return out
}

// visible are the tiles the role has not taken.
func (c *ymCtx) visible() []ymTile {
	var out []ymTile
	for _, t := range ymLayout(c) {
		if ymEvt(t.Evt).Type == ymEvtRedBag || !hasKey(c.d.Taken, t.Key) {
			out = append(out, t)
		}
	}
	return out
}

func hasKey(l []string, k string) bool {
	for _, x := range l {
		if x == k {
			return true
		}
	}
	return false
}

func (c *ymCtx) tileAt(index int64) (ymTile, bool) {
	for _, t := range c.visible() {
		if t.Index == index {
			return t, true
		}
	}
	return ymTile{}, false
}

func ymTileMsg(t ymTile) proto.Message {
	return proto.Message{"index": t.Index, "evtid": t.Evt, "is_walk": ymEvt(t.Evt).IsWalk, "res_id": 0}
}

func ymTileList(tiles []ymTile) []proto.Message {
	out := []proto.Message{}
	for _, t := range tiles {
		out = append(out, ymTileMsg(t))
	}
	return out
}

// ymLoad loads the state of the role and the server, rolls the server over (the timer monster that went to sleep, the gold monster's ranks
// mailed) and the role to the round it is in.
func ymLoad(st *store.Store, role *store.Role, now time.Time) (*ymCtx, error) {
	if fresh, err := st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	c := &ymCtx{role: role, now: now, d: &ymRole{}, w: &ymWorld{}}
	if _, err := st.LoadState(role.RID, ymModule, c.d); err != nil {
		return nil, err
	}
	if _, err := st.LoadGlobal(role.SrvID, ymModule, c.w); err != nil {
		return nil, err
	}
	start, end, open := ActivityOpen(st, role.SrvID, ymCamp, now)
	c.open = open
	if open {
		if c.w.Start != start {
			*c.w = ymWorld{Start: start}
		}
		c.w.End = end
	}
	if c.w.Timer == nil {
		c.w.Timer = map[int64]int64{}
	}
	if c.w.Gold == nil {
		c.w.Gold = map[int64]int64{}
	}
	if c.w.Seen == nil {
		c.w.Seen = map[int64]ymSeen{}
	}
	if c.w.TimerEnd > 0 && c.w.TimerEnd <= now.Unix() { // the timer monster went to sleep: the bar starts again
		c.w.TimerEnd, c.w.Val, c.w.Passed = 0, 0, nil
	}
	if c.w.End > 0 {
		c.goldUntil = c.w.End
		c.goldFrom = max(c.w.Start, c.w.End-ymGoldHrs*3600)
	}
	if c.w.End > 0 && now.Unix() >= c.w.End && !c.w.GoldDone {
		if err := ymSettleGold(st, role.SrvID, c.w); err != nil {
			return nil, err
		}
		c.w.GoldDone = true
		if err := st.SaveGlobal(role.SrvID, ymModule, c.w); err != nil {
			return nil, err
		}
	}
	if !open {
		return c, nil
	}
	if c.d.Start != start {
		*c.d = ymRole{Start: start}
	}
	if c.d.Pos == 0 {
		c.d.Pos = ymCells[0]
	}
	if day := dayKey(now); c.d.Day != day {
		c.d.Day, c.d.Reds = day, 0
	}
	if c.d.Exch == nil {
		c.d.Exch = map[int64]int64{}
	}
	if c.w.Round != c.d.Round {
		c.d.Round = c.w.Round
		c.d.Left[0], c.d.Buys[0] = ymNum("holiday_nian_timer_monster_free_time"), ymNum("holiday_nian_timer_monster_buy_time")
	}
	if c.goldFrom != c.d.GoldWin {
		c.d.GoldWin = c.goldFrom
		c.d.Left[1], c.d.Buys[1] = ymNum("holiday_nian_gold_monster_free_time"), ymNum("holiday_nian_gold_monster_buy_time")
	}
	// keys of tiles that are gone are forgotten
	var keep []string
	live := map[string]bool{}
	for _, t := range ymLayout(c) {
		live[t.Key] = true
	}
	for _, k := range c.d.Taken {
		if live[k] {
			keep = append(keep, k)
		}
	}
	c.d.Taken = keep
	c.w.Seen[role.RID] = ymSeen{Name: role.Name, Look: ymLook(st, role.RID), At: now.Unix(), Face: c.d.Faces2()}
	return c, nil
}

func (d *ymRole) Faces2() []ymFace { return d.Faces }

func ymLook(st *store.Store, rid int64) int64 {
	h := &homeState{}
	if _, err := st.LoadState(rid, homeModule, h); err == nil {
		return h.Used
	}
	return 0
}

func ymSave(st *store.Store, c *ymCtx) error {
	if err := st.SaveGlobal(c.role.SrvID, ymModule, c.w); err != nil {
		return err
	}
	return st.SaveState(c.role.RID, ymModule, c.d)
}

func ymWith(s *gateway.Session, st *store.Store, fn func(c *ymCtx) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	c, err := ymLoad(st, role, s.Now())
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	if !c.open {
		return nil
	}
	return ymSave(st, c)
}

// ymSettleGold mails the rank rewards of the gold monster.
func ymSettleGold(st *store.Store, srv string, w *ymWorld) error {
	for i, rid := range ymRanking(w.Gold) {
		rank := int64(i + 1)
		for _, r := range ymCfg.Ranks {
			if rank >= r.Min && rank <= r.Max {
				m := store.Mail{SrvID: srv, Subject: "巅峰BIG·MOM排名奖励", Content: "巅峰BIG·MOM已经离开，你的伤害排在第" + itoa64(rank) + "名，这是排名奖励。",
					FromName: "年兽", CreatedBy: "yearmonster", Attachments: attachmentsOf(r.Reward)}
				if _, err := st.CreateMail(m, []int64{rid}); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func ymRanking(m map[int64]int64) []int64 {
	var ids []int64
	for rid, v := range m {
		if v > 0 {
			ids = append(ids, rid)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] || (m[ids[i]] == m[ids[j]] && ids[i] < ids[j]) })
	return ids
}

// package items are the holiday assets the client shows in the bag (10350).
func ymSendAssets(s *gateway.Session, st *store.Store, rid int64) error {
	items, err := st.RoleItems(rid)
	if err != nil {
		return err
	}
	var ids []int64
	_ = json.Unmarshal(ymCfg.Const["package_item"], &ids)
	list := []proto.Message{}
	for _, id := range ids {
		list = append(list, proto.Message{"id": id, "val": items[id]})
	}
	return s.Send(10350, proto.Message{"holiday_assets": list})
}

func ymGain(s *gateway.Session, st *store.Store, rid int64, list [][2]int64) error {
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
	if err := sendBackpack(s, st); err != nil {
		return err
	}
	return ymSendAssets(s, st, rid)
}

func ymBase(st *store.Store, c *ymCtx) proto.Message {
	if !c.open {
		return proto.Message{"camp_id": 0, "val": 0, "max_val": ymMaxVal, "flag": 0, "end_time": 0, "last_red_pacekt_num": 0, "max_red_packet_num": 0, "look_id": 0, "list": []proto.Message{}}
	}
	flag := int64(0)
	if c.timerAwake() {
		flag = 1
	}
	h := &homeState{}
	_, _ = st.LoadState(c.role.RID, homeModule, h)
	figs := []proto.Message{}
	for _, f := range h.Figures {
		figs = append(figs, proto.Message{"id": f})
	}
	return proto.Message{"camp_id": 1, "val": c.w.Val, "max_val": ymMaxVal, "flag": flag, "end_time": c.w.TimerEnd, "last_red_pacekt_num": c.w.RedLeft, "max_red_packet_num": c.w.RedAll,
		"look_id": h.Used, "list": figs}
}

func ymMapMsg(c *ymCtx) proto.Message {
	return proto.Message{"index": c.d.Pos, "map_id": ymNum("holiday_nian_map_id"), "tile_list": ymTileList(c.visible())}
}

func ymRedPoints(c *ymCtx) int64 {
	if c.w.RedLeft > 0 && c.d.Reds < ymNum("holiday_nian_red_packet_day_limit") {
		return 1
	}
	return 0
}

// RegisterYearMonster registers the year monster action (28200~28224): a map to walk with events (cakes, fireworks, sweets, random
// rewards, red bags, a small monster, notices and two NPCs) and two big monsters. It runs while the operator has the activity with
// camp_id 93032 open in the schedule. Everybody hands in cakes (28211, 10 progress a cake) to the server's bar of 1800; at 450 and 1350 a
// rain of 50 red bags comes down (tiles to take, 5 a day each), and the full bar wakes the timer monster for a day: two fights and
// three bought ones (100 diamonds) a round, the damage ranks the roles. The gold monster is on the map for the last two days of the
// activity and its damage rank is mailed when the activity is over. Ours, not captured: the map's event placement and the hours they
// come back on (the client text gives the hours; the client has no placement), the cake's value, the boss's hp (never killed), the reward
// of a fight growing with its damage band, the small monster's power, who is shown on the map (the last five that were there in ten
// minutes). Not made: the barriers and the puzzle events, the exchange windows of the table are in the past and the client shows them
// as over whatever the server says.
func RegisterYearMonster(r *gateway.Router, st *store.Store) {
	r.OnEnter(func(s *gateway.Session, _ proto.Message) error {
		if role := s.Role(); role != nil {
			return ymSendAssets(s, st, role.RID)
		}
		return nil
	})
	fights := fightServiceOf(st)
	sendTiles := func(s *gateway.Session, tiles ...ymTile) error {
		if len(tiles) == 0 {
			return nil
		}
		return s.Send(28218, proto.Message{"tile_list": ymTileList(tiles)})
	}
	vanish := func(s *gateway.Session, c *ymCtx, t ymTile) error {
		c.d.Taken = append(c.d.Taken, t.Key)
		return sendTiles(s, ymTile{Index: t.Index, Evt: 0})
	}
	r.Handle(28200, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			if err := s.Send(28200, ymBase(st, c)); err != nil {
				return err
			}
			if !c.open {
				return nil
			}
			if err := ymSendAssets(s, st, c.role.RID); err != nil {
				return err
			}
			return s.Send(28201, ymMapMsg(c))
		})
	})
	r.Handle(28201, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			if !c.open {
				return nil
			}
			return s.Send(28201, ymMapMsg(c))
		})
	})
	r.Handle(28202, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			index := req.Int("index")
			refuse := func(msg string) error { return s.Send(28202, proto.Message{"code": 0, "msg": msg, "index": index}) }
			if !c.open {
				return refuse("活动未开启")
			}
			x, y := plXY(index)
			cx, cy := plXY(c.d.Pos)
			if !ymCfg.Map.walkable(x, y) {
				return refuse("目标点为不可行走区域，请选择其他目标点")
			}
			if plDistance(cx, cy, x, y) > 2 {
				return refuse("目标点太远")
			}
			if t, ok := c.tileAt(index); ok && ymEvt(t.Evt).Type != ymEvtStart {
				return refuse("目标点上有事件")
			}
			c.d.Pos = index
			return s.Send(28202, proto.Message{"code": 1, "msg": "", "index": index})
		})
	})
	r.Handle(28203, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			index, action := req.Int("index"), req.Int("action")
			reply := func(code int, msg string) error {
				return s.Send(28203, proto.Message{"code": code, "msg": msg, "index": index, "action": action})
			}
			if !c.open {
				return reply(0, "活动未开启")
			}
			t, ok := c.tileAt(index)
			if !ok {
				return reply(0, "事件已经不在了")
			}
			x, y := plXY(index)
			cx, cy := plXY(c.d.Pos)
			if plDistance(cx, cy, x, y) > ymTouch {
				return reply(0, "距离太远")
			}
			kind := ymEvt(t.Evt).Type
			switch kind {
			case ymEvtBoard, ymEvtDialog:
				if err := reply(1, ""); err != nil {
					return err
				}
				typ := int64(1)
				if kind == ymEvtDialog {
					typ = 2
				}
				return s.Send(28205, proto.Message{"index": index, "ext_list": []proto.Message{{"type": typ, "val1": t.Evt, "val2": 0}}})
			case ymEvtGoods:
				prize := ymGoodsPrize()
				if err := vanish(s, c, t); err != nil {
					return err
				}
				if err := reply(1, ""); err != nil {
					return err
				}
				if err := ymGain(s, st, c.role.RID, [][2]int64{prize}); err != nil {
					return err
				}
				return s.Send(28205, proto.Message{"index": index, "ext_list": []proto.Message{{"type": 4, "val1": prize[0], "val2": prize[1]}}})
			case ymEvtChar, ymEvtTribute, ymEvtFirework:
				item := int64(80301 + t.Evt - 10301)
				switch kind {
				case ymEvtTribute:
					item = ymNum("holiday_nian_tribute_id")
				case ymEvtFirework:
					item = ymNum("firework_itemid")
				}
				if err := vanish(s, c, t); err != nil {
					return err
				}
				if err := reply(1, ""); err != nil {
					return err
				}
				return ymGain(s, st, c.role.RID, [][2]int64{{item, 1}})
			case ymEvtRedBag:
				return ymTakeRed(s, st, c, reply)
			case ymEvtMonster:
				if action == 0 {
					if err := reply(1, ""); err != nil {
						return err
					}
					return s.Send(28210, proto.Message{"unit_id": 97004, "unit_lev": ymSmallLv, "power": ymSmallPW})
				}
				return ymFight(s, st, fights, c, t, req, reply, vanish)
			case ymEvtTimer, ymEvtGold:
				if action == 0 {
					if err := reply(1, ""); err != nil {
						return err
					}
					return s.Send(28215, ymChallengeMsg(c, kind, index))
				}
				return ymFight(s, st, fights, c, t, req, reply, vanish)
			case ymEvtStart:
				return reply(1, "")
			}
			return reply(0, "该事件不能操作")
		})
	})
	r.Handle(28204, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			if !c.open {
				return s.Send(28204, proto.Message{"flag": 0, "msg": "活动未开启", "is_first": 0})
			}
			return s.Send(28204, proto.Message{"flag": 1, "msg": "", "is_first": 0})
		})
	})
	r.Handle(28206, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(28206, proto.Message{"holiday_nian_item": []proto.Message{}})
	})
	r.Handle(28211, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			refuse := func(msg string) error { return s.Send(28211, proto.Message{"flag": 0, "msg": msg}) }
			n := req.Int("num")
			switch {
			case !c.open:
				return refuse("活动未开启")
			case n < 1:
				return refuse("数量不对")
			case c.w.Val >= ymMaxVal:
				return refuse("年兽已召唤, 请在年兽沉睡后再试")
			}
			n = min(n, (ymMaxVal-c.w.Val+ymCakeVal-1)/ymCakeVal)
			cake := ymNum("holiday_nian_tribute_id")
			if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{cake: n}, map[int64]int64{}, map[int64]int64{}); err != nil {
				if err == store.ErrConflict {
					return refuse("新春蛋糕数量不足!")
				}
				return err
			}
			before := c.w.Val
			c.w.Val = min(c.w.Val+n*ymCakeVal, ymMaxVal)
			for _, step := range ymCfg.Redbag[:len(ymCfg.Redbag)-1] {
				if before < step && c.w.Val >= step && !has(c.w.Passed, step) {
					c.w.Passed = append(c.w.Passed, step)
					c.w.RedSeq++
					c.w.RedAll = ymNum("holiday_nian_red_packet_max_num")
					c.w.RedLeft, c.w.RedLog = c.w.RedAll, nil
				}
			}
			if c.w.Val >= ymMaxVal { // the full bar wakes the timer monster
				c.w.Round++
				c.w.TimerEnd = c.now.Unix() + ymNum("holiday_nian_timer_monster_time")
				c.w.Timer = map[int64]int64{}
				c.d.Round = c.w.Round
				c.d.Left[0], c.d.Buys[0] = ymNum("holiday_nian_timer_monster_free_time"), ymNum("holiday_nian_timer_monster_buy_time")
			}
			if err := s.Send(28211, proto.Message{"flag": 1, "msg": "提交成功"}); err != nil {
				return err
			}
			if err := ymSendAssets(s, st, c.role.RID); err != nil {
				return err
			}
			if err := s.Send(28200, ymBase(st, c)); err != nil {
				return err
			}
			return s.Send(28201, ymMapMsg(c))
		})
	})
	r.Handle(28212, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			refuse := func(msg string) error { return s.Send(28212, proto.Message{"flag": 0, "msg": msg}) }
			k := req.Int("type") - 1
			if !c.open || k < 0 || k > 1 {
				return refuse("暂时不能购买")
			}
			if (k == 0 && !c.timerAwake()) || (k == 1 && !c.goldAwake()) {
				return refuse("年兽已经离开了")
			}
			if c.d.Buys[k] <= 0 {
				return refuse("购买次数已达上限")
			}
			price := ymPair("holiday_nian_timer_monster_buy_price")
			if k == 1 {
				price = ymPair("holiday_nian_gold_monster_buy_price")
			}
			if err := st.Exchange(c.role.RID, map[int64]int64{guildLabel(price[0]): price[1]}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
				if err == store.ErrConflict {
					return refuse("钻石不足")
				}
				return err
			}
			c.d.Buys[k]--
			c.d.Left[k]++
			if err := sendAssets(s, st, c.role.RID); err != nil {
				return err
			}
			return s.Send(28212, proto.Message{"flag": 1, "msg": ""})
		})
	})
	r.Handle(28213, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			board := c.w.Timer
			if req.Int("type") == 2 {
				board = c.w.Gold
			}
			ids := ymRanking(board)
			limit := int(req.Int("num"))
			if limit <= 0 || limit > ymRankMax {
				limit = ymRankMax
			}
			list := []proto.Message{}
			for i, rid := range ids {
				if i >= limit {
					break
				}
				who, err := st.RoleByID(rid)
				if err != nil {
					continue
				}
				face := int64(who.FaceID)
				if face == 0 {
					face = store.DefaultFaceID
				}
				all, _ := st.PartnersOf(rid)
				list = append(list, proto.Message{"rid": rid, "srv_id": who.SrvID, "lev": who.Lev, "face_id": face, "avatar_bid": 0, "name": who.Name, "dps": min(board[rid], 1<<32-1),
					"power": rolePower(bestHeroes(all, 5)), "rank": i + 1})
			}
			return s.Send(28213, proto.Message{"dps_list": list})
		})
	})
	r.Handle(28216, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			id, num := req.Int("id"), max(req.Int("num"), 1)
			refuse := func(msg string) error { return s.Send(28216, proto.Message{"flag": 0, "msg": msg}) }
			if !c.open {
				return refuse("活动未开启")
			}
			for _, e := range ymCfg.Exchange {
				if e.ID != id {
					continue
				}
				if c.d.Exch[id]+num > e.Time {
					return refuse("兑换次数不足")
				}
				spendItems := map[int64]int64{}
				for _, l := range e.Loss {
					if t, ok := itemTable[l[0]]; !ok || t.Type != 7 {
						spendItems[l[0]] += l[1] * num
					}
				}
				var prize [][2]int64
				for _, g := range e.Gain {
					prize = append(prize, [2]int64{g[0], g[1] * num})
				}
				if err := st.Exchange(c.role.RID, map[int64]int64{}, spendItems, map[int64]int64{}, map[int64]int64{}); err != nil {
					if err == store.ErrConflict {
						return refuse("材料不足")
					}
					return err
				}
				c.d.Exch[id] += num
				if err := ymGain(s, st, c.role.RID, prize); err != nil {
					return err
				}
				return s.Send(28216, proto.Message{"flag": 1, "msg": "兑换成功"})
			}
			return refuse("兑换不存在")
		})
	})
	r.Handle(28217, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			list := []proto.Message{}
			if c.open {
				for _, e := range ymCfg.Exchange {
					list = append(list, proto.Message{"id": e.ID, "last_num": max(0, e.Time-c.d.Exch[e.ID]), "flag": 1, "start_unixtime": c.w.Start, "end_unixtime": c.w.End})
				}
			}
			return s.Send(28217, proto.Message{"collection": list})
		})
	})
	r.Handle(28219, func(s *gateway.Session, req proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			var faces []ymFace
			for _, f := range req.Arr("face") {
				faces = append(faces, ymFace{f.Int("order"), f.Int("face_id")})
			}
			c.d.Faces = faces
			return s.Send(28219, proto.Message{"code": 1, "msg": ""})
		})
	})
	r.Handle(28220, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error { return s.Send(28220, proto.Message{"face": ymFaceMsgs(c.d.Faces)}) })
	})
	r.Handle(28221, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			refuse := func(msg string) error {
				return s.Send(28221, proto.Message{"code": 0, "msg": msg, "reward": []proto.Message{}})
			}
			if !c.open {
				return refuse("活动未开启")
			}
			if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{ymNum("firework_itemid"): 1}, map[int64]int64{}, map[int64]int64{}); err != nil {
				if err == store.ErrConflict {
					return refuse("烟花数量不足")
				}
				return err
			}
			prize := ymGoodsPrize()
			if err := ymGain(s, st, c.role.RID, [][2]int64{prize}); err != nil {
				return err
			}
			return s.Send(28221, proto.Message{"code": 1, "msg": "", "reward": []proto.Message{{"base_id": prize[0], "num": prize[1]}}})
		})
	})
	r.Handle(28222, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			var ids []int64
			limit := c.now.Unix() - ymNum("other_players_time")
			for rid, v := range c.w.Seen {
				if rid != c.role.RID && v.At >= limit {
					ids = append(ids, rid)
				}
			}
			sort.Slice(ids, func(i, j int) bool {
				return c.w.Seen[ids[i]].At > c.w.Seen[ids[j]].At || (c.w.Seen[ids[i]].At == c.w.Seen[ids[j]].At && ids[i] < ids[j])
			})
			if n := int(ymNum("other_players_limit")); len(ids) > n {
				ids = ids[:n]
			}
			list := []proto.Message{}
			for _, rid := range ids {
				v := c.w.Seen[rid]
				list = append(list, proto.Message{"rid": rid, "srv_id": c.role.SrvID, "name": v.Name, "look_id": v.Look, "honor_id": 0, "face": ymFaceMsgs(v.Face)})
			}
			return s.Send(28222, proto.Message{"player": list})
		})
	})
	r.Handle(28223, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			flag := int64(0)
			if c.open {
				flag = ymRedPoints(c)
			}
			return s.Send(28223, proto.Message{"flag": flag})
		})
	})
	r.Handle(28224, func(s *gateway.Session, _ proto.Message) error {
		return ymWith(s, st, func(c *ymCtx) error {
			timer, gold := int64(0), int64(0)
			if c.open {
				if c.timerAwake() && c.d.Left[0] > 0 {
					timer = 1
				}
				if c.goldAwake() && c.d.Left[1] > 0 {
					gold = 1
				}
			}
			return s.Send(28224, proto.Message{"timer_flag": timer, "gold_flag": gold})
		})
	})
}

func ymFaceMsgs(faces []ymFace) []proto.Message {
	out := []proto.Message{}
	if len(faces) == 0 {
		var def [][2]int64
		_ = json.Unmarshal(ymCfg.Const["holiday_nian_default_face"], &def)
		for _, f := range def {
			out = append(out, proto.Message{"order": f[0], "face_id": f[1]})
		}
		return out
	}
	for _, f := range faces {
		out = append(out, proto.Message{"order": f.Order, "face_id": f.Face})
	}
	return out
}

// ymGoodsPrize is what a random reward tile (and a firework) gives: ours, the client names only the drop id.
func ymGoodsPrize() [2]int64 {
	prizes := [][2]int64{{1, 20000}, {22, 5000}, {10450, 10}, {72001, 1}, {29905, 1}, {10040, 2}}
	return prizes[rand.Intn(len(prizes))]
}

func ymChallengeMsg(c *ymCtx, kind, index int64) proto.Message {
	k, unit, left := 0, int64(97001), c.w.TimerEnd-c.now.Unix()
	if kind == ymEvtGold {
		k, unit, left = 1, 97003, c.goldUntil-c.now.Unix()
	}
	return proto.Message{"type": kind, "unit_id": unit, "combat_time": c.d.Left[k], "buy_time": c.d.Buys[k], "last_time": max(0, left), "index": index}
}

// ymTakeRed (28203 on a red bag tile): one red bag of the rain, 5 a day for each role, diamonds at random.
func ymTakeRed(s *gateway.Session, st *store.Store, c *ymCtx, reply func(int, string) error) error {
	info := func(flag int64) proto.Message {
		logs := []proto.Message{}
		for _, l := range c.w.RedLog {
			items := []proto.Message{}
			for _, it := range l.Items {
				items = append(items, proto.Message{"base_id": it[0], "num": it[1]})
			}
			logs = append(logs, proto.Message{"rid": l.RID, "srv_id": l.SrvID, "name": l.Name, "face_id": l.Face, "avatar_bid": 0, "reward_list": items, "unixtime": l.Time})
		}
		return proto.Message{"flag": flag, "last_num": c.w.RedLeft, "all_num": c.w.RedAll, "role_num": c.d.Reds, "role_all_num": ymNum("holiday_nian_red_packet_day_limit"),
			"information": logs, "role_reward_list": []proto.Message{}}
	}
	if err := reply(1, ""); err != nil {
		return err
	}
	if c.w.RedLeft <= 0 || c.d.Reds >= ymNum("holiday_nian_red_packet_day_limit") {
		return s.Send(28214, info(0))
	}
	c.w.RedLeft--
	c.d.Reds++
	prize := [][2]int64{{3, 10 + rand.Int63n(91)}}
	face := int64(c.role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	c.w.RedLog = append([]ymRed{{RID: c.role.RID, SrvID: c.role.SrvID, Name: c.role.Name, Face: face, Time: c.now.Unix(), Items: prize}}, c.w.RedLog...)
	if n := int(ymNum("holiday_nian_red_packet_display_num")); len(c.w.RedLog) > n {
		c.w.RedLog = c.w.RedLog[:n]
	}
	if err := ymGain(s, st, c.role.RID, prize); err != nil {
		return err
	}
	return s.Send(28214, info(1))
}

// ymSlots reads the formation of a fight request: the type, the hallows and the heroes by place; skip says to fight without playing.
func ymSlots(req proto.Message) (formType int64, slots []store.FormationSlot, skip bool) {
	seen := map[int64]bool{}
	for _, e := range req.Arr("ext_list") {
		switch e.Int("type") {
		case 1:
			formType = e.Int("val1")
		case 3:
			if id := e.Int("val2"); id != 0 && !seen[id] && e.Int("val1") >= 1 {
				seen[id] = true
				slots = append(slots, store.FormationSlot{Pos: e.Int("val1"), PartnerID: id})
			}
		case 4:
			skip = e.Int("val1") == 1
		}
	}
	return max(1, formType), slots, skip
}

func ymBand(kind, damage int64) int64 {
	bands := ymCfg.Harm[strconv.FormatInt(kind, 10)]
	for i, b := range bands {
		if damage >= b[0] && damage < b[1] {
			return int64(len(bands) - i)
		}
	}
	if len(bands) > 0 && damage >= bands[0][1] {
		return int64(len(bands))
	}
	return 1
}

// ymFight (28203 action 1 on a monster): the small monster or one of the big ones, with the heroes of the request.
func ymFight(s *gateway.Session, st *store.Store, fights *FightService, c *ymCtx, t ymTile, req proto.Message, reply func(int, string) error, vanish func(*gateway.Session, *ymCtx, ymTile) error) error {
	kind := ymEvt(t.Evt).Type
	k := -1
	switch kind {
	case ymEvtTimer:
		k = 0
	case ymEvtGold:
		k = 1
	}
	if k >= 0 && c.d.Left[k] <= 0 {
		return reply(0, "挑战次数不足")
	}
	formType, slots, skip := ymSlots(req)
	if len(slots) == 0 {
		return reply(0, "至少需要上阵一个英雄")
	}
	team, objects, err := fights.teamFor(c.role, formType, slots)
	if err != nil {
		return err
	}
	if len(team) == 0 {
		return reply(0, "没有可以出战的英雄")
	}
	def, hp, name := ymCfg.Units.Small, int64(ymSmallHP), ymEvt(t.Evt).Name
	switch kind {
	case ymEvtTimer:
		def, hp = ymCfg.Units.Timer, ymBossHP
	case ymEvtGold:
		def, hp = ymCfg.Units.Gold, ymBossHP
	}
	unit, obj := monsterOf(def, 0)
	unit.hp, unit.maxHP = hp, hp
	obj["hp"], obj["hp_max"], obj["object_name"] = hp, hp, name
	rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
	damage := max(0, hp-sim.units[len(team)].hp)
	won := damage >= hp
	if k >= 0 {
		c.d.Left[k]--
	}
	if err := reply(1, ""); err != nil {
		return err
	}
	if skip {
		return ymResult(s, st, c, t, kind, damage, won, slots)
	}
	intro := proto.Message{
		"combat_type": combatYM, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
		"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": c.role.Name, "target_role_name": name,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	return fights.begin(s, c.role, combatYM, intro, rounds, won, func(s *gateway.Session, _ bool) error {
		return ymWith(s, st, func(c *ymCtx) error { return ymResult(s, st, c, t, kind, damage, won, slots) })
	})
}

// ymResult pays a fight and tells the client (28209).
func ymResult(s *gateway.Session, st *store.Store, c *ymCtx, t ymTile, kind, damage int64, won bool, slots []store.FormationSlot) error {
	if !c.open {
		return nil
	}
	var prize [][2]int64
	result := int64(2)
	switch kind {
	case ymEvtMonster:
		if cur, ok := c.tileAt(t.Index); ok && cur.Key == t.Key && won {
			result, prize = 1, ymEvt(t.Evt).Reward
			c.d.Taken = append(c.d.Taken, t.Key)
			if err := s.Send(28218, proto.Message{"tile_list": ymTileList([]ymTile{{Index: t.Index, Evt: 0}})}); err != nil {
				return err
			}
		}
	case ymEvtTimer, ymEvtGold:
		if (kind == ymEvtTimer && !c.timerAwake()) || (kind == ymEvtGold && !c.goldAwake()) {
			return nil
		}
		result = 1
		board := c.w.Timer
		if kind == ymEvtGold {
			board = c.w.Gold
		}
		board[c.role.RID] += damage
		scale := 1 + ymBand(kind, damage)/8
		for _, r := range ymEvt(t.Evt).Reward {
			prize = append(prize, [2]int64{r[0], r[1] * scale})
		}
	}
	if err := ymGain(s, st, c.role.RID, prize); err != nil {
		return err
	}
	all, err := st.PartnersOf(c.role.RID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Partner{}
	for _, h := range all {
		byID[h.PartnerID] = h
	}
	var hurts []proto.Message
	var best store.Partner
	var bestPower int64
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		pw := partnerMessage(h).Int("power")
		hurts = append(hurts, proto.Message{"rid": c.role.RID, "srvid": c.role.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": partnerTable[h.BID].Camp,
			"dps": damage / int64(max(len(slots), 1)), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
		if pw > bestPower {
			best, bestPower = h, pw
		}
	}
	rewards := []proto.Message{}
	for _, p := range prize {
		rewards = append(rewards, proto.Message{"base_id": p[0], "num": p[1]})
	}
	name := ymEvt(t.Evt).Name
	if err := s.Send(28209, proto.Message{"type": kind, "all_dps": min(damage, 1<<32-1), "best_partner": best.BID, "bid": best.BID, "lev": best.Lev, "star": best.Star, "use_skin": 0,
		"target_role_name": name, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": hurts}}, "evt_index": t.Index, "reward": rewards, "result": result}); err != nil {
		return err
	}
	return s.Send(28200, ymBase(st, c))
}
