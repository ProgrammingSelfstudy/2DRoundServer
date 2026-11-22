-- Server-wide fastest and lowest-power clears, plus each player's own fastest clear.
-- The references keep records playable when the old, unreferenced fights are pruned.
CREATE TABLE star_tower_replay_records (
  srv_id      TEXT NOT NULL,
  tower       INTEGER NOT NULL,
  kind        INTEGER NOT NULL,
  owner_id    INTEGER NOT NULL,
  replay_id   INTEGER NOT NULL REFERENCES replays(id) ON DELETE CASCADE,
  duration_ms INTEGER NOT NULL,
  power       INTEGER NOT NULL,
  PRIMARY KEY (srv_id, tower, kind, owner_id)
);
CREATE INDEX star_tower_replay_records_id ON star_tower_replay_records(replay_id);
CREATE INDEX replays_tower_recent ON replays(id) WHERE combat_type=7;
CREATE INDEX replays_other_recent ON replays(id) WHERE combat_type<>7;
