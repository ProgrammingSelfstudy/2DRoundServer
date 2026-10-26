-- The schedule of the holiday activities (the client's action tabs). spec is the JSON the game reads: the texts, the
-- panel and the aims; the window and the status are columns so the list can be filtered.
CREATE TABLE activities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  srv_id TEXT NOT NULL DEFAULT '',
  camp_id INTEGER NOT NULL,
  bid INTEGER NOT NULL,
  status INTEGER NOT NULL DEFAULT 1,
  start_at INTEGER NOT NULL DEFAULT 0,
  end_at INTEGER NOT NULL DEFAULT 0,
  spec TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX activities_srv ON activities(srv_id, status);
