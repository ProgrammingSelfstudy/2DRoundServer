-- Consecutive H5 advanced hero grants since the last rare hero, excluding shard-only outcomes.
ALTER TABLE roles ADD COLUMN advanced_summon_misses INTEGER NOT NULL DEFAULT 0 CHECK (advanced_summon_misses >= 0);
