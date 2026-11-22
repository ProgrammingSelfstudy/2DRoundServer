package game

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"2droundserver/internal/store"
)

// The H5 release builder also reads this file for the odds shown on the summon page.
//
//go:embed data/h5_advanced_summon.json
var h5AdvancedJSON []byte

type advancedTier struct {
	Name       string
	Weight     int
	Guaranteed bool
	Heroes     []int64
}

type advancedPool struct {
	Pity       int
	Tiers      []advancedTier
	rareHeroes []int64
	weight     int
}

var h5Advanced = func() advancedPool {
	var p advancedPool
	if err := json.Unmarshal(h5AdvancedJSON, &p); err != nil {
		panic(err)
	}
	eligible := map[int64]bool{}
	for _, entry := range summonTable[300].Pool {
		bid := int64(entry[0])
		hero := partnerTable[bid]
		if hero.InitStar == 5 && hero.Camp >= 1 && hero.Camp <= 5 {
			eligible[bid] = true
		}
	}
	for _, tier := range p.Tiers {
		if tier.Weight < 1 || len(tier.Heroes) == 0 {
			panic("invalid H5 advanced summon tier")
		}
		for _, bid := range tier.Heroes {
			if !eligible[bid] {
				panic(fmt.Sprintf("ineligible or duplicate H5 advanced summon hero %d", bid))
			}
			delete(eligible, bid)
		}
		p.weight += tier.Weight * len(tier.Heroes)
		if tier.Guaranteed {
			p.rareHeroes = append(p.rareHeroes, tier.Heroes...)
		}
	}
	if p.Pity < 1 || p.Pity > 255 || len(p.rareHeroes) == 0 {
		panic("invalid H5 advanced summon guarantee")
	}
	return p
}()

func (p *advancedPool) pick(misses int, roll float64) (int64, int) {
	if misses >= p.Pity-1 {
		return p.rareHeroes[int(roll*float64(len(p.rareHeroes)))], 0
	}
	slot := int(roll * float64(p.weight))
	for _, tier := range p.Tiers {
		size := len(tier.Heroes) * tier.Weight
		if slot < size {
			next := misses + 1
			if tier.Guaranteed {
				next = 0
			}
			return tier.Heroes[slot/tier.Weight], next
		}
		slot -= size
	}
	panic("H5 advanced summon roll out of range")
}

func (p *advancedPool) draw(misses int, count int64) ([]store.PartnerSeed, int) {
	seeds := make([]store.PartnerSeed, 0, count)
	for i := int64(0); i < count; i++ {
		bid, next := p.pick(misses, summonRand())
		seeds = append(seeds, store.PartnerSeed{BID: bid, Star: partnerTable[bid].InitStar})
		misses = next
	}
	return seeds, misses
}
