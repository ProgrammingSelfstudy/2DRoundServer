package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

type Account struct {
	ID           int64  `json:"id"`
	Platform     string `json:"platform"`
	Account      string `json:"account"`
	PasswordHash string `json:"-"`
	OpenID       string `json:"open_id"`
	UnionID      string `json:"union_id"`
	Status       int    `json:"status"`
	BanReason    string `json:"ban_reason"`
	CreatedAt    int64  `json:"created_at"`
	LastLoginAt  int64  `json:"last_login_at"`
}

const accountCols = `id,platform,account,password_hash,open_id,union_id,status,ban_reason,created_at,last_login_at`

func scanAccount(sc interface{ Scan(...any) error }) (*Account, error) {
	a := &Account{}
	err := sc.Scan(&a.ID, &a.Platform, &a.Account, &a.PasswordHash, &a.OpenID, &a.UnionID, &a.Status, &a.BanReason, &a.CreatedAt, &a.LastLoginAt)
	return a, wrapNoRows(err)
}

func (s *Store) CreateAccount(platform, account, passHash, openID string) (*Account, error) {
	r, err := s.db.Exec(`INSERT INTO accounts(platform,account,password_hash,open_id,created_at) VALUES(?,?,?,?,?)`,
		platform, account, passHash, openID, s.unix())
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	return s.AccountByID(id)
}

func (s *Store) AccountByID(id int64) (*Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountCols+` FROM accounts WHERE id=?`, id))
}

func (s *Store) AccountByName(platform, account string) (*Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountCols+` FROM accounts WHERE platform=? AND account=?`, platform, account))
}

// FindOrCreateAccount is used by the login service when auto_register is on.
func (s *Store) FindOrCreateAccount(platform, account string) (*Account, error) {
	a, err := s.AccountByName(platform, account)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	a, err = s.CreateAccount(platform, account, "", "")
	if errors.Is(err, ErrConflict) { // lost a race
		return s.AccountByName(platform, account)
	}
	return a, err
}

func (s *Store) ListAccounts(q string, limit, offset int) ([]Account, int, error) {
	like := "%" + q + "%"
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE account LIKE ?`, like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+accountCols+` FROM accounts WHERE account LIKE ? ORDER BY id DESC LIMIT ? OFFSET ?`, like, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func (s *Store) SetAccountStatus(id int64, status int, reason string) error {
	r, err := s.db.Exec(`UPDATE accounts SET status=?, ban_reason=? WHERE id=?`, status, reason, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if status == 0 { // a banned account loses its outstanding tokens
		_, err = s.db.Exec(`DELETE FROM login_tokens WHERE account_id=?`, id)
	}
	return err
}

func (s *Store) TouchAccountLogin(id int64) error {
	_, err := s.db.Exec(`UPDATE accounts SET last_login_at=? WHERE id=?`, s.unix(), id)
	return err
}

// IssueToken creates a random login token valid for ttlSeconds.
func (s *Store) IssueToken(accountID int64, ttlSeconds int64) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	now := s.unix()
	s.db.Exec(`DELETE FROM login_tokens WHERE expires_at < ?`, now)
	_, err := s.db.Exec(`INSERT INTO login_tokens(token,account_id,expires_at) VALUES(?,?,?)`, tok, accountID, now+ttlSeconds)
	return tok, err
}

// AccountByToken returns the account for a valid, unexpired token.
func (s *Store) AccountByToken(token string) (*Account, error) {
	var id int64
	var exp int64
	err := s.db.QueryRow(`SELECT account_id, expires_at FROM login_tokens WHERE token=?`, token).Scan(&id, &exp)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && exp < s.unix()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.AccountByID(id)
}

func (s *Store) SetAccountPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE accounts SET password_hash=? WHERE id=?`, hash, id)
	return err
}
