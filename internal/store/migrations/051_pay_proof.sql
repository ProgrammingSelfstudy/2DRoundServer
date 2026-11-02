-- A player may upload a screenshot of the payment on the order page; the operator looks at it before confirming. The image is kept
-- apart from the order so that listing orders stays light; proof_at on the order says whether (and when) one was uploaded.
ALTER TABLE pay_orders ADD COLUMN proof_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE pay_proofs (
  order_no TEXT PRIMARY KEY,
  mime TEXT NOT NULL,
  data BLOB NOT NULL
);
