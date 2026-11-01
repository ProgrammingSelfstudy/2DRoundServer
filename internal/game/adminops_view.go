package game

import (
	"sort"
	"time"

	"2droundserver/internal/store"
)

type adminSeason struct {
	Supported bool  `json:"supported"`
	Key       int64 `json:"key"`
	Start     int64 `json:"start_time"`
	End       int64 `json:"end_time"`
	Phase     int64 `json:"phase"`
	Due       bool  `json:"due"`
	Settled   bool  `json:"settled"`
	History   any   `json:"history,omitempty"`
}

// The same calendars/markers that gameplay loaders use, without running those
// loaders. In particular the cross/multi arenas settle on rollover, not at the
// end of a challenge window, and atAlways disables team-arena settlements.
func globalSeason(st *store.Store, srv string, state, service any, now time.Time) (adminSeason, error) {
	out := adminSeason{}
	switch b := state.(type) {
	case *atBoard:
		key, phase, end := atSeason(now)
		out = adminSeason{Supported: true, Key: key, Start: key, End: end, Phase: phase, Settled: b.Settled >= key, History: b.Settled}
		if !atAlways {
			for _, k := range []int64{key - 7*86400, key} {
				out.Due = out.Due || (b.Settled < k && now.Unix() >= atSettleAt(k))
			}
		}
	case *amBoard:
		key, end := amSeason(now)
		out = adminSeason{Supported: true, Key: key, End: end, Settled: b.Settled == key, History: b.Settled}
		future := false
		for _, r := range b.Roles {
			out.Due = out.Due || r.Season < key
			future = future || r.Season > key
		}
		if future {
			out.Due = false
		}
	case *caBoard:
		key, start, end := caSeason(now)
		out = adminSeason{Supported: true, Key: key, Start: start, End: end, Settled: b.Settled == key, History: b.Settled}
		future := false
		for _, r := range b.Roles {
			out.Due = out.Due || r.Season < key
			future = future || r.Season > key
		}
		if future {
			out.Due = false
		}
	case *eliteWorld:
		period, phase, start, end, _ := eliteSeason(now, eliteOpenAt(st, srv, now))
		ended := 2 * (period - 1)
		if phase >= 2 {
			ended++
		}
		out = adminSeason{Supported: true, Key: period, Start: start, End: end, Phase: phase, Due: b.Done < ended, Settled: ended > 0 && b.Done >= ended, History: b.Done}
	case *drWorld:
		start, err := drStart(st, srv)
		if err != nil {
			return out, err
		}
		out = adminSeason{Supported: true, Key: dayKey(start), Start: start.Unix(), End: drEnd(start), Due: !b.Settled && now.Unix() > drEnd(start), Settled: b.Settled, History: b.Frozen}
	case *gwWar:
		out.Supported = true
		out.Key = b.Key
		out.Settled = b.Done
		if b.Key != 0 {
			day, err := time.ParseInLocation("20060102", itoa64(b.Key), dayZone)
			if err != nil {
				return out, err
			}
			_, start, end, _ := gwTimeline(day)
			out.Start, out.End = start, end
			out.Phase, _ = gwPhase(day, now.Unix())
			out.Due = !b.Done && now.Unix() >= end
		}
	case *champState:
		c, ok := service.(*champSvc)
		if !ok {
			return out, &opsError{503, "champion service unavailable"}
		}
		now = c.now() // exactly the timer's clock
		out.Supported = true
		p := c.phaseOf(b, now)
		out.History = b.Last
		if b.Ev != nil {
			out.Key, out.Start, out.End = b.Ev.Key, b.Ev.T0, b.Ev.T0+champLength()
			out.Settled = b.Ev.Done || b.Ev.Skip
			out.Due = !out.Settled && now.Unix() >= out.End
		}
		out.Phase = p.status
	}
	return out, nil
}

func globalRanks(st *store.Store, srv string, state, service any, limit int) ([]AdminRankRow, error) {
	out := []AdminRankRow{}
	added := 0
	add := func(rank int64, data any) {
		if added < limit {
			added++
			out = append(out, AdminRankRow{Rank: rank, Data: data})
		}
	}
	switch b := state.(type) {
	case *eliteWorld:
		e, ok := service.(*eliteSvc)
		if !ok {
			return nil, &opsError{503, "elite service unavailable"}
		}
		rows, err := e.rankRows(srv)
		if err != nil {
			return nil, err
		}
		for i, v := range rows {
			add(int64(i+1), map[string]any{"rid": v.rid, "state": v.er})
		}
	case *gwWar:
		for _, p := range b.Pairs {
			for i := range p.Sides {
				side := &p.Sides[i]
				added = 0
				for _, row := range side.rankMessages() {
					add(row.Int("rank"), map[string]any{"gid": side.GID, "row": row})
				}
			}
		}

	case *atBoard:
		for i, v := range b.ranked() {
			add(int64(i+1), v)
		}
	case *amBoard:
		for i, v := range b.ranked() {
			add(int64(i+1), v)
		}
	case *caBoard:
		for i, v := range b.ranked() {
			add(int64(i+1), v)
		}
	case *champState:
		c, ok := service.(*champSvc)
		if !ok {
			return nil, &opsError{503, "champion service unavailable"}
		}
		if b.Ev != nil {
			ranks := c.ranks(b.Ev)
			for i, v := range b.Ev.Entries {
				out = append(out, AdminRankRow{Rank: ranks[i], Data: v})
			}
			sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
			if len(out) > limit {
				out = out[:limit]
			}
		}
	case *drWorld:
		for _, rk := range drCfg.Ranks {
			added = 0
			if b.Settled {
				for i, v := range b.Frozen[itoa64(rk.ID)] {
					add(int64(i+1), map[string]any{"type": rk.ID, "rid": v.RID, "val": v.Val})
				}
			} else {
				rows, err := ranking(st, srv, drTypes[rk.ID])
				if err != nil {
					return nil, err
				}
				for i, v := range rows {
					add(int64(i+1), map[string]any{"type": rk.ID, "role": v.role, "val": v.val})
				}
			}
		}
	case *ladderBoard:
		ids := make([]int64, 0, len(b.Ranks))
		for rank := range b.Ranks {
			ids = append(ids, rank)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, rank := range ids {
			add(rank, map[string]any{"rid": b.Ranks[rank]})
		}
	case *endRanks:
		for typ := int64(1); typ <= 4; typ++ {
			added = 0
			for i, v := range b.sorted(typ) {
				add(int64(i+1), map[string]any{"type": typ, "row": v})
			}
		}
	case *ptRanks:
		p, ok := service.(*ptSvc)
		if !ok {
			return nil, &opsError{503, "tower service unavailable"}
		}
		_, rows, err := p.rankOf(srv, 0)
		if err != nil {
			return nil, err
		}
		for i, v := range rows {
			add(int64(i+1), ptRowMessage(i, v))
		}
	case *sbWorld:
		for i, rid := range b.ranked() {
			add(int64(i+1), map[string]any{"rid": rid, "score": b.Score[rid]})
		}
	case *sweetWorld:
		for i, rid := range b.ranked() {
			add(int64(i+1), map[string]any{"rid": rid, "score": b.Score[rid]})
		}
	case *ymWorld:
		for i, rid := range ymRanking(b.Gold) {
			add(int64(i+1), map[string]any{"rid": rid, "damage": b.Gold[rid]})
		}
	case *tbWorld:
		for i, rid := range b.ranking(b.Dmg) {
			add(int64(i+1), map[string]any{"rid": rid, "damage": b.Dmg[rid]})
		}
	case *homeBoard:
		for i, rid := range b.ranked() {
			add(int64(i+1), map[string]any{"rid": rid, "score": b.score(rid)})
		}
	default:
		return nil, &opsError{405, "module has no registered ranking"}
	}
	return out, nil
}
