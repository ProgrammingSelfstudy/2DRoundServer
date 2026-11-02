package store

import "time"

// Operating data (migration 052). Sign-ups and payments are read from accounts, roles and pay_orders, which also gives their
// history from before this existed; logins, play time, command use and the online count are recorded here.

const (
	chinaOffset      = 8 * 3600
	maxAnalyticsDays = 90
	sampleKeepDays   = 60
)

// DayNo is the China-time day number of a unix time, the day every daily rule of the game uses.
func DayNo(unix int64) int64 { return (unix + chinaOffset) / 86400 }

func dayDate(day int64) string { return time.Unix(day*86400, 0).UTC().Format("2006-01-02") }

// RecordEvent keeps one event: "login" (acc), "enter" (rid), "logout" (rid, val = seconds connected). Callers ignore the error: a
// missed data point must never break the game.
func (s *Store) RecordEvent(name string, rid, acc, val int64) error {
	now := s.unix()
	_, err := s.db.Exec(`INSERT INTO events(ts,day,name,rid,acc,val) VALUES(?,?,?,?,?,?)`, now, DayNo(now), name, rid, acc, val)
	return err
}

type cmdKey struct {
	day int64
	cmd uint16
	rid int64
}

// RecordCmd counts one command of a role in memory; FlushCmds writes the counts.
func (s *Store) RecordCmd(rid int64, cmd uint16) {
	k := cmdKey{DayNo(s.unix()), cmd, rid}
	s.cmdMu.Lock()
	if s.cmds == nil {
		s.cmds = map[cmdKey]int64{}
	}
	s.cmds[k]++
	s.cmdMu.Unlock()
}

// FlushCmds writes the counted commands. On an error the counts are put back for the next flush.
func (s *Store) FlushCmds() error {
	s.cmdMu.Lock()
	buf := s.cmds
	s.cmds = nil
	s.cmdMu.Unlock()
	if len(buf) == 0 {
		return nil
	}
	err := s.writeCmds(buf)
	if err != nil {
		s.cmdMu.Lock()
		if s.cmds == nil {
			s.cmds = map[cmdKey]int64{}
		}
		for k, n := range buf {
			s.cmds[k] += n
		}
		s.cmdMu.Unlock()
	}
	return err
}

func (s *Store) writeCmds(buf map[cmdKey]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, n := range buf {
		if _, err := tx.Exec(`INSERT INTO cmd_daily(day,cmd,rid,hits) VALUES(?,?,?,?) ON CONFLICT(day,cmd,rid) DO UPDATE SET hits=hits+excluded.hits`,
			k.day, k.cmd, k.rid, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SampleOnline keeps how many roles are online now (one row per minute) and drops samples older than sampleKeepDays.
func (s *Store) SampleOnline(n int) error {
	now := s.unix()
	if _, err := s.db.Exec(`INSERT INTO online_samples(ts,n) VALUES(?,?) ON CONFLICT(ts) DO UPDATE SET n=MAX(n,excluded.n)`, now-now%60, n); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM online_samples WHERE ts<?`, now-sampleKeepDays*86400)
	return err
}

// AnalyticsDay is one China-time day.
type AnalyticsDay struct {
	Day           string `json:"day"`
	NewAccounts   int64  `json:"new_accounts"`
	NewRoles      int64  `json:"new_roles"`
	Active        int64  `json:"active"` // roles that entered the game (DAU)
	LoginAccounts int64  `json:"login_accounts"`
	Sessions      int64  `json:"sessions"` // finished connections of a role
	AvgSessionSec int64  `json:"avg_session_sec"`
	PeakOnline    int64  `json:"peak_online"`
	OrdersCreated int64  `json:"orders_created"`
	OrdersPaid    int64  `json:"orders_paid"`
	PayingRoles   int64  `json:"paying_roles"`
	PaidCents     int64  `json:"paid_cents"`
}

// AnalyticsRetention is the roles created on a day and how many of them entered the game 1, 3 and 7 days later. A figure is nil
// when that day is still to come or began before the first recorded event.
type AnalyticsRetention struct {
	Day    string `json:"day"`
	Cohort int64  `json:"cohort"`
	D1     *int64 `json:"d1"`
	D3     *int64 `json:"d3"`
	D7     *int64 `json:"d7"`
}

// AnalyticsCmd is how much a command was used in the window.
type AnalyticsCmd struct {
	Cmd   int   `json:"cmd"`
	Hits  int64 `json:"hits"`
	Roles int64 `json:"roles"`
}

// Analytics is the report of the last days days (1..maxAnalyticsDays), today included.
type Analytics struct {
	From       string               `json:"from"`
	To         string               `json:"to"`
	EventsFrom string               `json:"events_from"` // first day with a recorded event; "" before any
	Days       []AnalyticsDay       `json:"days"`
	Retention  []AnalyticsRetention `json:"retention"`
	Cmds       []AnalyticsCmd       `json:"cmds"`
}

type dayCount struct{ a, b, c int64 }

// perDay runs a query that returns (day, a, b, c) rows and keys them by day.
func (s *Store) perDay(query string, args ...any) (map[int64]dayCount, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]dayCount{}
	for rows.Next() {
		var d int64
		var c dayCount
		if err := rows.Scan(&d, &c.a, &c.b, &c.c); err != nil {
			return nil, err
		}
		out[d] = c
	}
	return out, rows.Err()
}

func (s *Store) Analytics(days int) (*Analytics, error) {
	if days < 1 {
		days = 1
	}
	if days > maxAnalyticsDays {
		days = maxAnalyticsDays
	}
	now := s.unix()
	today := DayNo(now)
	from := today - int64(days) + 1
	fromTs := from*86400 - chinaOffset

	newAcc, err := s.perDay(`SELECT (created_at+28800)/86400, COUNT(*), 0, 0 FROM accounts WHERE created_at>=? GROUP BY 1`, fromTs)
	if err != nil {
		return nil, err
	}
	newRoles, err := s.perDay(`SELECT (created_at+28800)/86400, COUNT(*), 0, 0 FROM roles WHERE created_at>=? GROUP BY 1`, fromTs)
	if err != nil {
		return nil, err
	}
	active, err := s.perDay(`SELECT day, COUNT(DISTINCT rid), 0, 0 FROM events WHERE name='enter' AND day>=? GROUP BY day`, from)
	if err != nil {
		return nil, err
	}
	logins, err := s.perDay(`SELECT day, COUNT(DISTINCT acc), 0, 0 FROM events WHERE name='login' AND day>=? GROUP BY day`, from)
	if err != nil {
		return nil, err
	}
	sessions, err := s.perDay(`SELECT day, COUNT(*), CAST(AVG(val) AS INTEGER), 0 FROM events WHERE name='logout' AND day>=? GROUP BY day`, from)
	if err != nil {
		return nil, err
	}
	peak, err := s.perDay(`SELECT (ts+28800)/86400, MAX(n), 0, 0 FROM online_samples WHERE ts>=? GROUP BY 1`, fromTs)
	if err != nil {
		return nil, err
	}
	created, err := s.perDay(`SELECT (created_at+28800)/86400, COUNT(*), 0, 0 FROM pay_orders WHERE created_at>=? GROUP BY 1`, fromTs)
	if err != nil {
		return nil, err
	}
	paid, err := s.perDay(`SELECT (paid_at+28800)/86400, COUNT(*), COUNT(DISTINCT rid), SUM(amount_cents) FROM pay_orders WHERE status=1 AND paid_at>=? GROUP BY 1`, fromTs)
	if err != nil {
		return nil, err
	}

	r := &Analytics{From: dayDate(from), To: dayDate(today)}
	for day := from; day <= today; day++ {
		r.Days = append(r.Days, AnalyticsDay{
			Day: dayDate(day), NewAccounts: newAcc[day].a, NewRoles: newRoles[day].a, Active: active[day].a, LoginAccounts: logins[day].a,
			Sessions: sessions[day].a, AvgSessionSec: sessions[day].b, PeakOnline: peak[day].a, OrdersCreated: created[day].a,
			OrdersPaid: paid[day].a, PayingRoles: paid[day].b, PaidCents: paid[day].c,
		})
	}

	var first int64
	if err := s.db.QueryRow(`SELECT COALESCE(MIN(day),0) FROM events`).Scan(&first); err != nil {
		return nil, err
	}
	if first > 0 {
		r.EventsFrom = dayDate(first)
	}
	if r.Retention, err = s.retention(from, today, first); err != nil {
		return nil, err
	}
	if r.Cmds, err = s.topCmds(from); err != nil {
		return nil, err
	}
	return r, nil
}

// retention counts, for the roles created on each day of the window, the ones that entered the game on day+1, +3 and +7.
func (s *Store) retention(from, today, firstEvent int64) ([]AnalyticsRetention, error) {
	rows, err := s.db.Query(`SELECT rid, (created_at+28800)/86400 FROM roles WHERE created_at>=?`, from*86400-chinaOffset)
	if err != nil {
		return nil, err
	}
	born := map[int64]int64{} // rid -> creation day
	for rows.Next() {
		var rid, day int64
		if err := rows.Scan(&rid, &day); err != nil {
			rows.Close()
			return nil, err
		}
		born[rid] = day
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[int64]map[int64]bool{} // rid -> days it entered the game
	rows, err = s.db.Query(`SELECT DISTINCT rid, day FROM events WHERE name='enter' AND day>=?`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rid, day int64
		if err := rows.Scan(&rid, &day); err != nil {
			return nil, err
		}
		if _, ok := born[rid]; ok {
			if seen[rid] == nil {
				seen[rid] = map[int64]bool{}
			}
			seen[rid][day] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cohort := map[int64]int64{}
	kept := map[int64]*[3]int64{}
	offsets := [3]int64{1, 3, 7}
	for rid, day := range born {
		cohort[day]++
		if kept[day] == nil {
			kept[day] = &[3]int64{}
		}
		for i, off := range offsets {
			if seen[rid][day+off] {
				kept[day][i]++
			}
		}
	}
	out := make([]AnalyticsRetention, 0, today-from+1)
	for day := from; day <= today; day++ {
		row := AnalyticsRetention{Day: dayDate(day), Cohort: cohort[day]}
		targets := [3]**int64{&row.D1, &row.D3, &row.D7}
		for i, off := range offsets {
			// the figure exists once that day is over (it is not today) and events were recorded since before it began
			if cohort[day] > 0 && day+off < today && firstDay(firstEvent) <= day+off {
				v := kept[day][i]
				*targets[i] = &v
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// firstDay is the day events began, or a day that never comes while there are none.
func firstDay(day int64) int64 {
	if day == 0 {
		return 1 << 40
	}
	return day
}

func (s *Store) topCmds(from int64) ([]AnalyticsCmd, error) {
	rows, err := s.db.Query(`SELECT cmd, SUM(hits), COUNT(DISTINCT rid) FROM cmd_daily WHERE day>=? GROUP BY cmd ORDER BY 2 DESC, 1 LIMIT 50`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AnalyticsCmd{}
	for rows.Next() {
		var c AnalyticsCmd
		if err := rows.Scan(&c.Cmd, &c.Hits, &c.Roles); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
