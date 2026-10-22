package store

import (
	"encoding/json"
)

type Admin struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Status       int    `json:"status"`
	LastLoginAt  int64  `json:"last_login_at"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

func (s *Store) CreateAdmin(username, hash, role string) (*Admin, error) {
	now := s.unix()
	r, err := s.db.Exec(`INSERT INTO admins(username,password_hash,role,created_at) VALUES(?,?,?,?)`, username, hash, role, now)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	return &Admin{ID: id, Username: username, PasswordHash: hash, Role: role, Status: 1, CreatedAt: now}, nil
}

func (s *Store) AdminByUsername(u string) (*Admin, error) {
	a := &Admin{}
	err := s.db.QueryRow(`SELECT id,username,password_hash,role,status,last_login_at,created_at FROM admins WHERE username=?`, u).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.Status, &a.LastLoginAt, &a.CreatedAt)
	return a, wrapNoRows(err)
}

func (s *Store) AdminByID(id int64) (*Admin, error) {
	a := &Admin{}
	err := s.db.QueryRow(`SELECT id,username,password_hash,role,status,last_login_at,created_at FROM admins WHERE id=?`, id).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.Status, &a.LastLoginAt, &a.CreatedAt)
	return a, wrapNoRows(err)
}

func (s *Store) ListAdmins() ([]Admin, error) {
	rows, err := s.db.Query(`SELECT id,username,role,status,last_login_at,created_at FROM admins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		var a Admin
		if err := rows.Scan(&a.ID, &a.Username, &a.Role, &a.Status, &a.LastLoginAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) TouchAdminLogin(id int64) error {
	_, err := s.db.Exec(`UPDATE admins SET last_login_at=? WHERE id=?`, s.unix(), id)
	return err
}

func (s *Store) SetAdminPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE admins SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) SetAdminStatus(id int64, status int) error {
	_, err := s.db.Exec(`UPDATE admins SET status=? WHERE id=?`, status, id)
	return err
}

type AuditLog struct {
	ID         int64  `json:"id"`
	AdminID    int64  `json:"admin_id"`
	AdminName  string `json:"admin_name"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	DetailJSON string `json:"detail"`
	IP         string `json:"ip"`
	CreatedAt  int64  `json:"created_at"`
}

func (s *Store) WriteAudit(adminID int64, adminName, action, target string, detail any, ip string) error {
	d := "{}"
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			d = string(b)
		}
	}
	_, err := s.db.Exec(`INSERT INTO audit_logs(admin_id,admin_name,action,target,detail_json,ip,created_at) VALUES(?,?,?,?,?,?,?)`,
		adminID, adminName, action, target, d, ip, s.unix())
	return err
}

func (s *Store) ListAudit(limit, offset int) ([]AuditLog, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id,admin_id,admin_name,action,target,detail_json,ip,created_at FROM audit_logs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.AdminID, &a.AdminName, &a.Action, &a.Target, &a.DetailJSON, &a.IP, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// UpdateAdmin changes the role and/or status; empty role and negative status leave a field alone.
func (s *Store) UpdateAdmin(id int64, role string, status int) error {
	if role != "" {
		if _, err := s.db.Exec(`UPDATE admins SET role=? WHERE id=?`, role, id); err != nil {
			return err
		}
	}
	if status >= 0 {
		if _, err := s.db.Exec(`UPDATE admins SET status=? WHERE id=?`, status, id); err != nil {
			return err
		}
	}
	return nil
}

// CountActiveSuperadmins is used to keep at least one working superadmin.
func (s *Store) CountActiveSuperadmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admins WHERE role='superadmin' AND status=1`).Scan(&n)
	return n, err
}

// Stats are the dashboard counters.
type Stats struct {
	Accounts, Roles, Servers, Mails, Notices int
}

func (s *Store) Stats() (Stats, error) {
	var st Stats
	for q, dst := range map[string]*int{
		`SELECT COUNT(*) FROM accounts`: &st.Accounts, `SELECT COUNT(*) FROM roles`: &st.Roles,
		`SELECT COUNT(*) FROM servers`: &st.Servers, `SELECT COUNT(*) FROM mails`: &st.Mails, `SELECT COUNT(*) FROM notices`: &st.Notices,
	} {
		if err := s.db.QueryRow(q).Scan(dst); err != nil {
			return st, err
		}
	}
	return st, nil
}
