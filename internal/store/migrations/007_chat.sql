-- Private messages to a role that was offline. They are handed over (and deleted)
-- the next time the role enters the game.
CREATE TABLE chat_offline (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  from_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  to_rid   INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  len      INTEGER NOT NULL DEFAULT 0,   -- the client's content flag (0 text, 1 voice, 2 shown item)
  msg      TEXT NOT NULL,
  sent_at  INTEGER NOT NULL
);
CREATE INDEX idx_chat_offline_to ON chat_offline(to_rid, id);
