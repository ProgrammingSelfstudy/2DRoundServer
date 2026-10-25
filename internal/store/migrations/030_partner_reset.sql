-- When a hero may be reborn again (unix seconds): the client shows "don't reborn the same hero
-- too often" until then (HeroVo.reset_time, 11067 end_time).
ALTER TABLE partners ADD COLUMN reset_until INTEGER NOT NULL DEFAULT 0;
