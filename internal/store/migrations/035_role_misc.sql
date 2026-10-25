-- The role model in use (0 = the one the face gives), redeem codes, and reports of other players.
ALTER TABLE role_settings ADD COLUMN look_id INTEGER NOT NULL DEFAULT 0;
CREATE TABLE redeem_codes (
  code TEXT PRIMARY KEY,
  note TEXT NOT NULL DEFAULT '',
  attachments TEXT NOT NULL,
  max_uses INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE redeem_uses (
  code TEXT NOT NULL REFERENCES redeem_codes(code) ON DELETE CASCADE,
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  used_at INTEGER NOT NULL,
  PRIMARY KEY (code, rid)
);
CREATE TABLE role_reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rid INTEGER NOT NULL,
  target_rid INTEGER NOT NULL,
  target_srv_id TEXT NOT NULL,
  kind INTEGER NOT NULL,
  msg TEXT NOT NULL,
  history TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
