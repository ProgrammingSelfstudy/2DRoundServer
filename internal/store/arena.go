package store

// ArenaPlayer is a role's standing in the arena.
type ArenaPlayer struct {
	RID, Score, Season, Battles, Day, FreeUsed, Bought, ContWin, RefTime, LogSeen int64
}

// ArenaPlayerOf reads a role's arena standing, creating it with the starting score the first time.
func (s *Store) ArenaPlayerOf(rid, startScore, now int64) (*ArenaPlayer, error) {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO arena_players(rid,score,created_at) VALUES(?,?,?)`, rid, startScore, now); err != nil {
		return nil, err
	}
	var a ArenaPlayer
	err := s.db.QueryRow(`SELECT rid,score,season,battles,day,free_used,bought,cont_win,ref_time,log_seen FROM arena_players WHERE rid=?`, rid).
		Scan(&a.RID, &a.Score, &a.Season, &a.Battles, &a.Day, &a.FreeUsed, &a.Bought, &a.ContWin, &a.RefTime, &a.LogSeen)
	return &a, err
}

// SaveArenaPlayer writes the standing back.
func (s *Store) SaveArenaPlayer(a *ArenaPlayer) error {
	_, err := s.db.Exec(`UPDATE arena_players SET score=?,season=?,battles=?,day=?,free_used=?,bought=?,cont_win=?,ref_time=?,log_seen=? WHERE rid=?`,
		a.Score, a.Season, a.Battles, a.Day, a.FreeUsed, a.Bought, a.ContWin, a.RefTime, a.LogSeen, a.RID)
	return err
}

// ArenaRank is the place of a role among the arena players of its server (1 = best; the earlier to arrive wins a tie).
func (s *Store) ArenaRank(rid int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*)+1 FROM arena_players a JOIN roles r ON r.rid=a.rid
		WHERE r.srv_id=(SELECT srv_id FROM roles WHERE rid=?)
		AND (a.score > (SELECT score FROM arena_players WHERE rid=?)
		 OR (a.score = (SELECT score FROM arena_players WHERE rid=?) AND (a.created_at < (SELECT created_at FROM arena_players WHERE rid=?)
		  OR (a.created_at = (SELECT created_at FROM arena_players WHERE rid=?) AND a.rid < ?))))`, rid, rid, rid, rid, rid, rid).Scan(&n)
	return n, err
}

// ArenaRow is one line of the ranking.
type ArenaRow struct {
	RID   int64
	Score int64
}

// ArenaTop lists the best arena players of a server, best first.
func (s *Store) ArenaTop(srvID string, limit int) ([]ArenaRow, error) {
	rows, err := s.db.Query(`SELECT a.rid,a.score FROM arena_players a JOIN roles r ON r.rid=a.rid WHERE r.srv_id=? ORDER BY a.score DESC, a.created_at, a.rid LIMIT ?`, srvID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaRow
	for rows.Next() {
		var r ArenaRow
		if err := rows.Scan(&r.RID, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ArenaNear lists other arena players of the server whose score is closest to the given one.
func (s *Store) ArenaNear(srvID string, rid, score int64, limit int) ([]ArenaRow, error) {
	rows, err := s.db.Query(`SELECT a.rid,a.score FROM arena_players a JOIN roles r ON r.rid=a.rid WHERE r.srv_id=? AND a.rid<>?
		ORDER BY ABS(a.score-?), a.rid LIMIT ?`, srvID, rid, score, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaRow
	for rows.Next() {
		var r ArenaRow
		if err := rows.Scan(&r.RID, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ArenaSlot is one of the opponents on a role's list; Info is the game's JSON of who it is.
type ArenaSlot struct {
	Idx, OppRID, Status int64
	Info                string
}

// ArenaSlots lists a role's opponents.
func (s *Store) ArenaSlots(rid int64) ([]ArenaSlot, error) {
	rows, err := s.db.Query(`SELECT idx,opp_rid,status,info FROM arena_slots WHERE rid=? ORDER BY idx`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaSlot
	for rows.Next() {
		var a ArenaSlot
		if err := rows.Scan(&a.Idx, &a.OppRID, &a.Status, &a.Info); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetArenaSlots replaces a role's opponents.
func (s *Store) SetArenaSlots(rid int64, slots []ArenaSlot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM arena_slots WHERE rid=?`, rid); err != nil {
		return err
	}
	for _, a := range slots {
		if _, err := tx.Exec(`INSERT INTO arena_slots(rid,idx,opp_rid,status,info) VALUES(?,?,?,?,?)`, rid, a.Idx, a.OppRID, a.Status, a.Info); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkArenaSlot sets an opponent's status (1 = fought).
func (s *Store) MarkArenaSlot(rid, idx, status int64) error {
	_, err := s.db.Exec(`UPDATE arena_slots SET status=? WHERE rid=? AND idx=?`, status, rid, idx)
	return err
}

// ArenaClaimed lists the season chests (by challenge count) a role opened in a season.
func (s *Store) ArenaClaimed(rid, season int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT num FROM arena_claims WHERE rid=? AND season=? ORDER BY num`, rid, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ClaimArenaChest pays a season chest once together with marking it. ErrConflict when it was opened already.
func (s *Store) ClaimArenaChest(rid, season, num int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO arena_claims(rid,season,num) VALUES(?,?,?)`, rid, season, num)
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

// ArenaLog is one line of a role's fight log; Info is the game's JSON.
type ArenaLog struct {
	ID   int64
	Info string
	Time int64
}

// AddArenaLog appends a line, keeping the last 50 of the role.
func (s *Store) AddArenaLog(rid int64, info string, now int64) error {
	if _, err := s.db.Exec(`INSERT INTO arena_logs(rid,info,time) VALUES(?,?,?)`, rid, info, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM arena_logs WHERE rid=? AND id NOT IN (SELECT id FROM arena_logs WHERE rid=? ORDER BY id DESC LIMIT 50)`, rid, rid)
	return err
}

// ArenaLogs lists a role's log, newest first.
func (s *Store) ArenaLogs(rid int64) ([]ArenaLog, error) {
	rows, err := s.db.Query(`SELECT id,info,time FROM arena_logs WHERE rid=? ORDER BY id DESC`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaLog
	for rows.Next() {
		var l ArenaLog
		if err := rows.Scan(&l.ID, &l.Info, &l.Time); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
