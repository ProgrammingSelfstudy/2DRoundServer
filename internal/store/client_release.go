package store

import (
	"database/sql"
	"strings"
)

const (
	ReleaseDraft      = 0
	ReleasePublished  = 1
	ReleaseRolledBack = 2
)

// ClientRelease is one bundle of client resources, described by the manifest the pipeline produced.
type ClientRelease struct {
	ID           int64  `json:"id"`
	Platform     string `json:"platform"`
	Version      string `json:"version"`
	ManifestURL  string `json:"manifest_url"`
	ManifestHash string `json:"manifest_hash"`
	Status       int    `json:"status"`
	Whitelist    string `json:"whitelist"`
	Note         string `json:"note"`
	CreatedAt    int64  `json:"created_at"`
	PublishedAt  int64  `json:"published_at"`
}

const releaseCols = `id,platform,version,manifest_url,manifest_hash,status,whitelist,note,created_at,published_at`

func scanRelease(sc interface{ Scan(...any) error }) (*ClientRelease, error) {
	var r ClientRelease
	err := sc.Scan(&r.ID, &r.Platform, &r.Version, &r.ManifestURL, &r.ManifestHash, &r.Status, &r.Whitelist, &r.Note, &r.CreatedAt, &r.PublishedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &r, err
}

// CreateClientRelease stores a draft; a platform cannot have the same version twice.
func (s *Store) CreateClientRelease(r ClientRelease, now int64) (*ClientRelease, error) {
	res, err := s.db.Exec(`INSERT INTO client_release(platform,version,manifest_url,manifest_hash,whitelist,note,created_at) VALUES(?,?,?,?,?,?,?)`,
		r.Platform, r.Version, r.ManifestURL, r.ManifestHash, r.Whitelist, r.Note, now)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	r.ID, _ = res.LastInsertId()
	r.Status, r.CreatedAt = ReleaseDraft, now
	return &r, nil
}

// ClientReleases lists the newest releases, optionally of one platform.
func (s *Store) ClientReleases(platform string, limit int) ([]ClientRelease, error) {
	q, args := `SELECT `+releaseCols+` FROM client_release`, []any{}
	if platform != "" {
		q += ` WHERE platform=?`
		args = append(args, platform)
	}
	rows, err := s.db.Query(q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientRelease{}
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// SetClientReleaseStatus publishes (stamping the time) or rolls back a release.
func (s *Store) SetClientReleaseStatus(id int64, status int, now int64) error {
	var res sql.Result
	var err error
	if status == ReleasePublished {
		res, err = s.db.Exec(`UPDATE client_release SET status=?, published_at=? WHERE id=?`, status, now, id)
	} else {
		res, err = s.db.Exec(`UPDATE client_release SET status=? WHERE id=?`, status, id)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetClientReleaseWhitelist changes who may see a release; empty means everyone.
func (s *Store) SetClientReleaseWhitelist(id int64, whitelist string) error {
	res, err := s.db.Exec(`UPDATE client_release SET whitelist=? WHERE id=?`, whitelist, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LiveClientRelease is the newest published release of a platform that the account may see: an open one, or one whose whitelist
// names the account. Returns ErrNotFound when there is none.
func (s *Store) LiveClientRelease(platform, account string) (*ClientRelease, error) {
	rows, err := s.db.Query(`SELECT `+releaseCols+` FROM client_release WHERE platform=? AND status=? ORDER BY published_at DESC, id DESC`, platform, ReleasePublished)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		if r.Whitelist == "" || whitelisted(r.Whitelist, account) {
			return r, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// ClientConfig reads the settings of a platform: the "*" defaults overlaid by the platform's own rows.
func (s *Store) ClientConfig(platform string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key,value,platform FROM client_config WHERE platform IN ('*',?) ORDER BY platform='*' DESC`, platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v, p string
		if err := rows.Scan(&k, &v, &p); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetClientConfig writes one setting of a platform ("*" for all); an empty value removes it.
func (s *Store) SetClientConfig(platform, key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM client_config WHERE platform=? AND key=?`, platform, key)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO client_config(platform,key,value) VALUES(?,?,?) ON CONFLICT(platform,key) DO UPDATE SET value=excluded.value`, platform, key, value)
	return err
}

func whitelisted(list, account string) bool {
	for _, a := range splitComma(list) {
		if a == account {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	return strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ' ' })
}
