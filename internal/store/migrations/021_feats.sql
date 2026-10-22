-- Cumulative counters that achievements count (key names are ours, e.g. "friend_present").
CREATE TABLE counters (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  key TEXT    NOT NULL,
  val INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, key)
);
-- Achievements whose reward the role took (the id is the client's data_get id).
CREATE TABLE feats_claimed (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  id  INTEGER NOT NULL,
  PRIMARY KEY (rid, id)
);
