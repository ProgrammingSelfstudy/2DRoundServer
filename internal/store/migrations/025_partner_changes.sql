-- The seer palace hero swap in progress: one pending swap per role, the hero and the
-- hero it would turn into. Saving turns the hero into new_bid; cancelling drops the row.
CREATE TABLE partner_changes (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  partner_id INTEGER NOT NULL REFERENCES partners(partner_id) ON DELETE CASCADE,
  new_bid INTEGER NOT NULL
);
