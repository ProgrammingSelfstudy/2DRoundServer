-- Consecutive H5 advanced draws since the last rare hero, including free draws.
ALTER TABLE roles ADD COLUMN advanced_summon_misses INTEGER NOT NULL DEFAULT 0 CHECK (advanced_summon_misses >= 0);
