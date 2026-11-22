package store

import (
	"database/sql"
	"errors"
)

// ResetBossDay initializes the new chapter once, even if members open it concurrently.
func (s *Store) ResetBossDay(next *BossState, previousReset int64) (bool, error) {
	return changedOne(s.db.Exec(`INSERT INTO guild_boss_state(gid,fid,hp,max_id,buff_lev,buff_end,reset_type,day,muster_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(gid) DO UPDATE SET fid=excluded.fid,hp=excluded.hp,
		buff_lev=excluded.buff_lev,buff_end=excluded.buff_end,reset_type=0,day=excluded.day
		WHERE guild_boss_state.day < excluded.day AND guild_boss_state.max_id=excluded.max_id AND guild_boss_state.reset_type=?`,
		next.GID, next.Fid, next.HP, next.MaxID, next.BuffLev, next.BuffEnd, next.ResetType, next.Day, next.MusterAt, previousReset))
}

type GuildBossChallenge struct {
	State                     *BossState
	RID, BossID, Damage, Free int64
	AttemptSpent              bool // the original client spends its attempt before playback
	Exp, Vitality             int64
	LevelExp                  []int64
	RewardAssets, RewardItems map[int64]int64
	KillAssets, KillItems     map[int64]int64
	RankMail                  func(rank, damage int64) *Mail
}

// ApplyGuildBossChallenge records damage and rewards together. When the attempt
// was not spent before playback, it consumes it in the same transaction.
func (s *Store) ApplyGuildBossChallenge(c GuildBossChallenge) (killed, firstKill bool, err error) {
	st := c.State
	tx, err := s.db.Begin()
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	// The first statement takes the write lock and checks that the chapter
	// still matches. A fight already playing can finish against a defeated boss.
	ok, err := changedOne(tx.Exec(`UPDATE guild_boss_state SET hp=hp WHERE gid=? AND day=? AND fid=?
		AND (? OR (hp>0 AND EXISTS(SELECT 1 FROM guild_members WHERE rid=? AND gid=?)))`,
		st.GID, st.Day, st.Fid, c.AttemptSpent, c.RID, st.GID))
	if err != nil || !ok {
		return false, false, bossConflict(err)
	}
	if !c.AttemptSpent {
		if err := spendGuildBossAttempt(tx, st.GID, c.RID, st.Day, c.Free); err != nil {
			return false, false, err
		}
	}
	_, _, killed, err = bossHitTx(tx, st.GID, st.Day, c.BossID, c.RID, c.Damage)
	if err != nil {
		return false, false, err
	}
	if err := creditTx(tx, c.RID, c.RewardAssets, c.RewardItems); err != nil {
		return false, false, err
	}
	if _, err := tx.Exec(`INSERT INTO guild_secret(gid,vitality) VALUES(?,?)
		ON CONFLICT(gid) DO UPDATE SET vitality=vitality+excluded.vitality`, st.GID, c.Vitality); err != nil {
		return false, false, err
	}
	if killed {
		firstKill, err = changedOne(tx.Exec(`INSERT OR IGNORE INTO guild_boss_kills(rid,boss_id) VALUES(?,?)`, c.RID, c.BossID))
		if err != nil {
			return false, false, err
		}
		if firstKill {
			if err := creditTx(tx, c.RID, c.KillAssets, c.KillItems); err != nil {
				return false, false, err
			}
		}
		if c.Exp > 0 {
			if err := addGuildExpTx(tx, st.GID, c.Exp, c.LevelExp); err != nil {
				return false, false, err
			}
		}
		if c.RankMail != nil {
			if err := s.guildBossRankMails(tx, c); err != nil {
				return false, false, err
			}
		}
	}
	return killed, firstKill, tx.Commit()
}

// SpendGuildBossAttempt reserves the original client's attempt at fight start.
func (s *Store) SpendGuildBossAttempt(gid, rid, day, free int64) error {
	return spendGuildBossAttempt(s.db, gid, rid, day, free)
}

func spendGuildBossAttempt(db interface {
	Exec(string, ...any) (sql.Result, error)
}, gid, rid, day, free int64) error {
	ok, err := changedOne(db.Exec(`INSERT INTO guild_boss_roles(rid,day,used,bought)
		SELECT ?,?,1,0 WHERE EXISTS(SELECT 1 FROM guild_members WHERE rid=? AND gid=?)
		ON CONFLICT(rid) DO UPDATE SET day=excluded.day,
		used=CASE WHEN day=excluded.day THEN used+1 ELSE 1 END,
		bought=CASE WHEN day=excluded.day THEN bought ELSE 0 END
		WHERE day < excluded.day OR (day=excluded.day AND used < ?+bought)`, rid, day, rid, gid, free))
	if err != nil || !ok {
		return bossConflict(err)
	}
	return nil
}

func (s *Store) guildBossRankMails(tx *sql.Tx, c GuildBossChallenge) error {
	list, err := bossDamages(tx, c.State.GID, c.State.Day, c.BossID)
	if err != nil {
		return err
	}
	for i, d := range list {
		m := c.RankMail(int64(i)+1, d.Total)
		if m == nil {
			continue
		}
		m.Scope, m.Type, m.CreatedAt = MailDirect, 1, s.unix()
		if _, err := insertMail(tx, *m, []int64{d.RID}); err != nil {
			return err
		}
	}
	return nil
}

// BuyGuildBossAttempt charges the quoted tier only if the membership, day and
// number of previous purchases still match.
func (s *Store) BuyGuildBossAttempt(gid int64, r *BossRole, free int64, requireAlive bool, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ok, err := changedOne(tx.Exec(`UPDATE guild_boss_roles SET bought=bought+1 WHERE rid=? AND day=? AND bought=? AND used>=?+bought
		AND EXISTS(SELECT 1 FROM guild_members WHERE rid=? AND gid=?)
		AND (NOT ? OR EXISTS(SELECT 1 FROM guild_boss_state WHERE gid=? AND day=? AND hp>0))`,
		r.RID, r.Day, r.Bought, free, r.RID, gid, requireAlive, gid, r.Day))
	if err != nil || !ok {
		return bossConflict(err)
	}
	if err := spendTx(tx, r.RID, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) BuyGuildBossBuff(st *BossState, rid, level, end, item, gold int64, requireAlive bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ok, err := changedOne(tx.Exec(`UPDATE guild_boss_state SET buff_lev=?,buff_end=?
		WHERE gid=? AND day=? AND buff_lev=? AND buff_end=? AND (NOT ? OR hp>0)
		AND EXISTS(SELECT 1 FROM guild_members WHERE rid=? AND gid=?)`,
		level, end, st.GID, st.Day, st.BuffLev, st.BuffEnd, requireAlive, rid, st.GID))
	if err != nil || !ok {
		return bossConflict(err)
	}
	err = spendTx(tx, rid, nil, map[int64]int64{item: 1})
	if errors.Is(err, ErrConflict) {
		err = spendTx(tx, rid, map[int64]int64{3: gold}, nil)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MusterGuildBoss(gid, rid, day, now, next int64) (bool, error) {
	return changedOne(s.db.Exec(`UPDATE guild_boss_state SET muster_at=? WHERE gid=? AND day=? AND muster_at<=?
		AND EXISTS(SELECT 1 FROM guild_members WHERE rid=? AND gid=? AND post IN (?,?))`,
		next, gid, day, now, rid, gid, PostLeader, PostAssistant))
}

func changedOne(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func bossConflict(err error) error {
	if err != nil {
		return err
	}
	return ErrConflict
}
