-- The guild secret area: per guild the vitality it has, the crusade running (or the last one), the boss of each
-- of the five lines; per role its challenges; per crusade (seq) what each member did and the chests they opened.
CREATE TABLE guild_secret (
  gid INTEGER PRIMARY KEY REFERENCES guilds(gid) ON DELETE CASCADE,
  seq INTEGER NOT NULL DEFAULT 0,
  vitality INTEGER NOT NULL DEFAULT 0,
  bid INTEGER NOT NULL DEFAULT 0,
  hp INTEGER NOT NULL DEFAULT 0,
  max_hp INTEGER NOT NULL DEFAULT 0,
  start_time INTEGER NOT NULL DEFAULT 0,
  end_time INTEGER NOT NULL DEFAULT 0,
  kill_time INTEGER NOT NULL DEFAULT 0,
  settled INTEGER NOT NULL DEFAULT 1,
  success INTEGER NOT NULL DEFAULT 0,
  lines TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE guild_secret_roles (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  seq INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  bought INTEGER NOT NULL DEFAULT 0,
  reset_at INTEGER NOT NULL DEFAULT 0,
  last_buy INTEGER NOT NULL DEFAULT 0,
  seen_seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE guild_secret_dmg (
  gid INTEGER NOT NULL REFERENCES guilds(gid) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  total INTEGER NOT NULL DEFAULT 0,
  last INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (gid, seq, rid)
);
CREATE TABLE guild_secret_claims (
  gid INTEGER NOT NULL,
  seq INTEGER NOT NULL,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  number INTEGER NOT NULL,
  PRIMARY KEY (gid, seq, rid, number)
);
