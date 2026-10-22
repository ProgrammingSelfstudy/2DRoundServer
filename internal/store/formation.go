package store

// FormationSlot puts a hero at a position.
type FormationSlot struct{ Pos, PartnerID int64 }

// Formation is a saved team.
type Formation struct {
	Type, FormationType, HallowsID int64
	Slots                          []FormationSlot
}

// Formation returns the role's saved formation of a kind, with only the heroes it
// still owns, or ErrNotFound when it never saved one.
func (s *Store) Formation(rid, typ int64) (*Formation, error) {
	f := Formation{Type: typ}
	if err := s.db.QueryRow(`SELECT formation_type,hallows_id FROM formations WHERE rid=? AND type=?`, rid, typ).Scan(&f.FormationType, &f.HallowsID); err != nil {
		return nil, wrapNoRows(err)
	}
	rows, err := s.db.Query(`SELECT f.pos,f.partner_id FROM formation_slots f JOIN partners p ON p.partner_id=f.partner_id AND p.rid=f.rid
		WHERE f.rid=? AND f.type=? ORDER BY f.pos`, rid, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sl FormationSlot
		if err := rows.Scan(&sl.Pos, &sl.PartnerID); err != nil {
			return nil, err
		}
		f.Slots = append(f.Slots, sl)
	}
	return &f, rows.Err()
}

// SaveFormation replaces the role's formation of a kind. It returns ErrNotFound,
// changing nothing, when a slot names a hero the role does not own.
func (s *Store) SaveFormation(rid int64, f Formation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sl := range f.Slots {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM partners WHERE partner_id=? AND rid=?`, sl.PartnerID, rid).Scan(&one); err != nil {
			return wrapNoRows(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO formations(rid,type,formation_type,hallows_id) VALUES(?,?,?,?)
		ON CONFLICT(rid,type) DO UPDATE SET formation_type=excluded.formation_type, hallows_id=excluded.hallows_id`, rid, f.Type, f.FormationType, f.HallowsID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM formation_slots WHERE rid=? AND type=?`, rid, f.Type); err != nil {
		return err
	}
	for _, sl := range f.Slots {
		if _, err := tx.Exec(`INSERT INTO formation_slots(rid,type,pos,partner_id) VALUES(?,?,?,?)`, rid, f.Type, sl.Pos, sl.PartnerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
