-- The talent skills ("天赋栏") a hero has learned: one skill per slot (pos 1-3).
-- skill_id is the skill's current level id; levelling up swaps it for the next one.
CREATE TABLE partner_talents (
  partner_id INTEGER NOT NULL REFERENCES partners(partner_id) ON DELETE CASCADE,
  pos INTEGER NOT NULL,
  skill_id INTEGER NOT NULL,
  PRIMARY KEY (partner_id, pos)
);
