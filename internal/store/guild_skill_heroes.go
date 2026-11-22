package store

// attachGuildSkills loads the ordinary skill levels for H5 roles. A server-wide
// roster uses the same single query. Learned skills stay with the role.
func (s *Store) attachGuildSkills(rosters map[int64][]Partner, filter string, arg any) error {
	rows, err := s.db.Query(`SELECT gs.rid,gs.career,gs.done*6+json_array_length(gs.active)
		FROM guild_skill_pve gs JOIN roles r ON r.rid=gs.rid JOIN accounts a ON a.id=r.account_id
		WHERE a.platform=? AND `+filter, PlatformH5, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	levels := make(map[int64]map[int]int)
	for rows.Next() {
		var rid int64
		var career, level int
		if err := rows.Scan(&rid, &career, &level); err != nil {
			return err
		}
		if levels[rid] == nil {
			levels[rid] = make(map[int]int)
		}
		levels[rid][career] = level
	}
	for rid, heroes := range rosters {
		for i := range heroes {
			heroes[i].GuildSkillLevels = levels[rid]
		}
	}
	return rows.Err()
}
