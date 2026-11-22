package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/store"
)

const (
	threeStarRandomShard = 29903
	fourStarRandomShard  = 29904
	fiveStarRandomShard  = 29905
)

// The H5 hero dungeon pays the same number of shards in the five-star pool.
func stoneLoot(rewards [][]int64, h5 bool) [][2]int64 {
	loot := pairsOf(rewards)
	if !h5 {
		return loot
	}
	var shards int64
	out := loot[:0]
	for _, reward := range loot {
		switch reward[0] {
		case threeStarRandomShard, fourStarRandomShard, fiveStarRandomShard:
			shards += reward[1]
		default:
			out = append(out, reward)
		}
	}
	if shards > 0 {
		out = append(out, [2]int64{fiveStarRandomShard, shards})
	}
	return out
}

func dungeonGains(rewards [][2]int64) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(rewards), 1, assets, items)
	return assets, items
}

// Keep the material dungeon's bag delta in the same order as the captured flow.
func sendStoneBalances(s *gateway.Session, db *store.Store, rid int64, before bagSnapshot) error {
	if err := sendAssets(s, db, rid); err != nil {
		return err
	}
	after, err := snapshotBags(db, rid)
	if err != nil {
		return err
	}
	return sendBagDelta(s, before, after)
}
