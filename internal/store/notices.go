package store

type Notice struct {
	ID        int64  `json:"id"`
	SrvID     string `json:"srv_id"` // '' = every server
	Title     string `json:"title"`
	Content   string `json:"content"`
	StartAt   int64  `json:"start_at"`
	EndAt     int64  `json:"end_at"` // 0 = never ends
	Status    int    `json:"status"` // 1 shown, 0 hidden
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

const noticeCols = `id,srv_id,title,content,start_at,end_at,status,created_by,created_at`

func scanNotice(sc interface{ Scan(...any) error }) (*Notice, error) {
	var n Notice
	err := sc.Scan(&n.ID, &n.SrvID, &n.Title, &n.Content, &n.StartAt, &n.EndAt, &n.Status, &n.CreatedBy, &n.CreatedAt)
	return &n, wrapNoRows(err)
}

func (s *Store) CreateNotice(n Notice) (*Notice, error) {
	n.CreatedAt = s.unix()
	res, err := s.db.Exec(`INSERT INTO notices(srv_id,title,content,start_at,end_at,status,created_by,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		n.SrvID, n.Title, n.Content, n.StartAt, n.EndAt, n.Status, n.CreatedBy, n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.ID, _ = res.LastInsertId()
	return &n, nil
}

func (s *Store) Notice(id int64) (*Notice, error) {
	return scanNotice(s.db.QueryRow(`SELECT `+noticeCols+` FROM notices WHERE id=?`, id))
}

func (s *Store) UpdateNotice(n Notice) error {
	res, err := s.db.Exec(`UPDATE notices SET srv_id=?,title=?,content=?,start_at=?,end_at=?,status=? WHERE id=?`,
		n.SrvID, n.Title, n.Content, n.StartAt, n.EndAt, n.Status, n.ID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteNotice(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notices WHERE id=?`, id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListNotices(limit, offset int) ([]Notice, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notices`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+noticeCols+` FROM notices ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *n)
	}
	return out, total, rows.Err()
}

// NoticeView is a notice as one role sees it.
type NoticeView struct {
	Notice
	Read bool `json:"read"`
}

// MaxListedNotices bounds one 10950 reply.
const MaxListedNotices = 50

// A role sees a notice that is shown, has started, has not ended, and is for every
// server or for the role's own.
const visibleToRole = `n.status=1 AND n.start_at <= ? AND (n.end_at=0 OR n.end_at > ?) AND (n.srv_id='' OR n.srv_id=?)`

func scanNoticeView(sc interface{ Scan(...any) error }) (NoticeView, error) {
	var v NoticeView
	var read int
	err := sc.Scan(&v.ID, &v.SrvID, &v.Title, &v.Content, &v.StartAt, &v.EndAt, &v.Status, &v.CreatedBy, &v.CreatedAt, &read)
	v.Read = read != 0
	return v, err
}

const noticeViewCols = `n.id,n.srv_id,n.title,n.content,n.start_at,n.end_at,n.status,n.created_by,n.created_at,(r.read_at IS NOT NULL)`

// NoticesForRole lists the notices the role may see, newest start first.
func (s *Store) NoticesForRole(role *Role) ([]NoticeView, error) {
	now := s.unix()
	rows, err := s.db.Query(`SELECT `+noticeViewCols+` FROM notices n
		LEFT JOIN role_notice_reads r ON r.notice_id=n.id AND r.rid=?
		WHERE `+visibleToRole+` ORDER BY n.start_at DESC, n.id DESC LIMIT ?`, role.RID, now, now, role.SrvID, MaxListedNotices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoticeView
	for rows.Next() {
		v, err := scanNoticeView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// NoticeForRole returns one notice if the role may see it, else ErrNotFound.
func (s *Store) NoticeForRole(role *Role, id int64) (*NoticeView, error) {
	now := s.unix()
	v, err := scanNoticeView(s.db.QueryRow(`SELECT `+noticeViewCols+` FROM notices n
		LEFT JOIN role_notice_reads r ON r.notice_id=n.id AND r.rid=?
		WHERE n.id=? AND `+visibleToRole, role.RID, id, now, now, role.SrvID))
	if err != nil {
		return nil, wrapNoRows(err)
	}
	return &v, nil
}

// MarkNoticeRead records that the role opened the notice (the first time wins).
func (s *Store) MarkNoticeRead(rid, noticeID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO role_notice_reads(rid,notice_id,read_at) VALUES(?,?,?)`, rid, noticeID, s.unix())
	return err
}
