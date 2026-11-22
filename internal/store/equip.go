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
	if baseID <= 0 {
		return ErrConflict
	}
	return s.ChangeEquipment(rid, partnerID, nil, []Equip{{Slot: slot, BaseID: baseID}})
}

// UnequipPiece takes the piece off a hero's slot and back into the ledger.
// ErrNotFound when the hero is not the role's or wears nothing there.
func (s *Store) UnequipPiece(rid, partnerID, slot int64) error {
	return s.ChangeEquipment(rid, partnerID, nil, []Equip{{Slot: slot}})
}
