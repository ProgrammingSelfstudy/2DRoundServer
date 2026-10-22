package store

import (
	"database/sql"
	"sync/atomic"
)

// LevelRule says how experience turns into role levels. The ledger keeps the exp
// of the current level in the ExpLabel asset: when it reaches ExpMax(lev) the role
// moves up a level, the exp carries over, and Reward(lev) is paid for each level
// reached. Without a rule (SetLevelRule not called) experience is just a number.
type LevelRule struct {
	ExpLabel int64
	MaxLev   int
	ExpMax   func(lev int) int64
	Reward   func(lev int) (assets, items map[int64]int64)
}

var levelRule atomic.Pointer[LevelRule]

// SetLevelRule installs the rule for every Store of the process.
func SetLevelRule(r *LevelRule) { levelRule.Store(r) }

// levelUpTx applies the rule to a role after experience was credited.
func levelUpTx(tx *sql.Tx, rid int64, r *LevelRule) error {
	var lev int
	if err := tx.QueryRow(`SELECT lev FROM roles WHERE rid=?`, rid).Scan(&lev); err != nil {
		return err
	}
	var exp int64
	if err := tx.QueryRow(`SELECT val FROM role_assets WHERE rid=? AND label=?`, rid, r.ExpLabel).Scan(&exp); err != nil {
		return err
	}
	from := lev
	rewardA, rewardI := map[int64]int64{}, map[int64]int64{}
	for lev < r.MaxLev {
		need := r.ExpMax(lev)
		if need <= 0 || exp < need {
			break
		}
		exp -= need
		lev++
		a, i := r.Reward(lev)
		for k, v := range a {
			rewardA[k] += v
		}
		for k, v := range i {
			rewardI[k] += v
		}
	}
	if lev == from {
		return nil
	}
	if _, err := tx.Exec(`UPDATE role_assets SET val=? WHERE rid=? AND label=?`, exp, rid, r.ExpLabel); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE roles SET lev=? WHERE rid=?`, lev, rid); err != nil {
		return err
	}
	return creditPlainTx(tx, rid, rewardA, rewardI)
}
