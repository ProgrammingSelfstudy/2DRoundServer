-- Free hero summons already used: one row per role, summon group and day (YYYYMMDD in the game's day zone).
CREATE TABLE summon_free (
  rid      INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  group_id INTEGER NOT NULL,
  day      INTEGER NOT NULL,
  PRIMARY KEY (rid, group_id, day)
);
