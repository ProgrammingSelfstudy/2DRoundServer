package store

import "fmt"

// Kinds of cosmetics a role can own (role_looks.kind).
const (
	LookFace     = "face"
	LookBackdrop = "backdrop"
	LookAvatar   = "avatar" // avatar frame
	LookBubble   = "bubble" // chat bubble
	LookTitle    = "title"
)

// RoleSettings are a role's personal settings; the zero value is what a role starts with.
type RoleSettings struct {
	Signature                               string
	ShowVIP, AutoPK                         int
	BackdropID, AvatarID, BubbleID, TitleID int64
}

// lookColumn maps a kind to the settings column that holds the one in use.
var lookColumn = map[string]string{LookBackdrop: "backdrop_id", LookAvatar: "avatar_id", LookBubble: "bubble_id", LookTitle: "title_id"}

// What a new role starts with, from the 10301 of a level-1 role of the original server (capture
// original-2026-10-22): the free frame "冒险徽章" is in use and auto fight is on.
const (
	DefaultAvatarID = 1000
	DefaultAutoPK   = 1
)

// SettingsOf reads a role's settings (the defaults when it never changed any).
func (s *Store) SettingsOf(rid int64) (RoleSettings, error) {
	var v RoleSettings
	err := s.db.QueryRow(`SELECT signature,show_vip,auto_pk,backdrop_id,avatar_id,bubble_id,title_id FROM role_settings WHERE rid=?`, rid).
		Scan(&v.Signature, &v.ShowVIP, &v.AutoPK, &v.BackdropID, &v.AvatarID, &v.BubbleID, &v.TitleID)
	if wrapIsNoRows(err) {
		return RoleSettings{AutoPK: DefaultAutoPK, AvatarID: DefaultAvatarID}, nil
	}
	return v, err
}

// setSetting writes one column of the role's settings row, creating the row. col comes from the
// code, never from a request.
func (s *Store) setSetting(rid int64, col string, val any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO role_settings(rid,auto_pk,avatar_id) VALUES(?,?,?) ON CONFLICT(rid) DO NOTHING`, rid, DefaultAutoPK, DefaultAvatarID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE role_settings SET %s=? WHERE rid=?`, col), val, rid); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureLook gives the role a cosmetic for good unless it already has a row for it.
func (s *Store) EnsureLook(rid int64, kind string, id int64) error {
	_, err := s.db.Exec(`INSERT INTO role_looks(rid,kind,base_id,end_time) VALUES(?,?,?,0) ON CONFLICT(rid,kind,base_id) DO NOTHING`, rid, kind, id)
	return err
}

func (s *Store) SetSignature(rid int64, v string) error { return s.setSetting(rid, "signature", v) }
func (s *Store) SetShowVIP(rid int64, v int) error      { return s.setSetting(rid, "show_vip", v) }
func (s *Store) SetAutoPK(rid int64, v int) error       { return s.setSetting(rid, "auto_pk", v) }

// UseLook sets the cosmetic of that kind the role uses (0 = none). ErrConflict when the role does
// not own it (or it ran out at now) and the id is not 0.
func (s *Store) UseLook(rid int64, kind string, id, now int64) error {
	col, ok := lookColumn[kind]
	if !ok {
		return fmt.Errorf("store: kind %q has no slot", kind)
	}
	if id != 0 {
		owned, err := s.OwnsLook(rid, kind, id, now)
		if err != nil {
			return err
		}
		if !owned {
			return ErrConflict
		}
	}
	return s.setSetting(rid, col, id)
}

// Look is a cosmetic a role owns; EndTime 0 is for good.
type Look struct{ ID, EndTime int64 }

// LooksOf lists what the role owns of a kind and that has not run out at now.
func (s *Store) LooksOf(rid int64, kind string, now int64) ([]Look, error) {
	rows, err := s.db.Query(`SELECT base_id,end_time FROM role_looks WHERE rid=? AND kind=? AND (end_time=0 OR end_time>?) ORDER BY base_id`, rid, kind, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Look
	for rows.Next() {
		var l Look
		if err := rows.Scan(&l.ID, &l.EndTime); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// OwnsLook reports whether the role owns the cosmetic and it has not run out at now.
func (s *Store) OwnsLook(rid int64, kind string, id, now int64) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM role_looks WHERE rid=? AND kind=? AND base_id=? AND (end_time=0 OR end_time>?)`, rid, kind, id, now).Scan(&one)
	if wrapIsNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// GrantLook gives the role a cosmetic until endTime (0 = for good); granting one it owns sets the
// new end time.
func (s *Store) GrantLook(rid int64, kind string, id, endTime int64) error {
	_, err := s.db.Exec(`INSERT INTO role_looks(rid,kind,base_id,end_time) VALUES(?,?,?,?) ON CONFLICT(rid,kind,base_id) DO UPDATE SET end_time=excluded.end_time`, rid, kind, id, endTime)
	return err
}

// ActivateLook gives the role a cosmetic it does not own (or whose time ran out at now) for what
// it costs, in one transaction. ErrConflict, changing nothing, when it owns it still or cannot pay.
func (s *Store) ActivateLook(rid int64, kind string, id, now, endTime int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	err = tx.QueryRow(`SELECT 1 FROM role_looks WHERE rid=? AND kind=? AND base_id=? AND (end_time=0 OR end_time>?)`, rid, kind, id, now).Scan(&one)
	if err == nil {
		return ErrConflict
	}
	if !wrapIsNoRows(err) {
		return err
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO role_looks(rid,kind,base_id,end_time) VALUES(?,?,?,?) ON CONFLICT(rid,kind,base_id) DO UPDATE SET end_time=excluded.end_time`, rid, kind, id, endTime); err != nil {
		return err
	}
	return tx.Commit()
}
