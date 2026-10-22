-- 1 once the player has used the rename (10343). The client's is_first_rename
-- is the opposite: 1 while the free first rename is still available.
ALTER TABLE roles ADD COLUMN renamed INTEGER NOT NULL DEFAULT 0;
