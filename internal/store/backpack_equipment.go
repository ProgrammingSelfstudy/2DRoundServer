package store

import (
	"database/sql"
	"slices"
)

// ChangeEquipment updates all the hero's selected slots in one transaction.
// A change with BaseID zero takes the piece off. A non-nil expected list must
// still match the hero's equipment, so a plan based on an old loadout is rejected.
func (s *Store) ChangeEquipment(rid, partnerID int64, expected, changes []Equip) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the write lock and verify membership before reading the slots.
	// The lock flag is preserved. It protects the hero from being consumed.
	res, err := tx.Exec(`UPDATE partners SET locked=locked WHERE partner_id=? AND rid=?`, partnerID, rid)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if expected != nil {
		current, err := listEquipmentTx(tx, partnerID)
		if err != nil {
			return err
		}
		if !slices.Equal(current, expected) {
			return ErrConflict
		}
	}
	for _, change := range changes {
		if err := applyEquipmentTx(tx, rid, partnerID, change); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func listEquipmentTx(tx *sql.Tx, partnerID int64) ([]Equip, error) {
	rows, err := tx.Query(`SELECT slot,base_id FROM hero_equips WHERE partner_id=? ORDER BY slot`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Equip
	for rows.Next() {
		var e Equip
		if err := rows.Scan(&e.Slot, &e.BaseID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func applyEquipmentTx(tx *sql.Tx, rid, partnerID int64, change Equip) error {
	var old int64
	err := tx.QueryRow(`SELECT base_id FROM hero_equips WHERE partner_id=? AND slot=?`, partnerID, change.Slot).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if change.BaseID == 0 {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if _, err := tx.Exec(`DELETE FROM hero_equips WHERE partner_id=? AND slot=?`, partnerID, change.Slot); err != nil {
			return err
		}
	} else {
		if err := spendTx(tx, rid, nil, map[int64]int64{change.BaseID: 1}); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO hero_equips(partner_id,slot,base_id) VALUES(?,?,?)
			ON CONFLICT(partner_id,slot) DO UPDATE SET base_id=excluded.base_id`, partnerID, change.Slot, change.BaseID); err != nil {
			return err
		}
	}
	if old != 0 {
		return creditPlainTx(tx, rid, nil, map[int64]int64{old: 1})
	}
	return nil
}
