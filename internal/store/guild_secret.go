package store

import (
	"database/sql"
	"errors"
)

// SecretState is a guild's secret area. Seq counts the crusades it opened; while Settled is 0 one is running
// (Bid the boss, HP what is left of MaxHP, until EndTime); Lines is the game's JSON of the boss each line is at.
type SecretState struct {
	GID, Seq, Vitality, Bid, HP, MaxHP, StartTime, EndTime, KillTime int64
	Settled, Success                                                 bool
	Lines                                                            string
}

// SecretStateOf reads it; a guild that never used it has the zero state (settled, no crusade).
func (s *Store) SecretStateOf(gid int64) (*SecretState, error) {
	st := &SecretState{GID: gid, Settled: true, Lines: "{}"}
	var settled, success int
	err := s.db.QueryRow(`SELECT seq,vitality,bid,hp,max_hp,start_time,end_time,kill_time,settled,success,lines FROM guild_secret WHERE gid=?`, gid).
		Scan(&st.Seq, &st.Vitality, &st.Bid, &st.HP, &st.MaxHP, &st.StartTime, &st.EndTime, &st.KillTime, &settled, &success, &st.Lines)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	st.Settled, st.Success = settled == 1, success == 1
	return st, err
}

// SaveSecretState writes it.
func (s *Store) SaveSecretState(st *SecretState) error {
	_, err := s.db.Exec(`INSERT INTO guild_secret(gid,seq,vitality,bid,hp,max_hp,start_time,end_time,kill_time,settled,success,lines) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(gid) DO UPDATE SET seq=excluded.seq,vitality=excluded.vitality,bid=excluded.bid,hp=excluded.hp,max_hp=excluded.max_hp,start_time=excluded.start_time,
		end_time=excluded.end_time,kill_time=excluded.kill_time,settled=excluded.settled,success=excluded.success,lines=excluded.lines`,
		st.GID, st.Seq, st.Vitality, st.Bid, st.HP, st.MaxHP, st.StartTime, st.EndTime, st.KillTime, b2i(st.Settled), b2i(st.Success), st.Lines)
	return err
}

// AddGuildVitality adds to a guild's vitality (the guild's activity, which opens a crusade).
func (s *Store) AddGuildVitality(gid, n int64) error {
	_, err := s.db.Exec(`INSERT INTO guild_secret(gid,vitality) VALUES(?,?) ON CONFLICT(gid) DO UPDATE SET vitality=vitality+excluded.vitality`, gid, n)
	return err
}

// GuildVitality reads it (0 for a guild that has none).
func (s *Store) GuildVitality(gid int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT vitality FROM guild_secret WHERE gid=?`, gid).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// SecretRole is a member's challenges of the secret area.
type SecretRole struct{ RID, Seq, Used, Bought, ResetAt, LastBuy, SeenSeq int64 }

// SecretRoleOf reads them (a role that never took part has none).
func (s *Store) SecretRoleOf(rid int64) (*SecretRole, error) {
	r := &SecretRole{RID: rid}
	err := s.db.QueryRow(`SELECT seq,used,bought,reset_at,last_buy,seen_seq FROM guild_secret_roles WHERE rid=?`, rid).Scan(&r.Seq, &r.Used, &r.Bought, &r.ResetAt, &r.LastBuy, &r.SeenSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

// SaveSecretRole writes them.
func (s *Store) SaveSecretRole(r *SecretRole) error {
	_, err := s.db.Exec(`INSERT INTO guild_secret_roles(rid,seq,used,bought,reset_at,last_buy,seen_seq) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET seq=excluded.seq,used=excluded.used,bought=excluded.bought,reset_at=excluded.reset_at,last_buy=excluded.last_buy,seen_seq=excluded.seen_seq`,
		r.RID, r.Seq, r.Used, r.Bought, r.ResetAt, r.LastBuy, r.SeenSeq)
	return err
}

// SecretHit takes damage off the running crusade's boss and notes it for the member, in one transaction. The
// damage counts for the ranking whatever the boss has left (a hit after the kill is a pursuit); applied is what
// the boss really lost. killed is true when this hit took the last hp, and the time is kept.
func (s *Store) SecretHit(gid, seq, rid, dmg, now int64) (applied, hpLeft int64, killed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, false, err
	}
	defer tx.Rollback()
	var hp int64
	if err := tx.QueryRow(`SELECT hp FROM guild_secret WHERE gid=? AND seq=?`, gid, seq).Scan(&hp); err != nil {
		return 0, 0, false, err
	}
	applied = min(dmg, hp)
	hpLeft = hp - applied
	killed = hp > 0 && hpLeft == 0
	if _, err := tx.Exec(`INSERT INTO guild_secret_dmg(gid,seq,rid,total,last) VALUES(?,?,?,?,?)
		ON CONFLICT(gid,seq,rid) DO UPDATE SET total=total+excluded.total, last=excluded.last`, gid, seq, rid, dmg, dmg); err != nil {
		return 0, 0, false, err
	}
	if killed {
		_, err = tx.Exec(`UPDATE guild_secret SET hp=0, kill_time=? WHERE gid=?`, now, gid)
	} else {
		_, err = tx.Exec(`UPDATE guild_secret SET hp=? WHERE gid=?`, hpLeft, gid)
	}
	if err != nil {
		return 0, 0, false, err
	}
	return applied, hpLeft, killed, tx.Commit()
}

// SecretDamages lists the members' damage in a crusade, the most first.
func (s *Store) SecretDamages(gid, seq int64) ([]BossDamage, error) {
	rows, err := s.db.Query(`SELECT rid,total,last FROM guild_secret_dmg WHERE gid=? AND seq=? ORDER BY total DESC, rid`, gid, seq)
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

// SecretClaimed lists the stage chests a member opened in a crusade.
func (s *Store) SecretClaimed(gid, seq, rid int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT number FROM guild_secret_claims WHERE gid=? AND seq=? AND rid=? ORDER BY number`, gid, seq, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ClaimSecretChest pays a stage chest once together with marking it. ErrConflict when it was opened already.
func (s *Store) ClaimSecretChest(gid, seq, rid, number int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO guild_secret_claims(gid,seq,rid,number) VALUES(?,?,?,?)`, gid, seq, rid, number)
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
