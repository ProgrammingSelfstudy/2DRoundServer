-- The VIP level of the role (set from the admin page; there is no recharge).
ALTER TABLE roles ADD COLUMN vip_lev INTEGER NOT NULL DEFAULT 0;
