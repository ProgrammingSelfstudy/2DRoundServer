package store

// Activity is one entry of the schedule: a holiday activity open from StartAt to EndAt (0 = never ends) on a server
// ('' = every server) while Status is 1. Spec is the JSON of the game's ActivitySpec.
type Activity struct {
	ID        int64  `json:"id"`
	SrvID     string `json:"srv_id"`
	CampID    int64  `json:"camp_id"`
	BID       int64  `json:"bid"`
	Status    int    `json:"status"` // 1 on, 0 off
	StartAt   int64  `json:"start_at"`
	EndAt     int64  `json:"end_at"`
	Spec      string `json:"spec"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

const activityCols = `id,srv_id,camp_id,bid,status,start_at,end_at,spec,created_by,created_at,updated_at`

func scanActivity(sc interface{ Scan(...any) error }) (*Activity, error) {
	var a Activity
	err := sc.Scan(&a.ID, &a.SrvID, &a.CampID, &a.BID, &a.Status, &a.StartAt, &a.EndAt, &a.Spec, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	return &a, wrapNoRows(err)
}

func (s *Store) CreateActivity(a Activity) (*Activity, error) {
	a.CreatedAt = s.unix()
	a.UpdatedAt = a.CreatedAt
	res, err := s.db.Exec(`INSERT INTO activities(srv_id,camp_id,bid,status,start_at,end_at,spec,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.SrvID, a.CampID, a.BID, a.Status, a.StartAt, a.EndAt, a.Spec, a.CreatedBy, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.ID, _ = res.LastInsertId()
	return &a, nil
}

func (s *Store) Activity(id int64) (*Activity, error) {
	return scanActivity(s.db.QueryRow(`SELECT `+activityCols+` FROM activities WHERE id=?`, id))
}

func (s *Store) UpdateActivity(a Activity) error {
	res, err := s.db.Exec(`UPDATE activities SET srv_id=?,camp_id=?,bid=?,status=?,start_at=?,end_at=?,spec=?,updated_at=? WHERE id=?`,
		a.SrvID, a.CampID, a.BID, a.Status, a.StartAt, a.EndAt, a.Spec, s.unix(), a.ID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteActivity(id int64) error {
	res, err := s.db.Exec(`DELETE FROM activities WHERE id=?`, id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActivities pages the whole schedule, newest first; srv filters to the entries of a server and the ones for all.
func (s *Store) ListActivities(srv string, limit, offset int) ([]Activity, int, error) {
	where, args := ``, []any{}
	if srv != "" {
		where, args = ` WHERE srv_id IN ('',?)`, []any{srv}
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM activities`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+activityCols+` FROM activities`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// ActivitiesOf is what is switched on for a server (its own and the ones for every server); the caller applies the
// time window.
func (s *Store) ActivitiesOf(srv string) ([]Activity, error) {
	rows, err := s.db.Query(`SELECT `+activityCols+` FROM activities WHERE status=1 AND srv_id IN ('',?) ORDER BY id`, srv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
