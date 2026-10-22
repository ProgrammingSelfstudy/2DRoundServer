-- Daily sign-in: how many days a role has signed in during a month (month is
-- YYYYMM, last_day YYYYMMDD, both in the game's day zone). A role that has not
-- signed in this month has an old month and counts as 0.
CREATE TABLE checkin (
  rid      INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  month    INTEGER NOT NULL,
  count    INTEGER NOT NULL,
  last_day INTEGER NOT NULL
);
