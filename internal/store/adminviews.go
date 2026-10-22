package store

import (
	"strconv"
	"strings"
)

// AccountRow is an account with how many roles it owns, for the admin list.
type AccountRow struct {
	Account
	RoleCount int `json:"role_count"`
}

// ListAccountsAdmin searches by account name or numeric id.
func (s *Store) ListAccountsAdmin(q string, limit, offset int) ([]AccountRow, int, error) {
	where, args := `account LIKE ? ESCAPE '\'`, []any{"%" + escapeLike(q) + "%"}
	if id, err := strconv.ParseInt(q, 10, 64); err == nil {
		where, args = `(account LIKE ? ESCAPE '\' OR id=?)`, append(args, id)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+accountCols+`,(SELECT COUNT(*) FROM roles r WHERE r.account_id=accounts.id)
		FROM accounts WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AccountRow
	for rows.Next() {
		var a AccountRow
		if err := rows.Scan(&a.ID, &a.Platform, &a.Account.Account, &a.PasswordHash, &a.OpenID, &a.UnionID, &a.Status, &a.BanReason, &a.CreatedAt, &a.LastLoginAt, &a.RoleCount); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// RoleRow is a role with the account that owns it.
type RoleRow struct {
	Role
	AccountName string `json:"account"`
}

// ListRolesAdmin searches by role name, account name or numeric role id.
func (s *Store) ListRolesAdmin(q, srvID string, limit, offset int) ([]RoleRow, int, error) {
	conds, args := []string{`(r.name LIKE ? ESCAPE '\' OR a.account LIKE ? ESCAPE '\')`}, []any{"%" + escapeLike(q) + "%", "%" + escapeLike(q) + "%"}
	if id, err := strconv.ParseInt(q, 10, 64); err == nil {
		conds[0], args = `(r.name LIKE ? ESCAPE '\' OR a.account LIKE ? ESCAPE '\' OR r.rid=?)`, append(args, id)
	}
	if srvID != "" {
		conds, args = append(conds, `r.srv_id=?`), append(args, srvID)
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM roles r JOIN accounts a ON a.id=r.account_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT r.rid,r.account_id,r.srv_id,r.name,r.lev,r.sex,r.career,r.face_id,r.status,r.created_at,r.last_login_at,a.account
		FROM roles r JOIN accounts a ON a.id=r.account_id WHERE `+where+` ORDER BY r.rid DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []RoleRow
	for rows.Next() {
		var r RoleRow
		if err := rows.Scan(&r.RID, &r.AccountID, &r.SrvID, &r.Name, &r.Lev, &r.Sex, &r.Career, &r.FaceID, &r.Status, &r.CreatedAt, &r.LastLoginAt, &r.AccountName); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// RoleIDsOfAccount lists every role an account owns (all servers).
func (s *Store) RoleIDsOfAccount(accountID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT rid FROM roles WHERE account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// escapeLike makes a user string literal inside a LIKE pattern (used with ESCAPE '\').
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
