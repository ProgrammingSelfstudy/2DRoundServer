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
