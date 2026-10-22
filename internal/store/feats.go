package store

// AddCounter adds n to a cumulative counter and returns its new value.
func (s *Store) AddCounter(rid int64, key string, n int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO counters(rid,key,val) VALUES(?,?,?) ON CONFLICT(rid,key) DO UPDATE SET val=val+excluded.val`, rid, key, n); err != nil {
		return 0, err
	}
	var v int64
	if err := tx.QueryRow(`SELECT val FROM counters WHERE rid=? AND key=?`, rid, key).Scan(&v); err != nil {
		return 0, err
	}
	return v, tx.Commit()
}

// Counter reads a counter (0 when never counted).
func (s *Store) Counter(rid int64, key string) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT val FROM counters WHERE rid=? AND key=?`, rid, key).Scan(&v)
	if wrapIsNoRows(err) {
		return 0, nil
	}
	return v, err
}

// FeatsClaimed lists the achievement ids whose reward the role took.
func (s *Store) FeatsClaimed(rid int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM feats_claimed WHERE rid=?`, rid)
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

// ClaimFeat marks an achievement taken and pays it, in one transaction. ErrConflict,
// changing nothing, when it was taken before.
func (s *Store) ClaimFeat(rid, id int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO feats_claimed(rid,id) VALUES(?,?)`, rid, id); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
