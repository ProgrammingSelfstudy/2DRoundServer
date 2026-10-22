package store

// ShopBought sums a role's purchases of each good between two days (inclusive, YYYYMMDD).
// A zero from means from the beginning.
func (s *Store) ShopBought(rid, from, to int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT eid,SUM(num) FROM shop_buys WHERE rid=? AND day>=? AND day<=? GROUP BY eid`, rid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var eid, n int64
		if err := rows.Scan(&eid, &n); err != nil {
			return nil, err
		}
		out[eid] = n
	}
	return out, rows.Err()
}

// ShopBuy pays for num pieces of a good and delivers it, in one transaction. limit
// is the most the role may have bought between fromDay and toDay including this
// purchase (0 = no limit). It returns ErrConflict, changing
// nothing, when the role cannot pay or the limit would be passed.
func (s *Store) ShopBuy(rid, eid, day, num, fromDay, toDay, limit int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if limit > 0 {
		var have int64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(num),0) FROM shop_buys WHERE rid=? AND eid=? AND day>=? AND day<=?`, rid, eid, fromDay, toDay).Scan(&have); err != nil {
			return err
		}
		if have+num > limit {
			return ErrConflict
		}
	}
	if _, err := tx.Exec(`INSERT INTO shop_buys(rid,eid,day,num) VALUES(?,?,?,?) ON CONFLICT(rid,eid,day) DO UPDATE SET num=num+excluded.num`, rid, eid, day, num); err != nil {
		return err
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
