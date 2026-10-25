package store

import (
	"database/sql"
	"errors"
)

// Replay is one kept fight. Meta is what the lists show of it and Data the fight script; both are opaque JSON to
// the store.
type Replay struct {
	ID          int64
	SrvID       string
	CombatType  int64
	ARID, BRID  int64
	Time        int64
	Likes       int64
	Shares      int64
	Plays       int64
	Public      bool
	Meta, Data  string
	IsCollected bool
}

// maxReplays is how many replays the server keeps; the oldest ones go first.
const maxReplays = 5000

// AddReplay keeps a fight and returns its id.
func (s *Store) AddReplay(r Replay) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO replays(srv_id,combat_type,a_rid,b_rid,time,meta,data) VALUES(?,?,?,?,?,?,?)`,
		r.SrvID, r.CombatType, r.ARID, r.BRID, r.Time, r.Meta, r.Data)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	// only the newest ones are kept, except those somebody collected
	_, err = s.db.Exec(`DELETE FROM replays WHERE id <= ? AND id NOT IN (SELECT replay_id FROM replay_collects)`, id-maxReplays)
	return id, err
}

const replayCols = `r.id,r.srv_id,r.combat_type,r.a_rid,r.b_rid,r.time,r.likes,r.shares,r.plays,r.public,r.meta,r.data,
  EXISTS(SELECT 1 FROM replay_collects c WHERE c.replay_id=r.id AND c.rid=?)`

type rowScanner interface{ Scan(...any) error }

func scanReplay(row rowScanner) (*Replay, error) {
	var r Replay
	var pub, col int64
	if err := row.Scan(&r.ID, &r.SrvID, &r.CombatType, &r.ARID, &r.BRID, &r.Time, &r.Likes, &r.Shares, &r.Plays, &pub, &r.Meta, &r.Data, &col); err != nil {
		return nil, err
	}
	r.Public, r.IsCollected = pub != 0, col != 0
	return &r, nil
}

// ReplayByID reads a replay; viewer is the role whose collect mark is filled in. It reports nil when there is none.
func (s *Store) ReplayByID(id, viewer int64) (*Replay, error) {
	r, err := scanReplay(s.db.QueryRow(`SELECT `+replayCols+` FROM replays r WHERE r.id=?`, viewer, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (s *Store) replayList(q string, args ...any) ([]Replay, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Replay
	for rows.Next() {
		r, err := scanReplay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ReplaysOf lists the newest replays a role fought in.
func (s *Store) ReplaysOf(rid int64, limit int) ([]Replay, error) {
	return s.replayList(`SELECT `+replayCols+` FROM replays r WHERE r.a_rid=? OR r.b_rid=? ORDER BY r.id DESC LIMIT ?`, rid, rid, rid, limit)
}

// CollectedReplays lists the replays a role collected, the newest first.
func (s *Store) CollectedReplays(rid int64) ([]Replay, error) {
	return s.replayList(`SELECT `+replayCols+` FROM replays r JOIN replay_collects c ON c.replay_id=r.id WHERE c.rid=? ORDER BY r.id DESC`, rid, rid)
}

// PublicReplays pages the replays shown to everybody. combat 0 = any fight type; byLikes sorts by likes, else the
// newest come first. It also returns how many replays there are in all.
func (s *Store) PublicReplays(viewer, combat int64, byLikes bool, start, num int) ([]Replay, int, error) {
	where, args := `r.public=1`, []any{}
	if combat != 0 {
		where += ` AND r.combat_type=?`
		args = append(args, combat)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM replays r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `r.id DESC`
	if byLikes {
		order = `r.likes DESC, r.id DESC`
	}
	list, err := s.replayList(`SELECT `+replayCols+` FROM replays r WHERE `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(append([]any{viewer}, args...), num, start)...)
	return list, total, err
}

// SetReplayCollect marks or unmarks a replay as collected; it reports false when the replay does not exist.
func (s *Store) SetReplayCollect(rid, id int64, on bool) (bool, error) {
	r, err := s.ReplayByID(id, rid)
	if err != nil || r == nil {
		return false, err
	}
	if on {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO replay_collects(rid,replay_id) VALUES(?,?)`, rid, id)
	} else {
		_, err = s.db.Exec(`DELETE FROM replay_collects WHERE rid=? AND replay_id=?`, rid, id)
	}
	return err == nil, err
}

// BumpReplay adds to the counters of a replay; share also makes it public.
func (s *Store) BumpReplay(id, likes, shares, plays int64) error {
	_, err := s.db.Exec(`UPDATE replays SET likes=likes+?, shares=shares+?, plays=plays+?, public=CASE WHEN ?>0 THEN 1 ELSE public END WHERE id=?`,
		likes, shares, plays, shares, id)
	return err
}
