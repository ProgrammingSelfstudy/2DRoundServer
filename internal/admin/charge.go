package admin

import (
	"net/http"
	"strconv"

	"2droundserver/internal/game"
)

func (s *Server) roleCharge(w http.ResponseWriter, r *http.Request, p *principal) {
	role, ok := s.moduleRole(w, r)
	if !ok {
		return
	}
	modules := []map[string]any{}
	now := s.Now()
	for _, key := range game.AdminChargeKeys() {
		m, ok := game.FindAdminModule("role", key)
		if !ok || m.View == nil {
			fail(w, 500, "charge module registry incomplete")
			return
		}
		raw, exists, err := s.Store.RoleModule(role.RID, key)
		if err != nil {
			internalErr(w, s, err)
			return
		}
		view, err := m.View(s.Store, strconv.FormatInt(role.RID, 10), key, now)
		if err != nil {
			internalErr(w, s, err)
			return
		}
		modules = append(modules, map[string]any{"key": key, "module": describeModule(m), "exists": exists, "data": raw, "view": view})
	}
	rc, err := s.Store.RoleCharge(role.RID)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	limit, offset := page(r)
	orders, total, err := s.Store.RolePayOrders(role.RID, limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"rid": role.RID, "charge": rc, "modules": modules, "orders": map[string]any{"items": nonNil(orders), "total": total, "limit": limit, "offset": offset}})
}
