-- Every role has one invite code, made when it is first asked for.
CREATE TABLE invite_codes (
  rid  INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  code INTEGER NOT NULL UNIQUE
);
-- Who entered whose code. A role can enter one code, once.
CREATE TABLE invite_binds (
  invitee_rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  inviter_rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_invite_binds_inviter ON invite_binds(inviter_rid, created_at);
-- How many times an inviter has claimed each reward step.
CREATE TABLE invite_claims (
  rid     INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  task_id INTEGER NOT NULL,
  times   INTEGER NOT NULL,
  PRIMARY KEY (rid, task_id)
);
