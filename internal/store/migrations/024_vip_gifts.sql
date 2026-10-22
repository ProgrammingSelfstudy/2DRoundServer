-- VIP level gifts a role bought on the VIP page (one per level, once).
CREATE TABLE vip_gifts (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  lev INTEGER NOT NULL,
  PRIMARY KEY (rid, lev)
);
