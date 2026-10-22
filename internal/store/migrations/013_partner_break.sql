-- How many times a hero was broken through (the client's break_lev: each step raises the level cap).
ALTER TABLE partners ADD COLUMN break_lev INTEGER NOT NULL DEFAULT 0;
