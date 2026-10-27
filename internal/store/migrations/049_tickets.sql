-- Customer-service tickets a player files (10810) and the messages on them: kind 1 is a question
-- of the player, kind 2 an answer of the staff.
CREATE TABLE tickets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rid INTEGER NOT NULL,
  srv_id TEXT NOT NULL,
  issue_type INTEGER NOT NULL,
  title TEXT NOT NULL,
  phone TEXT NOT NULL,
  email TEXT NOT NULL,
  phone_info TEXT NOT NULL,
  state INTEGER NOT NULL,
  read INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  answered_at INTEGER NOT NULL DEFAULT 0,
  finished_at INTEGER NOT NULL DEFAULT 0,
  score INTEGER NOT NULL DEFAULT 0,
  scored_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX tickets_rid ON tickets(rid);
CREATE TABLE ticket_msgs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id INTEGER NOT NULL,
  kind INTEGER NOT NULL,
  at INTEGER NOT NULL,
  content TEXT NOT NULL
);
CREATE INDEX ticket_msgs_ticket ON ticket_msgs(ticket_id);
