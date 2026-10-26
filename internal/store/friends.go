package store

import (
	"errors"
	"strings"
)

// ErrFriendsFull is returned when either side of a new friendship has reached the limit.
var ErrFriendsFull = errors.New("store: friend list full")

// qualified prefixes every column of roleCols with the table alias r.
var qualifiedRoleCols = "r." + strings.ReplaceAll(roleCols, ",", ",r.")

func (s *Store) scanRoles(query string, args ...any) ([]Role, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Friends lists a role's friends, oldest friendship first.
func (s *Store) Friends(rid int64) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM friends f JOIN roles r ON r.rid=f.friend_rid WHERE f.rid=? ORDER BY f.created_at, r.rid`, rid)
}

func (s *Store) AreFriends(a, b int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM friends WHERE rid=? AND friend_rid=?`, a, b).Scan(&n)
	return n > 0, err
}

func (s *Store) CountFriends(rid int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM friends WHERE rid=?`, rid).Scan(&n)
	return n, err
}

// RequestFriend records a request from -> to. ErrConflict when one is already
// pending in that direction.
func (s *Store) RequestFriend(from, to int64) error {
	_, err := s.db.Exec(`INSERT INTO friend_requests(from_rid,to_rid,created_at) VALUES(?,?,?)`, from, to, s.unix())
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) HasFriendRequest(from, to int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM friend_requests WHERE from_rid=? AND to_rid=?`, from, to).Scan(&n)
	return n > 0, err
}

// FriendRequestsTo lists who asked to be this role's friend, oldest first.
func (s *Store) FriendRequestsTo(rid int64) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM friend_requests q JOIN roles r ON r.rid=q.from_rid WHERE q.to_rid=? ORDER BY q.created_at, r.rid`, rid)
}

// DeclineFriendRequest drops the request from -> to; false when there was none.
func (s *Store) DeclineFriendRequest(from, to int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM friend_requests WHERE from_rid=? AND to_rid=?`, from, to)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeclineAllFriendRequests drops every pending request to rid.
func (s *Store) DeclineAllFriendRequests(rid int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM friend_requests WHERE to_rid=?`, rid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AcceptFriendRequest turns the pending request from -> to into a friendship in
// one transaction. ErrNotFound: no such request. ErrFriendsFull: either side
// already has max friends (the request stays).
func (s *Store) AcceptFriendRequest(from, to int64, max int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM friend_requests WHERE from_rid=? AND to_rid=?`, from, to)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// a request the other way round is now pointless
	if _, err := tx.Exec(`DELETE FROM friend_requests WHERE from_rid=? AND to_rid=?`, to, from); err != nil {
		return err
	}
	for _, rid := range []int64{from, to} {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM friends WHERE rid=?`, rid).Scan(&n); err != nil {
			return err
		}
		if n >= max {
			return ErrFriendsFull
		}
	}
	now := s.unix()
	for _, p := range [][2]int64{{from, to}, {to, from}} {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO friends(rid,friend_rid,created_at) VALUES(?,?,?)`, p[0], p[1], now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteFriend ends the friendship in both directions; false when there was none.
func (s *Store) DeleteFriend(a, b int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM friends WHERE (rid=? AND friend_rid=?) OR (rid=? AND friend_rid=?)`, a, b, b, a)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SearchRolesByName finds roles on a server whose name contains q (at most limit).
func (s *Store) SearchRolesByName(srvID, q string, limit int) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM roles r WHERE r.srv_id=? AND r.status=1 AND r.name LIKE ? ESCAPE '\' ORDER BY r.lev DESC, r.rid LIMIT ?`,
		srvID, "%"+escapeLike(q)+"%", limit)
}

// RecommendFriends lists roles on a server who are neither rid itself, nor its
// friends, nor already asked by it, nor on a black list with it, the most recently active first.
func (s *Store) RecommendFriends(srvID string, rid int64, limit int) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM roles r WHERE r.srv_id=? AND r.status=1 AND r.rid<>?
		AND NOT EXISTS (SELECT 1 FROM friends f WHERE f.rid=? AND f.friend_rid=r.rid)
		AND NOT EXISTS (SELECT 1 FROM friend_requests q WHERE q.from_rid=? AND q.to_rid=r.rid)
		AND NOT EXISTS (SELECT 1 FROM friend_blacklist b WHERE (b.rid=? AND b.black_rid=r.rid) OR (b.rid=r.rid AND b.black_rid=?))
		ORDER BY r.last_login_at DESC, r.rid DESC LIMIT ?`, srvID, rid, rid, rid, rid, rid, limit)
}

// Blacklist lists the roles rid put on its black list, oldest first.
func (s *Store) Blacklist(rid int64) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM friend_blacklist b JOIN roles r ON r.rid=b.black_rid WHERE b.rid=? ORDER BY b.created_at, r.rid`, rid)
}

// IsBlacklisted says whether rid has other on its black list.
func (s *Store) IsBlacklisted(rid, other int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM friend_blacklist WHERE rid=? AND black_rid=?`, rid, other).Scan(&n)
	return n > 0, err
}

// AddBlack puts other on rid's black list. The friendship (both ways) and the pending friend requests between
// the two end with it; wasFriend says there was a friendship. ErrConflict: already listed; ErrFriendsFull: the
// list holds max already.
func (s *Store) AddBlack(rid, other int64, max int) (wasFriend bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM friend_blacklist WHERE rid=?`, rid).Scan(&n); err != nil {
		return false, err
	}
	if n >= max {
		return false, ErrFriendsFull
	}
	if _, err := tx.Exec(`INSERT INTO friend_blacklist(rid,black_rid,created_at) VALUES(?,?,?)`, rid, other, s.unix()); err != nil {
		if isUnique(err) {
			return false, ErrConflict
		}
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM friends WHERE (rid=? AND friend_rid=?) OR (rid=? AND friend_rid=?)`, rid, other, other, rid)
	if err != nil {
		return false, err
	}
	gone, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM friend_requests WHERE (from_rid=? AND to_rid=?) OR (from_rid=? AND to_rid=?)`, rid, other, other, rid); err != nil {
		return false, err
	}
	return gone > 0, tx.Commit()
}

// RemoveBlack takes other off rid's black list; false when it was not on it.
func (s *Store) RemoveBlack(rid, other int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM friend_blacklist WHERE rid=? AND black_rid=?`, rid, other)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
