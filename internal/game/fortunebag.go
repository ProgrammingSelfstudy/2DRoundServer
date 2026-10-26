package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// fortunebag.json: the fortune bag draw (福袋翻牌, 28300~28308) of the client (tools/extract_fortunebag.py).
//
//go:embed data/fortunebag.json
var fortuneBagJSON []byte

const (
	fbModule = "fortunebag"
	fbBID    = 993045 // ActionRankCommonType.FortuneBagDraw
	fbCells  = 35     // 7 x 5 cards: 34 normal prizes and the ultimate one
	fbFirst  = 20     // the ultimate prize can come after 20 cards
)

type fbPrize struct {
	ID, Type int64
	Rewards  [][2]int64
}

type fbCamp struct {
	Rounds   [][]fbPrize
	Ultimate map[string][][2]int64
}

// prize is the normal prize of a round by its id (the ids go on from round to round).
func (c *fbCamp) prize(round, id int64) fbPrize {
	for _, p := range c.Rounds[round-1] {
		if p.ID == id {
			return p
		}
	}
	return fbPrize{}
}

var fbCfg = func() (c struct {
	Const struct {
		Cost          [2]int64
		DayDiscount   int64    `json:"day_discount"`
		DayOriginal   int64    `json:"day_original"`
		DiscountPrice [2]int64 `json:"discount_price"`
		OriginalPrice [2]int64 `json:"original_price"`
		MaxRound      int64    `json:"max_round"`
	}
	Camps map[string]*fbCamp
}) {
	if err := json.Unmarshal(fortuneBagJSON, &c); err != nil || len(c.Camps) == 0 || c.Const.MaxRound == 0 {
		panic("game: bad fortunebag.json")
	}
	return
}()

var fbCamps = func() []int64 {
	var ids []int64
	for k := range fbCfg.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}()

// fbUltimateTimes is how many times a role can take one ultimate prize over the rounds (not in the table: ours).
const fbUltimateTimes = 2

type fbRole struct {
	Start    int64
	Camp     int64
	Round    int64
	Opened   map[int64]int64 // card position -> the prize id of the round (10000 and up: the ultimate prize)
	Optional int64           // the ultimate prize chosen for the round
	Got      map[int64]int64 // ultimate prize -> times taken
	Day      int64
	Discount int64 // diamond draws at the discount used today
	Original int64 // diamond draws at the full price used today
}

var fbMu sync.Mutex

type fbCtx struct {
	d       *fbRole
	camp    *fbCamp
	open    bool
	start   int64
	end     int64
	ultGot  bool
	nextRnd bool
}

func fbLoad(s *gateway.Session, st *store.Store, role *store.Role) (*fbCtx, error) {
	now := s.Now()
	c := &fbCtx{}
	var camp int64
	for _, id := range fbCamps {
		if a, b, ok := ActivityOpen(st, role.SrvID, id, now); ok {
			camp, c.start, c.end, c.open = id, a, b, true
			break
		}
	}
	d := &fbRole{}
	if _, err := st.LoadState(role.RID, fbModule, d); err != nil {
		return nil, err
	}
	if d.Camp != camp || d.Start != c.start {
		*d = fbRole{Camp: camp, Start: c.start}
	}
	if d.Round == 0 {
		d.Round = 1
	}
	if d.Opened == nil {
		d.Opened = map[int64]int64{}
	}
	if d.Got == nil {
		d.Got = map[int64]int64{}
	}
	if d.Day != dayKey(now) {
		d.Day, d.Discount, d.Original = dayKey(now), 0, 0
	}
	c.d = d
	if c.open {
		c.camp = fbCfg.Camps[strconv.FormatInt(camp, 10)]
	}
	for _, id := range d.Opened {
		c.ultGot = c.ultGot || id >= 10000
	}
	c.nextRnd = c.open && c.ultGot && d.Round < fbCfg.Const.MaxRound
	return c, nil
}

func (c *fbCtx) state() proto.Message {
	list := []proto.Message{}
	var pos []int64
	for p := range c.d.Opened {
		pos = append(pos, p)
	}
	sort.Slice(pos, func(i, j int) bool { return pos[i] < pos[j] })
	for _, p := range pos {
		list = append(list, proto.Message{"id": c.d.Opened[p], "pos": p})
	}
	next := int64(0)
	if c.nextRnd {
		next = 1
	}
	return proto.Message{"end_time": c.end, "start_time": c.start, "round": c.d.Round, "draw_time": int64(len(c.d.Opened)), "optional_id": c.d.Optional,
		"gold_time": max(0, fbCfg.Const.DayDiscount-c.d.Discount), "next_round": next, "award_list": list,
		"original_gold_time": max(0, fbCfg.Const.DayOriginal-c.d.Original)}
}

func fbOwned(st *store.Store, rid, id int64) (int64, error) {
	assets, err := st.RoleAssets(rid)
	if err != nil {
		return 0, err
	}
	items, err := st.RoleItems(rid)
	if err != nil {
		return 0, err
	}
	return assets[id] + items[id], nil
}

// RegisterFortuneBag registers the fortune bag draw (28300 the state, 28301 what is left of the round, 28302 turn a card, 28303 the
// ultimate prizes, 28305 choose one, 28306 the red dot, 28307 the panel was opened, 28308 the next round). The operator opens the
// activity with the camp_id of one of the five tables (17473, 17553, 18488, 19545, 19562) and bid 993045 (panel type 144). Ours,
// not captured: a card costs 100 whistles (37013), or 2000 diamonds at the discount 5 times a day, then 5000 diamonds 5 times
// (the table's numbers; the client's text says 40% off); a card must not be turned before the ultimate prize is chosen; each
// round has 34 normal prizes and the ultimate one behind 35 cards in random places; from the 21st card the ultimate prize
// can come with a chance of (n-20)/15, so at the 35th it is sure; each ultimate prize can be taken twice and is open from the
// first round; after the ultimate prize the other cards can still be turned, and the next round can be started.
func RegisterFortuneBag(r *gateway.Router, st *store.Store) {
	with := func(s *gateway.Session, fn func(c *fbCtx, role *store.Role) error) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		fbMu.Lock()
		defer fbMu.Unlock()
		c, err := fbLoad(s, st, role)
		if err != nil {
			return err
		}
		return fn(c, role)
	}
	save := func(c *fbCtx, role *store.Role) error { return st.SaveState(role.RID, fbModule, c.d) }
	r.Handle(28300, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, _ *store.Role) error { return s.Send(28300, c.state()) })
	})
	r.Handle(28301, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, _ *store.Role) error {
			got := map[int64]int64{}
			for _, id := range c.d.Opened {
				if id < 10000 {
					got[c.camp.prize(c.d.Round, id).Type]++
				} else {
					got[id]++
				}
			}
			var types []int64
			for t := range got {
				types = append(types, t)
			}
			sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
			list := []proto.Message{}
			for _, t := range types {
				list = append(list, proto.Message{"type_id": t, "get_count": got[t]})
			}
			return s.Send(28301, proto.Message{"award_list": list})
		})
	})
	r.Handle(28302, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(c *fbCtx, role *store.Role) error {
			refuse := func(msg string) error { return s.Send(28302, proto.Message{"code": 0, "msg": msg}) }
			pos := req.Int("pos")
			switch {
			case !c.open:
				return refuse("活动未开启")
			case pos < 1 || pos > fbCells:
				return refuse("位置不对")
			case c.d.Optional == 0:
				return refuse("请先选择本轮的终极大奖")
			case c.d.Opened[pos] != 0:
				return refuse("这张已经翻过了")
			}
			spend := []sellGain{{fbCfg.Const.Cost[0], fbCfg.Const.Cost[1]}}
			have, err := fbOwned(st, role.RID, fbCfg.Const.Cost[0])
			if err != nil {
				return err
			}
			discount, original := false, false
			if have < fbCfg.Const.Cost[1] {
				switch {
				case c.d.Discount < fbCfg.Const.DayDiscount:
					spend, discount = []sellGain{{fbCfg.Const.DiscountPrice[0], fbCfg.Const.DiscountPrice[1]}}, true
				case c.d.Original < fbCfg.Const.DayOriginal:
					spend, original = []sellGain{{fbCfg.Const.OriginalPrice[0], fbCfg.Const.OriginalPrice[1]}}, true
				default:
					return refuse("今日钻石兑换次数已达上限")
				}
			}
			// what is behind the card
			n := int64(len(c.d.Opened)) + 1
			id := int64(0)
			if !c.ultGot && n > fbFirst && summonRand()*(fbCells-fbFirst) < float64(n-fbFirst) {
				id = c.d.Optional
			} else {
				var left []int64
				for _, p := range c.camp.Rounds[c.d.Round-1] {
					taken := false
					for _, o := range c.d.Opened {
						taken = taken || o == p.ID
					}
					if !taken {
						left = append(left, p.ID)
					}
				}
				if len(left) == 0 {
					return refuse("本轮的牌已经翻完了")
				}
				id = left[min(int(summonRand()*float64(len(left))), len(left)-1)]
			}
			var rewards [][2]int64
			if id >= 10000 {
				rewards = c.camp.Ultimate[strconv.FormatInt(id, 10)]
			} else {
				rewards = c.camp.prize(c.d.Round, id).Rewards
			}
			sa, si, ga, gi := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
			splitGains(spend, 1, sa, si)
			var known []sellGain
			for _, g := range guildGains(rewards) {
				if ItemKnown(g.ID) {
					known = append(known, g)
				}
			}
			splitGains(known, 1, ga, gi)
			if err := st.Exchange(role.RID, sa, si, ga, gi); err != nil {
				return refuse("道具不足")
			}
			c.d.Opened[pos] = id
			if id >= 10000 {
				c.d.Got[id]++
				c.ultGot = true
				c.nextRnd = c.d.Round < fbCfg.Const.MaxRound
			}
			if discount {
				c.d.Discount++
			}
			if original {
				c.d.Original++
			}
			if err := save(c, role); err != nil {
				return err
			}
			if err := s.Send(28302, proto.Message{"code": 1, "msg": ""}); err != nil {
				return err
			}
			if err := s.Send(28300, c.state()); err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			return sendBackpack(s, st)
		})
	})
	r.Handle(28303, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, _ *store.Role) error {
			if !c.open {
				return s.Send(28303, proto.Message{"code": 0, "msg": "活动未开启", "award_list": []proto.Message{}})
			}
			var types []string
			for t := range c.camp.Ultimate {
				types = append(types, t)
			}
			sort.Strings(types)
			list := []proto.Message{}
			for _, t := range types {
				id, _ := strconv.ParseInt(t, 10, 64)
				list = append(list, proto.Message{"type_id": id, "get_count": c.d.Got[id], "count": fbUltimateTimes, "round": 1})
			}
			return s.Send(28303, proto.Message{"code": 1, "msg": "", "award_list": list})
		})
	})
	r.Handle(28305, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(c *fbCtx, role *store.Role) error {
			refuse := func(msg string) error { return s.Send(28305, proto.Message{"code": 0, "msg": msg}) }
			t := req.Int("type_id")
			switch {
			case !c.open:
				return refuse("活动未开启")
			case c.camp.Ultimate[strconv.FormatInt(t, 10)] == nil:
				return refuse("没有这个大奖")
			case c.ultGot:
				return refuse("您已获得本轮终极大奖")
			case c.d.Got[t] >= fbUltimateTimes:
				return refuse("这个大奖已经选满了")
			}
			c.d.Optional = t
			if err := save(c, role); err != nil {
				return err
			}
			if err := s.Send(28305, proto.Message{"code": 1, "msg": ""}); err != nil {
				return err
			}
			return s.Send(28300, c.state())
		})
	})
	r.Handle(28306, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, role *store.Role) error {
			code := int64(0)
			if c.open {
				if have, err := fbOwned(st, role.RID, fbCfg.Const.Cost[0]); err != nil {
					return err
				} else if have >= fbCfg.Const.Cost[1] && len(c.d.Opened) < fbCells {
					code = 1
				}
			}
			return s.Send(28306, proto.Message{"code": code, "msg": ""})
		})
	})
	r.Handle(28307, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, _ *store.Role) error {
			return s.Send(28307, proto.Message{"code": 1, "msg": ""})
		})
	})
	r.Handle(28308, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *fbCtx, role *store.Role) error {
			if !c.nextRnd {
				return s.Send(28308, proto.Message{"code": 0, "msg": "本轮还没有结束"})
			}
			c.d.Round++
			c.d.Opened, c.d.Optional, c.ultGot, c.nextRnd = map[int64]int64{}, 0, false, false
			if err := save(c, role); err != nil {
				return err
			}
			if err := s.Send(28308, proto.Message{"code": 1, "msg": ""}); err != nil {
				return err
			}
			return s.Send(28300, c.state())
		})
	})
}
