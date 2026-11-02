package admin

import (
	"net/http"
	"strconv"
)

// analytics is the operating data page: per-day sign-ups, active roles, play time, peak online and payments, the retention of
// each day's new roles and the most used commands.
func (s *Server) analytics(w http.ResponseWriter, r *http.Request, p *principal) {
	days := 14
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 90 {
			fail(w, http.StatusBadRequest, "days is 1 to 90")
			return
		}
		days = n
	}
	rep, err := s.Store.Analytics(days)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, rep)
}
