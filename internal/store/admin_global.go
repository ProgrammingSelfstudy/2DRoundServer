package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// GlobalModule is a raw read for the owning gateway service under its lock.
// Administrative HTTP code must request it through the gateway, not call it.
func (s *Store) GlobalModule(srv, key string) (json.RawMessage, bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM global_state WHERE srv_id=? AND module=?`, srv, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(raw), true, nil
}
func (s *Store) GlobalModuleKeys(srv, prefix string) ([]string, error) {
	rows, err := s.db.Query(`SELECT module FROM global_state WHERE srv_id=? ORDER BY module`, srv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out, rows.Err()
}
