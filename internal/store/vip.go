package store

// VIPGiftLevels are the VIP levels whose gift the role has bought.
func (s *Store) VIPGiftLevels(rid int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT lev FROM vip_gifts WHERE rid=? ORDER BY lev`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var l int64
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// VIPGiftBuy pays for the gift of a VIP level and delivers it, in one transaction.
// It returns ErrConflict, changing nothing, when the gift was bought already or the
// role cannot pay.
func (s *Store) VIPGiftBuy(rid, lev int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO vip_gifts(rid,lev) VALUES(?,?)`, rid, lev)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
