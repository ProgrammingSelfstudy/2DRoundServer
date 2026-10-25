package store

import "database/sql"

// Talent is one learned talent skill of a hero: its slot and current skill id.
type Talent struct{ Pos, SkillID int64 }

// talentsOf lists the talents of the heroes matching where, by hero and slot.
func (s *Store) talentsOf(where string, arg any) (map[int64][]Talent, error) {
	rows, err := s.db.Query(`SELECT partner_id,pos,skill_id FROM partner_talents WHERE `+where+` ORDER BY partner_id,pos`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Talent{}
	for rows.Next() {
		var pid int64
		var t Talent
		if err := rows.Scan(&pid, &t.Pos, &t.SkillID); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], t)
	}
	return out, rows.Err()
}

// ChangeTalent pays for and stores one change of a hero's talent slot, in one
// transaction. from is the skill the slot must hold now (0 = empty), to the skill
// it holds after (0 = emptied). The role pays spendAssets / spendItems and is
// credited gainAssets / gainItems. It returns ErrConflict, changing nothing, when
// the slot no longer holds from, the hero is not the role's or the role cannot pay.
func (s *Store) ChangeTalent(rid, partnerID, pos, from, to int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var own int
	if err := tx.QueryRow(`SELECT 1 FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).Scan(&own); err != nil {
		if err == sql.ErrNoRows {
			return ErrConflict
		}
		return err
	}
	var cur int64
	switch err := tx.QueryRow(`SELECT skill_id FROM partner_talents WHERE partner_id=? AND pos=?`, partnerID, pos).Scan(&cur); {
	case err == sql.ErrNoRows:
		cur = 0
	case err != nil:
		return err
	}
	if cur != from {
		return ErrConflict
	}
	if to == 0 {
		_, err = tx.Exec(`DELETE FROM partner_talents WHERE partner_id=? AND pos=?`, partnerID, pos)
	} else {
		_, err = tx.Exec(`INSERT INTO partner_talents(partner_id,pos,skill_id) VALUES(?,?,?) ON CONFLICT(partner_id,pos) DO UPDATE SET skill_id=excluded.skill_id`, partnerID, pos, to)
	}
	if err != nil {
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
