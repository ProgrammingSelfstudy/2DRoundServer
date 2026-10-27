-- Payment by hand: a player asks to top up (an order), pays by the QR code the operator put in pay_config and the operator
-- confirms the order in the admin page, which delivers it. status: 0 waiting, 1 paid and delivered, 2 cancelled.
CREATE TABLE pay_orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  order_no TEXT NOT NULL UNIQUE,
  rid INTEGER NOT NULL,
  srv_id TEXT NOT NULL,
  charge_id INTEGER NOT NULL,
  amount_cents INTEGER NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  paid_at INTEGER NOT NULL DEFAULT 0,
  operator TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX pay_orders_rid ON pay_orders(rid);
CREATE INDEX pay_orders_status ON pay_orders(status, created_at);

-- Settings of the payment page: the QR code images (data URLs) of each channel and the notice text.
CREATE TABLE pay_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- What a role has paid in total, the VIP experience that came with it and how often each product was bought.
CREATE TABLE role_charge (
  rid INTEGER PRIMARY KEY,
  total_cents INTEGER NOT NULL DEFAULT 0,
  vip_exp INTEGER NOT NULL DEFAULT 0,
  bought TEXT NOT NULL DEFAULT '{}',
  first_at INTEGER NOT NULL DEFAULT 0,
  last_at INTEGER NOT NULL DEFAULT 0
);
