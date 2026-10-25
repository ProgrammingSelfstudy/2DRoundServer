package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// LoadState reads the JSON document a module keeps for a role into v. It reports false (and leaves v
// alone) when the role has none yet.
func (s *Store) LoadState(rid int64, module string, v any) (bool, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM module_state WHERE rid=? AND module=?`, rid, module).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(data), v)
}

// SaveState writes a module's document of a role.
func (s *Store) SaveState(rid int64, module string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO module_state(rid,module,data) VALUES(?,?,?) ON CONFLICT(rid,module) DO UPDATE SET data=excluded.data`, rid, module, string(b))
	return err
}

// LoadGlobal reads the JSON document a module keeps for a server.
func (s *Store) LoadGlobal(srvID, module string, v any) (bool, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM global_state WHERE srv_id=? AND module=?`, srvID, module).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(data), v)
}

// SaveGlobal writes a module's document of a server.
func (s *Store) SaveGlobal(srvID, module string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO global_state(srv_id,module,data) VALUES(?,?,?) ON CONFLICT(srv_id,module) DO UPDATE SET data=excluded.data`, srvID, module, string(b))
	return err
}
