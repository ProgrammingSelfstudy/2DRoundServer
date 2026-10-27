package store

// Ticket states (the client's: 1 filed, 2 answered, 3 finished and not rated, 4 rated).
const (
	TicketFiled    = 1
	TicketAnswered = 2
	TicketFinished = 3
	TicketRated    = 4
)

// Ticket is a customer-service ticket of a role.
type Ticket struct {
	ID, RID                               int64
	SrvID, Title, Phone, Email, PhoneInfo string
	IssueType, State                      int
	Read                                  bool // the player has seen the answer
	CreatedAt, AnsweredAt, FinishedAt     int64
	Score, ScoredAt                       int64
}

// TicketMsg is a question or an answer on a ticket.
type TicketMsg struct {
	Kind    int // 1 question, 2 answer
	At      int64
	Content string
}

const ticketCols = `id,rid,srv_id,issue_type,title,phone,email,phone_info,state,read,created_at,answered_at,finished_at,score,scored_at`

func scanTicket(sc interface{ Scan(...any) error }) (*Ticket, error) {
	var t Ticket
	err := sc.Scan(&t.ID, &t.RID, &t.SrvID, &t.IssueType, &t.Title, &t.Phone, &t.Email, &t.PhoneInfo, &t.State, &t.Read,
		&t.CreatedAt, &t.AnsweredAt, &t.FinishedAt, &t.Score, &t.ScoredAt)
	return &t, wrapNoRows(err)
}

// FileTicket opens a ticket with its first question.
func (s *Store) FileTicket(t Ticket, content string, now int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO tickets(rid,srv_id,issue_type,title,phone,email,phone_info,state,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		t.RID, t.SrvID, t.IssueType, t.Title, t.Phone, t.Email, t.PhoneInfo, TicketFiled, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO ticket_msgs(ticket_id,kind,at,content) VALUES(?,1,?,?)`, id, now, content); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// TicketByID reads one (ErrNotFound when there is none).
func (s *Store) TicketByID(id int64) (*Ticket, error) {
	return scanTicket(s.db.QueryRow(`SELECT `+ticketCols+` FROM tickets WHERE id=?`, id))
}

// TicketsOf lists a role's tickets, newest first.
func (s *Store) TicketsOf(rid int64) ([]Ticket, error) {
	return s.tickets(`WHERE rid=? ORDER BY id DESC`, rid)
}

// AllTickets lists the newest tickets of every role, up to limit.
func (s *Store) AllTickets(limit int) ([]Ticket, error) {
	return s.tickets(`ORDER BY id DESC LIMIT ?`, limit)
}

func (s *Store) tickets(tail string, args ...any) ([]Ticket, error) {
	rows, err := s.db.Query(`SELECT `+ticketCols+` FROM tickets `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// TicketMsgs lists a ticket's messages, oldest first.
func (s *Store) TicketMsgs(id int64) ([]TicketMsg, error) {
	rows, err := s.db.Query(`SELECT kind,at,content FROM ticket_msgs WHERE ticket_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketMsg
	for rows.Next() {
		var m TicketMsg
		if err := rows.Scan(&m.Kind, &m.At, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AskTicket adds a question to a ticket of the role and sets it back to filed. ErrNotFound for
// a ticket that is not theirs.
func (s *Store) AskTicket(rid, id int64, content string, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE tickets SET state=?,read=0,finished_at=0,score=0,scored_at=0 WHERE id=? AND rid=?`, TicketFiled, id, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`INSERT INTO ticket_msgs(ticket_id,kind,at,content) VALUES(?,1,?,?)`, id, now, content); err != nil {
		return err
	}
	return tx.Commit()
}

// AnswerTicket adds the staff's answer and marks the ticket answered.
func (s *Store) AnswerTicket(id int64, content string, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE tickets SET state=?,read=0,answered_at=? WHERE id=?`, TicketAnswered, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`INSERT INTO ticket_msgs(ticket_id,kind,at,content) VALUES(?,2,?,?)`, id, now, content); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishTicket closes an answered ticket of the role: score 0 only finishes it (not rated),
// 1-5 rates it. ErrNotFound for a ticket that is not theirs, ErrConflict for one nobody answered.
func (s *Store) FinishTicket(rid, id, score, now int64) error {
	t, err := s.TicketByID(id)
	if err != nil {
		return err
	}
	if t.RID != rid {
		return ErrNotFound
	}
	if t.State == TicketFiled {
		return ErrConflict
	}
	if score == 0 {
		if t.State == TicketAnswered {
			_, err = s.db.Exec(`UPDATE tickets SET state=?,finished_at=? WHERE id=?`, TicketFinished, now, id)
		}
		return err
	}
	_, err = s.db.Exec(`UPDATE tickets SET state=?,score=?,scored_at=?,finished_at=CASE WHEN finished_at=0 THEN ? ELSE finished_at END WHERE id=?`,
		TicketRated, score, now, now, id)
	return err
}

// ReadTicket marks the answer seen.
func (s *Store) ReadTicket(rid, id int64) error {
	_, err := s.db.Exec(`UPDATE tickets SET read=1 WHERE id=? AND rid=?`, id, rid)
	return err
}
