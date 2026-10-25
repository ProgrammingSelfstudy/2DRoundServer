package store

import (
	"database/sql"
	"errors"
)

// ToggleFollow makes rid follow target, or stop following when it did; it returns whether it follows now.
func (s *Store) ToggleFollow(rid, target, now int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM role_follows WHERE rid=? AND target_rid=?`, rid, target)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = s.db.Exec(`INSERT INTO role_follows(rid,target_rid,created_at) VALUES(?,?,?)`, rid, target, now)
	return err == nil, err
}

// Follows says whether rid follows target.
func (s *Store) Follows(rid, target int64) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM role_follows WHERE rid=? AND target_rid=?`, rid, target).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FansOf is how many roles follow target.
func (s *Store) FansOf(target int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM role_follows WHERE target_rid=?`, target).Scan(&n)
	return n, err
}

// FanRow is a role and its number of followers.
type FanRow struct{ RID, Fans int64 }

// FansTop lists the most followed roles of a server.
func (s *Store) FansTop(srvID string, limit int) ([]FanRow, error) {
	rows, err := s.db.Query(`SELECT f.target_rid, COUNT(*) AS n FROM role_follows f JOIN roles r ON r.rid=f.target_rid WHERE r.srv_id=?
		GROUP BY f.target_rid ORDER BY n DESC, f.target_rid LIMIT ?`, srvID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FanRow
	for rows.Next() {
		var r FanRow
		if err := rows.Scan(&r.RID, &r.Fans); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FansRank is the place of a role in FansTop (0 = nobody follows it).
func (s *Store) FansRank(srvID string, rid int64) (int64, error) {
	n, err := s.FansOf(rid)
	if err != nil || n == 0 {
		return 0, err
	}
	var rank int64
	err = s.db.QueryRow(`SELECT COUNT(*)+1 FROM (SELECT f.target_rid AS t, COUNT(*) AS n FROM role_follows f JOIN roles r ON r.rid=f.target_rid WHERE r.srv_id=? GROUP BY f.target_rid)
		WHERE n > ? OR (n = ? AND t < ?)`, srvID, n, n, rid).Scan(&rank)
	return rank, err
}

// BBS is one line of a message board; Type 1 is a message, 2 a reply (to ReplyName).
type BBS struct {
	ID, OwnerRID, AuthorRID, Type, Time int64
	ReplyName, Msg                      string
}

// AddBBS writes a line on a board, dropping the oldest ones past max.
func (s *Store) AddBBS(b BBS, max int) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO room_bbs(owner_rid,author_rid,type,reply_name,msg,time) VALUES(?,?,?,?,?,?)`, b.OwnerRID, b.AuthorRID, b.Type, b.ReplyName, b.Msg, b.Time)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	_, err = s.db.Exec(`DELETE FROM room_bbs WHERE owner_rid=? AND bbs_id NOT IN (SELECT bbs_id FROM room_bbs WHERE owner_rid=? ORDER BY bbs_id DESC LIMIT ?)`, b.OwnerRID, b.OwnerRID, max)
	return id, err
}

// BBSByID reads one line or returns ErrNotFound.
func (s *Store) BBSByID(id int64) (*BBS, error) {
	var b BBS
	err := s.db.QueryRow(`SELECT bbs_id,owner_rid,author_rid,type,time,reply_name,msg FROM room_bbs WHERE bbs_id=?`, id).
		Scan(&b.ID, &b.OwnerRID, &b.AuthorRID, &b.Type, &b.Time, &b.ReplyName, &b.Msg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &b, err
}

// BBSList lists a board, the newest first, from offset start.
func (s *Store) BBSList(owner int64, start, num int) ([]BBS, int64, error) {
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM room_bbs WHERE owner_rid=?`, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT bbs_id,owner_rid,author_rid,type,time,reply_name,msg FROM room_bbs WHERE owner_rid=? ORDER BY bbs_id DESC LIMIT ? OFFSET ?`, owner, num, start)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []BBS
	for rows.Next() {
		var b BBS
		if err := rows.Scan(&b.ID, &b.OwnerRID, &b.AuthorRID, &b.Type, &b.Time, &b.ReplyName, &b.Msg); err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

// DeleteBBS removes a line.
func (s *Store) DeleteBBS(id int64) error {
	_, err := s.db.Exec(`DELETE FROM room_bbs WHERE bbs_id=?`, id)
	return err
}

// LastBBSTime is when a role last wrote on any board (0 = never).
func (s *Store) LastBBSTime(author int64) (int64, error) {
	var t sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(time) FROM room_bbs WHERE author_rid=?`, author).Scan(&t)
	return t.Int64, err
}

// Badge is an honor badge a role owns.
type Badge struct{ ID, Time int64 }

// BadgesOf lists the badges a role owns, and the ones it wears (position -> badge).
func (s *Store) BadgesOf(rid int64) ([]Badge, map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT badge_id,time FROM role_badges WHERE rid=? ORDER BY badge_id`, rid)
	if err != nil {
		return nil, nil, err
	}
	var owned []Badge
	for rows.Next() {
		var b Badge
		if err := rows.Scan(&b.ID, &b.Time); err != nil {
			rows.Close()
			return nil, nil, err
		}
		owned = append(owned, b)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	rows, err = s.db.Query(`SELECT pos,badge_id FROM role_badge_use WHERE rid=?`, rid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	worn := map[int64]int64{}
	for rows.Next() {
		var pos, id int64
		if err := rows.Scan(&pos, &id); err != nil {
			return nil, nil, err
		}
		worn[pos] = id
	}
	return owned, worn, rows.Err()
}

// GrantBadge gives a role a badge (a no-op when it has it).
func (s *Store) GrantBadge(rid, id, now int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO role_badges(rid,badge_id,time) VALUES(?,?,?)`, rid, id, now)
	return err
}

// WearBadge puts a badge in a position (id 0 empties it). ErrConflict when the role does not own the badge.
func (s *Store) WearBadge(rid, pos, id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if id == 0 {
		if _, err := tx.Exec(`DELETE FROM role_badge_use WHERE rid=? AND pos=?`, rid, pos); err != nil {
			return err
		}
		return tx.Commit()
	}
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM role_badges WHERE rid=? AND badge_id=?`, rid, id).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM role_badge_use WHERE rid=? AND badge_id=?`, rid, id); err != nil { // a badge is worn once
		return err
	}
	if _, err := tx.Exec(`INSERT INTO role_badge_use(rid,pos,badge_id) VALUES(?,?,?) ON CONFLICT(rid,pos) DO UPDATE SET badge_id=excluded.badge_id`, rid, pos, id); err != nil {
		return err
	}
	return tx.Commit()
}
