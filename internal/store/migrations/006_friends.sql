-- A friendship is two rows, one per direction, so "my friends" is one index scan.
CREATE TABLE friends (
  rid        INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  friend_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (rid, friend_rid)
);
-- A pending request from from_rid to to_rid.
CREATE TABLE friend_requests (
  from_rid   INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  to_rid     INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (from_rid, to_rid)
);
CREATE INDEX idx_friend_requests_to ON friend_requests(to_rid, created_at);
-- When the role last disconnected; the friend list shows how long ago.
ALTER TABLE roles ADD COLUMN last_logout_at INTEGER NOT NULL DEFAULT 0;
