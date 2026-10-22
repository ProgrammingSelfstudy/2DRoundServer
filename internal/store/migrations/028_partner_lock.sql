-- A hero the owner locked (HeroConst.LockType.eHeroLock): it cannot be used as star-up fodder
-- or decomposed.
ALTER TABLE partners ADD COLUMN locked INTEGER NOT NULL DEFAULT 0;
