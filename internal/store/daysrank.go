package store

import (
	"database/sql"
	"errors"
	"slices"
)

const DaysRankKey = "daysrank"

type DaysRankRole struct{ Claimed []int64 }

func (s *Store) ClaimDaysRankTask(rid, id int64, assets, items map[int64]int64, validate func(*Store) error) error {
	return s.withTransaction(func(scoped *Store, tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO module_state(rid,module,data) VALUES(?,?,'{}') ON CONFLICT(rid,module) DO NOTHING`, rid, DaysRankKey); err != nil {
			return err
		}
		var state DaysRankRole
		if _, err := scoped.LoadState(rid, DaysRankKey, &state); err != nil {
			return err
		}
		if slices.Contains(state.Claimed, id) {
			return ErrConflict
		}
		if err := validate(scoped); err != nil {
			return err
		}
		state.Claimed = append(state.Claimed, id)
		if err := scoped.SaveState(rid, DaysRankKey, &state); err != nil {
			return err
		}
		return creditTx(tx, rid, assets, items)
	})
}

func (s *Store) DaysRankSettlement(srv string, settle func(*Store) error) error {
	return s.withTransaction(func(scoped *Store, tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO global_state(srv_id,module,data) VALUES(?,?,'{}') ON CONFLICT(srv_id,module) DO NOTHING`, srv, DaysRankKey); err != nil {
			return err
		}
		return settle(scoped)
	})
}

type DaysRankCandidate struct {
	RankRole
	H5, AccountActive, InArena bool
	Tower, Arena               int64
}

func (s *Store) DaysRankCandidates(srv string) ([]DaysRankCandidate, error) {
	rows, err := s.db.Query(`SELECT r.rid,r.name,r.lev,r.face_id,p.platform=?,p.status=1,COALESCE(t.max_tower,0),a.score
		FROM roles r JOIN accounts p ON p.id=r.account_id
		LEFT JOIN star_tower t ON t.rid=r.rid LEFT JOIN arena_players a ON a.rid=r.rid
		WHERE r.srv_id=? AND r.status=1`, PlatformH5, srv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DaysRankCandidate
	for rows.Next() {
		var candidate DaysRankCandidate
		var arena sql.NullInt64
		if err := rows.Scan(&candidate.RID, &candidate.Name, &candidate.Lev, &candidate.FaceID, &candidate.H5, &candidate.AccountActive, &candidate.Tower, &arena); err != nil {
			return nil, err
		}
		candidate.FaceID = faceOrDefault(candidate.FaceID)
		candidate.Arena, candidate.InArena = arena.Int64, arena.Valid
		out = append(out, candidate)
	}
	return out, rows.Err()
}

func (s *Store) DaysRankSpending(rid int64) (gold, essence int64, err error) {
	err = s.db.QueryRow(`SELECT gold,essence FROM daysrank_spending WHERE rid=?`, rid).Scan(&gold, &essence)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return
}

func recordDaysRankSpending(tx *sql.Tx, rid, gold, essence int64) error {
	if gold <= 0 && essence <= 0 {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO daysrank_spending(rid,gold,essence) VALUES(?,?,?)
		ON CONFLICT(rid) DO UPDATE SET gold=gold+excluded.gold,essence=essence+excluded.essence`, rid, max(0, gold), max(0, essence))
	return err
}

func (s *Store) PartnerBreaks(rid int64) ([]Partner, error) {
	rows, err := s.db.Query(`SELECT bid,star,break_lev FROM partners WHERE rid=? AND break_lev>0`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Partner
	for rows.Next() {
		var hero Partner
		if err := rows.Scan(&hero.BID, &hero.Star, &hero.BreakLev); err != nil {
			return nil, err
		}
		out = append(out, hero)
	}
	return out, rows.Err()
}
