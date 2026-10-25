package store

import (
	"database/sql"
	"errors"
	"sync/atomic"
)

// ResSlot is one crystal slot that has a row: the hero in it (0 = none) and when it may be used again.
type ResSlot struct{ Pos, PartnerID, CoolTime int64 }

// ResonanceRule works out, for a role's heroes (the ones in a slot have ResSlot > 0), the level of
// the crystal and the break step a hero in a slot shows; the game sets it.
type ResonanceRule func(all []Partner) (lev int, breakOf func(Partner) int)

var resonanceRule atomic.Pointer[ResonanceRule]

// SetResonanceRule installs the rule that fills ShownLev / ShownBreak of the heroes in a crystal slot.
func SetResonanceRule(r ResonanceRule) { resonanceRule.Store(&r) }

// slotsOf maps hero id -> slot position for the heroes of the role that sit in a slot.
func (s *Store) slotsOf(rid int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT partner_id,pos FROM resonance_slots WHERE rid=? AND partner_id IS NOT NULL`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, pos int64
		if err := rows.Scan(&id, &pos); err != nil {
			return nil, err
		}
		out[id] = pos
	}
	return out, rows.Err()
}

// applyResonance marks the heroes in a crystal slot and gives them the level and break step they show.
func (s *Store) applyResonance(rid int64, ps []Partner) error {
	slots, err := s.slotsOf(rid)
	if err != nil || len(slots) == 0 {
		return err
	}
	for i := range ps {
		ps[i].ResSlot = slots[ps[i].PartnerID]
	}
	rule := resonanceRule.Load()
	if rule == nil {
		return nil
	}
	lev, breakOf := (*rule)(ps)
	if lev <= 0 {
		for i := range ps {
			ps[i].ResSlot = 0 // no crystal level yet: nothing is resonated
		}
		return nil
	}
	for i := range ps {
		if ps[i].ResSlot > 0 {
			ps[i].ShownLev, ps[i].ShownBreak = lev, breakOf(ps[i])
		}
	}
	return nil
}

// ResonanceOf reads the slots that have a row and the numbers of unlocked slots.
func (s *Store) ResonanceOf(rid int64) (slots []ResSlot, gold, item int, err error) {
	rows, err := s.db.Query(`SELECT pos,COALESCE(partner_id,0),cool_time FROM resonance_slots WHERE rid=? ORDER BY pos`, rid)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var sl ResSlot
		if err := rows.Scan(&sl.Pos, &sl.PartnerID, &sl.CoolTime); err != nil {
			return nil, 0, 0, err
		}
		slots = append(slots, sl)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	err = s.db.QueryRow(`SELECT gold_count,item_count FROM role_resonance WHERE rid=?`, rid).Scan(&gold, &item)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return slots, gold, item, err
}

// PlaceResonance puts a hero in an empty slot whose cool-down is over at now. ErrNotFound for a hero
// that is not the role's; ErrConflict when the slot is taken or cooling down or the hero already sits in one.
func (s *Store) PlaceResonance(rid, pos, partnerID, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var cur sql.NullInt64
	var cool int64
	switch err := tx.QueryRow(`SELECT partner_id,cool_time FROM resonance_slots WHERE rid=? AND pos=?`, rid, pos).Scan(&cur, &cool); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case cur.Valid || cool > now:
		return ErrConflict
	}
	if err := tx.QueryRow(`SELECT 1 FROM resonance_slots WHERE partner_id=?`, partnerID).Scan(&one); err == nil {
		return ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO resonance_slots(rid,pos,partner_id,cool_time) VALUES(?,?,?,0) ON CONFLICT(rid,pos) DO UPDATE SET partner_id=excluded.partner_id, cool_time=0`, rid, pos, partnerID); err != nil {
		return err
	}
	return tx.Commit()
}

// TakeOffResonance empties a slot that holds that hero and starts its cool-down until coolUntil.
// ErrConflict when the hero is not in that slot.
func (s *Store) TakeOffResonance(rid, pos, partnerID, coolUntil int64) error {
	res, err := s.db.Exec(`UPDATE resonance_slots SET partner_id=NULL, cool_time=? WHERE rid=? AND pos=? AND partner_id=?`, coolUntil, rid, pos, partnerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// EndResonanceCool pays to end the cool-down of an empty slot at once. ErrConflict when the slot is
// not cooling down at now or the role cannot pay.
func (s *Store) EndResonanceCool(rid, pos, now int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE resonance_slots SET cool_time=0 WHERE rid=? AND pos=? AND partner_id IS NULL AND cool_time>?`, rid, pos, now)
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

// UnlockResonance pays for one more slot, by gold (gems, true) or by item, if the counter is still at
// `from` (so a repeated request cannot unlock twice). ErrConflict when it is not or the role cannot pay.
func (s *Store) UnlockResonance(rid int64, byGold bool, from int, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	col := "item_count"
	if byGold {
		col = "gold_count"
	}
	if _, err := tx.Exec(`INSERT INTO role_resonance(rid) VALUES(?) ON CONFLICT(rid) DO NOTHING`, rid); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE role_resonance SET `+col+`=`+col+`+1 WHERE rid=? AND `+col+`=?`, rid, from)
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

// ensureResonanceRow makes sure the role has its role_resonance row.
func (s *Store) ensureResonanceRow(rid int64) error {
	_, err := s.db.Exec(`INSERT INTO role_resonance(rid) VALUES(?) ON CONFLICT(rid) DO NOTHING`, rid)
	return err
}

// StoneOf reads the stone tablet level and the highest total of stars the offering heroes ever had.
func (s *Store) StoneOf(rid int64) (lev, maxStars int, err error) {
	err = s.db.QueryRow(`SELECT stone_lev,max_stars FROM role_resonance WHERE rid=?`, rid).Scan(&lev, &maxStars)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, 0, nil
	}
	return lev, maxStars, err
}

// RaiseMaxStars records a new highest total of stars (it never goes down).
func (s *Store) RaiseMaxStars(rid int64, stars int) error {
	if err := s.ensureResonanceRow(rid); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE role_resonance SET max_stars=? WHERE rid=? AND max_stars<?`, stars, rid, stars)
	return err
}

// LevelUpStone raises the stone tablet one level if it is still at `from`, and pays for it.
// ErrConflict, changing nothing, when it is not or the role cannot pay.
func (s *Store) LevelUpStone(rid int64, from int, assets, items map[int64]int64) error {
	if err := s.ensureResonanceRow(rid); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE role_resonance SET stone_lev=stone_lev+1 WHERE rid=? AND stone_lev=?`, rid, from)
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

// Extract is the alchemy batch of a role.
type Extract struct{ Total, Start, PerSec, Collected int64 }

// ExtractOf reads the batch; ok is false when none is running.
func (s *Store) ExtractOf(rid int64) (e Extract, ok bool, err error) {
	err = s.db.QueryRow(`SELECT total,start_time,per_sec,collected FROM resonance_extract WHERE rid=?`, rid).Scan(&e.Total, &e.Start, &e.PerSec, &e.Collected)
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

// StartExtract puts hero exp in: a new batch of `total` units, paid now. ErrConflict when a batch
// is running or the role cannot pay.
func (s *Store) StartExtract(rid, total, now, perSec int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO resonance_extract(rid,total,start_time,per_sec) VALUES(?,?,?,?)`, rid, total, now, perSec); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// RetrieveExtract ends the unfinished part of the batch: it keeps `done` units (finished at now) and
// pays the exp of the rest back. ErrConflict when the batch is gone or changed (total / collected).
func (s *Store) RetrieveExtract(rid, wasTotal, done int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE resonance_extract SET total=? WHERE rid=? AND total=?`, done, rid, wasTotal)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(`DELETE FROM resonance_extract WHERE rid=? AND total<=collected`, rid); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// CollectExtract takes `ready` finished units out of the batch (the batch ends when all are
// collected) and pays the items. ErrConflict when the batch is gone or `collected` changed.
func (s *Store) CollectExtract(rid, wasCollected, ready int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE resonance_extract SET collected=collected+? WHERE rid=? AND collected=?`, ready, rid, wasCollected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(`DELETE FROM resonance_extract WHERE rid=? AND collected>=total`, rid); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
