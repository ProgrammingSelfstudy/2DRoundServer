package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// ---- notices ----

type noticeReq struct {
	SrvID   string `json:"srv_id"` // "" = every server
	Title   string `json:"title"`
	Content string `json:"content"`
	StartAt int64  `json:"start_at"`
	EndAt   int64  `json:"end_at"` // 0 = never
	Status  int    `json:"status"`
}

func (s *Server) validateNotice(q *noticeReq) string {
	switch {
	case strings.TrimSpace(q.Title) == "" || utf8.RuneCountInString(q.Title) > 100:
		return "title is required, at most 100 characters"
	case strings.TrimSpace(q.Content) == "" || utf8.RuneCountInString(q.Content) > 2000:
		return "content is required, at most 2000 characters"
	case q.Status != 0 && q.Status != 1:
		return "status must be 0 (hidden) or 1 (shown)"
	case q.StartAt < 0 || q.EndAt < 0:
		return "times cannot be negative"
	case q.EndAt != 0 && q.EndAt <= q.StartAt:
		return "end_at must be after start_at (or 0 for no end)"
	}
	if q.SrvID != "" {
		if _, err := s.Store.Server(q.SrvID); err != nil {
			return "srv_id must be an existing server, or empty for all servers"
		}
	}
	return ""
}

// noticeReply is the notice itself plus what the live push did, so the UI can say
// "saved, but nobody was told" instead of failing: the notice is stored either way.
type noticeReply struct {
	*store.Notice
	Pushed    *int   `json:"pushed"`
	PushError string `json:"push_error,omitempty"`
}

// pushNotice asks the gateway to show (or drop) notice id for the online roles.
func (s *Server) pushNotice(path string, id int64) (*int, string) {
	var out struct {
		Pushed  int `json:"pushed"`
		Dropped int `json:"dropped"`
	}
	if err := s.gateway("POST", fmt.Sprintf("%s?id=%d", path, id), &out); err != nil {
		return nil, err.Error()
	}
	n := out.Pushed
	if path == "/push/notice/remove" {
		n = out.Dropped
	}
	return &n, ""
}

func (s *Server) listNotices(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListNotices(limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(rows), "total": total})
}

func (s *Server) createNotice(w http.ResponseWriter, r *http.Request, p *principal) {
	var q noticeReq
	if !decode(w, r, &q) {
		return
	}
	if msg := s.validateNotice(&q); msg != "" {
		fail(w, 422, msg)
		return
	}
	n, err := s.Store.CreateNotice(store.Notice{SrvID: q.SrvID, Title: q.Title, Content: q.Content, StartAt: q.StartAt, EndAt: q.EndAt, Status: q.Status, CreatedBy: p.Admin.Username})
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "notice.create", fmt.Sprint(n.ID), map[string]any{"title": n.Title, "srv_id": n.SrvID})
	pushed, pushErr := s.pushNotice("/push/notice", n.ID)
	writeJSON(w, 201, noticeReply{Notice: n, Pushed: pushed, PushError: pushErr})
}

func (s *Server) updateNotice(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var q noticeReq
	if !decode(w, r, &q) {
		return
	}
	if msg := s.validateNotice(&q); msg != "" {
		fail(w, 422, msg)
		return
	}
	err := s.Store.UpdateNotice(store.Notice{ID: id, SrvID: q.SrvID, Title: q.Title, Content: q.Content, StartAt: q.StartAt, EndAt: q.EndAt, Status: q.Status})
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such notice")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "notice.update", fmt.Sprint(id), map[string]any{"title": q.Title, "status": q.Status})
	n, _ := s.Store.Notice(id)
	pushed, pushErr := s.pushNotice("/push/notice", id)
	writeJSON(w, 200, noticeReply{Notice: n, Pushed: pushed, PushError: pushErr})
}

func (s *Server) deleteNotice(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.Store.DeleteNotice(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such notice")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "notice.delete", fmt.Sprint(id), nil)
	pushed, pushErr := s.pushNotice("/push/notice/remove", id)
	resp := map[string]any{"ok": true, "pushed": pushed}
	if pushErr != "" {
		resp["push_error"] = pushErr
	}
	writeJSON(w, 200, resp)
}

// ---- mail ----

type attachmentReq struct {
	Kind   string `json:"kind"`    // "asset" or "item"
	Asset  string `json:"asset"`   // asset name from /api/meta (kind asset)
	ItemID int64  `json:"item_id"` // item base id (kind item), or a raw asset label (kind asset without a name)
	Num    int64  `json:"num"`
}

type mailReq struct {
	Scope      string          `json:"scope"` // "direct" or "broadcast"
	SrvID      string          `json:"srv_id"`
	RoleIDs    []int64         `json:"role_ids"`
	Subject    string          `json:"subject"`
	Content    string          `json:"content"`
	FromName   string          `json:"from_name"`
	Type       int             `json:"type"`
	ExpireAt   int64           `json:"expire_at"`
	ExpireDays int             `json:"expire_days"`
	Attach     []attachmentReq `json:"attachments"`
}

const (
	maxAttachments = 8
	maxGrantItems  = 20   // entries in one direct grant
	maxGrantHeroes = 20   // heroes in one direct hero grant
	maxSkinDays    = 3650 // days of a timed skin grant
	maxAmount      = 1_000_000_000
	maxRecipients  = 500
)

// buildAttachments validates a list of currencies and items (the same shape a mail
// carries and a direct grant takes) and returns it as store attachments.
func buildAttachments(list []attachmentReq) ([]store.Attachment, string) {
	var out []store.Attachment
	for i, a := range list {
		if a.Num < 1 || a.Num > maxAmount {
			return nil, fmt.Sprintf("attachment %d: num must be 1 to %d", i+1, maxAmount)
		}
		switch a.Kind {
		case store.AttachAsset:
			label := a.ItemID
			if a.Asset != "" {
				id, ok := game.AssetLabelID(a.Asset)
				if !ok {
					return nil, fmt.Sprintf("attachment %d: unknown asset %q (see /api/meta)", i+1, a.Asset)
				}
				label = id
			}
			if _, ok := game.AssetName(label); !ok {
				return nil, fmt.Sprintf("attachment %d: unknown asset label %d", i+1, label)
			}
			out = append(out, store.Attachment{Kind: store.AttachAsset, ItemID: label, Num: a.Num})
		case store.AttachItem:
			if a.ItemID < 1 {
				return nil, fmt.Sprintf("attachment %d: item_id is required", i+1)
			}
			if !game.ItemKnown(a.ItemID) {
				return nil, fmt.Sprintf("attachment %d: unknown item %d (see /api/items)", i+1, a.ItemID)
			}
			if game.ItemIsCurrency(a.ItemID) {
				return nil, fmt.Sprintf("attachment %d: item %d is a currency, attach it as an asset", i+1, a.ItemID)
			}
			out = append(out, store.Attachment{Kind: store.AttachItem, ItemID: a.ItemID, Num: a.Num})
		default:
			return nil, fmt.Sprintf("attachment %d: kind must be asset or item", i+1)
		}
	}
	return out, ""
}

// buildMail validates the request and returns the mail to store.
func (s *Server) buildMail(q *mailReq, by string) (store.Mail, string) {
	m := store.Mail{Subject: q.Subject, Content: q.Content, FromName: q.FromName, Type: q.Type, CreatedBy: by}
	if m.FromName == "" {
		m.FromName = "GM"
	}
	if m.Type == 0 {
		m.Type = 1
	}
	switch {
	case strings.TrimSpace(q.Subject) == "" || utf8.RuneCountInString(q.Subject) > 60:
		return m, "subject is required, at most 60 characters"
	case utf8.RuneCountInString(q.Content) > 2000:
		return m, "content is at most 2000 characters"
	case utf8.RuneCountInString(m.FromName) > 20:
		return m, "from_name is at most 20 characters"
	case m.Type != 1 && m.Type != 3:
		return m, "type must be 1 or 3 (the client lists those)"
	case len(q.Attach) > maxAttachments:
		return m, fmt.Sprintf("at most %d attachments", maxAttachments)
	case q.ExpireAt != 0 && q.ExpireDays != 0:
		return m, "give expire_at or expire_days, not both"
	case q.ExpireDays < 0 || q.ExpireDays > 365:
		return m, "expire_days must be 1 to 365"
	}
	now := s.Now().Unix()
	switch {
	case q.ExpireDays > 0:
		m.ExpireAt = now + int64(q.ExpireDays)*86400
	case q.ExpireAt != 0 && q.ExpireAt <= now:
		return m, "expire_at is already in the past"
	default:
		m.ExpireAt = q.ExpireAt
	}
	atts, msg := buildAttachments(q.Attach)
	if msg != "" {
		return m, msg
	}
	m.Attachments = atts
	return m, ""
}

func (s *Server) listMails(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListMails(limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	// names of the items the listed mails carry, so the page can show them
	names := map[int64]string{}
	for _, m := range rows {
		for _, a := range m.Attachments {
			if a.Kind == store.AttachItem {
				if n, ok := game.ItemName(a.ItemID); ok {
					names[a.ItemID] = n
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(rows), "total": total, "item_names": names})
}

func (s *Server) sendMail(w http.ResponseWriter, r *http.Request, p *principal) {
	var q mailReq
	if !decode(w, r, &q) {
		return
	}
	m, msg := s.buildMail(&q, p.Admin.Username)
	if msg != "" {
		fail(w, 422, msg)
		return
	}
	var rids []int64
	switch q.Scope {
	case store.MailDirect:
		if len(q.RoleIDs) == 0 || len(q.RoleIDs) > maxRecipients {
			fail(w, 422, fmt.Sprintf("a direct mail needs 1 to %d role_ids", maxRecipients))
			return
		}
		seen := map[int64]bool{}
		var missing []int64
		for _, id := range q.RoleIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, err := s.Store.RoleByID(id); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					missing = append(missing, id)
					continue
				}
				internalErr(w, s, err)
				return
			}
			rids = append(rids, id)
		}
		if len(missing) > 0 {
			fail(w, 422, fmt.Sprintf("these roles do not exist: %v", missing))
			return
		}
	case store.MailBroadcast:
		if len(q.RoleIDs) != 0 {
			fail(w, 422, "a broadcast has no role_ids; use scope direct for chosen roles")
			return
		}
		if q.SrvID != "" {
			if _, err := s.Store.Server(q.SrvID); err != nil {
				fail(w, 422, "srv_id must be an existing server, or empty for all servers")
				return
			}
		}
		m.Scope, m.SrvID = store.MailBroadcast, q.SrvID
	default:
		fail(w, 422, "scope must be direct or broadcast")
		return
	}
	created, err := s.Store.CreateMail(m, rids)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	resp := map[string]any{"mail": created, "pushed": nil}
	var pushed struct {
		Pushed int `json:"pushed"`
	}
	if err := s.gateway("POST", fmt.Sprintf("/push/mail?id=%d", created.ID), &pushed); err != nil {
		resp["push_error"] = err.Error() // stored: offline roles still get it when they list their mail
	} else {
		resp["pushed"] = pushed.Pushed
	}
	s.audit(r, p, "mail.send", fmt.Sprint(created.ID), map[string]any{
		"scope": created.Scope, "srv_id": created.SrvID, "recipients": len(rids), "subject": created.Subject, "attachments": created.Attachments, "expire_at": created.ExpireAt,
	})
	writeJSON(w, 201, resp)
}
