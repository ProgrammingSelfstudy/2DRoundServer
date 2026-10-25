-- One JSON document per role and game module, for modules whose state is not worth a table of its own.
CREATE TABLE module_state (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  module TEXT NOT NULL,
  data TEXT NOT NULL,
  PRIMARY KEY (rid, module)
);
-- The same for the whole server (rankings, shared bosses, rotation clocks).
CREATE TABLE global_state (
  srv_id TEXT NOT NULL,
  module TEXT NOT NULL,
  data TEXT NOT NULL,
  PRIMARY KEY (srv_id, module)
);
