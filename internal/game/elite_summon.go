package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The elite summon (精英召唤, group 50051): one draw costs a 精英召唤卷 (10408), ten cost ten.
// The role picks one of five up heroes, who comes with a boosted chance and surely within a
// number of draws; the cumulative draws give rewards. The tables are the client's
// (tools/extract_elite_summon.py); one captured single draw settled the answer
// (testdata/captures/original-2026-10-21b/elite-summon.jsonl). Our own rules, not captured: the
// guarantee of 110 draws for every up hero (the client text says 110 for wind / water / fire
// and 150 for light / dark heroes, but the table has no element for them), the other draws
// taken from the advanced summon's pool (group 300) by star, the free daily draw left out.

//go:embed data/elite_summon.json
var eliteJSON []byte

const (
	eliteGroup = 50051
	elitePity  = 110
	// eliteScroll is recruit_type 4 of 23231: a draw paid with the scroll.
	eliteScrollType = 4
)

type eliteData struct {
	Scroll   int64
	GainOnce [][2]int64 `json:"gain_once"`
	GainTen  [][2]int64 `json:"gain_ten"`
	ItemOnce [][2]int64 `json:"item_once"`
	ItemTen  [][2]int64 `json:"item_ten"`
	Awards   []struct {
		ID, Times int64
		Reward    [][2]int64
	}
	Chances []struct {
		Up     int
		Chance float64
		Star   int
	}
	Wish []struct{ Item, BID int64 }
}

var eliteTable = func() *eliteData {
	var d eliteData
	if err := json.Unmarshal(eliteJSON, &d); err != nil || len(d.Wish) == 0 {
		panic("game: bad elite_summon.json")
	}
	return &d
}()

// eliteWishItem is the wish item (the hero card) of an up hero, by hero base id.
func eliteWishItem(bid int64) (int64, bool) {
	for _, w := range eliteTable.Wish {
		if w.BID == bid {
			return w.Item, true
		}
	}
	return 0, false
}

func eliteWishHero(item int64) (int64, bool) {
	for _, w := range eliteTable.Wish {
		if w.Item == item {
			return w.BID, true
		}
	}
	return 0, false
}

// eliteChance is the chance in percent of the draw outcome rows: the up hero, another 5-star,
// a 4-star and a 3-star (the table rows for wind / water / fire up heroes).
func eliteChance() (up, five, four float64) {
	for _, c := range eliteTable.Chances {
		switch {
		case c.Up == 1 && c.Chance == 1:
			up = c.Chance
		case c.Up == 0 && c.Star == 5 && c.Chance == 4:
			five = c.Chance
		case c.Up == 0 && c.Star == 4:
			four = c.Chance
		}
	}
	return
}

// eliteDraw is one draw: the hero base id and whether it is the up hero. since is the draws
// since the up hero last came, counting this one afterwards.
func eliteDraw(lucky int64, since *int64) (bid int64, isUp bool) {
	*since++
	up, five, four := eliteChance()
	r := summonRand() * 100
	switch {
	case *since >= elitePity || r < up:
		*since = 0
		return lucky, true
	case r < up+five:
		return elitePick(5, lucky), false
	case r < up+five+four:
		return elitePick(4, lucky), false
	}
	return elitePick(3, lucky), false
}

// elitePick takes a hero of a star from the advanced summon's pool (group 300), never the up
// hero; when the pool has none of that star, any hero of it.
func elitePick(star int, not int64) int64 {
	g := summonTable[300]
	sub := &summonGroup{}
	for _, p := range g.Pool {
		if bid := int64(p[0]); bid != not && partnerTable[bid].InitStar == star {
			sub.Pool = append(sub.Pool, p)
		}
	}
	if len(sub.Pool) == 0 {
		sub = g
	}
	return pickHero(sub, summonRand())
}

func eliteClaimed(s string) map[int64]bool {
	out := map[int64]bool{}
	for _, f := range strings.Split(s, ",") {
		if id, err := strconv.ParseInt(f, 10, 64); err == nil {
			out[id] = true
		}
	}
	return out
}

func eliteClaimedList(m map[int64]bool) string {
	var ids []string
	for _, a := range eliteTable.Awards {
		if m[a.ID] {
			ids = append(ids, strconv.FormatInt(a.ID, 10))
		}
	}
	return strings.Join(ids, ",")
}

// eliteLucky is the up hero's wish item: the picked one, else the first of the list.
func eliteLucky(e store.EliteSummon) int64 {
	if _, ok := eliteWishHero(e.Lucky); ok {
		return e.Lucky
	}
	return eliteTable.Wish[0].Item
}

// eliteState is the answer of 23230 (the elite summon page).
func eliteState(s *gateway.Session, e store.EliteSummon) proto.Message {
	list := []proto.Message{}
	for id := range eliteClaimed(e.Claimed) {
		list = append(list, proto.Message{"id": id})
	}
	must := elitePity - e.Since
	if must < 1 {
		must = 1
	}
	return proto.Message{
		"camp_id": eliteGroup, "free_time": dayStart(s.Now()).AddDate(0, 0, 1).Unix(), "times": e.Times,
		"start_time": 1741104000, "end_time": 1900079999, "item_id": 3686, "item_num": 0,
		"must_count": must, "reward_list": list, "lucky_bid": eliteLucky(e),
	}
}

// RegisterEliteSummon registers the elite summon: 23230 (the page), 23231 (draw with the
// scrolls), 23232 (claim a cumulative reward) and 23233 (pick the up hero).
func RegisterEliteSummon(r *gateway.Router, st *store.Store) {
	r.Handle(23230, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		e, err := st.EliteSummon(role.RID)
		if err != nil {
			return err
		}
		return s.Send(23230, eliteState(s, e))
	})
	r.Handle(23233, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		item := req.Int("lucky_bid")
		if _, ok := eliteWishHero(item); !ok {
			return s.Send(23233, proto.Message{"flag": 0, "msg": "该英雄暂不可选"})
		}
		e, err := st.EliteSummon(role.RID)
		if err != nil {
			return err
		}
		e.Lucky = item
		if err := st.SaveEliteSummon(role.RID, e); err != nil {
			return err
		}
		if err := s.Send(23233, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(23230, eliteState(s, e))
	})
	r.Handle(23232, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("award_id")
		reply := func(flag int, msg string) error {
			return s.Send(23232, proto.Message{"flag": flag, "msg": msg, "award_id": id, "self_award_id": req.Int("self_award_id")})
		}
		e, err := st.EliteSummon(role.RID)
		if err != nil {
			return err
		}
		claimed := eliteClaimed(e.Claimed)
		for _, a := range eliteTable.Awards {
			if a.ID != id {
				continue
			}
			if e.Times < a.Times {
				return reply(0, "招募次数不足")
			}
			if claimed[id] {
				return reply(0, "已领取")
			}
			gainA, gainI := map[int64]int64{}, map[int64]int64{}
			for _, g := range a.Reward {
				if !ItemKnown(g[0]) && g[0] > 100 {
					return reply(0, "奖励暂不可领")
				}
			}
			splitGains(toGains(a.Reward), 1, gainA, gainI)
			claimed[id] = true
			next := e
			next.Claimed = eliteClaimedList(claimed)
			if err := st.ClaimEliteAward(role.RID, e, next, gainA, gainI); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return reply(0, "已领取")
				}
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			if err := reply(1, ""); err != nil {
				return err
			}
			return s.Send(23230, eliteState(s, next))
		}
		return reply(0, "奖励不存在")
	})
	r.Handle(23231, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		times, kind := req.Int("times"), req.Int("recruit_type")
		refuse := func(msg string) error { return s.Send(23231, proto.Message{"flag": 0, "msg": msg}) }
		if (times != 1 && times != 10) || kind != eliteScrollType {
			return refuse("召唤方式不正确")
		}
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
		e, err := st.EliteSummon(role.RID)
		if err != nil {
			return err
		}
		lucky := eliteLucky(e)
		luckyBID, _ := eliteWishHero(lucky)
		cost, gain := eliteTable.ItemOnce, eliteTable.GainOnce
		if times == 10 {
			cost, gain = eliteTable.ItemTen, eliteTable.GainTen
		}
		res := store.SummonResult{SpendAssets: map[int64]int64{}, SpendItems: map[int64]int64{}, GainAssets: map[int64]int64{}}
		splitGains(toGains(cost), 1, res.SpendAssets, res.SpendItems)
		splitGains(toGains(gain), 1, res.GainAssets, map[int64]int64{})
		var rewards, bids []proto.Message
		next := e
		for i := int64(0); i < times; i++ {
			bid, up := eliteDraw(luckyBID, &next.Since)
			star := partnerTable[bid].InitStar
			res.Seeds = append(res.Seeds, store.PartnerSeed{BID: bid, Star: star})
			bids = append(bids, proto.Message{"partner_bid": bid, "init_star": star})
			if card, ok := heroCardItem(bid, star); ok {
				rewards = append(rewards, proto.Message{"base_id": card, "num": 1})
			} else if item, ok := eliteWishItem(bid); ok && up {
				rewards = append(rewards, proto.Message{"base_id": item, "num": 1})
			}
		}
		next.Times += times
		res.Elite = &next
		heroes, err := st.Summon(role.RID, res)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("召唤券不足")
			}
			return err
		}
		// the original's order (capture 2026-10-21b): the assets, the scrolls, the heroes, the
		// result (23201 with flag 3), then the page's new state
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
		if err := s.Send(23201, proto.Message{"group_id": eliteGroup, "times": times, "flag": 3, "rewards": nonNil(rewards),
			"partner_bids": bids, "partner_chips": []proto.Message{}, "must_five_num": 0}); err != nil {
			return err
		}
		if err := questEvent(s, st, role.RID, evtRecruit, times); err != nil {
			return err
		}
		return s.Send(23230, eliteState(s, next))
	})
}

func toGains(l [][2]int64) []sellGain {
	out := make([]sellGain, 0, len(l))
	for _, g := range l {
		out = append(out, sellGain{g[0], g[1]})
	}
	return out
}
