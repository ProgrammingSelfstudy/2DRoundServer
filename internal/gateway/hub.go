package gateway

import "sync"

// Hub tracks which role is online on which session, so a second login of the
// same role can kick the first and the admin side can push to online players.
type Hub struct {
	mu     sync.Mutex
	byRole map[int64]*Session
}

func NewHub() *Hub { return &Hub{byRole: map[int64]*Session{}} }

// Register binds rid to s and returns the session it replaced (if any).
func (h *Hub) Register(rid int64, s *Session) (previous *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	previous = h.byRole[rid]
	h.byRole[rid] = s
	if previous == s {
		return nil
	}
	return previous
}

// Unregister removes s, but only if it is still the registered session for rid.
func (h *Hub) Unregister(rid int64, s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byRole[rid] == s {
		delete(h.byRole, rid)
	}
}

func (h *Hub) Online(rid int64) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byRole[rid]
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byRole)
}

// Each calls fn for every online session (fn must not call back into the Hub).
func (h *Hub) Each(fn func(rid int64, s *Session)) {
	h.mu.Lock()
	snap := make(map[int64]*Session, len(h.byRole))
	for k, v := range h.byRole {
		snap[k] = v
	}
	h.mu.Unlock()
	for k, v := range snap {
		fn(k, v)
	}
}
