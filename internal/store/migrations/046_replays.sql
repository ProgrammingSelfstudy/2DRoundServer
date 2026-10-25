-- Fight replays: the fight script (data) and what the replay lists show (meta), kept per fight.
CREATE TABLE replays (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  srv_id TEXT NOT NULL,
  combat_type INTEGER NOT NULL,
  a_rid INTEGER NOT NULL,
  b_rid INTEGER NOT NULL,
  time INTEGER NOT NULL,
  likes INTEGER NOT NULL DEFAULT 0,
  shares INTEGER NOT NULL DEFAULT 0,
  plays INTEGER NOT NULL DEFAULT 0,
  public INTEGER NOT NULL DEFAULT 0,
  meta TEXT NOT NULL,
  data TEXT NOT NULL
);
CREATE INDEX replays_a ON replays(a_rid, id);
CREATE INDEX replays_b ON replays(b_rid, id);
CREATE INDEX replays_public ON replays(public, combat_type, id);
-- The replays a role collected.
CREATE TABLE replay_collects (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  replay_id INTEGER NOT NULL REFERENCES replays(id) ON DELETE CASCADE,
  PRIMARY KEY (rid, replay_id)
);
