package admin

import (
	"errors"
	"net/http"
	"strconv"

	"2droundserver/internal/store"
)

func (s *Server) listTables(w http.ResponseWriter, r *http.Request, p *principal) {
	writeJSON(w, 200, map[string]any{"items": store.AdminTables()})
}
func (s *Server) listRoleTables(w http.ResponseWriter, r *http.Request, p *principal) {
	role, ok := s.moduleRole(w, r)
	if !ok {
		return
	}
	items := []map[string]any{}
	for _, t := range store.AdminTables() {
		if t.Scope != "role" {
			continue
		}
		n, err := s.Store.AdminTableCount("role", t.Name, role.RID)
		if err != nil {
			internalErr(w, s, err)
			return
		}
		items = append(items, map[string]any{"name": t.Name, "total": n})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) tableRows(scope string) handler {
	return func(w http.ResponseWriter, r *http.Request, p *principal) {
		var id any
		var err error
		switch scope {
		case "role":
			role, ok := s.moduleRole(w, r)
			if !ok {
				return
			}
			id = role.RID
		case "guild":
			n, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if e != nil || n <= 0 {
				fail(w, 404, "no such guild")
				return
			}
			_, err = s.Store.GuildByID(n)
			id = n
		case "server":
			id = r.PathValue("id")
			_, err = s.Store.Server(id.(string))
		}
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 404, "no such owner")
			return
		}
		if err != nil {
			internalErr(w, s, err)
			return
		}
		limit, offset := 50, 0
		for _, key := range []string{"limit", "offset"} {
			if values, ok := r.URL.Query()[key]; ok {
				if len(values) != 1 {
					fail(w, 422, "invalid pagination")
					return
				}
				n, e := strconv.Atoi(values[0])
				if e != nil || n < 0 || (key == "limit" && (n < 1 || n > 200)) {
					fail(w, 422, "invalid pagination")
					return
				}
				if key == "limit" {
					limit = n
				} else {
					offset = n
				}
			}
		}
		out, err := s.Store.AdminTableRows(scope, r.PathValue("name"), id, limit, offset)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 404, "no such table in this scope")
			return
		}
		if err != nil {
			internalErr(w, s, err)
			return
		}
		writeJSON(w, 200, out)
	}
}
