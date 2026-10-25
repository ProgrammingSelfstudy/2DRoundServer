-- The guild boss: per guild the chapter being fought, the boss's hp left and the attack buff; per role the
-- day's challenges; per guild, day and boss what each member did; and the boss kills a role was paid for.
-- day columns hold the China-time day as YYYYMMDD.
CREATE TABLE guild_boss_state (
  gid INTEGER PRIMARY KEY REFERENCES guilds(gid) ON DELETE CASCADE,
  fid INTEGER NOT NULL DEFAULT 1,
  hp INTEGER NOT NULL DEFAULT 0,
  max_id INTEGER NOT NULL DEFAULT 0,
  buff_lev INTEGER NOT NULL DEFAULT 0,
  buff_end INTEGER NOT NULL DEFAULT 0,
  reset_type INTEGER NOT NULL DEFAULT 0,
  day INTEGER NOT NULL DEFAULT 0,
  muster_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE guild_boss_roles (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  day INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  bought INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE guild_boss_dmg (
  gid INTEGER NOT NULL REFERENCES guilds(gid) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  boss_id INTEGER NOT NULL,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  total INTEGER NOT NULL DEFAULT 0,
  last INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (gid, day, boss_id, rid)
);
CREATE TABLE guild_boss_kills (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  boss_id INTEGER NOT NULL,
  PRIMARY KEY (rid, boss_id)
);
