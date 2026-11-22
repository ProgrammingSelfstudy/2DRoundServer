package store

import (
	"database/sql"
	"errors"
)

const h5ArenaPlayers = `FROM arena_players a JOIN roles r ON r.rid=a.rid JOIN accounts p ON p.id=r.account_id
	WHERE r.srv_id=? AND p.platform=? AND r.status=1 AND p.status=1`

func (s *Store) ArenaH5Near(srvID string, rid, score int64, minStar, limit int) ([]ArenaRow, error) {
	return arenaRows(s.db.Query(`SELECT a.rid,a.score `+h5ArenaPlayers+`
		AND a.rid<>? AND EXISTS (SELECT 1 FROM partners h WHERE h.rid=a.rid AND h.star>=?)
		ORDER BY ABS(a.score-?), a.rid LIMIT ?`, srvID, PlatformH5, rid, minStar, score, limit))
}

func (s *Store) ArenaH5Top(srvID string, limit int) ([]ArenaRow, error) {
	return arenaRows(s.db.Query(`SELECT a.rid,a.score `+h5ArenaPlayers+`
		ORDER BY a.score DESC, a.created_at, a.rid LIMIT ?`, srvID, PlatformH5, limit))
}

func (s *Store) ArenaH5Rank(rid int64) (int64, error) {
	var rank int64
	err := s.db.QueryRow(`SELECT COUNT(*)+1 FROM arena_players me
		JOIN roles owner ON owner.rid=me.rid
		JOIN roles r ON r.srv_id=owner.srv_id
		JOIN accounts p ON p.id=r.account_id JOIN arena_players a ON a.rid=r.rid
		WHERE me.rid=? AND p.platform=? AND r.status=1 AND p.status=1
		AND (a.score>me.score OR (a.score=me.score AND
		     (a.created_at<me.created_at OR (a.created_at=me.created_at AND a.rid<me.rid))))`, rid, PlatformH5).Scan(&rank)
	return rank, err
}

func (s *Store) ArenaH5DefenderScore(srvID string, rid int64) (score int64, ok bool, err error) {
	err = s.db.QueryRow(`SELECT a.score `+h5ArenaPlayers+` AND a.rid=?`, srvID, PlatformH5, rid).Scan(&score)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return score, err == nil, err
}

// RollH5ArenaSeason updates the realm's H5 standings together, including idle players.
func (s *Store) RollH5ArenaSeason(srvID string, season, startingScore, keepPermil int64) error {
	_, err := s.db.Exec(`UPDATE arena_players
		SET score=CASE WHEN season=0 THEN score ELSE MAX(?,score*?/1000) END,
		    season=?, battles=0, cont_win=0
		WHERE season<? AND rid IN (SELECT r.rid FROM roles r JOIN accounts a ON a.id=r.account_id
		                           WHERE r.srv_id=? AND a.platform=?)`,
		startingScore, keepPermil, season, season, srvID, PlatformH5)
	return err
}

func (s *Store) ResetArenaDay(rid, day int64) error {
	_, err := s.db.Exec(`UPDATE arena_players SET day=?,free_used=0,bought=0 WHERE rid=? AND day<?`, day, rid, day)
	return err
}

func (s *Store) SetArenaLogSeen(rid, seen int64) error {
	_, err := s.db.Exec(`UPDATE arena_players SET log_seen=? WHERE rid=?`, seen, rid)
	return err
}

// BuyArenaTickets counts the purchase in the same transaction as the inventory exchange.
func (s *Store) BuyArenaTickets(rid, number, ticket, price int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE arena_players SET bought=bought+? WHERE rid=?`, number, rid)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if err = spendTx(tx, rid, map[int64]int64{3: price * number}, nil); err != nil {
		return err
	}
	if err = creditTx(tx, rid, nil, map[int64]int64{ticket: number}); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceArenaSlots(tx *sql.Tx, rid int64, slots []ArenaSlot) error {
	if _, err := tx.Exec(`DELETE FROM arena_slots WHERE rid=?`, rid); err != nil {
		return err
	}
	for _, slot := range slots {
		if _, err := tx.Exec(`INSERT INTO arena_slots(rid,idx,opp_rid,status,info) VALUES(?,?,?,?,?)`,
			rid, slot.Idx, slot.OppRID, slot.Status, slot.Info); err != nil {
			return err
		}
	}
	return nil
}

// RefreshArenaSlots replaces the list and advances its cooldown together.
func (s *Store) RefreshArenaSlots(rid, now, next int64, slots []ArenaSlot) error {
	return s.updateArenaSlots(rid, slots, `UPDATE arena_players SET ref_time=? WHERE rid=? AND ref_time<=?`, next, rid, now)
}

// InitArenaSlots installs the first list only if another request hasn't already installed one.
func (s *Store) InitArenaSlots(rid int64, slots []ArenaSlot) error {
	return s.updateArenaSlots(rid, slots, `UPDATE arena_players SET ref_time=ref_time WHERE rid=?
		AND NOT EXISTS (SELECT 1 FROM arena_slots WHERE rid=?)`, rid, rid)
}

func (s *Store) updateArenaSlots(rid int64, slots []ArenaSlot, guard string, args ...any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(guard, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	if err = replaceArenaSlots(tx, rid, slots); err != nil {
		return err
	}
	return tx.Commit()
}

// ArenaMatchTx exposes the existing arena and replay writes within one match transaction.
type ArenaMatchTx struct {
	*Store
	tx *sql.Tx
}

func (m *ArenaMatchTx) SpendTicket(rid, ticket int64) error {
	return spendTx(m.tx, rid, nil, map[int64]int64{ticket: 1})
}

func (m *ArenaMatchTx) CreditReward(rid int64, assets, items map[int64]int64) error {
	return creditTx(m.tx, rid, assets, items)
}

// CommitArenaMatch reserves this exact open slot before touching either player's standing.
// The reservation, payment, reward, replay and logs roll back together if settlement fails.
func (s *Store) CommitArenaMatch(rid int64, slot ArenaSlot, settle func(*ArenaMatchTx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE arena_slots SET status=1 WHERE rid=? AND idx=? AND opp_rid=? AND info=? AND status=0`,
		rid, slot.Idx, slot.OppRID, slot.Info)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	m := &ArenaMatchTx{Store: &Store{db: &transactionDB{tx}, now: s.now}, tx: tx}
	if err = settle(m); err != nil {
		return err
	}
	return tx.Commit()
}
