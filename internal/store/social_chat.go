package store

import "errors"

// ErrSocialBlocked means a request or private message between H5 roles is blocked in either direction.
var ErrSocialBlocked = errors.New("store: a role blocked the other")

const socialBlock = `EXISTS (SELECT 1 FROM friend_blacklist
	WHERE (rid=? AND black_rid=?) OR (rid=? AND black_rid=?))`

const h5SocialPair = `(SELECT COUNT(*) FROM roles r JOIN accounts a ON a.id=r.account_id
	WHERE r.rid IN (?,?) AND a.platform=?)=2`

const h5SocialBlock = `(` + socialBlock + ` AND ` + h5SocialPair + `)`

func (s *Store) H5SocialPair(first, second int64) (bool, error) {
	var pair bool
	err := s.db.QueryRow(`SELECT `+h5SocialPair, first, second, PlatformH5).Scan(&pair)
	return pair, err
}

// PrivateChatBlocked checks both directions of a conversation between H5 roles.
func (s *Store) PrivateChatBlocked(from, to int64) (bool, error) {
	var blocked bool
	err := s.db.QueryRow(`SELECT `+h5SocialBlock, from, to, to, from, from, to, PlatformH5).Scan(&blocked)
	return blocked, err
}

// ChatMuters returns the H5 listeners who excluded this speaker from their channels.
func (s *Store) ChatMuters(speaker int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT b.rid FROM friend_blacklist b JOIN roles r ON r.rid=b.rid
		JOIN accounts a ON a.id=r.account_id WHERE b.black_rid=? AND a.platform=?`, speaker, PlatformH5)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	muted := map[int64]bool{}
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err != nil {
			return nil, err
		}
		muted[rid] = true
	}
	return muted, rows.Err()
}

// DeliverOfflineChat removes a snapshot of the queue only after a successful
// delivery, serializing deliveries to the same recipient.
func (s *Store) DeliverOfflineChat(to int64, deliver func([]OfflineChat) error) error {
	unlock := s.LockChatRecipient(to)
	defer unlock()
	waiting, lastID, err := s.pendingOfflineChat(to)
	if err != nil || len(waiting) == 0 {
		return err
	}
	if err := deliver(waiting); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM chat_offline WHERE to_rid=? AND id<=?`, to, lastID)
	return err
}

// LockChatRecipient coordinates the choice between live delivery and queuing
// with the recipient's login draining the queue.
func (s *Store) LockChatRecipient(to int64) func() {
	return s.lockChatPair(to, to)
}

func (s *Store) lockChatPair(first, second int64) func() {
	width := uint64(len(s.chatLocks))
	low, high := uint64(first)%width, uint64(second)%width
	if low > high {
		low, high = high, low
	}
	s.chatLocks[low].Lock()
	if low == high {
		return s.chatLocks[low].Unlock
	}
	s.chatLocks[high].Lock()
	return func() {
		s.chatLocks[high].Unlock()
		s.chatLocks[low].Unlock()
	}
}

func (s *Store) pendingOfflineChat(to int64) ([]OfflineChat, int64, error) {
	rows, err := s.db.Query(`SELECT id,from_rid,len,msg,sent_at FROM chat_offline WHERE to_rid=? ORDER BY id`, to)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []OfflineChat
	var lastID int64
	for rows.Next() {
		var c OfflineChat
		if err := rows.Scan(&lastID, &c.FromRID, &c.Len, &c.Msg, &c.SentAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, lastID, rows.Err()
}
