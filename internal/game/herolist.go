package game

import (
	"sort"
	"strconv"
)

// HeroOption is one hero for the admin forms: the base id, its name, camp, the star
// it starts at and the highest star the client's table has for it.
type HeroOption struct {
	BID      int64  `json:"bid"`
	Name     string `json:"name"`
	Camp     int    `json:"camp"`
	InitStar int    `json:"init_star"`
	MaxStar  int    `json:"max_star"`
}

// HeroOptions lists every hero of the client's table by base id.
func HeroOptions() []HeroOption {
	out := make([]HeroOption, 0, len(partnerTable))
	for bid, p := range partnerTable {
		max := p.InitStar
		for k := range p.Stars {
			if n, err := strconv.Atoi(k); err == nil && n > max {
				max = n
			}
		}
		out = append(out, HeroOption{BID: bid, Name: p.Name, Camp: p.Camp, InitStar: p.InitStar, MaxStar: max})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BID < out[j].BID })
	return out
}

// HeroStarKnown reports whether the client's table has that hero at that star.
func HeroStarKnown(bid int64, star int) bool {
	p, ok := partnerTable[bid]
	if !ok {
		return false
	}
	_, ok = p.Stars[strconv.Itoa(star)]
	return ok
}
