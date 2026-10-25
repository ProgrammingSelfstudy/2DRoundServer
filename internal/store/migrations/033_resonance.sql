-- The resonance crystal's slots: a hero in a slot shows the crystal's level; a slot that was
-- emptied cools down until cool_time (unix seconds). partner_id is NULL for an empty slot (also
-- when the hero in it was deleted). gold_count / item_count are the slots the role unlocked.
CREATE TABLE resonance_slots (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  pos INTEGER NOT NULL,
  partner_id INTEGER REFERENCES partners(partner_id) ON DELETE SET NULL,
  cool_time INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, pos)
);
CREATE UNIQUE INDEX resonance_slots_partner ON resonance_slots(partner_id) WHERE partner_id IS NOT NULL;
CREATE TABLE role_resonance (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  gold_count INTEGER NOT NULL DEFAULT 0,
  item_count INTEGER NOT NULL DEFAULT 0
);
