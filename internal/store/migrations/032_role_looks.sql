-- Personal settings of a role (the signature, what it shows, and which cosmetics it uses) and
-- the cosmetics it owns. kind: face, backdrop, avatar (avatar frame), bubble, title.
CREATE TABLE role_settings (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  signature TEXT NOT NULL DEFAULT '',
  show_vip INTEGER NOT NULL DEFAULT 0,
  auto_pk INTEGER NOT NULL DEFAULT 0,
  backdrop_id INTEGER NOT NULL DEFAULT 0,
  avatar_id INTEGER NOT NULL DEFAULT 0,
  bubble_id INTEGER NOT NULL DEFAULT 0,
  title_id INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE role_looks (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  base_id INTEGER NOT NULL,
  end_time INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, kind, base_id)
);
