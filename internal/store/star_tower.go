package store

import (
	"database/sql"
	"errors"
)

// StarTower is a role's progress in the star tower.
type StarTower struct{ RID, MaxTower, ClearedAt, Day, Used, Bought int64 }

// StarTowerOf reads it (the zero state for a role that never climbed).
func (s *Store) StarTowerOf(rid int64) (*StarTower, error) {
	t := &StarTower{RID: rid}
	err := s.db.QueryRow(`SELECT max_tower,cleared_at,day,used,bought FROM star_tower WHERE rid=?`, rid).Scan(&t.MaxTower, &t.ClearedAt, &t.Day, &t.Used, &t.Bought)
	if errors.Is(err, sql.ErrNoRows) {
		return t, nil
	}
	return t, err
}

// SaveStarTower writes it.
func (s *Store) SaveStarTower(t *StarTower) error {
	_, err := s.db.Exec(`INSERT INTO star_tower(rid,max_tower,cleared_at,day,used,bought) VALUES(?,?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET max_tower=excluded.max_tower,cleared_at=excluded.cleared_at,day=excluded.day,used=excluded.used,bought=excluded.bought`,
		t.RID, t.MaxTower, t.ClearedAt, t.Day, t.Used, t.Bought)
	return err
}

// TowerRow is a role and its highest floor.
type TowerRow struct{ RID, Tower int64 }

// TowerTop lists the roles of a server that climbed furthest, the earlier to arrive first among equals.
func (s *Store) TowerTop(srvID string, limit int) ([]TowerRow, error) {
	rows, err := s.db.Query(`SELECT t.rid,t.max_tower FROM star_tower t JOIN roles r ON r.rid=t.rid WHERE r.srv_id=? AND t.max_tower>0
		ORDER BY t.max_tower DESC, t.cleared_at, t.rid LIMIT ?`, srvID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TowerRow
	for rows.Next() {
		var r TowerRow
		if err := rows.Scan(&r.RID, &r.Tower); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TowerClaimed lists the milestone rewards a role took.
func (s *Store) TowerClaimed(rid int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM star_tower_claims WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ClaimTowerMilestone pays a milestone once together with marking it. ErrConflict when it was taken already.
func (s *Store) ClaimTowerMilestone(rid, id int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO star_tower_claims(rid,id) VALUES(?,?)`, rid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
