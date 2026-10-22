-- Main-line campaign progress, one row per role. A role without a row is at the
-- start (see game.dramaState).
CREATE TABLE role_drama (
  rid        INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  mode       INTEGER NOT NULL,
  chapter_id INTEGER NOT NULL,
  dun_id     INTEGER NOT NULL,     -- the dungeon the player is on
  status     INTEGER NOT NULL,     -- client status code of dun_id
  max_dun_id INTEGER NOT NULL,     -- furthest dungeon reached
  is_first   INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);
