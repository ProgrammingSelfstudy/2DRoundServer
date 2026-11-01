package store

import (
	"database/sql"
	"errors"
)

// sqlStoreDB is implemented by the pool and a transaction-scoped Store. The
// ordinary Store keeps all existing transaction boundaries. Only the gateway's
// admin operation explicitly opts into a larger atomic unit of work.
type sqlStoreDB interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Begin() (*sql.Tx, error)
	Close() error
}
type transactionDB struct{ *sql.Tx }

func (d *transactionDB) Begin() (*sql.Tx, error) {
	return nil, errors.New("store: nested transaction is unsupported")
}
func (d *transactionDB) Close() error {
	return errors.New("store: cannot close transaction-scoped store")
}

// AdminTransaction runs existing settlement persistence, mail and audit against
// one transaction. It never replaces the live Store's DB or copies its mutexes.
// The caller must already hold the owning gameplay service lock; scoped must not
// escape fn, start a goroutine or perform unrelated nested transaction operations.
func (s *Store) AdminTransaction(fn func(scoped *Store) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	scoped := &Store{db: &transactionDB{tx}, now: s.now}
	if err = fn(scoped); err != nil {
		return err
	}
	return tx.Commit()
}
