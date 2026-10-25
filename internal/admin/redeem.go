package admin

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"2droundserver/internal/store"
)

var reRedeemCode = regexp.MustCompile(`^[A-Za-z0-9]{4,32}$`)

const maxRedeemUses = 1000000

// listRedeemCodes shows the redeem codes with how often each was used.
func (s *Server) listRedeemCodes(w http.ResponseWriter, r *http.Request, p *principal) {
	list, err := s.Store.RedeemCodes()
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(list)})
}

// createRedeemCode adds a code players can type in the game's settings (10945). Every role may use a
// code once; max_uses limits all roles together (0 = no limit), days the time it works (0 = for good).
func (s *Server) createRedeemCode(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Code    string          `json:"code"`
		Note    string          `json:"note"`
		Grants  []attachmentReq `json:"grants"`
		MaxUses int             `json:"max_uses"`
		Days    int             `json:"days"`
	}
	if !decode(w, r, &q) {
		return
	}
	switch {
	case !reRedeemCode.MatchString(q.Code):
		fail(w, http.StatusUnprocessableEntity, "code must be 4 to 32 letters or digits")
		return
	case len(q.Note) > 200:
		fail(w, http.StatusUnprocessableEntity, "note is at most 200 bytes")
		return
	case q.MaxUses < 0 || q.MaxUses > maxRedeemUses:
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("max_uses must be 0 to %d", maxRedeemUses))
		return
	case q.Days < 0 || q.Days > maxSkinDays:
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("days must be 0 (for good) to %d", maxSkinDays))
		return
	case len(q.Grants) == 0 || len(q.Grants) > maxGrantItems:
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("give 1 to %d rewards", maxGrantItems))
		return
	}
	atts, msg := buildAttachments(q.Grants)
	if msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	var expires int64
	if q.Days > 0 {
		expires = time.Now().Add(time.Duration(q.Days) * 24 * time.Hour).Unix()
	}
	err := s.Store.CreateRedeemCode(store.RedeemCode{Code: q.Code, Note: q.Note, Attachments: atts, MaxUses: q.MaxUses, ExpiresAt: expires})
	if errors.Is(err, store.ErrConflict) {
		fail(w, http.StatusConflict, "that code exists")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	code := store.NormalizeCode(q.Code)
	s.audit(r, p, "redeem.create", code, map[string]any{"grants": atts, "max_uses": q.MaxUses, "days": q.Days})
	writeJSON(w, 200, map[string]any{"ok": true, "code": code, "expires_at": expires})
}

// deleteRedeemCode removes a code; players who used it keep what they got.
func (s *Server) deleteRedeemCode(w http.ResponseWriter, r *http.Request, p *principal) {
	code := store.NormalizeCode(r.PathValue("code"))
	err := s.Store.DeleteRedeemCode(code)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such code")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "redeem.delete", code, nil)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// listReports shows what players reported (12770), newest first.
func (s *Server) listReports(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, _ := page(r)
	list, err := s.Store.Reports(limit)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	type row struct {
		ID          int64  `json:"id"`
		RID         int64  `json:"rid"`
		TargetRID   int64  `json:"target_rid"`
		TargetSrvID string `json:"target_srv_id"`
		Kind        int    `json:"type"`
		Msg         string `json:"msg"`
		CreatedAt   int64  `json:"created_at"`
	}
	out := make([]row, 0, len(list))
	for _, x := range list {
		out = append(out, row{x.ID, x.RID, x.TargetRID, x.TargetSrvID, x.Kind, x.Msg, x.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
