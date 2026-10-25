-- The resonance stone tablet: its level and the highest total of stars the offering heroes ever had,
-- and the alchemy batch in progress (units of hero exp put in, when it started, seconds per unit,
-- how many were already collected).
ALTER TABLE role_resonance ADD COLUMN stone_lev INTEGER NOT NULL DEFAULT 1;
ALTER TABLE role_resonance ADD COLUMN max_stars INTEGER NOT NULL DEFAULT 0;
CREATE TABLE resonance_extract (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  total INTEGER NOT NULL,
  start_time INTEGER NOT NULL,
  per_sec INTEGER NOT NULL,
  collected INTEGER NOT NULL DEFAULT 0
);
