-- The tavern's random shop (杂货店): one row per role with the day it was dealt for (the
-- free-refresh day), the free refreshes left and the six goods now on the shelf (JSON: the
-- pool index and whether it was bought).
CREATE TABLE variety_shop (
  rid       INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  day       INTEGER NOT NULL,
  free_left INTEGER NOT NULL,
  goods     TEXT NOT NULL
);
