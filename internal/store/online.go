package store

// OnlineSeconds is how long a role was online on the given day (a YYYYMMDD key).
func (s *Store) OnlineSeconds(rid, day int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT seconds FROM online_time WHERE rid=? AND day=?`, rid, day).Scan(&n)
	if err != nil {
		if wrapNoRows(err) == ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// AddOnlineSeconds adds to the role's online time of that day; if the stored
// time belongs to another day it starts over from n.
func (s *Store) AddOnlineSeconds(rid, day, n int64) error {
	_, err := s.db.Exec(`INSERT INTO online_time(rid,day,seconds) VALUES(?,?,?)
		ON CONFLICT(rid) DO UPDATE SET seconds = CASE WHEN day=excluded.day THEN seconds+excluded.seconds ELSE excluded.seconds END, day=excluded.day`, rid, day, n)
	return err
}

// OnlineGiftClaims lists the steps (by required seconds) claimed on that day.
func (s *Store) OnlineGiftClaims(rid, day int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT step_time FROM online_gift_claims WHERE rid=? AND day=? ORDER BY step_time`, rid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimOnlineGift records the claim and pays the reward in one transaction.
// ErrConflict: that step was already claimed on that day (nothing is paid).
func (s *Store) ClaimOnlineGift(rid, day, stepTime int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO online_gift_claims(rid,day,step_time,claimed_at) VALUES(?,?,?,?)`, rid, day, stepTime, s.unix()); err != nil {
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
