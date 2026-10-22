-- Campaign hook (idle) income: since is when the role last took it (unix seconds); a role with
-- no row has been accumulating since it was created.
CREATE TABLE drama_hook (
  rid   INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  since INTEGER NOT NULL
);
-- Campaign actions used per day (day is YYYYMMDD in the game's day zone): fast combats and sweeps.
CREATE TABLE drama_daily (
  rid   INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  day   INTEGER NOT NULL,
  quick INTEGER NOT NULL DEFAULT 0,
  sweep INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, day)
);
