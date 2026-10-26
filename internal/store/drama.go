package store

type Drama struct {
	RID       int64 `json:"rid"`
	Mode      int   `json:"mode"`
	ChapterID int   `json:"chapter_id"`
	DunID     int64 `json:"dun_id"`
	Status    int   `json:"status"`
	MaxDunID  int64 `json:"max_dun_id"`
	IsFirst   bool  `json:"is_first"`
	UpdatedAt int64 `json:"updated_at"`
}

// Drama returns the stored campaign progress, or ErrNotFound for a role at the start.
func (s *Store) Drama(rid int64) (*Drama, error) {
	var d Drama
	var first int
	err := s.db.QueryRow(`SELECT rid,mode,chapter_id,dun_id,status,max_dun_id,is_first,updated_at FROM role_drama WHERE rid=?`, rid).
		Scan(&d.RID, &d.Mode, &d.ChapterID, &d.DunID, &d.Status, &d.MaxDunID, &first, &d.UpdatedAt)
	d.IsFirst = first != 0
	return &d, wrapNoRows(err)
}

func (s *Store) SaveDrama(d Drama) error {
	_, err := s.db.Exec(`INSERT INTO role_drama(rid,mode,chapter_id,dun_id,status,max_dun_id,is_first,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET mode=excluded.mode, chapter_id=excluded.chapter_id, dun_id=excluded.dun_id,
		status=excluded.status, max_dun_id=excluded.max_dun_id, is_first=excluded.is_first, updated_at=excluded.updated_at`,
		d.RID, d.Mode, d.ChapterID, d.DunID, d.Status, d.MaxDunID, b2i(d.IsFirst), s.unix())
	return err
}

// ClearDungeon stores the new campaign state and pays the clear reward (currencies by
// label, items by base id) in one transaction.
func (s *Store) ClearDungeon(d Drama, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO role_drama(rid,mode,chapter_id,dun_id,status,max_dun_id,is_first,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET mode=excluded.mode, chapter_id=excluded.chapter_id, dun_id=excluded.dun_id,
		status=excluded.status, max_dun_id=excluded.max_dun_id, is_first=excluded.is_first, updated_at=excluded.updated_at`,
		d.RID, d.Mode, d.ChapterID, d.DunID, d.Status, d.MaxDunID, b2i(d.IsFirst), s.unix()); err != nil {
		return err
	}
	if err := creditTx(tx, d.RID, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// HookSince returns when the role last took its hook income (unix seconds), or the
// role's creation time before the first time.
func (s *Store) HookSince(rid int64) (int64, error) {
	var since int64
	err := s.db.QueryRow(`SELECT since FROM drama_hook WHERE rid=?`, rid).Scan(&since)
	if err == nil {
		return since, nil
	}
	if !wrapIsNoRows(err) {
		return 0, err
	}
	err = s.db.QueryRow(`SELECT created_at FROM roles WHERE rid=?`, rid).Scan(&since)
	return since, wrapNoRows(err)
}

// ClaimHook pays the hook income and restarts the accumulation at `now`, in one
// transaction. It returns ErrConflict, changing nothing, when the role took it in
// the meantime (its `since` is no longer expectedSince).
func (s *Store) ClaimHook(rid, expectedSince, now int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cur := expectedSince
	if err := tx.QueryRow(`SELECT since FROM drama_hook WHERE rid=?`, rid).Scan(&cur); err != nil && !wrapIsNoRows(err) {
		return err
	} else if err == nil && cur != expectedSince {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO drama_hook(rid,since) VALUES(?,?) ON CONFLICT(rid) DO UPDATE SET since=excluded.since`, rid, now); err != nil {
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// DramaDaily is what a role used of its daily campaign actions.
type DramaDaily struct{ Quick, Sweep int64 }

func (s *Store) DramaDaily(rid, day int64) (DramaDaily, error) {
	var d DramaDaily
	err := s.db.QueryRow(`SELECT quick,sweep FROM drama_daily WHERE rid=? AND day=?`, rid, day).Scan(&d.Quick, &d.Sweep)
	if wrapIsNoRows(err) {
		return d, nil
	}
	return d, err
}

// DramaAction counts a quick battle (addQuick) or sweeps (addSweep) for the day,
// takes the cost and pays the reward, in one transaction. A counter may not pass
// its maximum (0 = no maximum); the result is ErrConflict, changing nothing, when
// it would or the role cannot pay.
func (s *Store) DramaAction(rid, day, addQuick, addSweep, maxQuick, maxSweep int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO drama_daily(rid,day) VALUES(?,?) ON CONFLICT DO NOTHING`, rid, day); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE drama_daily SET quick=quick+?, sweep=sweep+? WHERE rid=? AND day=?
		AND (?=0 OR quick+?<=?) AND (?=0 OR sweep+?<=?)`, addQuick, addSweep, rid, day, maxQuick, addQuick, maxQuick, maxSweep, addSweep, maxSweep)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// DramaMilestonesTaken lists the milestone reward ids the role has taken, ascending.
func (s *Store) DramaMilestonesTaken(rid int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM drama_milestones WHERE rid=? ORDER BY id`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TakeDramaMilestone marks the milestone taken and pays it, in one transaction.
// It returns ErrConflict, changing nothing, when it was taken before.
func (s *Store) TakeDramaMilestone(rid, id int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO drama_milestones(rid,id) VALUES(?,?)`, rid, id); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// DramaProgress is the furthest dungeon (max_dun_id) of every role of a server that has any, and how many roles the
// server has.
func (s *Store) DramaProgress(srvID string) (maxDuns []int64, roles int64, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM roles WHERE srv_id=?`, srvID).Scan(&roles); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT d.max_dun_id FROM role_drama d JOIN roles r ON r.rid=d.rid WHERE r.srv_id=?`, srvID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		maxDuns = append(maxDuns, id)
	}
	return maxDuns, roles, rows.Err()
}
