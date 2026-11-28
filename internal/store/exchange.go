package store

import "database/sql"

// Exchange spends currencies and items and credits others in one transaction. It
// returns ErrConflict, changing nothing, when the role cannot pay.
func (s *Store) Exchange(rid int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	return s.ExchangeAndSave(rid, spendAssets, spendItems, gainAssets, gainItems, nil)
}

// ExchangeAndSave commits the balance changes and the gameplay state that earned them together.
func (s *Store) ExchangeAndSave(rid int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64, save func(*Store) error) error {
	return s.withTransaction(func(scoped *Store, tx *sql.Tx) error {
		if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
			return err
		}
		if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
			return err
		}
		if save != nil {
			return save(scoped)
		}
		return nil
	})
}
