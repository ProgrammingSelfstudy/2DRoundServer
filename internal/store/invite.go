package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
)

// ErrInviteFull is returned when an inviter already has the most invitees allowed.
var ErrInviteFull = errors.New("store: inviter has the most invitees allowed")

const (
	inviteCodeMin = 100000
	inviteCodeMax = 999999
)

// InviteCode returns the role's invite code, making one (six random digits,
// unique) the first time.
func (s *Store) InviteCode(rid int64) (int64, error) {
	var code int64
	err := s.db.QueryRow(`SELECT code FROM invite_codes WHERE rid=?`, rid).Scan(&code)
	if err == nil {
		return code, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	for i := 0; i < 50; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(inviteCodeMax-inviteCodeMin+1))
		if err != nil {
			return 0, err
		}
		code = n.Int64() + inviteCodeMin
		_, err = s.db.Exec(`INSERT INTO invite_codes(rid,code) VALUES(?,?)`, rid, code)
		if err == nil {
			return code, nil
		}
		if !isUnique(err) {
			return 0, err
		}
		// the code was taken, or another request made this role's code meanwhile
		if e := s.db.QueryRow(`SELECT code FROM invite_codes WHERE rid=?`, rid).Scan(&code); e == nil {
			return code, nil
		}
	}
	return 0, errors.New("store: no free invite code")
}

// RoleByInviteCode finds the role that owns a code.
func (s *Store) RoleByInviteCode(code int64) (*Role, error) {
	var rid int64
	if err := s.db.QueryRow(`SELECT rid FROM invite_codes WHERE code=?`, code).Scan(&rid); err != nil {
		return nil, wrapNoRows(err)
	}
	return s.RoleByID(rid)
}

// BindInvite records that invitee entered inviter's code. ErrConflict: the
// invitee has already entered a code. ErrInviteFull: the inviter has max
// invitees already.
func (s *Store) BindInvite(invitee, inviter int64, max int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM invite_binds WHERE inviter_rid=?`, inviter).Scan(&n); err != nil {
		return err
	}
	if n >= max {
		return ErrInviteFull
	}
	if _, err := tx.Exec(`INSERT INTO invite_binds(invitee_rid,inviter_rid,created_at) VALUES(?,?,?)`, invitee, inviter, s.unix()); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	return tx.Commit()
}

// HasInviter reports whether the role has entered a code.
func (s *Store) HasInviter(invitee int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM invite_binds WHERE invitee_rid=?`, invitee).Scan(&n)
	return n > 0, err
}

// Invitees lists the roles that entered the inviter's code, oldest first.
func (s *Store) Invitees(inviter int64) ([]Role, error) {
	return s.scanRoles(`SELECT `+qualifiedRoleCols+` FROM invite_binds b JOIN roles r ON r.rid=b.invitee_rid WHERE b.inviter_rid=? ORDER BY b.created_at, r.rid`, inviter)
}

// InviteClaims returns how many times each reward step was claimed (task id -> times).
func (s *Store) InviteClaims(rid int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT task_id,times FROM invite_claims WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ClaimInvite counts one more claim of a step and pays its reward in one
// transaction. ErrConflict: the step was already claimed `allowed` times, which
// is how many the caller worked out the role may claim.
func (s *Store) ClaimInvite(rid, taskID, allowed int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var had int64
	switch err := tx.QueryRow(`SELECT times FROM invite_claims WHERE rid=? AND task_id=?`, rid, taskID).Scan(&had); {
	case err == sql.ErrNoRows:
	case err != nil:
		return err
	}
	if had >= allowed {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO invite_claims(rid,task_id,times) VALUES(?,?,1) ON CONFLICT(rid,task_id) DO UPDATE SET times=times+1`, rid, taskID); err != nil {
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
