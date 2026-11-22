package store

import (
	"database/sql"
	"encoding/json"
)

// TakeStoneAttempt checks the attempt that was priced, then charges it and grants
// any sweep loot together. A later attempt must be priced from the new count.
func (s *Store) TakeStoneAttempt(rid, kind, day, used, limit, cost int64, assets, items map[int64]int64) error {
	if used < 0 || used >= limit {
		return ErrConflict
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO stone_dungeon_state(rid,type,day,count) VALUES(?,?,?,0)
		ON CONFLICT(rid,type) DO NOTHING`, rid, kind, day); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE stone_dungeon_state SET day=?, count=?
		WHERE rid=? AND type=? AND ((day=? AND count=?) OR (day<? AND ?=0))`,
		day, used+1, rid, kind, day, used, day, used)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	if cost > 0 {
		if err := spendTx(tx, rid, map[int64]int64{3: cost}, nil); err != nil {
			return err
		}
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearStoneDungeon records a victory and grants loot in the same transaction.
func (s *Store) ClearStoneDungeon(rid, id int64, assets, items, firstAssets, firstItems map[int64]int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO stone_dungeon_clears(rid,id) VALUES(?,?) ON CONFLICT(rid,id) DO NOTHING`, rid, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return false, err
	}
	if n == 1 {
		if err := creditTx(tx, rid, firstAssets, firstItems); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

// ModuleStateChange describes a transition from an observed module document.
// Previous is empty when RoleModule found no document for the role.
type ModuleStateChange struct {
	RID                   int64
	Previous              json.RawMessage
	Next                  any
	GainAssets, GainItems map[int64]int64
}

// CommitModuleState applies the transitions and credits together. A stale
// document rolls back the whole transaction, including any other role's gains.
func (s *Store) CommitModuleState(module string, changes ...ModuleStateChange) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, change := range changes {
		data, err := json.Marshal(change.Next)
		if err != nil {
			return err
		}
		var res sql.Result
		if len(change.Previous) == 0 {
			res, err = tx.Exec(`INSERT INTO module_state(rid,module,data) VALUES(?,?,?)
				ON CONFLICT(rid,module) DO NOTHING`, change.RID, module, string(data))
		} else {
			res, err = tx.Exec(`UPDATE module_state SET data=? WHERE rid=? AND module=? AND data=?`,
				string(data), change.RID, module, string(change.Previous))
		}
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		if err := creditTx(tx, change.RID, change.GainAssets, change.GainItems); err != nil {
			return err
		}
	}
	return tx.Commit()
}
