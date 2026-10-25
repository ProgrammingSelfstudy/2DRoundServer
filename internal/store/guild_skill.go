package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// GuildSkills is everything a role has put into guild skills, by career (2 mage, 3 warrior, 4 tank, 5 support).
type GuildSkills struct {
	PveDone   map[int]int         // heavens finished
	PveActive map[int][]int64     // skills activated in the current heaven, in order
	PvpAttrs  map[int]map[int]int // career -> attribute id -> level; present once the career broke through
	PvpLev    map[int]int         // career -> PvP skill level
	PveReset  bool                // the cheap first reset of the ordinary skills was used
	PvpReset  bool                // the same for the PvP skills
}

type skillQuerier interface {
	Query(q string, args ...any) (*sql.Rows, error)
	QueryRow(q string, args ...any) *sql.Row
}

func loadGuildSkills(q skillQuerier, rid int64) (*GuildSkills, error) {
	g := &GuildSkills{PveDone: map[int]int{}, PveActive: map[int][]int64{}, PvpAttrs: map[int]map[int]int{}, PvpLev: map[int]int{}}
	rows, err := q.Query(`SELECT career,done,active FROM guild_skill_pve WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c, done int
		var raw string
		if err := rows.Scan(&c, &done, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var active []int64
		if err := json.Unmarshal([]byte(raw), &active); err != nil {
			rows.Close()
			return nil, err
		}
		g.PveDone[c], g.PveActive[c] = done, active
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = q.Query(`SELECT career,attrs,skill_lev FROM guild_skill_pvp WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c, lev int
		var raw string
		if err := rows.Scan(&c, &raw, &lev); err != nil {
			rows.Close()
			return nil, err
		}
		attrs := map[int]int{}
		if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
			rows.Close()
			return nil, err
		}
		g.PvpAttrs[c], g.PvpLev[c] = attrs, lev
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var pve, pvp int
	switch err := q.QueryRow(`SELECT pve_reset_used,pvp_reset_used FROM guild_skill_flags WHERE rid=?`, rid).Scan(&pve, &pvp); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	}
	g.PveReset, g.PvpReset = pve == 1, pvp == 1
	return g, nil
}

// GuildSkillsOf reads a role's guild skills (all empty for a role that never used them).
func (s *Store) GuildSkillsOf(rid int64) (*GuildSkills, error) { return loadGuildSkills(s.db, rid) }

// UpdateGuildSkills lets fn change a role's guild skills and say what that costs and gives back; the
// change, the payment and the refund happen in one transaction. ErrConflict when the role cannot pay;
// an error from fn undoes everything and is returned as it is.
func (s *Store) UpdateGuildSkills(rid int64, fn func(*GuildSkills) (spendA, spendI, gainA, gainI map[int64]int64, err error)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	g, err := loadGuildSkills(tx, rid)
	if err != nil {
		return err
	}
	spendA, spendI, gainA, gainI, err := fn(g)
	if err != nil {
		return err
	}
	if err := spendTx(tx, rid, spendA, spendI); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainA, gainI); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guild_skill_pve WHERE rid=?`, rid); err != nil {
		return err
	}
	for c, done := range g.PveDone {
		active := g.PveActive[c]
		if active == nil {
			active = []int64{}
		}
		raw, _ := json.Marshal(active)
		if _, err := tx.Exec(`INSERT INTO guild_skill_pve(rid,career,done,active) VALUES(?,?,?,?)`, rid, c, done, string(raw)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM guild_skill_pvp WHERE rid=?`, rid); err != nil {
		return err
	}
	for c, attrs := range g.PvpAttrs {
		raw, _ := json.Marshal(attrs)
		if _, err := tx.Exec(`INSERT INTO guild_skill_pvp(rid,career,attrs,skill_lev) VALUES(?,?,?,?)`, rid, c, string(raw), g.PvpLev[c]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO guild_skill_flags(rid,pve_reset_used,pvp_reset_used) VALUES(?,?,?)
		ON CONFLICT(rid) DO UPDATE SET pve_reset_used=excluded.pve_reset_used, pvp_reset_used=excluded.pvp_reset_used`, rid, b2i(g.PveReset), b2i(g.PvpReset)); err != nil {
		return err
	}
	return tx.Commit()
}
