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

// seerpalace.json is the client's seer palace (先知殿) summon table
// (tools/extract_seerpalace.py): per group what a summon costs, what every summon
// pays besides the pool, and the pool of 5-star and 4-star entries.
//
//go:embed data/seerpalace.json
var seerpalaceJSON []byte

type seerGroup struct {
	Cost [][2]int64 // item_once: the crystal one summon takes
	Gain [][2]int64 // exchange_once_gain: paid with every summon
	Pool []seerEntry
}

// (Groups 10000-50000 are the star-card divination of the same window command: they
// cost star-card points and draw only 5-star entries of one camp, with no bonus.)

// seerEntry is one pool entry: the item, its amount and its chance in percent as
// the client shows it (the 5-star lists add up to 15 %, the 4-star ones to 85 %).
type seerEntry struct {
	Item, Num int64
	Chance    float64
}

var seerTable = func() map[int64]*seerGroup {
	var f struct {
		Groups map[int64]struct {
			ItemOnce [][2]int64 `json:"item_once"`
			GainOnce [][2]int64 `json:"gain_once"`
		} `json:"groups"`
		Award map[int64]map[string][][3]float64 `json:"award"`
		Score map[int64]struct {
			ItemOnce [][2]int64   `json:"item_once"`
			Pool     [][3]float64 `json:"pool"`
		} `json:"score"`
	}
	if err := json.Unmarshal(seerpalaceJSON, &f); err != nil || len(f.Award) == 0 || len(f.Score) == 0 {
		panic("game: bad seerpalace.json")
	}
	entries := func(l [][3]float64) (out []seerEntry) {
		for _, e := range l {
			out = append(out, seerEntry{int64(e[0]), int64(e[1]), e[2]})
		}
		return
	}
	out := map[int64]*seerGroup{}
	for id, lists := range f.Award {
		g := &seerGroup{Cost: f.Groups[id].ItemOnce, Gain: f.Groups[id].GainOnce}
		g.Pool = append(entries(lists["5"]), entries(lists["4"])...)
		out[id] = g
	}
	for id, sc := range f.Score {
		out[id] = &seerGroup{Cost: sc.ItemOnce, Pool: entries(sc.Pool)}
	}
	for _, g := range out {
		if len(g.Cost) == 0 || len(g.Pool) == 0 {
			panic("game: bad seerpalace group")
		}
	}
	return out
}()

// seerChangeCost is what turning a 4-star or 5-star hero into another one costs
// (data_seerpalace_const hero_change4 / hero_change5): seer essence (14002).
var seerChangeCost = map[int]int64{4: 20, 5: 100}

const seerChangeItem = 14002

// seerChangeCamps are the camps whose heroes can be swapped (water, fire, wind).
func seerChangeCamp(camp int) bool { return camp >= 1 && camp <= 3 }

// seerChangeTargets are the heroes a hero of that base id and star can turn into:
// the other heroes of its camp that start at the same star, in base id order.
func seerChangeTargets(bid int64, star int) []int64 {
	from, ok := partnerTable[bid]
	if !ok || !seerChangeCamp(from.Camp) {
		return nil
	}
	var out []int64
	for id, p := range partnerTable {
		if id != bid && p.Camp == from.Camp && p.InitStar == star {
			if _, has := p.Stars[strconv.Itoa(star)]; has {
				out = append(out, id)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

const (
	seerScoreFirst = 10000 // the first group of the star-card divination
	seerScoreVIP   = 5     // data_seerpalace_const.recruit_vip
)

// seerOpenKey is the counter that says the summon tab was opened on a day.
func seerOpenKey(now time.Time) string { return "seer_open_" + strconv.FormatInt(dayKey(now), 10) }

// pickSeer draws one pool entry; r is a random number in [0, 1). The chances are
// used as weights, so they need not add up to exactly 100.
func pickSeer(g *seerGroup, r float64) seerEntry {
	total := 0.0
	for _, e := range g.Pool {
		total += e.Chance
	}
	r *= total
	for _, e := range g.Pool {
		if r < e.Chance {
			return e
		}
		r -= e.Chance
	}
	return g.Pool[len(g.Pool)-1]
}

// RegisterSeerpalace registers 23213 (summon from a seer palace card). One crystal
// buys one draw from the group's pool; the guaranteed bonus of the group comes
// with it. The reply lists everything received, which the client shows in its
// reward window. 23213 has no message field and any reply opens that window, so a
// refused summon is not answered at all (the same as 23201).
func RegisterSeerpalace(r *gateway.Router, st *store.Store) {
	// 23234: the summon tab of the window was opened. The client uses the answer (23235)
	// to decide on the red dot of the star-card summon: flag 0 = not opened yet, so the
	// dot may show; flag 1 = opened, no dot. Our policy: the dot is cleared for the rest
	// of the China-time day (the original rule is unknown). Nothing is pushed at login,
	// so the dot is not driven before the first opening.
	r.Handle(23234, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if _, err := st.AddCounter(role.RID, seerOpenKey(s.Now()), 1); err != nil {
			return err
		}
		// the original answers with 23235 and then 23234, both flag 1 (capture 2026-10-21b)
		if err := s.Send(23235, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return s.Send(23234, proto.Message{"flag": 1, "msg": ""})
	})
	registerSeerChange(r, st)
	r.Handle(23213, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gid := req.Int("group_id")
		g, ok := seerTable[gid]
		if !ok {
			return nil
		}
		if gid >= seerScoreFirst { // the divination needs VIP 5 (recruit_vip), which the client checks too
			fresh, err := st.RoleByID(role.RID)
			if err != nil {
				return err
			}
			if fresh.VIPLev < seerScoreVIP {
				return nil
			}
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		for _, c := range g.Cost {
			splitGains([]sellGain{{c[0], c[1]}}, 1, spendA, spendI)
		}
		haveI, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		haveA, err := st.RoleAssets(role.RID)
		if err != nil {
			return err
		}
		for id, n := range spendI {
			if haveI[id] < n {
				return nil
			}
		}
		for id, n := range spendA {
			if haveA[id] < n {
				return nil
			}
		}
		won := pickSeer(g, summonRand())
		got := []sellGain{{won.Item, won.Num}}
		for _, x := range g.Gain {
			got = append(got, sellGain{x[0], x[1]})
		}
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		splitGains(got, 1, gainA, gainI)
		if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return nil
			}
			return err
		}
		rewards := make([]proto.Message, 0, len(got))
		for _, x := range got {
			rewards = append(rewards, proto.Message{"base_id": x.ID, "num": x.Amount})
		}
		if err := s.Send(23213, proto.Message{"group_id": gid, "rewards": rewards}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
