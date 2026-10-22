-- Seconds a role has been online on one calendar day (the day key is YYYYMMDD in
-- the game's day zone); a new day starts again from 0.
CREATE TABLE online_time (
  rid     INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  day     INTEGER NOT NULL,
  seconds INTEGER NOT NULL DEFAULT 0
);
-- Online-gift steps a role has claimed on a day, by the step's required seconds.
CREATE TABLE online_gift_claims (
  rid        INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  day        INTEGER NOT NULL,
  step_time  INTEGER NOT NULL,
  claimed_at INTEGER NOT NULL,
  PRIMARY KEY (rid, day, step_time)
);
