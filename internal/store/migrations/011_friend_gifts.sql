-- Friend gifts ("友情点"): one row per gift a role sends a friend on a day (day is YYYYMMDD in
-- the game's day zone). claimed_day is 0 until the receiver draws it, then the day it was
-- drawn. A gift can only be drawn on the day it was sent.
CREATE TABLE friend_gifts (
  from_rid    INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  to_rid      INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  day         INTEGER NOT NULL,
  claimed_day INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (from_rid, to_rid, day)
);
CREATE INDEX idx_friend_gifts_to ON friend_gifts(to_rid, day);
