package store

type Server struct {
	SrvID       string `json:"srv_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	ZoneID      int    `json:"zone_id"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	WSURL       string `json:"ws_url"`
	Status      int    `json:"status"` // 1 open, 0 maintenance
	IsNew       bool   `json:"is_new"`
	IsRecommend bool   `json:"is_recommend"`
	OpenTime    int64  `json:"open_time"`
	MainSrvID   string `json:"main_srv_id"`
	GroupID     int    `json:"group_id"`
	CreatedAt   int64  `json:"created_at"`
}

const serverCols = `srv_id,name,platform,zone_id,host,port,ws_url,status,is_new,is_recommend,open_time,main_srv_id,group_id,created_at`

func scanServer(sc interface{ Scan(...any) error }) (*Server, error) {
	var v Server
	var isNew, isRec int
	err := sc.Scan(&v.SrvID, &v.Name, &v.Platform, &v.ZoneID, &v.Host, &v.Port, &v.WSURL, &v.Status,
		&isNew, &isRec, &v.OpenTime, &v.MainSrvID, &v.GroupID, &v.CreatedAt)
	v.IsNew, v.IsRecommend = isNew != 0, isRec != 0
	return &v, wrapNoRows(err)
}

// UpsertServer inserts the server or updates every field except created_at.
func (s *Store) UpsertServer(v Server) error {
	_, err := s.db.Exec(`INSERT INTO servers(`+serverCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(srv_id) DO UPDATE SET name=excluded.name, platform=excluded.platform, zone_id=excluded.zone_id,
		host=excluded.host, port=excluded.port, ws_url=excluded.ws_url, status=excluded.status, is_new=excluded.is_new,
		is_recommend=excluded.is_recommend, open_time=excluded.open_time, main_srv_id=excluded.main_srv_id, group_id=excluded.group_id`,
		v.SrvID, v.Name, v.Platform, v.ZoneID, v.Host, v.Port, v.WSURL, v.Status,
		b2i(v.IsNew), b2i(v.IsRecommend), v.OpenTime, v.MainSrvID, v.GroupID, s.unix())
	return err
}

// EnsureServer creates the server only if it does not exist yet (never overwrites admin edits).
func (s *Store) EnsureServer(v Server) error {
	if _, err := s.Server(v.SrvID); err == nil {
		return nil
	} else if err != ErrNotFound {
		return err
	}
	return s.UpsertServer(v)
}

func (s *Store) Server(id string) (*Server, error) {
	return scanServer(s.db.QueryRow(`SELECT `+serverCols+` FROM servers WHERE srv_id=?`, id))
}

func (s *Store) ListServers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT ` + serverCols + ` FROM servers ORDER BY group_id, zone_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		v, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) DeleteServer(id string) error {
	r, err := s.db.Exec(`DELETE FROM servers WHERE srv_id=?`, id)
	if err != nil {
		if isForeignKey(err) {
			return ErrConflict // server still has roles
		}
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
