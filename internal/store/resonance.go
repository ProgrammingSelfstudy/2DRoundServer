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
