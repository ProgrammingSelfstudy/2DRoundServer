package store

import (
	"database/sql"
	"errors"
)

// PartnerChange is the pending hero swap of a role: ok is false when there is none.
func (s *Store) PartnerChange(rid int64) (partnerID, newBID int64, ok bool, err error) {
	err = s.db.QueryRow(`SELECT partner_id,new_bid FROM partner_changes WHERE rid=?`, rid).Scan(&partnerID, &newBID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	return partnerID, newBID, err == nil, err
}

// StartPartnerChange takes the cost and records that the hero would turn into newBID,
// in one transaction. It returns ErrConflict, changing nothing, when the role has a swap
// pending already, the hero is not its own or the role cannot pay.
func (s *Store) StartPartnerChange(rid, partnerID, newBID int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		return err
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO partner_changes(rid,partner_id,new_bid) VALUES(?,?,?)`, rid, partnerID, newBID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelPartnerChange drops the pending swap of a hero; it reports whether there was one.
func (s *Store) CancelPartnerChange(rid, partnerID int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM partner_changes WHERE rid=? AND partner_id=?`, rid, partnerID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SavePartnerChange turns the hero into the pending hero base id and drops the swap. As the
// original server does (capture 2026-10-21b), the result is a new hero with a new partner id
// (level, star and breakthrough stay, so do the worn equipment and the formation places) and
// the old hero is gone. It returns the new hero, or ErrNotFound when no swap of that hero is
// pending.
func (s *Store) SavePartnerChange(rid, partnerID int64) (*Partner, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var newBID int64
	err = tx.QueryRow(`SELECT new_bid FROM partner_changes WHERE rid=? AND partner_id=?`, rid, partnerID).Scan(&newBID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`INSERT INTO partners(rid,bid,lev,star,break_lev,created_at)
		SELECT rid,?,lev,star,break_lev,created_at FROM partners WHERE partner_id=? AND rid=?`, newBID, partnerID, rid)
	if err != nil {
		return nil, err
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE hero_equips SET partner_id=? WHERE partner_id=?`, newID, partnerID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE formation_slots SET partner_id=? WHERE rid=? AND partner_id=?`, newID, rid, partnerID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM partner_changes WHERE rid=?`, rid); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.PartnerByID(rid, newID)
}
