package game

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"2droundserver/internal/store"
)

// Owners are bound by the real gameplay registrations, never synthesized by the
// admin process. A missing owner is unavailable, not permission to bypass it.
type adminGlobalOwner struct {
	mu      sync.Locker
	service any
}
type adminRuntime struct {
	mu     sync.RWMutex
	owners map[string]adminGlobalOwner
	locks  sync.Map
}

var adminRuntimes sync.Map // *store.Store -> gateway-owned service bindings
func adminRuntimeOf(st *store.Store) *adminRuntime {
	v, _ := adminRuntimes.LoadOrStore(st, &adminRuntime{owners: map[string]adminGlobalOwner{}})
	return v.(*adminRuntime)
}
func adminModuleMutex(st *store.Store, key string) *sync.Mutex {
	v, _ := adminRuntimeOf(st).locks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func bindAdminGlobal(st *store.Store, key string, mu sync.Locker, svc any) {
	r := adminRuntimeOf(st)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owners[key] = adminGlobalOwner{mu, svc}
}
func globalOwner(st *store.Store, key string) (AdminModule, adminGlobalOwner, error) {
	m, ok := FindAdminModule("global", key)
	if !ok {
		return m, adminGlobalOwner{}, &opsError{404, "unknown global module"}
	}
	rt := adminRuntimeOf(st)
	rt.mu.RLock()
	o, ok := rt.owners[m.Key]
	rt.mu.RUnlock()
	if !ok {
		return m, o, &opsError{503, "module service is not registered on this gateway"}
	}
	return m, o, nil
}

type opsError struct {
	status  int
	message string
}

func (e *opsError) Error() string { return e.message }
func opsWrite(w http.ResponseWriter, out any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := 500
		var e *opsError
		if errors.As(err, &e) {
			status = e.status
		}
		if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(out)
}

// globalSnapshot must be called with the owning service lock held. It does not
// call loaders: a GET cannot roll seasons, send mail, or initialize saved rows.
func globalSnapshot(st *store.Store, srv, key string, m AdminModule, o adminGlobalOwner, now time.Time) (map[string]any, error) {
	raw, found, err := st.GlobalModule(srv, key)
	if err != nil {
		return nil, err
	}
	state := m.New()
	if found {
		if err = json.Unmarshal(raw, state); err != nil {
			return nil, err
		}
	}
	season, err := globalSeason(st, srv, state, o.service, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{"key": key, "exists": found, "data": raw, "view": map[string]any{"state": state, "season": season}}, nil
}
func adminGlobalView(st *store.Store, srv, key string, now time.Time) (any, error) {
	m, o, err := globalOwner(st, key)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out, err := globalSnapshot(st, srv, key, m, o, now)
	if err != nil {
		return nil, err
	}
	return out["view"], nil
}
func globalRankFor(key string) func(*store.Store, string, int) ([]AdminRankRow, error) {
	return func(st *store.Store, srv string, limit int) ([]AdminRankRow, error) {
		m, o, err := globalOwner(st, key)
		if err != nil {
			return nil, err
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		raw, found, err := st.GlobalModule(srv, key)
		if err != nil {
			return nil, err
		}
		state := m.New()
		if found {
			if err = json.Unmarshal(raw, state); err != nil {
				return nil, err
			}
		}
		return globalRanks(st, srv, state, o.service, limit)
	}
}

func (m *MailService) registerAdminOps(mux *http.ServeMux) {
	clock := func() time.Time {
		if m.Now != nil {
			return m.Now()
		}
		return time.Now()
	}
	valid := func(r *http.Request) (string, error) {
		srv := r.URL.Query().Get("srv")
		if srv == "" || (m.SrvID != "" && srv != m.SrvID) {
			return "", &opsError{404, "server is not owned by this gateway"}
		}
		_, err := m.Store.Server(srv)
		return srv, err
	}
	mux.HandleFunc("POST /ops/season/settle", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		var op AdminOperation
		if !readAdminOperation(w, r, &op) {
			return
		}
		if !validAdminReason(op.Reason) {
			opsWrite(w, nil, &opsError{422, "reason must be 1 to 200 nonblank characters without controls"})
			return
		}
		out, err := settleGlobal(m.Store, srv, r.URL.Query().Get("key"), clock(), op)
		opsWrite(w, out, err)
	})
	mux.HandleFunc("GET /ops/global", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		key := r.URL.Query().Get("key")
		if key != "" {
			mod, o, err := globalOwner(m.Store, key)
			if err != nil {
				opsWrite(w, nil, err)
				return
			}
			o.mu.Lock()
			out, err := globalSnapshot(m.Store, srv, key, mod, o, clock())
			o.mu.Unlock()
			opsWrite(w, out, err)
			return
		}
		items := []any{}
		for _, mod := range AdminModules() {
			if mod.Scope != "global" {
				continue
			}
			rt := adminRuntimeOf(m.Store)
			rt.mu.RLock()
			o, ok := rt.owners[mod.Key]
			rt.mu.RUnlock()
			if !ok {
				opsWrite(w, nil, &opsError{503, "module owner unavailable: " + mod.Key})
				return
			}
			o.mu.Lock()
			keys := []string{mod.Key}
			if strings.HasSuffix(mod.Key, "*") {
				keys, err = m.Store.GlobalModuleKeys(srv, strings.TrimSuffix(mod.Key, "*"))
			}
			if err == nil {
				for _, key := range keys {
					if _, ok := FindAdminModule("global", key); !ok {
						continue
					}
					var row map[string]any
					row, err = globalSnapshot(m.Store, srv, key, mod, o, clock())
					if err != nil {
						break
					}
					items = append(items, row)
				}
			}
			o.mu.Unlock()
			if err != nil {
				opsWrite(w, nil, err)
				return
			}
		}
		opsWrite(w, map[string]any{"items": items}, nil)
	})
	mux.HandleFunc("GET /ops/rank", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		key := r.URL.Query().Get("key")
		mod, _, err := globalOwner(m.Store, key)
		if err == nil && mod.Rank == nil {
			err = &opsError{405, "module has no registered ranking"}
		}
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		limit := 50
		if q := r.URL.Query().Get("limit"); q != "" {
			limit, err = strconv.Atoi(q)
			if err != nil || limit < 1 || limit > 200 {
				opsWrite(w, nil, &opsError{422, "limit must be 1 to 200"})
				return
			}
		}
		items, err := mod.Rank(m.Store, srv, limit)
		opsWrite(w, map[string]any{"items": items}, err)
	})
	mux.HandleFunc("GET /ops/season", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		key := r.URL.Query().Get("key")
		mod, o, err := globalOwner(m.Store, key)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		o.mu.Lock()
		out, err := globalSnapshot(m.Store, srv, key, mod, o, clock())
		o.mu.Unlock()
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		opsWrite(w, out["view"].(map[string]any)["season"], nil)
	})
	mux.HandleFunc("GET /ops/overview", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		sv, err := m.Store.Server(srv)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		lev, err := m.Store.WorldLevel(srv)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		open := sv.OpenTime
		if open == 0 {
			open = sv.CreatedAt
		}
		key, start, end := arenaSeason(clock())
		opsWrite(w, map[string]any{"srv_id": srv, "world_lev": lev, "open_time": open, "open_day": openDay(time.Unix(open, 0), clock()), "arena": map[string]any{"season": key, "start_time": start, "end_time": end}}, nil)
	})
}
