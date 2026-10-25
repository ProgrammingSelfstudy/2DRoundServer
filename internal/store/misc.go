package store

import (
	"encoding/json"
	"errors"
	"strings"
)

// RedeemCode is a code a player can type in for a reward (10945).
type RedeemCode struct {
	Code        string       `json:"code"`
	Note        string       `json:"note"`
	Attachments []Attachment `json:"attachments"`
	MaxUses     int          `json:"max_uses"` // 0 = unlimited; every role can use a code once
	Used        int          `json:"used"`
	ExpiresAt   int64        `json:"expires_at"` // 0 = never
	CreatedAt   int64        `json:"created_at"`
}

var (
	ErrCodeUnknown = errors.New("store: no such redeem code")
	ErrCodeExpired = errors.New("store: redeem code expired")
	ErrCodeUsedUp  = errors.New("store: redeem code used up")
	ErrCodeTaken   = errors.New("store: redeem code already used by this role")
)

// NormalizeCode is how codes are compared: trimmed and upper-cased.
func NormalizeCode(c string) string { return strings.ToUpper(strings.TrimSpace(c)) }

// CreateRedeemCode adds a code; ErrConflict when the code exists.
func (s *Store) CreateRedeemCode(c RedeemCode) error {
	raw, err := json.Marshal(c.Attachments)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO redeem_codes(code,note,attachments,max_uses,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		NormalizeCode(c.Code), c.Note, string(raw), c.MaxUses, c.ExpiresAt, s.unix())
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// RedeemCodes lists the codes, newest first.
func (s *Store) RedeemCodes() ([]RedeemCode, error) {
	rows, err := s.db.Query(`SELECT code,note,attachments,max_uses,used,expires_at,created_at FROM redeem_codes ORDER BY created_at DESC, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RedeemCode
	for rows.Next() {
		var c RedeemCode
		var raw string
		if err := rows.Scan(&c.Code, &c.Note, &raw, &c.MaxUses, &c.Used, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Attachments); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteRedeemCode removes a code (and who used it); ErrNotFound when there is none.
func (s *Store) DeleteRedeemCode(code string) error {
	res, err := s.db.Exec(`DELETE FROM redeem_codes WHERE code=?`, NormalizeCode(code))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redeem uses a code for a role: the rewards go into its ledgers in the same transaction that counts the
// use. It returns what was given, or ErrCodeUnknown / ErrCodeExpired / ErrCodeUsedUp / ErrCodeTaken.
func (s *Store) Redeem(rid int64, code string, now int64) ([]Attachment, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var c RedeemCode
	var raw string
	err = tx.QueryRow(`SELECT code,attachments,max_uses,used,expires_at FROM redeem_codes WHERE code=?`, NormalizeCode(code)).
		Scan(&c.Code, &raw, &c.MaxUses, &c.Used, &c.ExpiresAt)
	if wrapIsNoRows(err) {
		return nil, ErrCodeUnknown
	}
	if err != nil {
		return nil, err
	}
	switch {
	case c.ExpiresAt > 0 && c.ExpiresAt <= now:
		return nil, ErrCodeExpired
	case c.MaxUses > 0 && c.Used >= c.MaxUses:
		return nil, ErrCodeUsedUp
	}
	if _, err := tx.Exec(`INSERT INTO redeem_uses(code,rid,used_at) VALUES(?,?,?)`, c.Code, rid, now); err != nil {
		if isUnique(err) {
			return nil, ErrCodeTaken
		}
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE redeem_codes SET used=used+1 WHERE code=?`, c.Code); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &c.Attachments); err != nil {
		return nil, err
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	for _, a := range c.Attachments {
		if a.Kind == AttachAsset {
			assets[a.ItemID] += a.Num
		} else {
			items[a.ItemID] += a.Num
		}
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return nil, err
	}
	return c.Attachments, tx.Commit()
}

// Report is a player's report of another one (12770).
type Report struct {
	ID, RID, TargetRID int64
	TargetSrvID        string
	Kind               int
	Msg, History       string
	CreatedAt          int64
}

// AddReport stores a report.
func (s *Store) AddReport(r Report) error {
	_, err := s.db.Exec(`INSERT INTO role_reports(rid,target_rid,target_srv_id,kind,msg,history,created_at) VALUES(?,?,?,?,?,?,?)`,
		r.RID, r.TargetRID, r.TargetSrvID, r.Kind, r.Msg, r.History, s.unix())
	return err
}

// Reports lists the reports, newest first, at most limit.
func (s *Store) Reports(limit int) ([]Report, error) {
	rows, err := s.db.Query(`SELECT id,rid,target_rid,target_srv_id,kind,msg,history,created_at FROM role_reports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.RID, &r.TargetRID, &r.TargetSrvID, &r.Kind, &r.Msg, &r.History, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WorldLevel is the average level of the highest-level roles of a server (at most 10), 0 without roles.
func (s *Store) WorldLevel(srvID string) (int, error) {
	rows, err := s.db.Query(`SELECT lev FROM roles WHERE srv_id=? AND status=1 ORDER BY lev DESC LIMIT 10`, srvID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	sum, n := 0, 0
	for rows.Next() {
		var l int
		if err := rows.Scan(&l); err != nil {
			return 0, err
		}
		sum, n = sum+l, n+1
	}
	if n == 0 {
		return 0, rows.Err()
	}
	return sum / n, rows.Err()
}
