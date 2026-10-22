package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// summon.json is the client's hero summon table (tools/extract_summon.py): per
// group the costs, the bonus per summon and the pool (hero base id and its
// chance in percent, as the client displays it).
//
//go:embed data/summon.json
var summonJSON []byte

type summonGroup struct {
	Name               string
	DrawList           []int64
	ItemOnce, ItemFive [][2]int64 // the group's summon item for 1 / 10 draws
	ExtOnce, ExtFive   [][2]int64 // its second item
	ExchOnce, ExchFive [][2]int64 // the currency price for 1 / 10 draws
	GainOnce, GainFive [][2]int64 // the currency bought along with a priced summon
	HeroGain           [][2]int64 // paid back per draw (summon points)
	Pool               [][2]float64
}

var summonTable = func() map[int64]*summonGroup {
	var f struct {
		Groups map[int64]struct {
			Name     string       `json:"name"`
			DrawList []int64      `json:"draw_list"`
			ItemOnce [][2]int64   `json:"item_once"`
			ItemFive [][2]int64   `json:"item_five"`
			ExtOnce  [][2]int64   `json:"ext_item_once"`
			ExtFive  [][2]int64   `json:"ext_item_five"`
			ExchOnce [][2]int64   `json:"exchange_once"`
			ExchFive [][2]int64   `json:"exchange_five"`
			GainOnce [][2]int64   `json:"exchange_once_gain"`
			GainFive [][2]int64   `json:"exchange_five_gain"`
			HeroGain [][2]int64   `json:"recruit_hero_gain"`
			Pool     [][2]float64 `json:"pool"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(summonJSON, &f); err != nil || len(f.Groups) == 0 {
		panic("game: bad summon.json")
	}
	out := map[int64]*summonGroup{}
	for id, g := range f.Groups {
		out[id] = &summonGroup{g.Name, g.DrawList, g.ItemOnce, g.ItemFive, g.ExtOnce, g.ExtFive, g.ExchOnce, g.ExchFive, g.GainOnce, g.GainFive, g.HeroGain, g.Pool}
	}
	return out
}()

// Summon kinds in 23201's recruit_type (from the client's calls).
const (
	summonFree     = 1
	summonExchange = 3 // pay the group's currency price, which also buys a currency bonus
	summonItem     = 4 // the group's summon item
	summonExtItem  = 5 // the group's second item, or the points of the points group
)

// In the 23201 answer, `flag` says which config the result window reads: 0 is the
// normal summon table (RecruitData). The holiday summons use 1-3.

// freeSummonGroups are the groups with a daily free single draw: the original server's
// 23200 (captured 2026-10-21) offers one in the basic (100) and the advanced (300) summon,
// none in the friendship summon (200).
var freeSummonGroups = map[int64]bool{100: true, 300: true}

// freeResetHour is the hour of the China-time day at which the free draws come back: the
// original sends free_cd_end = 05:00 of the next day after a free draw.
const freeResetHour = 5

// summonDay is the free-draw day a moment belongs to (YYYYMMDD, days turn at freeResetHour).
func summonDay(t time.Time) int64 { return dayKey(t.Add(-freeResetHour * time.Hour)) }

// nextFreeReset is the moment the free draws come back next.
func nextFreeReset(t time.Time) time.Time {
	return dayStart(t.Add(-freeResetHour*time.Hour)).AddDate(0, 0, 1).Add(freeResetHour * time.Hour)
}

// summonRand is the random source of a draw (a test replaces it).
var summonRand = rand.Float64

// pickHero draws one hero base id from the pool by its chances.
func pickHero(g *summonGroup, r float64) int64 {
	total := 0.0
	for _, p := range g.Pool {
		total += p[1]
	}
	x := r * total
	for _, p := range g.Pool {
		if x < p[1] {
			return int64(p[0])
		}
		x -= p[1]
	}
	return int64(g.Pool[len(g.Pool)-1][0])
}

// summonCost works out what a summon of `times` draws with this payment kind
// costs, what it pays back besides the heroes, and why it cannot go ahead.
func summonCost(g *summonGroup, times, kind int64) (spend, gain []sellGain, msg string) {
	ten := times > 1
	conv := func(l [][2]int64) (out []sellGain) {
		for _, e := range l {
			out = append(out, sellGain{e[0], e[1]})
		}
		return
	}
	pick := func(once, five [][2]int64) []sellGain {
		if ten {
			return conv(five)
		}
		return conv(once)
	}
	switch kind {
	case summonItem:
		spend = pick(g.ItemOnce, g.ItemFive)
	case summonExtItem:
		spend = pick(g.ExtOnce, g.ExtFive)
		if len(spend) == 0 { // the points group has only an exchange price
			spend = pick(g.ExchOnce, g.ExchFive)
		}
	case summonExchange:
		spend = pick(g.ExchOnce, g.ExchFive)
		gain = pick(g.GainOnce, g.GainFive)
	}
	if len(spend) == 0 {
		return nil, nil, "该召唤方式暂不可用"
	}
	return spend, gain, ""
}

// RegisterSummon registers the hero summon: 23200 (state), 23201 (summon) and
// 23202 (state of one group).
func RegisterSummon(r *gateway.Router, st *store.Store) {
	kv := func(key, val int64) proto.Message { return proto.Message{"key": key, "val": val, "str_val": ""} }
	// freeLeft says whether the group's free summon is still there today.
	freeLeft := func(s *gateway.Session, rid, group int64) (bool, error) {
		if !freeSummonGroups[group] {
			return false, nil
		}
		used, err := st.FreeSummonUsed(rid, group, summonDay(s.Now()))
		return !used, err
	}
	state := func(s *gateway.Session, rid int64, groups []int64) ([]proto.Message, error) {
		out := []proto.Message{}
		for _, id := range groups {
			free, err := freeLeft(s, rid, id)
			if err != nil {
				return nil, err
			}
			n := int64(0)
			if free {
				n = 1
			}
			// one entry per draw size the group offers; kv key 4 = free summons left (only the
			// single draw can be free, 1 shows the red dot), key 5 = when the next one comes (0)
			draws := []proto.Message{}
			if g, ok := summonTable[id]; ok {
				sizes := append([]int64(nil), g.DrawList...)
				sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] }) // the original lists 10 before 1
				for _, t := range sizes {
					left := int64(0)
					if t == 1 {
						left = n
					}
					draws = append(draws, proto.Message{"times": t, "kv_list": []proto.Message{kv(4, left), kv(5, 0)}})
				}
			}
			out = append(out, proto.Message{"group_id": id, "draw_list": draws})
		}
		return out, nil
	}
	// freeState is the 23202 body: how many free draws are left and, once today's is used,
	// when it comes back.
	freeState := func(s *gateway.Session, group int64, free bool) proto.Message {
		if free || !freeSummonGroups[group] {
			return proto.Message{"group_id": group, "free_cd_end": 0, "free_times": b2i(free)}
		}
		return proto.Message{"group_id": group, "free_cd_end": nextFreeReset(s.Now()).Unix(), "free_times": 0}
	}
	groupIDs := func() []int64 { return []int64{100, 200, 300, 400} }

	r.Handle(23200, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list, err := state(s, role.RID, groupIDs())
		if err != nil {
			return err
		}
		return s.Send(23200, proto.Message{"recruit_list": list, "is_share": 0, "is_day_share": 0, "must_five_num": 0})
	})
	r.Handle(23202, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("group_id")
		free, err := freeLeft(s, role.RID, id)
		if err != nil {
			return err
		}
		return s.Send(23202, freeState(s, id, free))
	})
	r.Handle(23201, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gid, times, kind := req.Int("group_id"), req.Int("times"), req.Int("recruit_type")
		// Any 23201 opens the result window and 23201 has no message field, so a refused
		// summon is not answered at all (the client checks the cost itself before sending).
		refuse := func() error { return nil }
		g, ok := summonTable[gid]
		allowed := false
		if ok {
			for _, t := range g.DrawList {
				allowed = allowed || t == times
			}
		}
		if !allowed {
			return refuse()
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
			return refuse()
		}
		res := store.SummonResult{SpendAssets: map[int64]int64{}, SpendItems: map[int64]int64{}, GainAssets: map[int64]int64{}}
		if kind == summonFree {
			free, err := freeLeft(s, role.RID, gid)
			if err != nil {
				return err
			}
			if !free || times != 1 {
				return refuse()
			}
			res.FreeGroup, res.FreeDay = gid, summonDay(s.Now())
		} else {
			spend, gain, msg := summonCost(g, times, kind)
			if msg != "" {
				return refuse()
			}
			splitGains(spend, 1, res.SpendAssets, res.SpendItems)
			splitGains(gain, 1, res.GainAssets, map[int64]int64{})
		}
		for _, h := range g.HeroGain { // points for every draw, paid with 10302 and not listed
			res.GainAssets[h[0]] += h[1] * times
		}
		// rewards has one entry per draw, in step with partner_bids: the card item of the hero
		// drawn (the original server's 23201, captured 2026-10-21), which is what the result
		// page shows for the heroes without a reveal animation.
		var rewards []proto.Message
		bids := make([]proto.Message, 0, times)
		for i := int64(0); i < times; i++ {
			bid := pickHero(g, summonRand())
			star := partnerTable[bid].InitStar
			res.Seeds = append(res.Seeds, store.PartnerSeed{BID: bid, Star: star})
			bids = append(bids, proto.Message{"partner_bid": bid, "init_star": star})
			if card, ok := heroCardItem(bid, star); ok {
				rewards = append(rewards, proto.Message{"base_id": card, "num": 1})
			}
		}
		heroes, err := st.Summon(role.RID, res)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse()
			}
			return err
		}
		if err := s.Send(23201, proto.Message{"group_id": gid, "times": times, "flag": 0, "rewards": nonNil(rewards),
			"partner_bids": bids, "partner_chips": []proto.Message{}, "must_five_num": 0}); err != nil {
			return err
		}
		msgs := make([]proto.Message, 0, len(heroes))
		for _, h := range heroes {
			msgs = append(msgs, partnerMessage(h))
		}
		if err := s.Send(11001, proto.Message{"partners": msgs}); err != nil {
			return err
		}
		if err := questEvent(s, st, role.RID, evtRecruit, times); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if kind == summonFree { // the original follows a free draw with the group's new free state
			return s.Send(23202, freeState(s, gid, false))
		}
		return nil
	})
}

func nonNil(l []proto.Message) []proto.Message {
	if l == nil {
		return []proto.Message{}
	}
	return l
}

// heroCardItem is the item that stands for a drawn hero in the result page: the client's
// hero cards 24000-28999 by camp (24 water to 28 dark), initial star and number in the
// camp's list: (23+camp)*1000 + (star-1)*100 + (bid%100 - 1). It matched all 12 draws of
// the original server's capture; a hero whose card is not in the item table has none.
func heroCardItem(bid int64, star int) (int64, bool) {
	p, ok := partnerTable[bid]
	if !ok || p.Camp < 1 || p.Camp > 5 || star < 1 || bid%100 < 1 {
		return 0, false
	}
	card := int64(23+p.Camp)*1000 + int64(star-1)*100 + bid%100 - 1
	return card, ItemKnown(card)
}
