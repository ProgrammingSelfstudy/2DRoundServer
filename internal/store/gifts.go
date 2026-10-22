package store

import "errors"

// ErrGiftLimit is returned when a daily limit is used up.
var ErrGiftLimit = errors.New("store: daily gift limit reached")

// GiftState is what one role has sent and can draw on one day, per friend.
type GiftState struct {
	Sent      map[int64]bool // friends this role already sent a gift to today
	Waiting   map[int64]bool // friends whose gift to this role is not drawn yet
	SentCount int            // gifts sent today
	DrawCount int            // gifts drawn today
}

// GiftState reads a role's gifts for a day.
func (s *Store) GiftState(rid, day int64) (GiftState, error) {
	g := GiftState{Sent: map[int64]bool{}, Waiting: map[int64]bool{}}
	rows, err := s.db.Query(`SELECT to_rid FROM friend_gifts WHERE from_rid=? AND day=?`, rid, day)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var to int64
		if err := rows.Scan(&to); err != nil {
			rows.Close()
			return g, err
		}
		g.Sent[to] = true
	}
	rows.Close()
	g.SentCount = len(g.Sent)
	rows, err = s.db.Query(`SELECT from_rid FROM friend_gifts WHERE to_rid=? AND day=? AND claimed_day=0`, rid, day)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var from int64
		if err := rows.Scan(&from); err != nil {
			rows.Close()
			return g, err
		}
		g.Waiting[from] = true
	}
	rows.Close()
	err = s.db.QueryRow(`SELECT COUNT(*) FROM friend_gifts WHERE to_rid=? AND claimed_day=?`, rid, day).Scan(&g.DrawCount)
	return g, err
}

// SendGift records a gift from -> to on a day and pays the sender's reward, in one
// transaction. ErrConflict: it already sent that friend one today. ErrGiftLimit: it has
// sent maxSends today.
func (s *Store) SendGift(from, to, day int64, maxSends int, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM friend_gifts WHERE from_rid=? AND day=?`, from, day).Scan(&n); err != nil {
		return err
	}
	if n >= maxSends {
		return ErrGiftLimit
	}
	if _, err := tx.Exec(`INSERT INTO friend_gifts(from_rid,to_rid,day) VALUES(?,?,?)`, from, to, day); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if err := creditTx(tx, from, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// DrawGift marks the gift from -> to of that day as drawn by `to` and pays the reward. The
// reward is picked by the caller through reward(drawnToday), because it depends on how
// many gifts were drawn already today. ErrNotFound: there is no gift waiting.
func (s *Store) DrawGift(from, to, day int64, reward func(drawnToday int) (assets, items map[int64]int64)) (drawnToday int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT COUNT(*) FROM friend_gifts WHERE to_rid=? AND claimed_day=?`, to, day).Scan(&drawnToday); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`UPDATE friend_gifts SET claimed_day=? WHERE from_rid=? AND to_rid=? AND day=? AND claimed_day=0`, day, from, to, day)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	assets, items := reward(drawnToday)
	if err := creditTx(tx, to, assets, items); err != nil {
		return 0, err
	}
	return drawnToday, tx.Commit()
}
