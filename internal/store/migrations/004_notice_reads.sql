-- Which notices a role has opened (the client greys those out). Deleting a role or
-- a notice removes its rows.
CREATE TABLE role_notice_reads (
  rid       INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  notice_id INTEGER NOT NULL REFERENCES notices(id) ON DELETE CASCADE,
  read_at   INTEGER NOT NULL,
  PRIMARY KEY (rid, notice_id)
);
CREATE INDEX idx_notices_window ON notices(status, start_at, end_at);
