package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"2droundserver/internal/store"
)

type ticketRow struct {
	ID         int64    `json:"id"`
	RID        int64    `json:"rid"`
	SrvID      string   `json:"srv_id"`
	IssueType  int      `json:"issue_type"`
	Title      string   `json:"title"`
	Phone      string   `json:"phone"`
	Email      string   `json:"email"`
	PhoneInfo  string   `json:"phone_info"`
	State      int      `json:"state"`
	Score      int64    `json:"score"`
	CreatedAt  int64    `json:"created_at"`
	AnsweredAt int64    `json:"answered_at"`
	Questions  []string `json:"questions"`
	Answers    []string `json:"answers"`
}

// listTickets shows the newest customer-service tickets with their questions and answers.
func (s *Server) listTickets(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, _ := page(r)
	list, err := s.Store.AllTickets(limit)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	out := make([]ticketRow, 0, len(list))
	for _, t := range list {
		msgs, err := s.Store.TicketMsgs(t.ID)
		if err != nil {
			internalErr(w, s, err)
			return
		}
		row := ticketRow{t.ID, t.RID, t.SrvID, t.IssueType, t.Title, t.Phone, t.Email, t.PhoneInfo, t.State, t.Score, t.CreatedAt, t.AnsweredAt, []string{}, []string{}}
		for _, m := range msgs {
			if m.Kind == 1 {
				row.Questions = append(row.Questions, m.Content)
			} else {
				row.Answers = append(row.Answers, m.Content)
			}
		}
		out = append(out, row)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

// replyTicket answers a ticket; the player sees it next time they open the ticket list.
func (s *Server) replyTicket(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &q) {
		return
	}
	if strings.TrimSpace(q.Content) == "" || utf8.RuneCountInString(q.Content) > 1000 {
		fail(w, 422, "the answer must be 1-1000 characters")
		return
	}
	err := s.Store.AnswerTicket(id, q.Content, s.Now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such ticket")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "ticket.reply", strconv.FormatInt(id, 10), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
