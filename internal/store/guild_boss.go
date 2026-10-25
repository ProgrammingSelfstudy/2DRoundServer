package store

import (
	"database/sql"
	"errors"
)

// BossState is a guild's boss fight: Fid the chapter of the day (its boss is the chapter's), HP what is left of
// the boss, MaxID the highest chapter killed. Day is the day it was set up for.
type BossState struct {
	GID, Fid, HP, MaxID, BuffLev, BuffEnd, ResetType, Day, MusterAt int64
}

// BossStateOf reads a guild's boss state; found is false for a guild that never opened it.
func (s *Store) BossStateOf(gid int64) (st *BossState, found bool, err error) {
	st = &BossState{GID: gid}
	err = s.db.QueryRow(`SELECT fid,hp,max_id,buff_lev,buff_end,reset_type,day,muster_at FROM guild_boss_state WHERE gid=?`, gid).
		Scan(&st.Fid, &st.HP, &st.MaxID, &st.BuffLev, &st.BuffEnd, &st.ResetType, &st.Day, &st.MusterAt)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	return st, err == nil, err
}

// SaveBossState writes the state (creating it).
func (s *Store) SaveBossState(st *BossState) error {
	_, err := s.db.Exec(`INSERT INTO guild_boss_state(gid,fid,hp,max_id,buff_lev,buff_end,reset_type,day,muster_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(gid) DO UPDATE SET fid=excluded.fid,hp=excluded.hp,max_id=excluded.max_id,buff_lev=excluded.buff_lev,buff_end=excluded.buff_end,
		reset_type=excluded.reset_type,day=excluded.day,muster_at=excluded.muster_at`,
		st.GID, st.Fid, st.HP, st.MaxID, st.BuffLev, st.BuffEnd, st.ResetType, st.Day, st.MusterAt)
	return err
}

// BossRole is a role's boss challenges of a day.
type BossRole struct{ RID, Day, Used, Bought int64 }

// BossRoleOf reads them (a role that never fought has none).
func (s *Store) BossRoleOf(rid int64) (*BossRole, error) {
	r := &BossRole{RID: rid}
	err := s.db.QueryRow(`SELECT day,used,bought FROM guild_boss_roles WHERE rid=?`, rid).Scan(&r.Day, &r.Used, &r.Bought)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

// SaveBossRole writes them.
func (s *Store) SaveBossRole(r *BossRole) error {
	_, err := s.db.Exec(`INSERT INTO guild_boss_roles(rid,day,used,bought) VALUES(?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET day=excluded.day,used=excluded.used,bought=excluded.bought`, r.RID, r.Day, r.Used, r.Bought)
	return err
}

// BossHit takes damage off the boss and notes it for the member, in one transaction: the damage counts only as
// far as the boss has hp left (applied), last keeps what the member's fight did before that cut. killed is true
// when this hit took the last hp; the chapter then counts as the highest killed.
func (s *Store) BossHit(gid, day, bossID, rid, dmg int64) (applied, hpLeft int64, killed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, false, err
	}
	defer tx.Rollback()
	var hp, fid, maxID int64
	if err := tx.QueryRow(`SELECT hp,fid,max_id FROM guild_boss_state WHERE gid=?`, gid).Scan(&hp, &fid, &maxID); err != nil {
		return 0, 0, false, err
	}
	applied = min(dmg, hp)
	hpLeft = hp - applied
	if _, err := tx.Exec(`INSERT INTO guild_boss_dmg(gid,day,boss_id,rid,total,last) VALUES(?,?,?,?,?,?)
		ON CONFLICT(gid,day,boss_id,rid) DO UPDATE SET total=total+excluded.total, last=excluded.last`, gid, day, bossID, rid, applied, dmg); err != nil {
		return 0, 0, false, err
	}
	killed = hp > 0 && hpLeft == 0
	if killed {
		maxID = max(maxID, fid)
	}
	if _, err := tx.Exec(`UPDATE guild_boss_state SET hp=?, max_id=? WHERE gid=?`, hpLeft, maxID, gid); err != nil {
		return 0, 0, false, err
	}
	return applied, hpLeft, killed, tx.Commit()
}

// BossDamage is what a member did to a boss in a day.
type BossDamage struct{ RID, Total, Last int64 }

// BossDamages lists the members' damage to a boss on a day, the most first.
func (s *Store) BossDamages(gid, day, bossID int64) ([]BossDamage, error) {
	rows, err := s.db.Query(`SELECT rid,total,last FROM guild_boss_dmg WHERE gid=? AND day=? AND boss_id=? ORDER BY total DESC, rid`, gid, day, bossID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BossDamage
	for rows.Next() {
		var d BossDamage
		if err := rows.Scan(&d.RID, &d.Total, &d.Last); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// BossLastDamage is what the member's latest fight against a boss did (the mop-up repeats it).
func (s *Store) BossLastDamage(gid, day, bossID, rid int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT last FROM guild_boss_dmg WHERE gid=? AND day=? AND boss_id=? AND rid=?`, gid, day, bossID, rid).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// BossDayTotal is all the damage a guild did on a day.
func (s *Store) BossDayTotal(gid, day int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(total),0) FROM guild_boss_dmg WHERE gid=? AND day=?`, gid, day).Scan(&n)
	return n, err
}

// ClaimBossKill marks that a role was paid the kill reward of a boss; false when it was paid already.
func (s *Store) ClaimBossKill(rid, bossID int64) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO guild_boss_kills(rid,boss_id) VALUES(?,?)`, rid, bossID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// BossGuildRow is one guild of the server's boss ranking.
type BossGuildRow struct {
	GID, MaxID, DayTotal, LeaderRID int64
	Name                            string
}

// BossGuilds ranks the guilds of a server: the highest chapter killed first, then the damage of the day.
func (s *Store) BossGuilds(srvID string, day int64) ([]BossGuildRow, error) {
	rows, err := s.db.Query(`SELECT g.gid,g.name,g.leader_rid,COALESCE(b.max_id,0),
		COALESCE((SELECT SUM(total) FROM guild_boss_dmg d WHERE d.gid=g.gid AND d.day=?),0) AS t
		FROM guilds g LEFT JOIN guild_boss_state b ON b.gid=g.gid WHERE g.srv_id=? ORDER BY COALESCE(b.max_id,0) DESC, t DESC, g.gid`, day, srvID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BossGuildRow
	for rows.Next() {
		var r BossGuildRow
		if err := rows.Scan(&r.GID, &r.Name, &r.LeaderRID, &r.MaxID, &r.DayTotal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddGuildExp gives a guild exp and levels it up by the table (exp needed per level, 0 = the last level).
func (s *Store) AddGuildExp(gid, exp int64, levelExp []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lev, have int64
	if err := tx.QueryRow(`SELECT lev,exp FROM guilds WHERE gid=?`, gid).Scan(&lev, &have); err != nil {
		return err
	}
	have += exp
	for int(lev) < len(levelExp) && levelExp[lev-1] > 0 && have >= levelExp[lev-1] {
		have -= levelExp[lev-1]
		lev++
	}
	if _, err := tx.Exec(`UPDATE guilds SET lev=?, exp=? WHERE gid=?`, lev, have, gid); err != nil {
		return err
	}
	return tx.Commit()
}
