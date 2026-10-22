-- Mail: direct mails have a role_mails row per recipient from the start; broadcast
-- mails (scope='broadcast', srv_id '' = every server) get a row per role lazily,
-- when the role first lists its mail.
ALTER TABLE mails ADD COLUMN scope TEXT NOT NULL DEFAULT 'direct';   -- direct | broadcast
ALTER TABLE mails ADD COLUMN type  INTEGER NOT NULL DEFAULT 1;       -- client mail type (it lists 1 and 3)

-- kind 'asset': item_id is an asset label (client Config.ItemData.data_assets_label2id)
-- kind 'item':  item_id is an item base id
ALTER TABLE mail_attachments ADD COLUMN kind TEXT NOT NULL DEFAULT 'item';

ALTER TABLE role_mails ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0;   -- soft delete

-- Ledgers that receive claimed attachments until the bag / currency systems exist.
CREATE TABLE role_assets (
  rid   INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  label INTEGER NOT NULL,
  val   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, label)
);
CREATE TABLE role_items (
  rid     INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  item_id INTEGER NOT NULL,
  num     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, item_id)
);
CREATE INDEX idx_mails_scope ON mails(scope, srv_id, created_at);
