-- The material dungeons (stone dungeons): the attempts a role made today per kind (the day turns at 5:00,
-- stored as that day's YYYYMMDD) and the dungeons it has cleared.
CREATE TABLE stone_dungeon_state (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  type INTEGER NOT NULL,
  day INTEGER NOT NULL,
  count INTEGER NOT NULL,
  PRIMARY KEY (rid, type)
);
CREATE TABLE stone_dungeon_clears (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  id INTEGER NOT NULL,
  PRIMARY KEY (rid, id)
);
