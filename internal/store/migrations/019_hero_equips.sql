-- Equipment worn by heroes: one piece per slot (the slot is the item type, 1-4). The piece is
-- taken out of the role's item ledger when worn and goes back when taken off.
CREATE TABLE hero_equips (
  partner_id INTEGER NOT NULL REFERENCES partners(partner_id) ON DELETE CASCADE,
  slot       INTEGER NOT NULL,
  base_id    INTEGER NOT NULL,
  PRIMARY KEY (partner_id, slot)
);
