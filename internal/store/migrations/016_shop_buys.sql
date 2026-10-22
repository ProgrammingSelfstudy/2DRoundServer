-- Purchases in the exchange shops: how many pieces of one good a role bought on a day
-- (day is YYYYMMDD in the game's day zone). Day / week / month limits sum these rows.
CREATE TABLE shop_buys (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  eid INTEGER NOT NULL,
  day INTEGER NOT NULL,
  num INTEGER NOT NULL,
  PRIMARY KEY (rid, eid, day)
);
