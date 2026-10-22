-- The elite summon (精英召唤, group 50051): the picked up hero (its wish item id, 0 = none yet),
-- the draws so far, the draws since the up hero last came and the claimed cumulative rewards
-- (comma separated award ids).
CREATE TABLE elite_summon (
  rid       INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  lucky     INTEGER NOT NULL DEFAULT 0,
  times     INTEGER NOT NULL DEFAULT 0,
  since     INTEGER NOT NULL DEFAULT 0,
  claimed   TEXT NOT NULL DEFAULT ''
);
