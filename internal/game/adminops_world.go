package game

import (
	"net/http"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type AdminWorldLevelRequest struct {
	AdminOperation
	Override int   `json:"override"`
	Until    int64 `json:"until"`
}

func worldLevelInfo(st *store.Store, srv string, now time.Time) (store.WorldLevelInfo, error) {
	_, owner, err := globalOwner(st, store.WorldLevelModule)
	if err != nil {
		return store.WorldLevelInfo{}, err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return st.WorldLevelInfoAt(srv, now)
}
func (m *MailService) registerWorldLevelOps(mux *http.ServeMux, valid func(*http.Request) (string, error), clock func() time.Time) {
	// World-level administration is itself owned by this gateway service. The same
	// lock guards /ops/global reads, world-level GET/PUT and the post-commit pushes.
	bindAdminGlobal(m.Store, store.WorldLevelModule, adminModuleMutex(m.Store, store.WorldLevelModule), m)
	mux.HandleFunc("GET /ops/worldlev", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		out, err := worldLevelInfo(m.Store, srv, clock())
		opsWrite(w, out, err)
	})
	mux.HandleFunc("PUT /ops/worldlev", func(w http.ResponseWriter, r *http.Request) {
		srv, err := valid(r)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		var req AdminWorldLevelRequest
		if !readAdminOperation(w, r, &req) {
			return
		}
		if !validAdminReason(req.Reason) || req.Override < 0 || req.Override > 200 || req.Until < 0 {
			opsWrite(w, nil, &opsError{422, "override must be 0 to 200, until nonnegative and reason 1 to 200 characters"})
			return
		}
		_, owner, err := globalOwner(m.Store, store.WorldLevelModule)
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		owner.mu.Lock()
		defer owner.mu.Unlock()
		var after store.WorldLevelInfo
		err = m.Store.AdminTransaction(func(tx *store.Store) error {
			before, err := tx.WorldLevelInfoAt(srv, clock())
			if err != nil {
				return err
			}
			value := store.WorldLevelOverride{Override: req.Override, Until: req.Until}
			if err = tx.SaveWorldLevelOverride(srv, value); err != nil {
				return err
			}
			after, err = tx.WorldLevelInfoAt(srv, clock())
			if err != nil {
				return err
			}
			return req.AdminOperation.audit(tx, "server.worldlev", srv, map[string]any{"before": before, "after": after})
		})
		if err != nil {
			opsWrite(w, nil, err)
			return
		}
		pushed, failed := 0, 0
		if m.Hub != nil {
			m.Hub.Each(func(_ int64, s *gateway.Session) {
				if role := s.Role(); role != nil && role.SrvID == srv {
					if err := s.Send(10905, proto.Message{"world_lev": after.Effective}); err != nil {
						failed++
					} else {
						pushed++
					}
				}
			})
		}
		opsWrite(w, map[string]any{"world_level": after, "pushed": pushed, "push_errors": failed}, nil)
	})
}
func init() {
	registerAdminModule(AdminModule{Key: store.WorldLevelModule, Title: "世界等级覆盖", Window: "10192/10905", Scope: "global", New: adminState[store.WorldLevelOverride], View: adminGlobalView})
}
