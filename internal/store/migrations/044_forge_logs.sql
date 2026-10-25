-- The forge house's compose records: what a role made, the coin it cost and when.
CREATE TABLE forge_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  time INTEGER NOT NULL,
  coin INTEGER NOT NULL,
  items TEXT NOT NULL
);
CREATE INDEX forge_logs_rid ON forge_logs(rid, id);
