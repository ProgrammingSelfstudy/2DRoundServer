package store

// StoneCounts is how many attempts a role made on the given day, per dungeon kind.
func (s *Store) StoneCounts(rid, day int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT type,count FROM stone_dungeon_state WHERE rid=? AND day=?`, rid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var t, n int64
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, rows.Err()
}

// StoneAttempt counts one more attempt of a kind on the day and takes its cost, in one transaction. It returns
// ErrConflict, changing nothing, when the role has already made `limit` attempts today or cannot pay.
func (s *Store) StoneAttempt(rid, typ, day, limit int64, spendAssets, spendItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var have int64
	if err := tx.QueryRow(`SELECT count FROM stone_dungeon_state WHERE rid=? AND type=? AND day=?`, rid, typ, day).Scan(&have); err != nil && !wrapIsNoRows(err) {
		return err
	}
	if have >= limit {
		return ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO stone_dungeon_state(rid,type,day,count) VALUES(?,?,?,1)
		ON CONFLICT(rid,type) DO UPDATE SET day=excluded.day, count=CASE WHEN stone_dungeon_state.day=excluded.day THEN stone_dungeon_state.count+1 ELSE 1 END`, rid, typ, day); err != nil {
		return err
	}
	return tx.Commit()
}

// StoneCleared lists the dungeons a role has cleared.
func (s *Store) StoneCleared(rid int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM stone_dungeon_clears WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MarkStoneCleared notes a cleared dungeon and says whether it was the first time.
func (s *Store) MarkStoneCleared(rid, id int64) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO stone_dungeon_clears(rid,id) VALUES(?,?)`, rid, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
