CREATE TABLE h5_shop_purchases (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  request_id TEXT NOT NULL,
  offer_id INTEGER NOT NULL,
  item_id INTEGER NOT NULL,
  bundle INTEGER NOT NULL,
  price INTEGER NOT NULL,
  count INTEGER NOT NULL,
  currency_id INTEGER NOT NULL,
  PRIMARY KEY (rid, request_id)
);
