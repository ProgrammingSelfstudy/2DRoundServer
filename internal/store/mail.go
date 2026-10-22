package store

import (
	"database/sql"
	"errors"
)

const (
	MailDirect    = "direct"
	MailBroadcast = "broadcast"

	AttachAsset = "asset"
	AttachItem  = "item"

	MaxListedMails = 50 // the client's own MailModel.MAX_COUNT
)

var (
	ErrNoAttachments = errors.New("store: mail has no attachments to claim")
	ErrNotDeletable  = errors.New("store: mail cannot be deleted yet")
)

type Attachment struct {
	Kind   string `json:"kind"`
	ItemID int64  `json:"item_id"` // asset label or item base id
	Num    int64  `json:"num"`
}

type Mail struct {
	ID          int64        `json:"id"`
	SrvID       string       `json:"srv_id"` // '' = every server (broadcast only)
	Scope       string       `json:"scope"`
	Type        int          `json:"type"`
	Subject     string       `json:"subject"`
	Content     string       `json:"content"`
	FromName    string       `json:"from_name"`
	CreatedBy   string       `json:"created_by"`
	CreatedAt   int64        `json:"created_at"`
	ExpireAt    int64        `json:"expire_at"` // 0 = never
	Attachments []Attachment `json:"attachments"`
}

// RoleMail is a mail as one role sees it.
type RoleMail struct {
	Mail
	ReadAt    int64 `json:"read_at"`
	ClaimedAt int64 `json:"claimed_at"`
}

// Status is the client's code: 0 unread, 1 read, 2 attachments claimed.
func (m RoleMail) Status() int {
	switch {
	case m.ClaimedAt > 0:
		return 2
	case m.ReadAt > 0:
		return 1
	}
	return 0
}

// HasItems is true while there is something left to claim.
func (m RoleMail) HasItems() bool { return len(m.Attachments) > 0 && m.ClaimedAt == 0 }

// CreateMail stores a mail. A direct mail is delivered to rids at once; a
// broadcast mail is delivered lazily (rids must be empty).
func (s *Store) CreateMail(m Mail, rids []int64) (*Mail, error) {
	if m.Scope == "" {
		m.Scope = MailDirect
	}
	if m.Scope == MailDirect && len(rids) == 0 {
		return nil, errors.New("store: a direct mail needs recipients")
	}
	if m.Scope == MailBroadcast && len(rids) != 0 {
		return nil, errors.New("store: a broadcast mail has no explicit recipients")
	}
	if m.Type == 0 {
		m.Type = 1
	}
	if m.FromName == "" {
		m.FromName = "GM"
	}
	m.CreatedAt = s.unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO mails(srv_id,subject,content,from_name,created_by,created_at,expire_at,scope,type) VALUES(?,?,?,?,?,?,?,?,?)`,
		m.SrvID, m.Subject, m.Content, m.FromName, m.CreatedBy, m.CreatedAt, m.ExpireAt, m.Scope, m.Type)
	if err != nil {
		return nil, err
	}
	m.ID, _ = res.LastInsertId()
	for _, a := range m.Attachments {
		if a.Num <= 0 || (a.Kind != AttachAsset && a.Kind != AttachItem) {
			return nil, errors.New("store: bad attachment")
		}
		if _, err := tx.Exec(`INSERT INTO mail_attachments(mail_id,item_id,num,kind) VALUES(?,?,?,?)`, m.ID, a.ItemID, a.Num, a.Kind); err != nil {
			return nil, err
		}
	}
	for _, rid := range rids {
		if _, err := tx.Exec(`INSERT INTO role_mails(mail_id,rid) VALUES(?,?)`, m.ID, rid); err != nil {
			return nil, err
		}
	}
	return &m, tx.Commit()
}

func (s *Store) attachmentsOf(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, mailID int64) ([]Attachment, error) {
	rows, err := q.Query(`SELECT kind,item_id,num FROM mail_attachments WHERE mail_id=? ORDER BY id`, mailID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.Kind, &a.ItemID, &a.Num); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const roleMailCols = `m.id,m.srv_id,m.scope,m.type,m.subject,m.content,m.from_name,m.created_by,m.created_at,m.expire_at,rm.read_at,rm.claimed_at`

func scanRoleMail(sc interface{ Scan(...any) error }) (RoleMail, error) {
	var r RoleMail
	err := sc.Scan(&r.ID, &r.SrvID, &r.Scope, &r.Type, &r.Subject, &r.Content, &r.FromName, &r.CreatedBy, &r.CreatedAt, &r.ExpireAt, &r.ReadAt, &r.ClaimedAt)
	return r, err
}

// DeliverBroadcasts creates the role_mails rows a role is due: broadcast mails
// for its server (or every server) sent after the role was created and not
// expired yet. Rows that exist (even deleted ones) are left alone.
func (s *Store) DeliverBroadcasts(role *Role) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO role_mails(mail_id,rid)
		SELECT m.id, ? FROM mails m
		WHERE m.scope='broadcast' AND (m.srv_id='' OR m.srv_id=?) AND m.created_at >= ?
		  AND (m.expire_at=0 OR m.expire_at > ?)`, role.RID, role.SrvID, role.CreatedAt, s.unix())
	return err
}

// MailsForRole lists the role's mails, newest first, at most MaxListedMails,
// after delivering any broadcast it is due. Attachments are filled in.
func (s *Store) MailsForRole(role *Role) ([]RoleMail, error) {
	if err := s.DeliverBroadcasts(role); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+roleMailCols+` FROM role_mails rm JOIN mails m ON m.id=rm.mail_id
		WHERE rm.rid=? AND rm.deleted_at=0 ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, role.RID, MaxListedMails)
	if err != nil {
		return nil, err
	}
	var out []RoleMail
	for rows.Next() {
		r, err := scanRoleMail(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Attachments, err = s.attachmentsOf(s.db, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MailForRole returns one mail of the role, or ErrNotFound (also for deleted mails).
func (s *Store) MailForRole(rid, mailID int64) (*RoleMail, error) {
	r, err := scanRoleMail(s.db.QueryRow(`SELECT `+roleMailCols+` FROM role_mails rm JOIN mails m ON m.id=rm.mail_id
		WHERE rm.rid=? AND rm.mail_id=? AND rm.deleted_at=0`, rid, mailID))
	if err != nil {
		return nil, wrapNoRows(err)
	}
	if r.Attachments, err = s.attachmentsOf(s.db, r.ID); err != nil {
		return nil, err
	}
	return &r, nil
}

// MarkMailRead sets read_at once and returns it (an earlier read time is kept).
func (s *Store) MarkMailRead(rid, mailID int64) (int64, error) {
	now := s.unix()
	if _, err := s.db.Exec(`UPDATE role_mails SET read_at=? WHERE rid=? AND mail_id=? AND deleted_at=0 AND read_at=0`, now, rid, mailID); err != nil {
		return 0, err
	}
	var at int64
	err := s.db.QueryRow(`SELECT read_at FROM role_mails WHERE rid=? AND mail_id=? AND deleted_at=0`, rid, mailID).Scan(&at)
	return at, wrapNoRows(err)
}

// ClaimMail moves a mail's attachments into the role's ledgers, once. It marks
// the mail read and claimed in the same transaction. ErrNoAttachments covers a
// mail with nothing to take and one already claimed.
func (s *Store) ClaimMail(rid, mailID int64) (claimed []Attachment, claimedAt int64, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	claimed, claimedAt, err = s.claimTx(tx, rid, mailID)
	if err != nil {
		return nil, 0, err
	}
	return claimed, claimedAt, tx.Commit()
}

func (s *Store) claimTx(tx *sql.Tx, rid, mailID int64) ([]Attachment, int64, error) {
	now := s.unix()
	// The UPDATE only matches an unclaimed row, so two concurrent claims cannot both succeed.
	res, err := tx.Exec(`UPDATE role_mails SET claimed_at=?, read_at=CASE WHEN read_at=0 THEN ? ELSE read_at END
		WHERE rid=? AND mail_id=? AND deleted_at=0 AND claimed_at=0
		  AND EXISTS (SELECT 1 FROM mail_attachments a WHERE a.mail_id=role_mails.mail_id)`, now, now, rid, mailID)
	if err != nil {
		return nil, 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM role_mails WHERE rid=? AND mail_id=? AND deleted_at=0`, rid, mailID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		if err != nil {
			return nil, 0, err
		}
		return nil, 0, ErrNoAttachments
	}
	atts, err := s.attachmentsOf(tx, mailID)
	if err != nil {
		return nil, 0, err
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	for _, a := range atts {
		if a.Kind == AttachAsset {
			assets[a.ItemID] += a.Num
		} else {
			items[a.ItemID] += a.Num
		}
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return nil, 0, err
	}
	return atts, now, nil
}

// ClaimedMail is one mail taken by ClaimAllMails.
type ClaimedMail struct {
	ID        int64
	ClaimedAt int64
	Taken     []Attachment
}

// ClaimAllMails claims every mail of the role that still has attachments.
func (s *Store) ClaimAllMails(role *Role) ([]ClaimedMail, error) {
	mails, err := s.MailsForRole(role)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var out []ClaimedMail
	for _, m := range mails {
		if !m.HasItems() {
			continue
		}
		taken, at, err := s.claimTx(tx, role.RID, m.ID)
		if errors.Is(err, ErrNoAttachments) || errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, ClaimedMail{ID: m.ID, ClaimedAt: at, Taken: taken})
	}
	return out, tx.Commit()
}

// DeleteMails soft-deletes the given mails of a role and returns the ids that
// were deleted. A mail with attachments still to claim is kept, as is an unread
// one (the client only offers deleting read mails that have nothing left).
func (s *Store) DeleteMails(rid int64, ids []int64) ([]int64, error) {
	var done []int64
	now := s.unix()
	for _, id := range ids {
		res, err := s.db.Exec(`UPDATE role_mails SET deleted_at=? WHERE rid=? AND mail_id=? AND deleted_at=0 AND read_at>0
			AND (claimed_at>0 OR NOT EXISTS (SELECT 1 FROM mail_attachments a WHERE a.mail_id=role_mails.mail_id))`, now, rid, id)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			done = append(done, id)
		}
	}
	return done, nil
}

// RoleAssets returns the asset ledger (label -> amount).
func (s *Store) RoleAssets(rid int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT label,val FROM role_assets WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var l, v int64
		if err := rows.Scan(&l, &v); err != nil {
			return nil, err
		}
		out[l] = v
	}
	return out, rows.Err()
}

// RoleItems returns the item ledger (item base id -> count).
func (s *Store) RoleItems(rid int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT item_id,num FROM role_items WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var i, n int64
		if err := rows.Scan(&i, &n); err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, rows.Err()
}

// DirectRecipients returns the roles a direct mail was delivered to.
func (s *Store) DirectRecipients(mailID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT rid FROM role_mails WHERE mail_id=? AND deleted_at=0`, mailID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MailByID returns a mail with its attachments (no per-role state).
func (s *Store) MailByID(id int64) (*Mail, error) {
	var m Mail
	err := s.db.QueryRow(`SELECT id,srv_id,scope,type,subject,content,from_name,created_by,created_at,expire_at FROM mails WHERE id=?`, id).
		Scan(&m.ID, &m.SrvID, &m.Scope, &m.Type, &m.Subject, &m.Content, &m.FromName, &m.CreatedBy, &m.CreatedAt, &m.ExpireAt)
	if err != nil {
		return nil, wrapNoRows(err)
	}
	m.Attachments, err = s.attachmentsOf(s.db, m.ID)
	return &m, err
}

// SentMail is a mail with how many roles it reached, for the admin list.
type SentMail struct {
	Mail
	Recipients int `json:"recipients"` // role_mails rows so far (a broadcast fills in lazily)
	Claimed    int `json:"claimed"`
}

// ListMails returns the mails ever sent, newest first.
func (s *Store) ListMails(limit, offset int) ([]SentMail, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM mails`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT m.id,m.srv_id,m.scope,m.type,m.subject,m.content,m.from_name,m.created_by,m.created_at,m.expire_at,
		(SELECT COUNT(*) FROM role_mails r WHERE r.mail_id=m.id),
		(SELECT COUNT(*) FROM role_mails r WHERE r.mail_id=m.id AND r.claimed_at>0)
		FROM mails m ORDER BY m.id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var out []SentMail
	for rows.Next() {
		var m SentMail
		if err := rows.Scan(&m.ID, &m.SrvID, &m.Scope, &m.Type, &m.Subject, &m.Content, &m.FromName, &m.CreatedBy, &m.CreatedAt, &m.ExpireAt, &m.Recipients, &m.Claimed); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		if out[i].Attachments, err = s.attachmentsOf(s.db, out[i].ID); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// ItemSale is a number of pieces of one item taken out of a role's ledger.
type ItemSale struct{ ItemID, Num int64 }

// SellItems removes the sold items and adds the gains in one transaction. It
// returns ErrConflict, changing nothing, when the role does not hold that many
// of an item. A gain is an asset (currency, by label) or an item (by base id).
func (s *Store) SellItems(rid int64, sold []ItemSale, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range sold {
		res, err := tx.Exec(`UPDATE role_items SET num=num-? WHERE rid=? AND item_id=? AND num>=?`, it.Num, rid, it.ItemID, it.Num)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrConflict
		}
	}
	if _, err := tx.Exec(`DELETE FROM role_items WHERE rid=? AND num<=0`, rid); err != nil {
		return err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// creditTx adds currencies (by label) and items (by base id) to a role's ledgers.
// Experience (the level rule's label) is turned into levels as it comes in.
func creditTx(tx *sql.Tx, rid int64, assets, items map[int64]int64) error {
	if err := creditPlainTx(tx, rid, assets, items); err != nil {
		return err
	}
	if r := levelRule.Load(); r != nil && assets[r.ExpLabel] > 0 {
		return levelUpTx(tx, rid, r)
	}
	return nil
}

func creditPlainTx(tx *sql.Tx, rid int64, assets, items map[int64]int64) error {
	for label, n := range assets {
		if _, err := tx.Exec(`INSERT INTO role_assets(rid,label,val) VALUES(?,?,?) ON CONFLICT(rid,label) DO UPDATE SET val=val+excluded.val`, rid, label, n); err != nil {
			return err
		}
	}
	for id, n := range items {
		if _, err := tx.Exec(`INSERT INTO role_items(rid,item_id,num) VALUES(?,?,?) ON CONFLICT(rid,item_id) DO UPDATE SET num=num+excluded.num`, rid, id, n); err != nil {
			return err
		}
	}
	return nil
}
