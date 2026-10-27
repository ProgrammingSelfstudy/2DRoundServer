package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

const (
	PayWaiting   = 0
	PayDone      = 1
	PayCancelled = 2
)

// PayOrder is a top-up a player asked for.
type PayOrder struct {
	ID          int64  `json:"id"`
	OrderNo     string `json:"order_no"`
	RID         int64  `json:"rid"`
	SrvID       string `json:"srv_id"`
	ChargeID    int64  `json:"charge_id"`
	AmountCents int64  `json:"amount_cents"`
	Status      int    `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	PaidAt      int64  `json:"paid_at"`
	Operator    string `json:"operator"`
	Note        string `json:"note"`
}

const payCols = `id,order_no,rid,srv_id,charge_id,amount_cents,status,created_at,paid_at,operator,note`

func scanPay(sc interface{ Scan(...any) error }) (*PayOrder, error) {
	var o PayOrder
	err := sc.Scan(&o.ID, &o.OrderNo, &o.RID, &o.SrvID, &o.ChargeID, &o.AmountCents, &o.Status, &o.CreatedAt, &o.PaidAt, &o.Operator, &o.Note)
	return &o, wrapNoRows(err)
}

// CreatePayOrder opens an order; the number is made by the caller and must be new.
func (s *Store) CreatePayOrder(o PayOrder) (*PayOrder, error) {
	o.CreatedAt = s.unix()
	res, err := s.db.Exec(`INSERT INTO pay_orders(order_no,rid,srv_id,charge_id,amount_cents,status,created_at) VALUES(?,?,?,?,?,0,?)`,
		o.OrderNo, o.RID, o.SrvID, o.ChargeID, o.AmountCents, o.CreatedAt)
	if err != nil {
		return nil, err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = PayWaiting
	return &o, nil
}

func (s *Store) PayOrderByNo(no string) (*PayOrder, error) {
	return scanPay(s.db.QueryRow(`SELECT `+payCols+` FROM pay_orders WHERE order_no=?`, no))
}

// PayOrders lists orders newest first; status -1 = all.
func (s *Store) PayOrders(status, limit int) ([]PayOrder, error) {
	q := `SELECT ` + payCols + ` FROM pay_orders`
	var args []any
	if status >= 0 {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayOrder
	for rows.Next() {
		o, err := scanPay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// PendingPayOrders counts a role's orders that wait for payment.
func (s *Store) PendingPayOrders(rid int64) (n int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pay_orders WHERE rid=? AND status=0`, rid).Scan(&n)
	return
}

// CancelPayOrder closes a waiting order; ErrConflict when it is not waiting any more.
func (s *Store) CancelPayOrder(no, operator, note string) error {
	res, err := s.db.Exec(`UPDATE pay_orders SET status=2,operator=?,note=? WHERE order_no=? AND status=0`, operator, note, no)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// RoleCharge is what a role has paid.
type RoleCharge struct {
	RID        int64            `json:"rid"`
	TotalCents int64            `json:"total_cents"`
	VIPExp     int64            `json:"vip_exp"`
	Bought     map[string]int64 `json:"bought"` // charge id -> how often
	FirstAt    int64            `json:"first_at"`
	LastAt     int64            `json:"last_at"`
}

func (s *Store) RoleCharge(rid int64) (*RoleCharge, error) {
	c := &RoleCharge{RID: rid, Bought: map[string]int64{}}
	var bought string
	err := s.db.QueryRow(`SELECT total_cents,vip_exp,bought,first_at,last_at FROM role_charge WHERE rid=?`, rid).Scan(&c.TotalCents, &c.VIPExp, &bought, &c.FirstAt, &c.LastAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(bought), &c.Bought); err != nil {
		return nil, err
	}
	if c.Bought == nil {
		c.Bought = map[string]int64{}
	}
	return c, nil
}

// ConfirmPayOrder marks a waiting order paid, credits what it delivers and adds it to the role's totals, in one
// transaction. ErrConflict, changing nothing, when the order is not waiting (it was confirmed or cancelled already).
func (s *Store) ConfirmPayOrder(no, operator, note string, vipExp int64, gainAssets, gainItems map[int64]int64) (*PayOrder, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := scanPay(tx.QueryRow(`SELECT `+payCols+` FROM pay_orders WHERE order_no=?`, no))
	if err != nil {
		return nil, err
	}
	now := s.unix()
	res, err := tx.Exec(`UPDATE pay_orders SET status=1,paid_at=?,operator=?,note=? WHERE order_no=? AND status=0`, now, operator, note, no)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	if err := creditTx(tx, o.RID, gainAssets, gainItems); err != nil {
		return nil, err
	}
	c := &RoleCharge{Bought: map[string]int64{}}
	var bought string
	switch err := tx.QueryRow(`SELECT total_cents,vip_exp,bought,first_at FROM role_charge WHERE rid=?`, o.RID).Scan(&c.TotalCents, &c.VIPExp, &bought, &c.FirstAt); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal([]byte(bought), &c.Bought); err != nil {
			return nil, err
		}
	}
	if c.FirstAt == 0 {
		c.FirstAt = now
	}
	c.TotalCents += o.AmountCents
	c.VIPExp += vipExp
	c.Bought[itoa(o.ChargeID)]++
	b, _ := json.Marshal(c.Bought)
	if _, err := tx.Exec(`INSERT INTO role_charge(rid,total_cents,vip_exp,bought,first_at,last_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET total_cents=excluded.total_cents,vip_exp=excluded.vip_exp,bought=excluded.bought,first_at=excluded.first_at,last_at=excluded.last_at`,
		o.RID, c.TotalCents, c.VIPExp, string(b), c.FirstAt, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	o.Status, o.PaidAt, o.Operator, o.Note = PayDone, now, operator, note
	return o, nil
}

// PayConfig reads the payment page settings.
func (s *Store) PayConfig() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key,value FROM pay_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetPayConfig writes one setting; an empty value removes it.
func (s *Store) SetPayConfig(key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM pay_config WHERE key=?`, key)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO pay_config(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
