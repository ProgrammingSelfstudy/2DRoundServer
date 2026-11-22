package store

import (
	"database/sql"
	"errors"
)

var ErrHeroCapacity = errors.New("store: hero capacity")

// ComposePartners spends the materials and creates all heroes in one
// transaction. Spending first obtains the SQLite write lock before checking
// capacity, so simultaneous requests cannot both claim the last slot.
func (s *Store) ComposePartners(rid, itemID, count int64, seeds []PartnerSeed, capacity func(int) int) ([]Partner, error) {
	if count <= 0 || len(seeds) == 0 {
		return nil, ErrConflict
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := spendTx(tx, rid, nil, map[int64]int64{itemID: count}); err != nil {
		return nil, err
	}
	var buys, owned int
	var starterGranted bool
	if err := tx.QueryRow(`SELECT hero_slot_buys, partners_init, (SELECT COUNT(*) FROM partners WHERE rid=?) FROM roles WHERE rid=?`, rid, rid).
		Scan(&buys, &starterGranted, &owned); err != nil {
		return nil, wrapNoRows(err)
	}
	if !starterGranted {
		owned++ // H5's single starter is granted when the hero roster first opens.
	}
	if owned+len(seeds) > capacity(buys) {
		return nil, ErrHeroCapacity
	}
	out, err := insertPartnersTx(tx, rid, s.unix(), seeds)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func insertPartnersTx(tx *sql.Tx, rid, now int64, seeds []PartnerSeed) ([]Partner, error) {
	out := make([]Partner, 0, len(seeds))
	for _, sd := range seeds {
		res, err := tx.Exec(`INSERT INTO partners(rid,bid,lev,star,created_at) VALUES(?,?,1,?,?)`, rid, sd.BID, sd.Star, now)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		out = append(out, Partner{PartnerID: id, RID: rid, BID: sd.BID, Lev: 1, Star: sd.Star})
	}
	return out, nil
}
