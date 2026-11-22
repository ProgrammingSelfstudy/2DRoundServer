package store

import "slices"

const LoginDayStateKey = "loginday"

type LoginDayState struct {
	Taken []int64 `json:"taken"`
}

func (s LoginDayState) Has(day int64) bool { return slices.Contains(s.Taken, day) }

func (s *Store) LoginDay(rid int64) (LoginDayState, error) {
	var state LoginDayState
	_, err := s.LoadState(rid, LoginDayStateKey, &state)
	return state, err
}

// ClaimLoginDay commits the receipt and the ledgers in the same transaction.
// The first statement acquires the SQLite write lock before reading the receipt,
// so a concurrent claim reads the committed state even across gateway processes.
func (s *Store) ClaimLoginDay(rid, day int64, assets, items map[int64]int64) (LoginDayState, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return LoginDayState{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO module_state(rid,module,data) VALUES(?,?,?) ON CONFLICT(rid,module) DO NOTHING`,
		rid, LoginDayStateKey, `{"taken":[]}`); err != nil {
		return LoginDayState{}, err
	}
	scoped := &Store{db: &transactionDB{tx}, now: s.now}
	state, err := scoped.LoginDay(rid)
	if err != nil {
		return LoginDayState{}, err
	}
	if state.Has(day) {
		return state, ErrConflict
	}
	state.Taken = append(state.Taken, day)
	if err := scoped.SaveState(rid, LoginDayStateKey, &state); err != nil {
		return LoginDayState{}, err
	}
	if err := creditTx(tx, rid, assets, items); err != nil {
		return LoginDayState{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoginDayState{}, err
	}
	return state, nil
}
