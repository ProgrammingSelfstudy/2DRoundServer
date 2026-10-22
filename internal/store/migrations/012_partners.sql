-- Heroes ("partners"). partner_id is the id the client uses for one hero of one role (unique
-- across the table, so it can also be used on its own). partners_init is 1 once the role was
-- given its starter heroes, so deleting every hero later does not hand them out again.
CREATE TABLE partners (
  partner_id INTEGER PRIMARY KEY AUTOINCREMENT,
  rid        INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  bid        INTEGER NOT NULL,
  lev        INTEGER NOT NULL DEFAULT 1,
  star       INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_partners_rid ON partners(rid);
ALTER TABLE roles ADD COLUMN partners_init INTEGER NOT NULL DEFAULT 0;
