-- Hero formations: one per role and kind (the client's PartnerConst.Fun_Form: 0 campaign, 1 arena, ...).
-- Slots name the heroes by partner_id; a hero that no longer exists is simply not listed.
CREATE TABLE formations (
  rid            INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  type           INTEGER NOT NULL,
  formation_type INTEGER NOT NULL,
  hallows_id     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, type)
);
CREATE TABLE formation_slots (
  rid        INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  type       INTEGER NOT NULL,
  pos        INTEGER NOT NULL,
  partner_id INTEGER NOT NULL,
  PRIMARY KEY (rid, type, pos)
);
