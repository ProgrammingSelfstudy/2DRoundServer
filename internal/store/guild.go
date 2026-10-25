package store

import (
	"database/sql"
	"errors"
)

// Posts in a guild (the client's GuildConst.post_type).
const (
	PostLeader    = 1
	PostAssistant = 2
	PostMember    = 3
)

// Guild is one guild; the day columns hold the China-time day as YYYYMMDD.
type Guild struct {
	GID                         int64
	SrvID, Name, Sign, Notice   string
	LeaderRID                   int64
	Lev, Exp, DayExp, DayExpDay int64
	ApplyType, ApplyLev         int
	ApplyPower                  int64
	RecruitNum, RecruitDay      int
	KickNum, KickDay            int
	CreatedAt, RenamedAt        int64
}

const guildCols = `gid,srv_id,name,sign,notice,leader_rid,lev,exp,day_exp,day_exp_day,apply_type,apply_lev,apply_power,recruit_num,recruit_day,kick_num,kick_day,created_at,renamed_at`

func scanGuild(sc interface{ Scan(...any) error }) (*Guild, error) {
	var g Guild
	err := sc.Scan(&g.GID, &g.SrvID, &g.Name, &g.Sign, &g.Notice, &g.LeaderRID, &g.Lev, &g.Exp, &g.DayExp, &g.DayExpDay,
		&g.ApplyType, &g.ApplyLev, &g.ApplyPower, &g.RecruitNum, &g.RecruitDay, &g.KickNum, &g.KickDay, &g.CreatedAt, &g.RenamedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

// GuildByID reads a guild or returns ErrNotFound.
func (s *Store) GuildByID(gid int64) (*Guild, error) {
	return scanGuild(s.db.QueryRow(`SELECT `+guildCols+` FROM guilds WHERE gid=?`, gid))
}

// GuildMember is a role's place in its guild.
type GuildMember struct {
	RID, GID, JoinTime           int64
	Post                         int
	Donate, DayDonate, DonateDay int64
	DonateType                   int
}

// MemberOf returns the role's place in a guild, or ErrNotFound when it has none.
func (s *Store) MemberOf(rid int64) (*GuildMember, error) {
	var m GuildMember
	err := s.db.QueryRow(`SELECT rid,gid,post,join_time,donate,day_donate,donate_day,donate_type FROM guild_members WHERE rid=?`, rid).
		Scan(&m.RID, &m.GID, &m.Post, &m.JoinTime, &m.Donate, &m.DayDonate, &m.DonateDay, &m.DonateType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// GuildOfRole returns the guild a role is in and its place there, or ErrNotFound.
func (s *Store) GuildOfRole(rid int64) (*Guild, *GuildMember, error) {
	m, err := s.MemberOf(rid)
	if err != nil {
		return nil, nil, err
	}
	g, err := s.GuildByID(m.GID)
	return g, m, err
}

// MemberRow is a member with what the member list shows of the role.
type MemberRow struct {
	GuildMember
	SrvID, Name      string
	Lev, Sex, VIPLev int
	FaceID           int
	LastLoginAt      int64
	LastLogoutAt     int64
}

// MembersOf lists a guild's members: the leader first, then assistants, then by contribution.
func (s *Store) MembersOf(gid int64) ([]MemberRow, error) {
	rows, err := s.db.Query(`SELECT m.rid,m.gid,m.post,m.join_time,m.donate,m.day_donate,m.donate_day,m.donate_type,
		r.srv_id,r.name,r.lev,r.sex,r.vip_lev,r.face_id,r.last_login_at,r.last_logout_at
		FROM guild_members m JOIN roles r ON r.rid=m.rid WHERE m.gid=? ORDER BY m.post, m.donate DESC, m.rid`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemberRow
	for rows.Next() {
		var x MemberRow
		if err := rows.Scan(&x.RID, &x.GID, &x.Post, &x.JoinTime, &x.Donate, &x.DayDonate, &x.DonateDay, &x.DonateType,
			&x.SrvID, &x.Name, &x.Lev, &x.Sex, &x.VIPLev, &x.FaceID, &x.LastLoginAt, &x.LastLogoutAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GuildRow is a guild with its member count, for the list.
type GuildRow struct {
	Guild
	Members    int
	LeaderName string
}

// GuildsOf lists the guilds of a server, biggest level first, the ones whose name contains `name`
// when it is not empty; page is 1-based. It returns the page and how many guilds matched.
func (s *Store) GuildsOf(srvID, name string, page, num int) ([]GuildRow, int, error) {
	where, args := `g.srv_id=?`, []any{srvID}
	if name != "" {
		where += ` AND g.name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM guilds g WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	rows, err := s.db.Query(`SELECT `+prefixCols("g", guildCols)+`,(SELECT COUNT(*) FROM guild_members m WHERE m.gid=g.gid),COALESCE(r.name,'')
		FROM guilds g LEFT JOIN roles r ON r.rid=g.leader_rid WHERE `+where+` ORDER BY g.lev DESC, g.exp DESC, g.gid LIMIT ? OFFSET ?`,
		append(args, num, (page-1)*num)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []GuildRow
	for rows.Next() {
		var x GuildRow
		g := &x.Guild
		if err := rows.Scan(&g.GID, &g.SrvID, &g.Name, &g.Sign, &g.Notice, &g.LeaderRID, &g.Lev, &g.Exp, &g.DayExp, &g.DayExpDay,
			&g.ApplyType, &g.ApplyLev, &g.ApplyPower, &g.RecruitNum, &g.RecruitDay, &g.KickNum, &g.KickDay, &g.CreatedAt, &g.RenamedAt,
			&x.Members, &x.LeaderName); err != nil {
			return nil, 0, err
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

func prefixCols(p, cols string) string {
	out, start := "", 0
	for i := 0; i <= len(cols); i++ {
		if i == len(cols) || cols[i] == ',' {
			if out != "" {
				out += ","
			}
			out += p + "." + cols[start:i]
			start = i + 1
		}
	}
	return out
}

// MemberCount counts a guild's members.
func (s *Store) MemberCount(gid int64) (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM guild_members WHERE gid=?`, gid).Scan(&n)
	return
}

// ErrNameTaken: a guild of that name exists on the server.
var ErrNameTaken = errors.New("store: guild name taken")

// CreateGuild makes a guild with the role as its leader and takes the cost, in one transaction.
// ErrNameTaken for a used name, ErrConflict when the role already has a guild or cannot pay.
func (s *Store) CreateGuild(g Guild, now int64, assets, items map[int64]int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM guild_members WHERE rid=?`, g.LeaderRID).Scan(&one); err == nil {
		return 0, ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO guilds(srv_id,name,sign,leader_rid,apply_type,apply_lev,apply_power,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		g.SrvID, g.Name, g.Sign, g.LeaderRID, g.ApplyType, g.ApplyLev, g.ApplyPower, now)
	if err != nil {
		if isUnique(err) {
			return 0, ErrNameTaken
		}
		return 0, err
	}
	gid, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO guild_members(rid,gid,post,join_time) VALUES(?,?,?,?)`, g.LeaderRID, gid, PostLeader, now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM guild_applies WHERE rid=?`, g.LeaderRID); err != nil {
		return 0, err
	}
	if err := spendTx(tx, g.LeaderRID, assets, items); err != nil {
		return 0, err
	}
	return gid, tx.Commit()
}

// JoinGuild puts a role in a guild as a member and drops its applications. ErrConflict when it already
// has a guild or the guild holds `capacity` members.
func (s *Store) JoinGuild(gid, rid, now int64, capacity int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM guild_members WHERE gid=?`, gid).Scan(&n); err != nil {
		return err
	}
	if n >= capacity {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO guild_members(rid,gid,post,join_time) VALUES(?,?,?,?)`, rid, gid, PostMember, now); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guild_applies WHERE rid=?`, rid); err != nil {
		return err
	}
	return tx.Commit()
}

// AddApply records that a role asked to join; false when it already had.
func (s *Store) AddApply(gid, rid, now int64) (bool, error) {
	res, err := s.db.Exec(`INSERT INTO guild_applies(gid,rid,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, gid, rid, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteApply removes an application; false when there was none.
func (s *Store) DeleteApply(gid, rid int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM guild_applies WHERE gid=? AND rid=?`, gid, rid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AppliedGuilds lists the guilds the role has asked to join.
func (s *Store) AppliedGuilds(rid int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT gid FROM guild_applies WHERE rid=?`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var g int64
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out[g] = true
	}
	return out, rows.Err()
}

// ApplicantRow is a role that asked to join a guild.
type ApplicantRow struct {
	RID                 int64
	SrvID, Name         string
	Lev, VIPLev, FaceID int
	LastLogoutAt        int64
	LastLoginAt         int64
}

// AppliesOf lists a guild's applicants, oldest first.
func (s *Store) AppliesOf(gid int64) ([]ApplicantRow, error) {
	rows, err := s.db.Query(`SELECT r.rid,r.srv_id,r.name,r.lev,r.vip_lev,r.face_id,r.last_login_at,r.last_logout_at
		FROM guild_applies a JOIN roles r ON r.rid=a.rid WHERE a.gid=? ORDER BY a.created_at, r.rid`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApplicantRow
	for rows.Next() {
		var x ApplicantRow
		if err := rows.Scan(&x.RID, &x.SrvID, &x.Name, &x.Lev, &x.VIPLev, &x.FaceID, &x.LastLoginAt, &x.LastLogoutAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ApplyCount counts a guild's applicants.
func (s *Store) ApplyCount(gid int64) (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM guild_applies WHERE gid=?`, gid).Scan(&n)
	return
}

// LeaveGuild takes a member out and starts its cool-down. ErrNotFound when it is in no guild.
func (s *Store) LeaveGuild(rid, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM guild_members WHERE rid=?`, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`INSERT INTO guild_cooldowns(rid,quit_time,was_leader) VALUES(?,?,0) ON CONFLICT(rid) DO UPDATE SET quit_time=excluded.quit_time, was_leader=0`, rid, now); err != nil {
		return err
	}
	return tx.Commit()
}

// DisbandGuild deletes a guild with its members and applications; its leader's cool-down is marked.
func (s *Store) DisbandGuild(gid, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var leader int64
	if err := tx.QueryRow(`SELECT leader_rid FROM guilds WHERE gid=?`, gid).Scan(&leader); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guilds WHERE gid=?`, gid); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO guild_cooldowns(rid,quit_time,was_leader) VALUES(?,?,1) ON CONFLICT(rid) DO UPDATE SET quit_time=excluded.quit_time, was_leader=1`, leader, now); err != nil {
		return err
	}
	return tx.Commit()
}

// CooldownOf reads when a role last left a guild (0 = never) and whether it had led it.
func (s *Store) CooldownOf(rid int64) (quitTime int64, wasLeader bool, err error) {
	var w int
	err = s.db.QueryRow(`SELECT quit_time,was_leader FROM guild_cooldowns WHERE rid=?`, rid).Scan(&quitTime, &w)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return quitTime, w == 1, err
}

// SetMemberPost sets a member's post. ErrNotFound when it is not in that guild.
func (s *Store) SetMemberPost(gid, rid int64, post int) error {
	res, err := s.db.Exec(`UPDATE guild_members SET post=? WHERE gid=? AND rid=?`, post, gid, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AssistantCount counts a guild's assistants.
func (s *Store) AssistantCount(gid int64) (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM guild_members WHERE gid=? AND post=?`, gid, PostAssistant).Scan(&n)
	return
}

// TransferLeader makes `to` the leader and the old leader an assistant, in one transaction.
func (s *Store) TransferLeader(gid, from, to int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE guild_members SET post=? WHERE gid=? AND rid=? AND post=?`, PostAssistant, gid, from, PostLeader)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	res, err = tx.Exec(`UPDATE guild_members SET post=? WHERE gid=? AND rid=?`, PostLeader, gid, to)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE guilds SET leader_rid=? WHERE gid=?`, to, gid); err != nil {
		return err
	}
	return tx.Commit()
}

// SetGuildApply sets who may join: the way (0 free, 1 asks, 2 closed), the least level and power.
func (s *Store) SetGuildApply(gid int64, applyType, applyLev int, applyPower int64) error {
	_, err := s.db.Exec(`UPDATE guilds SET apply_type=?, apply_lev=?, apply_power=? WHERE gid=?`, applyType, applyLev, applyPower, gid)
	return err
}

// SetGuildSign sets the guild's declaration.
func (s *Store) SetGuildSign(gid int64, sign string) error {
	_, err := s.db.Exec(`UPDATE guilds SET sign=? WHERE gid=?`, sign, gid)
	return err
}

// RenameGuild renames a guild and takes the cost. ErrNameTaken, or ErrConflict when the role cannot pay.
func (s *Store) RenameGuild(gid, payer int64, name string, now int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE guilds SET name=?, renamed_at=? WHERE gid=?`, name, now, gid); err != nil {
		if isUnique(err) {
			return ErrNameTaken
		}
		return err
	}
	if err := spendTx(tx, payer, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// CountRecruit counts a recruit announcement of the day (`limit` a day), returning how many were made
// before it. ErrConflict when the limit is reached; the cost is paid in the same transaction.
func (s *Store) CountRecruit(gid, payer, day int64, limit int, assets, items map[int64]int64) (before int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var num int
	var d int64
	if err := tx.QueryRow(`SELECT recruit_num,recruit_day FROM guilds WHERE gid=?`, gid).Scan(&num, &d); err != nil {
		return 0, err
	}
	if d != day {
		num = 0
	}
	if num >= limit {
		return num, ErrConflict
	}
	if _, err := tx.Exec(`UPDATE guilds SET recruit_num=?, recruit_day=? WHERE gid=?`, num+1, day, gid); err != nil {
		return 0, err
	}
	if err := spendTx(tx, payer, assets, items); err != nil {
		return 0, err
	}
	return num, tx.Commit()
}

// KickMember removes a member (counting the day's kicks, `limit` a day) and starts its cool-down.
// ErrConflict when the limit is reached; ErrNotFound when it is not in that guild.
func (s *Store) KickMember(gid, rid, now, day int64, limit int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var num int
	var d int64
	if err := tx.QueryRow(`SELECT kick_num,kick_day FROM guilds WHERE gid=?`, gid).Scan(&num, &d); err != nil {
		return err
	}
	if d != day {
		num = 0
	}
	if num >= limit {
		return ErrConflict
	}
	res, err := tx.Exec(`DELETE FROM guild_members WHERE gid=? AND rid=?`, gid, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE guilds SET kick_num=?, kick_day=? WHERE gid=?`, num+1, day, gid); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO guild_cooldowns(rid,quit_time,was_leader) VALUES(?,?,0) ON CONFLICT(rid) DO UPDATE SET quit_time=excluded.quit_time, was_leader=0`, rid, now); err != nil {
		return err
	}
	return tx.Commit()
}

// GuildLog is one line of a guild's log.
type GuildLog struct {
	ID, GID, RID, Time int64
	Type               int
	RoleName, Msg      string
}

// AddGuildLog appends a line to the log, keeping the last 200 lines.
func (s *Store) AddGuildLog(l GuildLog) error {
	if _, err := s.db.Exec(`INSERT INTO guild_logs(gid,type,rid,role_name,msg,time) VALUES(?,?,?,?,?,?)`, l.GID, l.Type, l.RID, l.RoleName, l.Msg, l.Time); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM guild_logs WHERE gid=? AND id <= (SELECT id FROM guild_logs WHERE gid=? ORDER BY id DESC LIMIT 1 OFFSET 200)`, l.GID, l.GID)
	return err
}

// GuildLogs lists a guild's log, oldest first, at most limit of the latest.
func (s *Store) GuildLogs(gid int64, limit int) ([]GuildLog, error) {
	rows, err := s.db.Query(`SELECT id,gid,type,rid,role_name,msg,time FROM (SELECT * FROM guild_logs WHERE gid=? ORDER BY id DESC LIMIT ?) ORDER BY id`, gid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GuildLog
	for rows.Next() {
		var l GuildLog
		if err := rows.Scan(&l.ID, &l.GID, &l.Type, &l.RID, &l.RoleName, &l.Msg, &l.Time); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DonateResult is what a donation changed in the guild.
type DonateResult struct {
	Lev, Exp, DayExp int64
	LevelUps         int
}

// Donate pays a donation, credits the donor and adds exp to the guild, in one transaction. A member
// donates once a day (the client's rule). levelExp[i] is the exp that ends guild level i+1; the guild
// levels up while its exp reaches it, and stops at the last level. ErrConflict when the member already
// donated today or cannot pay.
func (s *Store) Donate(rid, day int64, kind int, spendAssets, spendItems, gainAssets, gainItems map[int64]int64, guildExp int64, levelExp []int64) (DonateResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return DonateResult{}, err
	}
	defer tx.Rollback()
	var gid, donateDay int64
	if err := tx.QueryRow(`SELECT gid,donate_day FROM guild_members WHERE rid=?`, rid).Scan(&gid, &donateDay); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DonateResult{}, ErrNotFound
		}
		return DonateResult{}, err
	}
	if donateDay == day {
		return DonateResult{}, ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return DonateResult{}, err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return DonateResult{}, err
	}
	contribution := gainAssets[10]
	if _, err := tx.Exec(`UPDATE guild_members SET donate=donate+?, day_donate=?, donate_day=?, donate_type=? WHERE rid=?`, contribution, contribution, day, kind, rid); err != nil {
		return DonateResult{}, err
	}
	var g Guild
	if err := tx.QueryRow(`SELECT lev,exp,day_exp,day_exp_day FROM guilds WHERE gid=?`, gid).Scan(&g.Lev, &g.Exp, &g.DayExp, &g.DayExpDay); err != nil {
		return DonateResult{}, err
	}
	if g.DayExpDay != day {
		g.DayExp = 0
	}
	g.Exp += guildExp
	g.DayExp += guildExp
	var ups int
	for int(g.Lev) < len(levelExp) && levelExp[g.Lev-1] > 0 && g.Exp >= levelExp[g.Lev-1] {
		g.Exp -= levelExp[g.Lev-1]
		g.Lev++
		ups++
	}
	if _, err := tx.Exec(`UPDATE guilds SET lev=?, exp=?, day_exp=?, day_exp_day=? WHERE gid=?`, g.Lev, g.Exp, g.DayExp, day, gid); err != nil {
		return DonateResult{}, err
	}
	return DonateResult{Lev: g.Lev, Exp: g.Exp, DayExp: g.DayExp, LevelUps: ups}, tx.Commit()
}

// ClaimDonateBox gives a member the reward of a donation box of the day once. ErrConflict when it was
// taken already or the guild's donation of the day has not reached `need`.
func (s *Store) ClaimDonateBox(rid, day int64, boxID int, need int64, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dayExp, expDay int64
	if err := tx.QueryRow(`SELECT g.day_exp,g.day_exp_day FROM guild_members m JOIN guilds g ON g.gid=m.gid WHERE m.rid=?`, rid).Scan(&dayExp, &expDay); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if expDay != day || dayExp < need {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO guild_box_claims(rid,day,box_id) VALUES(?,?,?)`, rid, day, boxID); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimedBoxes lists the donation boxes the role took on that day.
func (s *Store) ClaimedBoxes(rid, day int64) ([]int, error) {
	rows, err := s.db.Query(`SELECT box_id FROM guild_box_claims WHERE rid=? AND day=? ORDER BY box_id`, rid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var b int
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GuildDayExp is the guild's donation exp of the day (0 when it has none today).
func (s *Store) GuildDayExp(gid, day int64) (int64, error) {
	var e, d int64
	if err := s.db.QueryRow(`SELECT day_exp,day_exp_day FROM guilds WHERE gid=?`, gid).Scan(&e, &d); err != nil {
		return 0, err
	}
	if d != day {
		return 0, nil
	}
	return e, nil
}

// Impeach makes `to` the leader in place of `from` (who becomes a member) and takes the cost, in one
// transaction. ErrConflict when `from` is no longer the leader or `to` cannot pay.
func (s *Store) Impeach(gid, from, to int64, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE guild_members SET post=? WHERE gid=? AND rid=? AND post=?`, PostMember, gid, from, PostLeader)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	res, err = tx.Exec(`UPDATE guild_members SET post=? WHERE gid=? AND rid=?`, PostLeader, gid, to)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE guilds SET leader_rid=? WHERE gid=?`, to, gid); err != nil {
		return err
	}
	if err := spendTx(tx, to, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}
