package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type AdminTableInfo struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Group   string   `json:"group"`
	Scope   string   `json:"scope"`
	Owner   string   `json:"owner"`
	Columns []string `json:"columns"`
}
type adminTable struct {
	AdminTableInfo
	from, where, selectSQL, order string
}
type AdminTablePage struct {
	Rows   []map[string]any `json:"rows"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

func AdminTables() []AdminTableInfo {
	out := make([]AdminTableInfo, 0, len(adminTableRegistry))
	for _, t := range adminTableRegistry {
		v := t.AdminTableInfo
		v.Columns = append([]string(nil), v.Columns...)
		out = append(out, v)
	}
	return out
}
func AdminTableExclusions() map[string]string {
	out := map[string]string{}
	for k, v := range adminTableExclusions {
		out[k] = v
	}
	return out
}
func findAdminTable(scope, name string) (adminTable, bool) {
	for _, t := range adminTableRegistry {
		if t.Scope == scope && t.Name == name {
			return t, true
		}
	}
	return adminTable{}, false
}
func tableArgs(t adminTable, id any) []any {
	a := []any{}
	for i := 0; i < strings.Count(t.where, "?"); i++ {
		a = append(a, id)
	}
	return a
}

// AdminTableCount only accepts registered names and bound ownership values.
func (s *Store) AdminTableCount(scope, name string, id any) (int64, error) {
	t, ok := findAdminTable(scope, name)
	if !ok {
		return 0, ErrNotFound
	}
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM "+t.from+" WHERE "+t.where, tableArgs(t, id)...).Scan(&n)
	return n, err
}
func (s *Store) AdminTableRows(scope, name string, id any, limit, offset int) (AdminTablePage, error) {
	out := AdminTablePage{Rows: []map[string]any{}, Limit: limit, Offset: offset}
	t, ok := findAdminTable(scope, name)
	if !ok {
		return out, ErrNotFound
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return out, fmt.Errorf("invalid pagination")
	}
	// A read transaction keeps count and page on the same SQLite snapshot.
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	args := tableArgs(t, id)
	if err = tx.QueryRow("SELECT COUNT(*) FROM "+t.from+" WHERE "+t.where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query("SELECT "+t.selectSQL+" FROM "+t.from+" WHERE "+t.where+" ORDER BY "+t.order+" LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]any, len(t.Columns))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return out, err
		}
		row := map[string]any{}
		truncated := []string{}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if text, ok := v.(string); ok && len(text) > 4096 {
				text = text[:4096]
				for !utf8.ValidString(text) {
					text = text[:len(text)-1]
				}
				v = text
				truncated = append(truncated, t.Columns[i])
			}
			row[t.Columns[i]] = v
		}
		row["truncated"] = truncated
		out.Rows = append(out.Rows, row)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
