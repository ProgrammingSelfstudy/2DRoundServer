package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

type moduleInfo struct {
	Key      string            `json:"key"`
	Title    string            `json:"title"`
	Window   string            `json:"window"`
	Scope    string            `json:"scope"`
	HasView  bool              `json:"has_view"`
	HasRank  bool              `json:"has_rank"`
	Editable bool              `json:"editable"`
	Resets   []moduleResetInfo `json:"resets"`
}

type moduleResetInfo struct {
	Item  string `json:"item"`
	Title string `json:"title"`
}

func describeModule(m game.AdminModule) moduleInfo {
	resets := make([]moduleResetInfo, 0, len(m.Resets))
	for _, r := range m.Resets {
		resets = append(resets, moduleResetInfo{r.Item, r.Title})
	}
	return moduleInfo{m.Key, m.Title, m.Window, m.Scope, m.View != nil, m.Rank != nil, m.Editable, resets}
}

func (s *Server) listModules(w http.ResponseWriter, r *http.Request, p *principal) {
	items := []moduleInfo{}
	for _, m := range game.AdminModules() {
		items = append(items, describeModule(m))
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) moduleRole(w http.ResponseWriter, r *http.Request) (*store.Role, bool) {
	rid, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return nil, false
	}
	if err != nil {
		internalErr(w, s, err)
		return nil, false
	}
	return role, true
}

func (s *Server) listRoleModules(w http.ResponseWriter, r *http.Request, p *principal) {
	role, ok := s.moduleRole(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.RoleModules(role.RID)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	type row struct {
		Key        string      `json:"key"`
		Registered bool        `json:"registered"`
		Bytes      int         `json:"bytes"`
		Module     *moduleInfo `json:"module"`
	}
	items := []row{}
	for _, value := range rows {
		item := row{Key: value.Key, Bytes: len(value.Data)}
		if m, exists := game.FindAdminModule("role", value.Key); exists {
			info := describeModule(m)
			item.Registered, item.Module = true, &info
		}
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) roleModule(w http.ResponseWriter, r *http.Request, p *principal) {
	role, ok := s.moduleRole(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	m, exists := game.FindAdminModule("role", key)
	if !exists {
		fail(w, 404, "no such role module")
		return
	}
	data, exists, err := s.Store.RoleModule(role.RID, key)
	if err != nil || (exists && !json.Valid(data)) {
		if err == nil {
			err = errors.New("stored role module is not valid JSON")
		}
		internalErr(w, s, err)
		return
	}
	out := map[string]any{"key": key, "module": describeModule(m), "exists": exists, "data": data}
	if m.View != nil {
		view, err := m.View(s.Store, strconv.FormatInt(role.RID, 10), key, s.Now())
		if err != nil {
			internalErr(w, s, err)
			return
		}
		out["view"] = view
	}
	writeJSON(w, 200, out)
}

func (s *Server) resetRoleModule(w http.ResponseWriter, r *http.Request, p *principal) {
	role, ok := s.moduleRole(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	m, exists := game.FindAdminModule("role", key)
	if !exists {
		fail(w, 404, "no such role module")
		return
	}
	s.resetModule(w, r, p, role, key, m)
}

// resetModule accepts a resolved descriptor so the mechanism can be tested with
// isolated in-memory reset functions without enabling any production whitelist.
func (s *Server) resetModule(w http.ResponseWriter, r *http.Request, p *principal, role *store.Role, key string, m game.AdminModule) {
	var body struct{ Item, Reason string }
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > 200 || strings.IndexFunc(body.Reason, unicode.IsControl) >= 0 || body.Item == "" {
		fail(w, 422, "item is required and reason must be 1 to 200 characters without control characters")
		return
	}
	// Missing/null/malformed online values are unknown, never an offline answer.
	var online struct {
		Online *bool `json:"online"`
	}
	if err := s.gateway("GET", "/online?rid="+strconv.FormatInt(role.RID, 10), &online); err != nil || online.Online == nil {
		fail(w, 502, "gateway unavailable or online status unknown; no data changed")
		return
	}
	if *online.Online {
		fail(w, 409, "role is online; kick it offline first")
		return
	}
	// Share the payment-hook lock before reading before or the current paid base.
	// The gateway check stays first; compare-before/audit transaction stays last.
	unlock := s.Store.LockRoleCharge(role.RID)
	defer unlock()
	var allowed *game.AdminReset
	for i := range m.Resets {
		if m.Resets[i].Item == body.Item {
			allowed = &m.Resets[i]
			break
		}
	}
	if allowed == nil || (body.Item != "all" && allowed.Apply == nil) || (body.Item == "all" && allowed.Apply != nil) {
		fail(w, 422, "reset item is not in the module whitelist")
		return
	}
	before, exists, err := s.Store.RoleModule(role.RID, key)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if !exists {
		fail(w, 404, "role has no saved module state")
		return
	}
	var after json.RawMessage
	if body.Item != "all" {
		state := m.New()
		decoder := json.NewDecoder(bytes.NewReader(before))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(state); err != nil {
			internalErr(w, s, err)
			return
		}
		if err := allowed.Apply(s.Store, role.RID, state, s.Now()); err != nil {
			fail(w, 422, err.Error())
			return
		}
		after, err = json.Marshal(state)
		if err != nil {
			internalErr(w, s, err)
			return
		}
	}
	detail, err := json.Marshal(map[string]any{"rid": role.RID, "key": key, "item": body.Item, "reason": body.Reason, "diff": moduleDiff(before, after)})
	if err != nil {
		internalErr(w, s, err)
		return
	}
	audit := store.AuditLog{AdminID: p.Admin.ID, AdminName: p.Admin.Username, Action: "role.module.reset", Target: role.Name, DetailJSON: string(detail), IP: clientIP(r)}
	if err := s.Store.CommitRoleModuleReset(role.RID, key, before, after, audit); err != nil {
		if errors.Is(err, store.ErrConflict) {
			fail(w, 409, "module changed concurrently; refresh before retrying")
		} else {
			internalErr(w, s, err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "key": key, "item": body.Item, "deleted": after == nil})
}

func moduleDiff(before, after json.RawMessage) map[string]any {
	summary := func(value json.RawMessage) map[string]any {
		hash := sha256.Sum256(value)
		return map[string]any{"bytes": len(value), "sha256": hex.EncodeToString(hash[:]), "exists": value != nil}
	}
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(before, &a)
	_ = json.Unmarshal(after, &b)
	keys := map[string]bool{}
	for key := range a {
		keys[key] = true
	}
	for key := range b {
		keys[key] = true
	}
	changed := []string{}
	for key := range keys {
		if string(a[key]) != string(b[key]) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	total := len(changed)
	if total > 64 {
		changed = changed[:64]
	}
	return map[string]any{"before": summary(before), "after": summary(after), "changed_fields": changed, "changed_field_count": total}
}
