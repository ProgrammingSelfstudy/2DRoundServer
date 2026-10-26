package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// time_summon.json: the time summon (限定召唤, recruit_holiday_data.lua) of the client (tools/extract_time_summon.py).
//
//go:embed data/time_summon.json
var timeSummonJSON []byte

const (
	timeSummonModule = "timesummon"
	timeSummonFree   = 1
	timeSummonGold   = 3
	timeSummonItem   = 4
)

type timeSummonRow struct {
	ID     int64
	Star   int
	Weight float64
	Up     bool
}

type timeSummonGroup struct {
	Once     [][2]int64      `json:"once"`
	Ten      [][2]int64      `json:"ten"`
	GoldOnce [][2]int64      `json:"gold_once"`
	GoldTen  [][2]int64      `json:"gold_ten"`
	GainOnce [][2]int64      `json:"gain_once"`
	GainTen  [][2]int64      `json:"gain_ten"`
	Rows     []timeSummonRow `json:"rows"`
}

type timeSummonCamp struct {
	Group  int64 `json:"group"`
	Awards []struct {
		ID, Times int64
		Reward    [][2]int64
	} `json:"awards"`
}

var timeSummonCfg = func() (out struct {
	Const  map[string]int64
	Camps  map[string]*timeSummonCamp
	Groups map[string]*timeSummonGroup
}) {
	if err := json.Unmarshal(timeSummonJSON, &out); err != nil || len(out.Camps) == 0 {
		panic("game: bad time_summon.json")
	}
	return
}()

var timeSummonCamps = func() []int64 {
	var ids []int64
	for k := range timeSummonCfg.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}()

type timeSummonRole struct{ Camps map[int64]*timeSummonProg }

type timeSummonProg struct {
	Times    int64
	Claimed  []int64
	LastFree int64
}

func (p *timeSummonProg) freeAt() int64 { return p.LastFree + timeSummonCfg.Const["c_interval"] }

// timeSummonOpen is the camp the operator has opened on the role's server: the first by id.
func timeSummonOpen(st *store.Store, role *store.Role, now time.Time) (camp, start, end int64) {
	for _, c := range timeSummonCamps {
		if s, e, open := ActivityOpen(st, role.SrvID, c, now); open {
			return c, s, e
		}
	}
	return 0, 0, 0
}

// timeSummonDraw is one draw of a group. The first groups list their heroes with their own weights (and a chip item); the
// others give the odds of the stars and of the up heroes inside the 5-star share, and take the other heroes from the advanced
// summon's pool by star (the client text: "can draw every hero of the advanced pool"). It returns the hero base id, or the
// item (chip) when hero is 0.
func timeSummonDraw(g *timeSummonGroup) (hero, item int64) {
	direct := false
	for _, r := range g.Rows {
		if r.ID > 0 && !r.Up {
			direct = true
		}
	}
	total := 0.0
	for _, r := range g.Rows {
		if direct == (r.ID > 0) || r.Up {
			total += r.Weight
		}
	}
	x := summonRand() * total
	var hit timeSummonRow
	for _, r := range g.Rows {
		if !(direct == (r.ID > 0) || r.Up) {
			continue
		}
		hit = r
		if x -= r.Weight; x < 0 {
			break
		}
	}
	if direct {
		if hit.Star == 0 {
			return 0, hit.ID
		}
		return hit.ID, 0
	}
	if hit.Up {
		return hit.ID, 0
	}
	var up int64
	for _, r := range g.Rows {
		if r.Up {
			up = r.ID
		}
	}
	return elitePick(hit.Star, up), 0
}

// RegisterTimeSummon registers the time summon page: 23216 (the state), 23217 (draw), 23218 (take the cumulative awards) and
// 23219 (the hero preview battle, not open). It runs for the camp the operator opened in the activity schedule. A draw costs
// the summon item (10407) or diamonds, the first of a day is free; the heroes come as in the normal summon (result 23201).
// Not captured: the odds are the client table's, the 4-star guarantee of a ten draw is the client text's, the chip item of the
// first groups comes as one item (the table has no number).
func RegisterTimeSummon(r *gateway.Router, st *store.Store) {
	load := func(role *store.Role, camp int64) (*timeSummonRole, *timeSummonProg, error) {
		d := &timeSummonRole{}
		if _, err := st.LoadState(role.RID, timeSummonModule, d); err != nil {
			return nil, nil, err
		}
		if d.Camps == nil {
			d.Camps = map[int64]*timeSummonProg{}
		}
		if d.Camps[camp] == nil {
			d.Camps[camp] = &timeSummonProg{}
		}
		return d, d.Camps[camp], nil
	}
	state := func(s *gateway.Session, role *store.Role) (proto.Message, error) {
		camp, start, end := timeSummonOpen(st, role, s.Now())
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
			"item_id": timeSummonCfg.Const["common_s"], "item_num": 0, "must_count": 0, "reward_list": taken}, nil
	}
	sendState := func(s *gateway.Session, role *store.Role) error {
		m, err := state(s, role)
		if err != nil {
			return err
		}
		return s.Send(23216, m)
	}
	r.Handle(23216, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		return sendState(s, role)
	})
	r.Handle(23217, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(23217, proto.Message{"flag": 0, "msg": msg}) }
		camp, _, _ := timeSummonOpen(st, role, s.Now())
		if camp == 0 {
			return refuse("活动未开启")
		}
		times, kind := req.Int("times"), req.Int("recruit_type")
		if times != 1 && times != 10 {
			return refuse("次数不对")
		}
		c := timeSummonCfg.Camps[strconv.FormatInt(camp, 10)]
		g := timeSummonCfg.Groups[strconv.FormatInt(c.Group, 10)]
		have, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		buys, err := st.HeroSlotBuys(role.RID)
		if err != nil {
			return err
		}
		if len(have)+int(times) > heroSlotCapacity(buys) {
			return refuse("英雄数量已达上限")
		}
		d, p, err := load(role, camp)
		if err != nil {
			return err
		}
		res := store.SummonResult{SpendAssets: map[int64]int64{}, SpendItems: map[int64]int64{}, GainAssets: map[int64]int64{}}
		switch kind {
		case timeSummonFree:
			if times != 1 || p.freeAt() > s.Now().Unix() {
				return refuse("今天已经免费召唤过了")
			}
			p.LastFree = s.Now().Unix()
		case timeSummonItem:
			splitGains(toGains(map[int64][][2]int64{1: g.Once, 10: g.Ten}[times]), 1, res.SpendAssets, res.SpendItems)
		case timeSummonGold:
			splitGains(toGains(map[int64][][2]int64{1: g.GoldOnce, 10: g.GoldTen}[times]), 1, res.SpendAssets, res.SpendItems)
			splitGains(toGains(map[int64][][2]int64{1: g.GainOnce, 10: g.GainTen}[times]), 1, res.GainAssets, map[int64]int64{})
		default:
			return refuse("召唤方式不对")
		}
		var rewards, bids []proto.Message
		chips := map[int64]int64{}
		good := false
		for i := int64(0); i < times; i++ {
			hero, item := timeSummonDraw(g)
			if i == times-1 && times == 10 && !good && hero != 0 && partnerTable[hero].InitStar < 4 { // a ten draw has a 4-star at least
				hero = elitePick(4, 0)
			}
			if hero == 0 {
				good = true
				if ItemKnown(item) {
					chips[item]++
					rewards = append(rewards, proto.Message{"base_id": item, "num": 1})
				}
				continue
			}
			star := partnerTable[hero].InitStar
			good = good || star >= 4
			res.Seeds = append(res.Seeds, store.PartnerSeed{BID: hero, Star: star})
			bids = append(bids, proto.Message{"partner_bid": hero, "init_star": star})
			if card, ok := heroCardItem(hero, star); ok {
				rewards = append(rewards, proto.Message{"base_id": card, "num": 1})
			}
		}
		heroes, err := st.Summon(role.RID, res)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("召唤券不足")
			}
			return err
		}
		if len(chips) > 0 { // Summon pays assets only; the chip items follow
			gi := map[int64]int64{}
			for id, n := range chips {
				splitGains([]sellGain{{id, n}}, 1, map[int64]int64{}, gi)
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, gi); err != nil {
				return err
			}
		}
		p.Times += times
		if err := st.SaveState(role.RID, timeSummonModule, d); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		msgs := make([]proto.Message, 0, len(heroes))
		for _, h := range heroes {
			msgs = append(msgs, partnerMessage(h))
		}
		if err := s.Send(11001, proto.Message{"partners": msgs}); err != nil {
			return err
		}
		if err := s.Send(23201, proto.Message{"group_id": c.Group, "times": times, "flag": 3, "rewards": nonNil(rewards),
			"partner_bids": nonNil(bids), "partner_chips": []proto.Message{}, "must_five_num": 0}); err != nil {
			return err
		}
		if err := questEvent(s, st, role.RID, evtRecruit, times); err != nil {
			return err
		}
		return sendState(s, role)
	})
	r.Handle(23218, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(23218, proto.Message{"flag": 0, "msg": msg}) }
		camp, _, _ := timeSummonOpen(st, role, s.Now())
		if camp == 0 {
			return refuse("活动未开启")
		}
		d, p, err := load(role, camp)
		if err != nil {
			return err
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		n := 0
		for _, a := range timeSummonCfg.Camps[strconv.FormatInt(camp, 10)].Awards {
			if a.Times <= p.Times && !has(p.Claimed, a.ID) {
				splitGains(toGains(a.Reward), 1, ga, gi)
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
		if err := st.SaveState(role.RID, timeSummonModule, d); err != nil {
			return err
		}
		if err := s.Send(23218, proto.Message{"flag": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return sendState(s, role)
	})
	r.Handle(23219, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(23219, proto.Message{"flag": 0, "msg": "暂未开放"})
	})
}
