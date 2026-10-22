package store

// OfflineChat is a private message waiting for its recipient.
type OfflineChat struct {
	FromRID int64
	Len     int
	Msg     string
	SentAt  int64
}

// AddOfflineChat keeps a message for a role that is not online. Only the newest
// keep messages per recipient are kept; older ones are dropped.
func (s *Store) AddOfflineChat(from, to int64, flag int, msg string, keep int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO chat_offline(from_rid,to_rid,len,msg,sent_at) VALUES(?,?,?,?,?)`, from, to, flag, msg, s.unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chat_offline WHERE to_rid=? AND id NOT IN (SELECT id FROM chat_offline WHERE to_rid=? ORDER BY id DESC LIMIT ?)`, to, to, keep); err != nil {
		return err
	}
	return tx.Commit()
}

// TakeOfflineChat returns the waiting messages of a role, oldest first, and
// deletes them: a message is handed over once.
func (s *Store) TakeOfflineChat(to int64) ([]OfflineChat, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT from_rid,len,msg,sent_at FROM chat_offline WHERE to_rid=? ORDER BY id`, to)
	if err != nil {
		return nil, err
	}
	var out []OfflineChat
	for rows.Next() {
		var c OfflineChat
		if err := rows.Scan(&c.FromRID, &c.Len, &c.Msg, &c.SentAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(out) > 0 {
		if _, err := tx.Exec(`DELETE FROM chat_offline WHERE to_rid=?`, to); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}
