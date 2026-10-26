package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// ---- the activity schedule ----

type activityReq struct {
	SrvID   string          `json:"srv_id"` // "" = every server
	CampID  int64           `json:"camp_id"`
	BID     int64           `json:"bid"`
	Status  int             `json:"status"`
	StartAt int64           `json:"start_at"`
	EndAt   int64           `json:"end_at"` // 0 = never
	Spec    json.RawMessage `json:"spec"`
}

// activityView is a schedule row with its spec as an object, so the UI can edit it.
type activityView struct {
	store.Activity
	Spec json.RawMessage `json:"spec"`
}

func viewActivity(a store.Activity) activityView {
	return activityView{Activity: a, Spec: json.RawMessage(a.Spec)}
}

func (s *Server) activityFromReq(w http.ResponseWriter, q *activityReq) (store.Activity, bool) {
	a := store.Activity{SrvID: q.SrvID, CampID: q.CampID, BID: q.BID, Status: q.Status, StartAt: q.StartAt, EndAt: q.EndAt, Spec: string(q.Spec)}
	if len(q.Spec) == 0 {
		fail(w, 422, "spec is required")
		return a, false
	}
	if msg := game.ValidateActivity(&a); msg != "" {
		fail(w, 422, msg)
		return a, false
	}
	if q.SrvID != "" {
		if _, err := s.Store.Server(q.SrvID); err != nil {
			fail(w, 422, "srv_id must be an existing server, or empty for all servers")
			return a, false
		}
	}
	return a, true
}

func (s *Server) listActivities(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListActivities(r.URL.Query().Get("srv_id"), limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	items := make([]activityView, 0, len(rows))
	for _, a := range rows {
		items = append(items, viewActivity(a))
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (s *Server) createActivity(w http.ResponseWriter, r *http.Request, p *principal) {
	var q activityReq
	if !decode(w, r, &q) {
		return
	}
	a, ok := s.activityFromReq(w, &q)
	if !ok {
		return
	}
	a.CreatedBy = p.Admin.Username
	row, err := s.Store.CreateActivity(a)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "activity.create", fmt.Sprint(row.ID), map[string]any{"camp_id": row.CampID, "bid": row.BID, "srv_id": row.SrvID})
	writeJSON(w, 201, viewActivity(*row))
}

func (s *Server) updateActivity(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var q activityReq
	if !decode(w, r, &q) {
		return
	}
	a, ok := s.activityFromReq(w, &q)
	if !ok {
		return
	}
	a.ID = id
	err := s.Store.UpdateActivity(a)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such activity")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "activity.update", fmt.Sprint(id), map[string]any{"camp_id": a.CampID, "bid": a.BID, "status": a.Status})
	row, _ := s.Store.Activity(id)
	writeJSON(w, 200, viewActivity(*row))
}

func (s *Server) deleteActivity(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.Store.DeleteActivity(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such activity")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "activity.delete", fmt.Sprint(id), nil)
	writeJSON(w, 200, map[string]any{"ok": true})
}
