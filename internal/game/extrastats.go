package game

import (
	"encoding/json"
	"fmt"

	"2droundserver/internal/store"
)

// extraStats adds the hit, dodge and crit rates from equipment, horcrux and guild
// training that the hero entry carries. The values are per mille.
func extraStats(p store.Partner) map[string]int64 {
	out := map[string]int64{}
	for _, key := range []string{"crit_rate", "crit_ratio", "hit_magic", "dodge_magic"} {
		if bonus := guildSkillBonus(p, key); bonus != 0 {
			out[key] = bonus
		}
	}
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
