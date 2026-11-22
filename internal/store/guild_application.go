package store

// AcceptGuildApplication approves an extant application with the manager's current
// permission and the guild's current roster in the same write transaction.
// When the guild is full the application stays pending.
func (s *Store) AcceptGuildApplication(gid, manager, applicant, now, quitCooldown int64, capacity int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ok, err := changedOne(tx.Exec(`INSERT INTO guild_members(rid,gid,post,join_time)
		SELECT a.rid,a.gid,?,? FROM guild_applies a
		JOIN guilds g ON g.gid=a.gid
		JOIN roles r ON r.rid=a.rid AND r.srv_id=g.srv_id
		WHERE a.gid=? AND a.rid=?
		AND EXISTS(SELECT 1 FROM guild_members m WHERE m.rid=? AND m.gid=a.gid AND m.post IN (?,?))
		AND NOT EXISTS(SELECT 1 FROM guild_cooldowns WHERE rid=a.rid AND quit_time>?)
		AND (SELECT COUNT(*) FROM guild_members WHERE gid=a.gid) < ?
		ON CONFLICT(rid) DO NOTHING`, PostMember, now, gid, applicant, manager, PostLeader, PostAssistant, now-quitCooldown, capacity))
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrConflict
	}
	if _, err := tx.Exec(`DELETE FROM guild_applies WHERE rid=?`, applicant); err != nil {
		return err
	}
	return tx.Commit()
}
