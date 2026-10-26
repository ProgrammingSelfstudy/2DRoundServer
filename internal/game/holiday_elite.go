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

// holiday_elite.json: the holiday elite summon (新春赐福 and its kin) of the client (tools/extract_holiday_elite.py).
//
//go:embed data/holiday_elite.json
var holidayEliteJSON []byte

const (
	holEliteModule = "holelite"
	holEliteUpRate = 0.5 // "when an item is drawn there is a 50% chance it is one of the up items" (the client text)
	holEliteFree   = 1
	holEliteItem   = 4
	holEliteGold   = 3
)

type holEliteGroup struct {
	Once     [][2]int64 `json:"once"`
	Ten      [][2]int64 `json:"ten"`
	GoldOnce [][2]int64 `json:"gold_once"`
	GoldTen  [][2]int64 `json:"gold_ten"`
	GainOnce [][2]int64 `json:"gain_once"`
	GainTen  [][2]int64 `json:"gain_ten"`
	Pool     []struct {
		ID, Num int64
		Weight  float64
	} `json:"pool"`
	Ups []struct{ ID, Num int64 } `json:"ups"`
}

type holEliteCamp struct {
	Group  int64 `json:"group"`
	Awards []struct {
		ID, Times int64
		Reward    [][2]int64
	} `json:"awards"`
}

var holEliteCfg = func() (out struct {
	Const  map[string]int64          `json:"const"`
	Camps  map[string]*holEliteCamp  `json:"camps"`
	Groups map[string]*holEliteGroup `json:"groups"`
}) {
	if err := json.Unmarshal(holidayEliteJSON, &out); err != nil || len(out.Camps) == 0 {
		panic("game: bad holiday_elite.json")
	}
	return
}()

// holEliteCamps are the camp ids of the activities, in order.
var holEliteCamps = func() []int64 {
	var ids []int64
	for k := range holEliteCfg.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}()

type holEliteRole struct {
	Camps map[int64]*holEliteProg
}

type holEliteProg struct {
	Times    int64
	Claimed  []int64
	LastFree int64
}

// holEliteOpen is the camp whose activity the operator has opened on the role's server: the first by id.
func holEliteOpen(st *store.Store, role *store.Role, now time.Time) (camp, start, end int64) {
	for _, c := range holEliteCamps {
		if s, e, open := ActivityOpen(st, role.SrvID, c, now); open {
			return c, s, e
		}
	}
	return 0, 0, 0
}

func (p *holEliteProg) freeAt() int64 { return p.LastFree + holEliteCfg.Const["c_interval"] }

func holElitePick(g *holEliteGroup) (id, num int64) {
	if len(g.Ups) > 0 && summonRand() < holEliteUpRate {
		u := g.Ups[int(summonRand()*float64(len(g.Ups)))%len(g.Ups)]
		return u.ID, u.Num
	}
	total := 0.0
	for _, p := range g.Pool {
		total += p.Weight
	}
	r := summonRand() * total
	for _, p := range g.Pool {
		if r -= p.Weight; r < 0 {
			return p.ID, p.Num
		}
	}
	last := g.Pool[len(g.Pool)-1]
	return last.ID, last.Num
}

// RegisterHolidayElite registers the holiday elite summon page (23220 the state, 23221 draw, 23222 take the cumulative awards).
// It runs for the camp the operator opened in the activity schedule (a camp id of the client table, 51193 and so on). A draw
// costs the summon item (37010) or diamonds, the first of a day is free; each draw is an item from the prize pool, half of
// them from the up items. The pool is the table's, the weights and the 50% are what the client text says; the guarantee of
// the up item and of an orange item in ten are not done.
func RegisterHolidayElite(r *gateway.Router, st *store.Store) {
	load := func(role *store.Role, camp int64) (*holEliteRole, *holEliteProg, error) {
		d := &holEliteRole{}
		if _, err := st.LoadState(role.RID, holEliteModule, d); err != nil {
			return nil, nil, err
		}
		if d.Camps == nil {
			d.Camps = map[int64]*holEliteProg{}
		}
		if d.Camps[camp] == nil {
			d.Camps[camp] = &holEliteProg{}
		}
		return d, d.Camps[camp], nil
	}
	state := func(s *gateway.Session, role *store.Role) (proto.Message, error) {
		camp, start, end := holEliteOpen(st, role, s.Now())
		if camp == 0 {
			return proto.Message{"camp_id": 0, "free_time": 0, "times": 0, "start_time": 0, "end_time": 0, "item_id": 0, "item_num": 0,
				"must_count": 0, "reward_list": []proto.Message{}}, nil
		}
		_, p, err := load(role, camp)
		if err != nil {
			return nil, err
		}
		taken := []proto.Message{}
		for _, id := range p.Claimed {
			taken = append(taken, proto.Message{"id": id})
		}
		free := p.freeAt()
		if free <= s.Now().Unix() {
			free = 0
		}
		return proto.Message{"camp_id": camp, "free_time": free, "times": p.Times, "start_time": start, "end_time": end,
			"item_id": holEliteCfg.Const["common_s"], "item_num": 0, "must_count": 0, "reward_list": taken}, nil
	}
	r.Handle(23220, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		m, err := state(s, role)
		if err != nil {
			return err
		}
		return s.Send(23220, m)
	})
	r.Handle(23221, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(23221, proto.Message{"flag": 0, "msg": msg}) }
		camp, _, _ := holEliteOpen(st, role, s.Now())
		if camp == 0 {
			return refuse("活动未开启")
		}
		times, kind := req.Int("times"), req.Int("recruit_type")
		if times != 1 && times != 10 {
			return refuse("次数不对")
		}
		g := holEliteCfg.Groups[strconv.FormatInt(holEliteCfg.Camps[strconv.FormatInt(camp, 10)].Group, 10)]
		d, p, err := load(role, camp)
		if err != nil {
			return err
		}
		spendA, spendI, gainA, gainI := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
		cost := func(list [][2]int64, into map[int64]int64) {
			for _, c := range list {
				into[c[0]] += c[1]
			}
		}
		switch kind {
		case holEliteFree:
			if times != 1 || p.freeAt() > s.Now().Unix() {
				return refuse("今天已经免费召唤过了")
			}
			p.LastFree = s.Now().Unix()
		case holEliteItem:
			cost(map[int64][][2]int64{1: g.Once, 10: g.Ten}[times], spendI)
		case holEliteGold:
			cost(map[int64][][2]int64{1: g.GoldOnce, 10: g.GoldTen}[times], spendA)
			splitGains(holEliteGains(map[int64][][2]int64{1: g.GainOnce, 10: g.GainTen}[times]), 1, gainA, gainI)
		default:
			return refuse("召唤方式不对")
		}
		for i := int64(0); i < times; i++ {
			id, num := holElitePick(g)
			if ItemKnown(id) {
				splitGains([]sellGain{{id, num}}, 1, gainA, gainI)
			}
		}
		if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			return refuse("材料不足")
		}
		p.Times += times
		if err := st.SaveState(role.RID, holEliteModule, d); err != nil {
			return err
		}
		if err := s.Send(23221, proto.Message{"flag": 1, "msg": "召唤成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := questEvent(s, st, role.RID, evtRecruit, times); err != nil {
			return err
		}
		m, err := state(s, role)
		if err != nil {
			return err
		}
		return s.Send(23220, m)
	})
	r.Handle(23222, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(23222, proto.Message{"flag": 0, "msg": msg}) }
		camp, _, _ := holEliteOpen(st, role, s.Now())
		if camp == 0 {
			return refuse("活动未开启")
		}
		d, p, err := load(role, camp)
		if err != nil {
			return err
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		n := 0
		for _, a := range holEliteCfg.Camps[strconv.FormatInt(camp, 10)].Awards {
			if a.Times <= p.Times && !has(p.Claimed, a.ID) {
				splitGains(holEliteGains(a.Reward), 1, ga, gi)
				p.Claimed = append(p.Claimed, a.ID)
				n++
			}
		}
		if n == 0 {
			return refuse("没有可领取的奖励")
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, holEliteModule, d); err != nil {
			return err
		}
		if err := s.Send(23222, proto.Message{"flag": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		m, err := state(s, role)
		if err != nil {
			return err
		}
		return s.Send(23220, m)
	})
}

func holEliteGains(list [][2]int64) []sellGain {
	out := make([]sellGain, 0, len(list))
	for _, e := range list {
		out = append(out, sellGain{e[0], e[1]})
	}
	return out
}
