-- The arena (loop match): a role's score and the day's challenges, the three opponents on its list, the
-- season chests it opened and its fight log. day columns hold the China-time day as YYYYMMDD; a season is
-- named by the YYYYMMDD of its Monday.
CREATE TABLE arena_players (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  score INTEGER NOT NULL DEFAULT 1000,
  season INTEGER NOT NULL DEFAULT 0,
  battles INTEGER NOT NULL DEFAULT 0,
  day INTEGER NOT NULL DEFAULT 0,
  free_used INTEGER NOT NULL DEFAULT 0,
  bought INTEGER NOT NULL DEFAULT 0,
  cont_win INTEGER NOT NULL DEFAULT 0,
  ref_time INTEGER NOT NULL DEFAULT 0,
  log_seen INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE arena_slots (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  opp_rid INTEGER NOT NULL,
  info TEXT NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, idx)
);
CREATE TABLE arena_claims (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  season INTEGER NOT NULL,
  num INTEGER NOT NULL,
  PRIMARY KEY (rid, season, num)
);
CREATE TABLE arena_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  info TEXT NOT NULL,
  time INTEGER NOT NULL
);
CREATE INDEX arena_logs_rid ON arena_logs(rid, id);
