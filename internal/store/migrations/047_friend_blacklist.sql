-- The black list: rid put black_rid on it.
CREATE TABLE friend_blacklist (
  rid INTEGER NOT NULL,
  black_rid INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (rid, black_rid)
);
