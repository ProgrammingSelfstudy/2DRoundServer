package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type RoleModuleRow struct {
	Key  string
	Data json.RawMessage
}

// RoleModules reads existing rows only. It never initializes or rolls state.
func (s *Store) RoleModules(rid int64) ([]RoleModuleRow, error) {
	rows, err := s.db.Query(`SELECT module,data FROM module_state WHERE rid=? ORDER BY module`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleModuleRow{}
	for rows.Next() {
		var key, data string
		if err := rows.Scan(&key, &data); err != nil {
			return nil, err
		}
		out = append(out, RoleModuleRow{key, json.RawMessage(data)})
	}
	return out, rows.Err()
}

func (s *Store) RoleModule(rid int64, key string) (json.RawMessage, bool, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM module_state WHERE rid=? AND module=?`, rid, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return json.RawMessage(data), err == nil, err
}

// CommitRoleModuleReset is admin-only infrastructure: compare the exact stored
// before value, change one row and append its audit entry in a single transaction.
// A nil after value deletes the row. An audit failure rolls back the change.
func (s *Store) CommitRoleModuleReset(rid int64, key string, before, after json.RawMessage, audit AuditLog) error {
	if !json.Valid(before) || (after != nil && !json.Valid(after)) || !json.Valid([]byte(audit.DetailJSON)) || audit.Action != "role.module.reset" {
		return fmt.Errorf("invalid module reset payload or audit")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var result sql.Result
	if after == nil {
		result, err = tx.Exec(`DELETE FROM module_state WHERE rid=? AND module=? AND data=?`, rid, key, string(before))
	} else {
		result, err = tx.Exec(`UPDATE module_state SET data=? WHERE rid=? AND module=? AND data=?`, string(after), rid, key, string(before))
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO audit_logs(admin_id,admin_name,action,target,detail_json,ip,created_at) VALUES(?,?,?,?,?,?,?)`,
		audit.AdminID, audit.AdminName, audit.Action, audit.Target, audit.DetailJSON, audit.IP, s.unix()); err != nil {
		return err
	}
	return tx.Commit()
}
