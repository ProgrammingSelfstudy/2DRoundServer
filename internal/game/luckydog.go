package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// luckydog.json: the lucky dog action (28400~28403) of the client (tools/extract_luckydog.py): 7 daily periods, each with
// three quests that give a number, a draw at 23:50 and four prizes.
//
//go:embed data/luckydog.json
var luckyDogJSON []byte

const (
	luckyModule = "luckydog"
	luckyCamp   = 993046 // ActionRankCommonType.lucky_dog: the camp_id the operator gives the activity in the schedule
)

type luckyPrize struct {
	Digits int64
	Award  [][2]int64
}

var luckyCfg = func() (c struct {
	Periods []struct {
		Period int64
		Quests []int64
	}
	Quests map[string]struct {
		Event  string
		Target int64
	}
	Awards [][]luckyPrize
}) {
	if err := json.Unmarshal(luckyDogJSON, &c); err != nil || len(c.Periods) == 0 || len(c.Awards) != len(c.Periods) {
		panic("game: bad luckydog.json")
	}
	return
}()

// luckyEvent maps the event of a quest to the one this server counts; the quests about systems that report none (the dial,
// sharing the game, paying) get none and stay open.
func luckyEvent(label string) string {
	switch label {
	case "evt_arena_fight_result": // the wins are not told apart from the challenges
		return evtArena
	case "evt_high_recruit": // nor the advanced draws from the others
		return evtRecruit
	case questFastCombat, evtFriendPresent, evtStoneFight:
		return label
	}
	return ""
}

type luckyQuest struct {
	Val     int64
	Num     int64 // the number the finished quest gave; 0 when it is not finished
	Claimed bool
}

type luckyRole struct {
	Start   int64                           // the activity's start this belongs to; a new one begins again
	Base    int64                           // the first day, when the activity has no start
	Seen    int64                           // the day the panel was opened (the login red dot)
	Periods map[int64]map[int64]*luckyQuest // period -> quest id
}

func (d *luckyRole) quest(period, id int64) *luckyQuest {
	if d.Periods == nil {
		d.Periods = map[int64]map[int64]*luckyQuest{}
	}
	if d.Periods[period] == nil {
		d.Periods[period] = map[int64]*luckyQuest{}
	}
	if d.Periods[period][id] == nil {
		d.Periods[period][id] = &luckyQuest{}
	}
	return d.Periods[period][id]
}

// luckyCtx is a role's view of the activity now.
type luckyCtx struct {
	d       *luckyRole
	open    bool
	start   int64
	end     int64
	base    time.Time
	now     time.Time
	current int64 // the period of today
}

// drawn is whether the numbers of a period are out: they come at 23:50 of its day.
func (c *luckyCtx) drawn(period int64) bool {
	return !c.now.Before(c.base.AddDate(0, 0, int(period-1)).Add(23*time.Hour + 50*time.Minute))
}

var luckyMu sync.Mutex

func luckyLoad(st *store.Store, role *store.Role, now time.Time) (*luckyCtx, error) {
	start, end, open := ActivityOpen(st, role.SrvID, luckyCamp, now)
	d := &luckyRole{}
	if _, err := st.LoadState(role.RID, luckyModule, d); err != nil {
		return nil, err
	}
	if d.Start != start {
		*d = luckyRole{Start: start}
	}
	if d.Base == 0 {
		d.Base = dayStart(now).Unix()
	}
	c := &luckyCtx{d: d, open: open, start: start, end: end, now: now}
	c.base = time.Unix(d.Base, 0)
	if start != 0 {
		c.base = dayStart(time.Unix(start, 0))
	}
	days := int64(dayStart(now).Sub(c.base) / (24 * time.Hour))
	c.current = min(max(days+1, 1), int64(len(luckyCfg.Periods)))
	return c, nil
}

// luckyType is the prize a number wins: 1~3 when its last 3, 2 or 1 digits are those of the period's prizes, else 4.
func luckyType(period, num int64) int64 {
	prizes := luckyCfg.Awards[period-1]
	mod := int64(1000)
	for i := 0; i < 3; i++ {
		if num%mod == prizes[i].Digits {
			return int64(i + 1)
		}
		mod /= 10
	}
	return 4
}

func (c *luckyCtx) questMsg(period, id int64) proto.Message {
	q := c.d.quest(period, id)
	finish, typ := int64(0), int64(0)
	if q.Num != 0 {
		finish = 1
		if q.Claimed {
			finish = 2
		}
		if c.drawn(period) {
			typ = luckyType(period, q.Num)
		}
	}
	return proto.Message{"quest_id": id, "val": min(q.Val, luckyCfg.Quests[strconv.FormatInt(id, 10)].Target), "finish": finish, "num": q.Num, "lucky_type": typ}
}

func (c *luckyCtx) info() proto.Message {
	if !c.open {
		return proto.Message{"period": 0, "start_time": 0, "end_time": 0, "info": []proto.Message{}}
	}
	list := []proto.Message{}
	for _, p := range luckyCfg.Periods[:c.current] {
		quests := []proto.Message{}
		for _, id := range p.Quests {
			quests = append(quests, c.questMsg(p.Period, id))
		}
		state := int64(0)
		if c.drawn(p.Period) {
			state = 1
		}
		list = append(list, proto.Message{"period": p.Period, "state": state, "quests": quests})
	}
	return proto.Message{"period": c.current, "start_time": c.base.Unix(), "end_time": c.end, "info": list}
}

// luckyDogEvent counts a game event for the quests of today's period (only before its numbers are drawn) and, when a quest
// is finished, gives its number and sends the role the new state (28400).
func luckyDogEvent(s *gateway.Session, st *store.Store, role *store.Role, label string, n int64) error {
	if role == nil || n <= 0 {
		return nil
	}
	wanted := false
	for _, q := range luckyCfg.Quests {
		wanted = wanted || luckyEvent(q.Event) == label
	}
	if !wanted {
		return nil
	}
	luckyMu.Lock()
	defer luckyMu.Unlock()
	c, err := luckyLoad(st, role, s.Now())
	if err != nil || !c.open || c.drawn(c.current) {
		return err
	}
	changed := false
	for _, id := range luckyCfg.Periods[c.current-1].Quests {
		def := luckyCfg.Quests[strconv.FormatInt(id, 10)]
		q := c.d.quest(c.current, id)
		if luckyEvent(def.Event) != label || q.Num != 0 {
			continue
		}
		q.Val = min(q.Val+n, def.Target)
		if q.Val >= def.Target {
			q.Num = 10000000 + int64(summonRand()*90000000)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if err := st.SaveState(role.RID, luckyModule, c.d); err != nil {
		return err
	}
	return s.Send(28400, c.info())
}

// RegisterLuckyDog registers the lucky dog action (28400 the state, 28401 take the prizes of a period, 28402 the red dot of the
// login, 28403 the panel was opened). The operator opens the activity with camp_id 993046 in the schedule (panel type 145). Ours,
// not captured: the periods are the days from the activity's start, the three quests of the period count games events before
// its draw at 23:50 (the table's text) and each finished one gives a random 8-digit number; the prizes are told from the last
// 3, 2 and 1 digits that the table gives for the period (the client prints them as XXXXX766 and so on), anything else wins the
// last prize. The numbers of a period can be taken after its draw, until the activity ends. The quests about the dial, the
// daily dungeon beyond the fights counted, sharing and paying have no event here and stay open.
func RegisterLuckyDog(r *gateway.Router, st *store.Store) {
	with := func(s *gateway.Session, fn func(c *luckyCtx, role *store.Role) error) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		luckyMu.Lock()
		defer luckyMu.Unlock()
		c, err := luckyLoad(st, role, s.Now())
		if err != nil {
			return err
		}
		return fn(c, role)
	}
	r.Handle(28400, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *luckyCtx, _ *store.Role) error { return s.Send(28400, c.info()) })
	})
	r.Handle(28401, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(c *luckyCtx, role *store.Role) error {
			refuse := func(msg string) error { return s.Send(28401, proto.Message{"code": 0, "msg": msg}) }
			p := req.Int("period")
			switch {
			case !c.open:
				return refuse("活动未开启")
			case p < 1 || p > c.current:
				return refuse("期数不对")
			case !c.drawn(p):
				return refuse("还没有开奖")
			}
			var ids []int64
			for _, id := range luckyCfg.Periods[p-1].Quests {
				if q := c.d.quest(p, id); q.Num != 0 && !q.Claimed {
					ids = append(ids, id)
				}
			}
			if len(ids) == 0 {
				return refuse("没有可领取的奖励")
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			var gain [][2]int64
			for _, id := range ids {
				q := c.d.quest(p, id)
				q.Claimed = true
				gain = append(gain, luckyCfg.Awards[p-1][luckyType(p, q.Num)-1].Award...)
			}
			if err := st.SaveState(role.RID, luckyModule, c.d); err != nil {
				return err
			}
			if err := retGain(s, st, role.RID, gain); err != nil {
				return err
			}
			if err := s.Send(28401, proto.Message{"code": 1, "msg": ""}); err != nil {
				return err
			}
			return s.Send(28400, c.info())
		})
	})
	r.Handle(28402, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *luckyCtx, _ *store.Role) error {
			code := int64(0)
			if c.open && c.d.Seen != dayKey(c.now) {
				code = 1
			}
			return s.Send(28402, proto.Message{"code": code, "msg": ""})
		})
	})
	r.Handle(28403, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *luckyCtx, role *store.Role) error {
			if !c.open || c.d.Seen == dayKey(c.now) {
				return nil
			}
			c.d.Seen = dayKey(c.now)
			return st.SaveState(role.RID, luckyModule, c.d)
		})
	})
}
