package store

import "slices"

const (
	LoginDayStateKey      = "loginday"
	SevenLoginDayStateKey = "sevenloginday"
)

type LoginDayState struct {
	Taken []int64 `json:"taken"`
}

func (s LoginDayState) Has(day int64) bool { return slices.Contains(s.Taken, day) }

func loginDayStateKey(seven bool) string {
	if seven {
		return SevenLoginDayStateKey
	}
	return LoginDayStateKey
}

func (st *Store) LoginDay(rid int64) (LoginDayState, error) {
	return st.loginDay(rid, false)
}

func (st *Store) SevenLoginDay(rid int64) (LoginDayState, error) {
	return st.loginDay(rid, true)
}

func (st *Store) loginDay(rid int64, seven bool) (LoginDayState, error) {
	var state LoginDayState
	_, err := st.LoadState(rid, loginDayStateKey(seven), &state)
	return state, err
}

func (st *Store) ClaimLoginDay(rid, day int64, assets, items map[int64]int64) (LoginDayState, error) {
	return st.claimLoginDay(rid, day, false, assets, items)
}

func (st *Store) ClaimSevenLoginDay(rid, day int64, assets, items map[int64]int64) (LoginDayState, error) {
	return st.claimLoginDay(rid, day, true, assets, items)
}

// claimLoginDay commits the receipt and the ledgers in the same transaction.
// The first statement acquires the SQLite write lock before reading the receipt,
// so a concurrent claim reads the committed state even across gateway processes.
func (st *Store) claimLoginDay(rid, day int64, seven bool, assets, items map[int64]int64) (LoginDayState, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return LoginDayState{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO module_state(rid,module,data) VALUES(?,?,?) ON CONFLICT(rid,module) DO NOTHING`,
		rid, loginDayStateKey(seven), `{"taken":[]}`); err != nil {
		return LoginDayState{}, err
	}
	scoped := &Store{db: &transactionDB{tx}, now: st.now}
	state, err := scoped.loginDay(rid, seven)
	if err != nil {
		return LoginDayState{}, err
	}
	if state.Has(day) {
		return state, ErrConflict
	}
	state.Taken = append(state.Taken, day)
	if err := scoped.SaveState(rid, loginDayStateKey(seven), &state); err != nil {
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
