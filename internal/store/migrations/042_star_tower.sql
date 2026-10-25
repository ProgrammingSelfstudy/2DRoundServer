-- The star tower (the seabed prison): the highest floor a role cleared, its challenges of the day (the day
-- turns at 5:00, stored as the YYYYMMDD of that day), and the milestone rewards it took.
CREATE TABLE star_tower (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  max_tower INTEGER NOT NULL DEFAULT 0,
  cleared_at INTEGER NOT NULL DEFAULT 0,
  day INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  bought INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE star_tower_claims (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  id INTEGER NOT NULL,
  PRIMARY KEY (rid, id)
);
