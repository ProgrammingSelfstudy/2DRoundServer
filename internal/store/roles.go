package store

import "fmt"

type Role struct {
	RID         int64  `json:"rid"`
	AccountID   int64  `json:"account_id"`
	SrvID       string `json:"srv_id"`
	Name        string `json:"name"`
	Lev         int    `json:"lev"`
	Sex         int    `json:"sex"`
	Career      int    `json:"career"`
	FaceID      int    `json:"face_id"`
	Status      int    `json:"status"` // 1 normal, 0 banned
	CreatedAt   int64  `json:"created_at"`
	LastLoginAt int64  `json:"last_login_at"`
	Renamed     int    `json:"renamed"` // 1 once 10343 has been used
	// LastLogoutAt is when the role last disconnected (0 = never, or still online).
	LastLogoutAt int64 `json:"last_logout_at"`
	// VIPLev is the VIP level, 0 = none. Nothing raises it but the admin page.
	VIPLev int `json:"vip_lev"`
}

const roleCols = `rid,account_id,srv_id,name,lev,sex,career,face_id,status,created_at,last_login_at,renamed,last_logout_at,vip_lev`

func scanRole(sc interface{ Scan(...any) error }) (*Role, error) {
	var r Role
	err := sc.Scan(&r.RID, &r.AccountID, &r.SrvID, &r.Name, &r.Lev, &r.Sex, &r.Career, &r.FaceID, &r.Status, &r.CreatedAt, &r.LastLoginAt, &r.Renamed, &r.LastLogoutAt, &r.VIPLev)
	return &r, wrapNoRows(err)
}

// CreateRole returns ErrConflict when the name is already used on that server.
func (s *Store) CreateRole(accountID int64, srvID, name string, sex, career, faceID int) (*Role, error) {
	res, err := s.db.Exec(`INSERT INTO roles(account_id,srv_id,name,sex,career,face_id,created_at) VALUES(?,?,?,?,?,?,?)`,
		accountID, srvID, name, sex, career, faceID, s.unix())
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.RoleByID(id)
}

func (s *Store) RoleByID(rid int64) (*Role, error) {
	return scanRole(s.db.QueryRow(`SELECT `+roleCols+` FROM roles WHERE rid=?`, rid))
}

func (s *Store) RolesByAccount(accountID int64, srvID string) ([]Role, error) {
	rows, err := s.db.Query(`SELECT `+roleCols+` FROM roles WHERE account_id=? AND srv_id=? ORDER BY rid`, accountID, srvID)
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

// RenameRole sets a new name and, on the first rename only, the sex (the client
// tells the player the sex cannot be changed afterwards). It returns ErrConflict
// when the name is taken on that server and ErrNotFound for an unknown role.
func (s *Store) RenameRole(rid int64, name string, sex int) (*Role, error) {
	res, err := s.db.Exec(`UPDATE roles SET name=?, sex=CASE WHEN renamed=0 THEN ? ELSE sex END, renamed=1 WHERE rid=?`, name, sex, rid)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.RoleByID(rid)
}

func (s *Store) TouchRoleLogin(rid int64) error {
	_, err := s.db.Exec(`UPDATE roles SET last_login_at=? WHERE rid=?`, s.unix(), rid)
	return err
}

func (s *Store) TouchRoleLogout(rid int64) error {
	_, err := s.db.Exec(`UPDATE roles SET last_logout_at=? WHERE rid=?`, s.unix(), rid)
	return err
}

// SetRoleLevel sets a role's level (the caller checks the range). ErrNotFound for an unknown role.
func (s *Store) SetRoleLevel(rid int64, lev int) error {
	r, err := s.db.Exec(`UPDATE roles SET lev=? WHERE rid=?`, lev, rid)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRoleVIP sets a role's VIP level (the caller checks the range). ErrNotFound for an unknown role.
func (s *Store) SetRoleVIP(rid int64, vip int) error {
	r, err := s.db.Exec(`UPDATE roles SET vip_lev=? WHERE rid=?`, vip, rid)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetRoleStatus(rid int64, status int) error {
	r, err := s.db.Exec(`UPDATE roles SET status=? WHERE rid=?`, status, rid)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListRoles(q string, limit, offset int) ([]Role, int, error) {
	like := "%" + q + "%"
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM roles WHERE name LIKE ?`, like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+roleCols+` FROM roles WHERE name LIKE ? ORDER BY rid DESC LIMIT ? OFFSET ?`, like, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// CreateRoleAutoName creates a role whose name is prefix+rid, for clients that
// create the role first and ask the player for a name afterwards. The row is
// inserted under a temporary unique name and renamed in the same transaction.
func (s *Store) CreateRoleAutoName(accountID int64, srvID, prefix string, sex, career, faceID int) (*Role, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tmp := fmt.Sprintf("\x00tmp-%d-%d", accountID, s.now().UnixNano())
	res, err := tx.Exec(`INSERT INTO roles(account_id,srv_id,name,sex,career,face_id,created_at) VALUES(?,?,?,?,?,?,?)`,
		accountID, srvID, tmp, sex, career, faceID, s.unix())
	if err != nil {
		return nil, err
	}
	rid, _ := res.LastInsertId()
	name := fmt.Sprintf("%s%d", prefix, rid)
	for i := 0; ; i++ { // a player may already have taken this exact name
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s_%d", name, i)
		}
		if _, err = tx.Exec(`UPDATE roles SET name=? WHERE rid=?`, cand, rid); err == nil {
			break
		}
		if !isUnique(err) || i > 20 {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.RoleByID(rid)
}

// MarkStaleSessionsOffline sets the logout time of every role that has logged in
// after its last recorded logout: those are sessions that ended without the gateway
// seeing it (it was killed or crashed). Their real end is not known; now is the
// latest it can have been. It returns how many roles were fixed.
func (s *Store) MarkStaleSessionsOffline() (int64, error) {
	res, err := s.db.Exec(`UPDATE roles SET last_logout_at=? WHERE last_login_at > last_logout_at`, s.unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
