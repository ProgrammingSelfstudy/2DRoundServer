package store

import "database/sql"

// CheckinState is a role's sign-in record.
type CheckinState struct {
	Month, Count, LastDay int64
}

// Checkin returns the role's record; the zero value when it never signed in.
func (s *Store) Checkin(rid int64) (CheckinState, error) {
	var c CheckinState
	err := s.db.QueryRow(`SELECT month,count,last_day FROM checkin WHERE rid=?`, rid).Scan(&c.Month, &c.Count, &c.LastDay)
	if err == sql.ErrNoRows {
		return CheckinState{}, nil
	}
	return c, err
}

// DoCheckin records a sign-in on day (YYYYMMDD) of month (YYYYMM) and pays the
// reward, in one transaction. before is the sign-in count of that month the
// caller based its reward on; ErrConflict means the role already signed in
// that day or the record changed since (nothing is paid).
func (s *Store) DoCheckin(rid, month, day, before int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur CheckinState
	switch err := tx.QueryRow(`SELECT month,count,last_day FROM checkin WHERE rid=?`, rid).Scan(&cur.Month, &cur.Count, &cur.LastDay); {
	case err == sql.ErrNoRows:
	case err != nil:
		return err
	}
	have := cur.Count
	if cur.Month != month {
		have = 0
	}
	if cur.LastDay == day || have != before {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO checkin(rid,month,count,last_day) VALUES(?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET month=excluded.month, count=excluded.count, last_day=excluded.last_day`, rid, month, before+1, day); err != nil {
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
