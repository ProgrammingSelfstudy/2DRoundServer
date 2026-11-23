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
	Pity        int
	Tiers       []advancedTier
	rareHeroes  []int64
	weight      int
	shardPool   [][2]float64
	shardChance float64
}

var h5Advanced = func() advancedPool {
	var p advancedPool
	if err := json.Unmarshal(h5AdvancedJSON, &p); err != nil {
		panic(err)
	}
	eligible := map[int64]bool{}
	var totalWeight, shardWeight float64
	for _, entry := range summonTable[300].Pool {
		bid := int64(entry[0])
		hero := partnerTable[bid]
		totalWeight += entry[1]
		if !h5HeroEligible(bid, hero.InitStar) {
			p.shardPool = append(p.shardPool, entry)
			shardWeight += entry[1]
		} else if hero.Camp >= 1 && hero.Camp <= 5 {
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
	if p.Pity < 1 || p.Pity > 255 || len(p.rareHeroes) == 0 || totalWeight <= shardWeight {
		panic("invalid H5 advanced summon guarantee")
	}
	p.shardChance = shardWeight / totalWeight
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

func (p *advancedPool) remaining(misses int) int {
	return max(1, p.Pity-misses)
}

func (p *advancedPool) draw(misses int, count int64, draws *summonDrawResults) ([]store.PartnerSeed, map[int64]int64, int, error) {
	var result store.SummonResult
	for i := int64(0); i < count; i++ {
		roll := summonRand()
		var bid int64
		next := misses
		// Keep the legacy shard odds; tier weights apply only within the hero portion.
		if roll < p.shardChance {
			bid = pickWeightedHero(p.shardPool, roll/p.shardChance)
		} else {
			bid, next = p.pick(misses, (roll-p.shardChance)/(1-p.shardChance))
		}
		grants := len(result.Seeds)
		if !draws.addHero(&result, true, bid, 0) {
			return nil, nil, misses, errSummonRewardUnavailable
		}
		// Only a granted hero can advance or reset pity, so a shard leaves a due guarantee pending.
		if len(result.Seeds) > grants {
			misses = next
		}
	}
	draws.remaining = p.remaining(misses)
	return result.Seeds, result.GainItems, misses, nil
}
