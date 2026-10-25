-- Guilds: the guild, its members (one guild per role), who asked to join, its log, the cool-downs after
-- leaving, and the donation boxes a role took. day columns hold the China-time day as YYYYMMDD.
CREATE TABLE guilds (
  gid INTEGER PRIMARY KEY AUTOINCREMENT,
  srv_id TEXT NOT NULL,
  name TEXT NOT NULL,
  sign TEXT NOT NULL DEFAULT '',
  notice TEXT NOT NULL DEFAULT '',
  leader_rid INTEGER NOT NULL,
  lev INTEGER NOT NULL DEFAULT 1,
  exp INTEGER NOT NULL DEFAULT 0,
  day_exp INTEGER NOT NULL DEFAULT 0,
  day_exp_day INTEGER NOT NULL DEFAULT 0,
  apply_type INTEGER NOT NULL DEFAULT 0,
  apply_lev INTEGER NOT NULL DEFAULT 1,
  apply_power INTEGER NOT NULL DEFAULT 0,
  recruit_num INTEGER NOT NULL DEFAULT 0,
  recruit_day INTEGER NOT NULL DEFAULT 0,
  kick_num INTEGER NOT NULL DEFAULT 0,
  kick_day INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  renamed_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE (srv_id, name)
);
CREATE TABLE guild_members (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  gid INTEGER NOT NULL REFERENCES guilds(gid) ON DELETE CASCADE,
  post INTEGER NOT NULL DEFAULT 3,
  join_time INTEGER NOT NULL,
  donate INTEGER NOT NULL DEFAULT 0,
  day_donate INTEGER NOT NULL DEFAULT 0,
  donate_day INTEGER NOT NULL DEFAULT 0,
  donate_type INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX guild_members_gid ON guild_members(gid);
CREATE TABLE guild_applies (
  gid INTEGER NOT NULL REFERENCES guilds(gid) ON DELETE CASCADE,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (gid, rid)
);
CREATE TABLE guild_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  gid INTEGER NOT NULL REFERENCES guilds(gid) ON DELETE CASCADE,
  type INTEGER NOT NULL,
  rid INTEGER NOT NULL,
  role_name TEXT NOT NULL,
  msg TEXT NOT NULL,
  time INTEGER NOT NULL
);
CREATE INDEX guild_logs_gid ON guild_logs(gid, id);
CREATE TABLE guild_cooldowns (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  quit_time INTEGER NOT NULL,
  was_leader INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE guild_box_claims (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  box_id INTEGER NOT NULL,
  PRIMARY KEY (rid, day, box_id)
);
