-- Daily quests: progress of a role's quest on one day (day is YYYYMMDD in the game's day zone).
-- claimed is 1 once the reward was taken.
CREATE TABLE daily_quests (
  rid      INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  quest_id INTEGER NOT NULL,
  day      INTEGER NOT NULL,
  value    INTEGER NOT NULL DEFAULT 0,
  claimed  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, quest_id, day)
);
