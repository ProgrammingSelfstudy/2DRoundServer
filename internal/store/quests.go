package store

// QuestRow is a role's progress on one daily quest.
type QuestRow struct {
	Value   int64
	Claimed bool
}

// QuestRows returns the role's rows of that day, by quest id.
func (s *Store) QuestRows(rid, day int64) (map[int64]QuestRow, error) {
	rows, err := s.db.Query(`SELECT quest_id,value,claimed FROM daily_quests WHERE rid=? AND day=?`, rid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]QuestRow{}
	for rows.Next() {
		var id int64
		var r QuestRow
		var c int
		if err := rows.Scan(&id, &r.Value, &c); err != nil {
			return nil, err
		}
		r.Claimed = c != 0
		out[id] = r
	}
	return out, rows.Err()
}

// AddQuestProgress adds n to the quest's value, never above max, and returns the new value.
func (s *Store) AddQuestProgress(rid, questID, day, n, max int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO daily_quests(rid,quest_id,day,value) VALUES(?,?,?,0) ON CONFLICT DO NOTHING`, rid, questID, day); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE daily_quests SET value=MIN(value+?, ?) WHERE rid=? AND quest_id=? AND day=? AND claimed=0`, n, max, rid, questID, day); err != nil {
		return 0, err
	}
	var v int64
	if err := tx.QueryRow(`SELECT value FROM daily_quests WHERE rid=? AND quest_id=? AND day=?`, rid, questID, day).Scan(&v); err != nil {
		return 0, err
	}
	return v, tx.Commit()
}

// ClaimQuest marks the quest claimed and pays the reward, in one transaction. It
// returns ErrConflict, changing nothing, when the quest is not finished
// (value < need) or was already claimed.
func (s *Store) ClaimQuest(rid, questID, day, need int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE daily_quests SET claimed=1 WHERE rid=? AND quest_id=? AND day=? AND claimed=0 AND value>=?`, rid, questID, day, need)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
