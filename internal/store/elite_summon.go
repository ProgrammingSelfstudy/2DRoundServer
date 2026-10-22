package store

import (
	"database/sql"
	"errors"
)

// EliteSummon is the saved state of a role's elite summon.
type EliteSummon struct {
	Lucky, Times, Since int64
	Claimed             string // award ids, comma separated
}

// EliteSummon returns the role's elite summon (all zero when it never used it).
func (s *Store) EliteSummon(rid int64) (EliteSummon, error) {
	var e EliteSummon
	err := s.db.QueryRow(`SELECT lucky,times,since,claimed FROM elite_summon WHERE rid=?`, rid).Scan(&e.Lucky, &e.Times, &e.Since, &e.Claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return EliteSummon{}, nil
	}
	return e, err
}

// SaveEliteSummon replaces the role's elite summon.
func (s *Store) SaveEliteSummon(rid int64, e EliteSummon) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveEliteTx(tx, rid, e); err != nil {
		return err
	}
	return tx.Commit()
}

func saveEliteTx(tx *sql.Tx, rid int64, e EliteSummon) error {
	_, err := tx.Exec(`INSERT INTO elite_summon(rid,lucky,times,since,claimed) VALUES(?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET lucky=excluded.lucky, times=excluded.times, since=excluded.since, claimed=excluded.claimed`,
		rid, e.Lucky, e.Times, e.Since, e.Claimed)
	return err
}

// ClaimEliteAward gives the reward of a cumulative award once: it records the award as
// claimed in claimed (the state the caller read) and credits the gains, in one transaction.
// ErrConflict, changing nothing, when the state moved on since it was read.
func (s *Store) ClaimEliteAward(rid int64, old, next EliteSummon, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE elite_summon SET claimed=? WHERE rid=? AND claimed=? AND times=?`, next.Claimed, rid, old.Claimed, old.Times)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
