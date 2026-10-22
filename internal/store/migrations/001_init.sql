CREATE TABLE admins (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'operator',   -- superadmin | operator | viewer
  status        INTEGER NOT NULL DEFAULT 1,         -- 1 active, 0 disabled
  last_login_at INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

CREATE TABLE audit_logs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  admin_id    INTEGER NOT NULL,
  admin_name  TEXT NOT NULL,
  action      TEXT NOT NULL,
  target      TEXT NOT NULL DEFAULT '',
  detail_json TEXT NOT NULL DEFAULT '{}',
  ip          TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_audit_created ON audit_logs(created_at);

CREATE TABLE accounts (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  platform      TEXT NOT NULL,
  account       TEXT NOT NULL,
  password_hash TEXT NOT NULL DEFAULT '',
  open_id       TEXT NOT NULL DEFAULT '',
  union_id      TEXT NOT NULL DEFAULT '',
  status        INTEGER NOT NULL DEFAULT 1,         -- 1 normal, 0 banned
  ban_reason    TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  last_login_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(platform, account)
);

CREATE TABLE servers (
  srv_id       TEXT PRIMARY KEY,                    -- platform_zoneid
  name         TEXT NOT NULL,
  platform     TEXT NOT NULL,
  zone_id      INTEGER NOT NULL,
  host         TEXT NOT NULL,
  port         INTEGER NOT NULL DEFAULT 8800,
  ws_url       TEXT NOT NULL DEFAULT '',            -- for mini-game clients (wss)
  status       INTEGER NOT NULL DEFAULT 1,          -- 1 open, 0 maintenance
  is_new       INTEGER NOT NULL DEFAULT 0,
  is_recommend INTEGER NOT NULL DEFAULT 0,
  open_time    INTEGER NOT NULL DEFAULT 0,
  main_srv_id  TEXT NOT NULL DEFAULT '',
  group_id     INTEGER NOT NULL DEFAULT 1,
  created_at   INTEGER NOT NULL
);

CREATE TABLE roles (
  rid           INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id    INTEGER NOT NULL REFERENCES accounts(id),
  srv_id        TEXT NOT NULL REFERENCES servers(srv_id),
  name          TEXT NOT NULL,
  lev           INTEGER NOT NULL DEFAULT 1,
  sex           INTEGER NOT NULL DEFAULT 0,
  career        INTEGER NOT NULL DEFAULT 0,
  face_id       INTEGER NOT NULL DEFAULT 0,
  status        INTEGER NOT NULL DEFAULT 1,         -- 1 normal, 0 banned
  created_at    INTEGER NOT NULL,
  last_login_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(srv_id, name)
);
CREATE INDEX idx_roles_account ON roles(account_id, srv_id);

CREATE TABLE login_tokens (
  token      TEXT PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES accounts(id),
  expires_at INTEGER NOT NULL
);

CREATE TABLE notices (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  srv_id     TEXT NOT NULL DEFAULT '',              -- '' = all servers
  title      TEXT NOT NULL,
  content    TEXT NOT NULL,
  start_at   INTEGER NOT NULL DEFAULT 0,
  end_at     INTEGER NOT NULL DEFAULT 0,            -- 0 = never ends
  status     INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE mails (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  srv_id     TEXT NOT NULL DEFAULT '',              -- '' = all servers (broadcast)
  subject    TEXT NOT NULL,
  content    TEXT NOT NULL,
  from_name  TEXT NOT NULL DEFAULT 'GM',
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expire_at  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE mail_attachments (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  mail_id INTEGER NOT NULL REFERENCES mails(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL,
  num     INTEGER NOT NULL
);

-- One row per recipient. Broadcast mails are fanned out to existing roles when
-- sent and to new roles on creation (see store.DeliverBroadcastMails).
CREATE TABLE role_mails (
  mail_id    INTEGER NOT NULL REFERENCES mails(id) ON DELETE CASCADE,
  rid        INTEGER NOT NULL REFERENCES roles(rid),
  read_at    INTEGER NOT NULL DEFAULT 0,
  claimed_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (mail_id, rid)
);
CREATE INDEX idx_role_mails_rid ON role_mails(rid);
