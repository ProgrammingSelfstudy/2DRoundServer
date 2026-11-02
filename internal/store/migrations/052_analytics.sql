-- Operating data. Sign-ups and payments are read from accounts, roles and pay_orders, so only what those cannot say is kept here:
-- who logged in or entered the game and for how long (events), how often each command is used (cmd_daily) and how many were online
-- (online_samples). day is a China-time day number, (unix + 8h) / 1 day, like the rest of the game's daily rules.
CREATE TABLE events (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  ts   INTEGER NOT NULL,
  day  INTEGER NOT NULL,
  name TEXT NOT NULL,                  -- login (account), enter (role), logout (role; val = seconds connected)
  rid  INTEGER NOT NULL DEFAULT 0,
  acc  INTEGER NOT NULL DEFAULT 0,
  val  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX events_day ON events(name, day);
CREATE INDEX events_rid ON events(rid, day);
CREATE TABLE cmd_daily (
  day  INTEGER NOT NULL,
  cmd  INTEGER NOT NULL,
  rid  INTEGER NOT NULL,
  hits INTEGER NOT NULL,
  PRIMARY KEY (day, cmd, rid)
);
CREATE TABLE online_samples (
  ts INTEGER PRIMARY KEY,              -- start of the minute
  n  INTEGER NOT NULL
);
