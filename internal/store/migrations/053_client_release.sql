-- What the new clients (mini game, H5, app) are told at start-up. client_config holds the settings the operator edits (CDN base, notice,
-- feature switches, forced update); platform "*" is the default and a platform's own row wins. client_release is one published bundle of
-- resources per platform: status 0 draft, 1 published, 2 rolled back. Only the newest published release per platform is served, a
-- whitelist (comma separated account names) limits a release to testers while it is non-empty.
CREATE TABLE client_config (
  platform TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (platform, key)
);
CREATE TABLE client_release (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  platform TEXT NOT NULL,
  version TEXT NOT NULL,
  manifest_url TEXT NOT NULL,
  manifest_hash TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL DEFAULT 0,
  whitelist TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  published_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE (platform, version)
);
CREATE INDEX client_release_platform ON client_release(platform, status, published_at);
