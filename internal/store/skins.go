package store

// Skin is a skin a role owns; EndTime 0 is permanent.
type Skin struct{ ID, EndTime int64 }

// SkinsOf lists the skins the role owns that have not run out at now.
func (s *Store) SkinsOf(rid, now int64) ([]Skin, error) {
	rows, err := s.db.Query(`SELECT skin_id,end_time FROM role_skins WHERE rid=? AND (end_time=0 OR end_time>?) ORDER BY skin_id`, rid, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skin
	for rows.Next() {
		var k Skin
		if err := rows.Scan(&k.ID, &k.EndTime); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GrantSkin gives the role a skin until endTime (0 = for good). Granting a skin it owns sets the
// new end time, so a permanent grant also turns a timed skin permanent.
func (s *Store) GrantSkin(rid, skinID, endTime int64) error {
	_, err := s.db.Exec(`INSERT INTO role_skins(rid,skin_id,end_time) VALUES(?,?,?) ON CONFLICT(rid,skin_id) DO UPDATE SET end_time=excluded.end_time`, rid, skinID, endTime)
	return err
}

// WearSkin sets the skin a hero wears (0 = none). ErrNotFound for a hero that is not the role's,
// ErrConflict when the role does not own the skin (or it ran out at now).
func (s *Store) WearSkin(rid, partnerID, skinID, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if skinID != 0 {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM role_skins WHERE rid=? AND skin_id=? AND (end_time=0 OR end_time>?)`, rid, skinID, now).Scan(&one); err != nil {
			if wrapIsNoRows(err) {
				return ErrConflict
			}
			return err
		}
	}
	res, err := tx.Exec(`UPDATE partners SET use_skin=? WHERE partner_id=? AND rid=?`, skinID, partnerID, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// UpgradeSkin turns an owned skin into its next level and pays for it, in one transaction; a hero
// that wears the old one wears the new one. ErrConflict, changing nothing, when the role does not
// own `from` (or it ran out at now), already owns `to`, or cannot pay.
func (s *Store) UpgradeSkin(rid, from, to, now int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var end int64
	if err := tx.QueryRow(`SELECT end_time FROM role_skins WHERE rid=? AND skin_id=? AND (end_time=0 OR end_time>?)`, rid, from, now).Scan(&end); err != nil {
		if wrapIsNoRows(err) {
			return ErrConflict
		}
		return err
	}
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM role_skins WHERE rid=? AND skin_id=?`, rid, to).Scan(&one); err == nil {
		return ErrConflict
	} else if !wrapIsNoRows(err) {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM role_skins WHERE rid=? AND skin_id=?`, rid, from); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO role_skins(rid,skin_id,end_time) VALUES(?,?,?)`, rid, to, end); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE partners SET use_skin=? WHERE rid=? AND use_skin=?`, to, rid, from); err != nil {
		return err
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
