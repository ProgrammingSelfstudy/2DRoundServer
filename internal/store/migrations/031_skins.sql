-- Skins a role owns (end_time 0 = permanent, else unix seconds), and the skin a hero wears
-- (0 = none). Skins are handed out from the admin page.
CREATE TABLE role_skins (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  skin_id INTEGER NOT NULL,
  end_time INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, skin_id)
);
ALTER TABLE partners ADD COLUMN use_skin INTEGER NOT NULL DEFAULT 0;
