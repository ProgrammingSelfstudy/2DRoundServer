package store

const (
	TowerReplayFastest = 1
	TowerReplayLowest  = 2
	TowerReplayOwn     = 3
)

type TowerReplayRecord struct {
	Kind, ReplayID, DurationMS, Power int64
	Meta                              string
}

// AddTowerReplay stores the script and updates the floor's records in the same transaction.
// Defeats and battles ended early remain viewable from their result, without setting a timed clear record.
func (s *Store) AddTowerReplay(r Replay, tower, durationMS, power int64, completedWin bool) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertReplay(tx, r)
	if err != nil {
		return 0, err
	}
	if completedWin {
		for _, kind := range []int64{TowerReplayFastest, TowerReplayLowest, TowerReplayOwn} {
			var ownerID int64
			if kind == TowerReplayOwn {
				ownerID = r.ARID
			}
			_, err := tx.Exec(`INSERT INTO star_tower_replay_records(srv_id,tower,kind,owner_id,replay_id,duration_ms,power) VALUES(?,?,?,?,?,?,?)
				ON CONFLICT(srv_id,tower,kind,owner_id) DO UPDATE SET replay_id=excluded.replay_id,duration_ms=excluded.duration_ms,power=excluded.power
				WHERE CASE WHEN excluded.kind=? THEN (excluded.power,excluded.duration_ms) < (power,duration_ms)
				ELSE (excluded.duration_ms,excluded.power) < (duration_ms,power) END`,
				r.SrvID, tower, kind, ownerID, id, durationMS, power, TowerReplayLowest)
			if err != nil {
				return 0, err
			}
		}
	}
	if err := pruneReplays(tx, true); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// TowerReplayRecords returns the floor's two server records and the viewing role's own record.
func (s *Store) TowerReplayRecords(srvID string, tower, rid int64) ([]TowerReplayRecord, error) {
	rows, err := s.db.Query(`SELECT t.kind,t.replay_id,t.duration_ms,t.power,r.meta
		FROM star_tower_replay_records t JOIN replays r ON r.id=t.replay_id
		WHERE t.srv_id=? AND t.tower=? AND t.owner_id IN (0,?) ORDER BY t.kind`, srvID, tower, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TowerReplayRecord
	for rows.Next() {
		var row TowerReplayRecord
		if err := rows.Scan(&row.Kind, &row.ReplayID, &row.DurationMS, &row.Power, &row.Meta); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
