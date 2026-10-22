-- How many times the role bought extra hero slots (each purchase adds slots).
ALTER TABLE roles ADD COLUMN hero_slot_buys INTEGER NOT NULL DEFAULT 0;
