package store

import "database/sql"

// equipsOf reads the worn pieces of the heroes a WHERE clause on hero_equips selects.
func (s *Store) equipsOf(where string, arg any) (map[int64][]Equip, error) {
	rows, err := s.db.Query(`SELECT partner_id,slot,base_id FROM hero_equips WHERE `+where+` ORDER BY partner_id,slot`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Equip{}
	for rows.Next() {
		var pid int64
		var e Equip
		if err := rows.Scan(&pid, &e.Slot, &e.BaseID); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], e)
	}
	return out, rows.Err()
}

// releaseEquipsTx gives the pieces a hero wears back to the role's item ledger.
func releaseEquipsTx(tx *sql.Tx, rid, partnerID int64) error {
	rows, err := tx.Query(`SELECT base_id FROM hero_equips WHERE partner_id=?`, partnerID)
	if err != nil {
		return err
	}
	back := map[int64]int64{}
	for rows.Next() {
		var b int64
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		back[b]++
	}
	rows.Close()
	if len(back) == 0 {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM hero_equips WHERE partner_id=?`, partnerID); err != nil {
		return err
	}
	return creditPlainTx(tx, rid, nil, back)
}

// EquipPiece puts one piece (a base id) from the role's item ledger on a hero's
// slot; a piece already there goes back to the ledger. ErrConflict, changing
// nothing, when the role does not hold the piece; ErrNotFound for a hero that
// is not the role's.
func (s *Store) EquipPiece(rid, partnerID, slot, baseID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).Scan(&one); err != nil {
		return wrapNoRows(err)
	}
	res, err := tx.Exec(`UPDATE role_items SET num=num-1 WHERE rid=? AND item_id=? AND num>=1`, rid, baseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(`DELETE FROM role_items WHERE rid=? AND num<=0`, rid); err != nil {
		return err
	}
	var old int64
	switch err := tx.QueryRow(`SELECT base_id FROM hero_equips WHERE partner_id=? AND slot=?`, partnerID, slot).Scan(&old); {
	case err == nil:
		if err := creditPlainTx(tx, rid, nil, map[int64]int64{old: 1}); err != nil {
			return err
		}
	case err != sql.ErrNoRows:
		return err
	}
	if _, err := tx.Exec(`INSERT INTO hero_equips(partner_id,slot,base_id) VALUES(?,?,?) ON CONFLICT(partner_id,slot) DO UPDATE SET base_id=excluded.base_id`, partnerID, slot, baseID); err != nil {
		return err
	}
	return tx.Commit()
}

// UnequipPiece takes the piece off a hero's slot and back into the ledger.
// ErrNotFound when the hero is not the role's or wears nothing there.
func (s *Store) UnequipPiece(rid, partnerID, slot int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var base int64
	err = tx.QueryRow(`SELECT e.base_id FROM hero_equips e JOIN partners p ON p.partner_id=e.partner_id
		WHERE e.partner_id=? AND e.slot=? AND p.rid=?`, partnerID, slot, rid).Scan(&base)
	if err != nil {
		return wrapNoRows(err)
	}
	if _, err := tx.Exec(`DELETE FROM hero_equips WHERE partner_id=? AND slot=?`, partnerID, slot); err != nil {
		return err
	}
	if err := creditPlainTx(tx, rid, nil, map[int64]int64{base: 1}); err != nil {
		return err
	}
	return tx.Commit()
}
