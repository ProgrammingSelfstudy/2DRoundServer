ALTER TABLE pay_orders ADD COLUMN offer_id INTEGER NOT NULL DEFAULT 0;

UPDATE pay_orders SET offer_id=-1 WHERE id IN (
  SELECT COALESCE(MIN(CASE WHEN status=1 THEN id END),MIN(CASE WHEN status=0 THEN id END))
  FROM pay_orders WHERE charge_id IN (301,1705) GROUP BY rid
);

CREATE UNIQUE INDEX pay_orders_live_offer ON pay_orders(rid,offer_id)
WHERE offer_id<>0 AND status IN (0,1);
