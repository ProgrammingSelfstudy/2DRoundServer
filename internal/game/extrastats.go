package game

import (
	"encoding/json"
	"fmt"

	"2droundserver/internal/store"
)

// extraStats adds up the attributes of a hero's worn artifacts, holy equipment (and its suits) and horcrux that are not the four main ones:
// the hit, dodge and crit numbers a hero entry carries (per mille, like the client's attribute values). They go into the hero entry, which
// is also what the fight reads, so they work in battle. dam / res / tenacity / cure and the like have no place in the entry and are left.
func extraStats(p store.Partner) map[string]int64 {
	out := map[string]int64{}
	add := func(key string, v int64) {
		switch key {
		case "crit_rate", "crit_ratio", "hit_rate", "dodge_rate", "hit_magic", "dodge_magic":
			out[key] += v
		}
	}
	for _, a := range p.Artifacts {
		for _, x := range a.Attr {
			add(artAttrKeys[x.ID], x.Val)
		}
	}
	count := map[int64]int{}
	for _, x := range p.Holy {
		for _, a := range x.Main {
			add(holyAttrKeys[a.ID], a.Val)
		}
		for _, a := range x.Rand {
			add(holyAttrKeys[a.ID], a.Val)
		}
		count[holyTable.Bases[x.Base].Group]++
	}
	for g, n := range count {
		for num, bonus := range holyTable.Suits[g] {
			if n < num {
				continue
			}
			for _, raw := range bonus {
				var pair []any
				if json.Unmarshal(raw, &pair) == nil && len(pair) == 2 {
					key, _ := pair[0].(string)
					v, _ := pair[1].(float64)
					add(key, int64(v))
				}
			}
		}
	}
	if hero, ok := horcruxTable.Heroes[p.BID]; ok && p.Horcrux.Status >= 1 {
		row := horcruxTable.Levels[p.Horcrux.Lev]
		for _, slot := range hero.Slots {
			for _, a := range row.Attr[fmt.Sprint(slot)] {
				key, _ := a[0].(string)
				v, _ := a[1].(float64)
				add(key, int64(v))
			}
		}
	}
	return out
}
