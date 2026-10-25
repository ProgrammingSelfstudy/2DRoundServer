-- The personal space: the home city and who may write on the message board (settings), who follows whom,
-- the message board, and the honor badges a role owns and wears.
ALTER TABLE role_settings ADD COLUMN city_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE role_settings ADD COLUMN bbs_set INTEGER NOT NULL DEFAULT 1;
CREATE TABLE role_follows (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  target_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (rid, target_rid)
);
CREATE INDEX role_follows_target ON role_follows(target_rid);
CREATE TABLE room_bbs (
  bbs_id INTEGER PRIMARY KEY AUTOINCREMENT,
  owner_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  author_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  type INTEGER NOT NULL,
  reply_name TEXT NOT NULL DEFAULT '',
  msg TEXT NOT NULL,
  time INTEGER NOT NULL
);
CREATE INDEX room_bbs_owner ON room_bbs(owner_rid, bbs_id);
CREATE TABLE role_badges (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  badge_id INTEGER NOT NULL,
  time INTEGER NOT NULL,
  PRIMARY KEY (rid, badge_id)
);
CREATE TABLE role_badge_use (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  pos INTEGER NOT NULL,
  badge_id INTEGER NOT NULL,
  PRIMARY KEY (rid, pos)
);
