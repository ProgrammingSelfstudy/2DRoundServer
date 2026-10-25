package store

// PartnerResetUntil is the time (unix seconds) before which the hero may not be reborn again.
func (s *Store) PartnerResetUntil(rid, partnerID int64) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT reset_until FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).Scan(&v)
	if wrapIsNoRows(err) {
		return 0, ErrNotFound
	}
	return v, err
}

// RebornPartner sets a hero back to level 1 and break step 0 (its star stays), takes its worn
// equipment and learned talents off, pays the refund and counts the reborn under counterKey,
// all in one transaction. It returns ErrConflict, changing nothing, when the hero is no longer at
// (fromLev, fromBreak), is still cooling down at now, or the counter has reached maxCount.
func (s *Store) RebornPartner(rid, partnerID int64, fromLev, fromBreak int, now, until int64, counterKey string, maxCount int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used int64
	if err := tx.QueryRow(`SELECT COALESCE((SELECT val FROM counters WHERE rid=? AND key=?),0)`, rid, counterKey).Scan(&used); err != nil {
		return err
	}
	if used >= maxCount {
		return ErrConflict
	}
	res, err := tx.Exec(`UPDATE partners SET lev=1, break_lev=0, reset_until=? WHERE partner_id=? AND rid=? AND lev=? AND break_lev=? AND reset_until<=?`,
		until, partnerID, rid, fromLev, fromBreak, now)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := releaseEquipsTx(tx, rid, partnerID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM partner_talents WHERE partner_id=?`, partnerID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO counters(rid,key,val) VALUES(?,?,1) ON CONFLICT(rid,key) DO UPDATE SET val=val+1`, rid, counterKey); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// OfferPartners deletes heroes of the role (their equipment goes back to the item ledger) and
// pays the refund, in one transaction. It returns ErrConflict, changing nothing, when a hero is
// not the role's, is locked, or is listed twice.
func (s *Store) OfferPartners(rid int64, ids []int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var locked int
		if err := tx.QueryRow(`SELECT locked FROM partners WHERE partner_id=? AND rid=?`, id, rid).Scan(&locked); err != nil || locked != 0 {
			return ErrConflict
		}
		if err := releaseEquipsTx(tx, rid, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM partners WHERE partner_id=? AND rid=?`, id, rid); err != nil {
			return err
		}
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// ReturnPartner sets a hero back to a lower star (the "回退"): it takes the new star, level and
// break step, closes the talent slots that no longer open (their talents are dropped), pays the
// cost and the refund and adds the returned copies of the hero, in one transaction. It returns
// ErrConflict, changing nothing, when the hero is no longer at (fromStar, fromLev, fromBreak),
// the role cannot pay, or the copies would overfill the role's hero slots (capacity).
func (s *Store) ReturnPartner(rid, partnerID int64, fromStar, fromLev, fromBreak, toStar, toLev, toBreak int, dropTalentPos []int64, copies []PartnerSeed, capacity int, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) ([]Partner, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE partners SET star=?, lev=?, break_lev=? WHERE partner_id=? AND rid=? AND star=? AND lev=? AND break_lev=?`,
		toStar, toLev, toBreak, partnerID, rid, fromStar, fromLev, fromBreak)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	for _, pos := range dropTalentPos {
		if _, err := tx.Exec(`DELETE FROM partner_talents WHERE partner_id=? AND pos=?`, partnerID, pos); err != nil {
			return nil, err
		}
	}
	var have int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM partners WHERE rid=?`, rid).Scan(&have); err != nil {
		return nil, err
	}
	if have+len(copies) > capacity {
		return nil, ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return nil, err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return nil, err
	}
	out := make([]Partner, 0, len(copies))
	for _, sd := range copies {
		r, err := tx.Exec(`INSERT INTO partners(rid,bid,lev,star,created_at) VALUES(?,?,1,?,?)`, rid, sd.BID, sd.Star, s.unix())
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		out = append(out, Partner{PartnerID: id, RID: rid, BID: sd.BID, Lev: 1, Star: sd.Star})
	}
	return out, tx.Commit()
}
