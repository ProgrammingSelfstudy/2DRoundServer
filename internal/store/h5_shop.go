package store

import (
	"database/sql"
	"errors"
)

var (
	ErrShopLimit = errors.New("store: shop limit reached")
	ErrShopOrder = errors.New("store: request ID belongs to a different shop order")
)

// H5ShopOrder is the quote and count the player confirmed.
type H5ShopOrder struct {
	OfferID, ItemID, CurrencyID, Bundle, Price, Count int64
}

const h5ShopReceiptQuery = `SELECT offer_id,item_id,bundle,price,count,currency_id
	FROM h5_shop_purchases WHERE rid=? AND request_id=?`

func scanH5ShopReceipt(row *sql.Row) (H5ShopOrder, error) {
	var r H5ShopOrder
	err := row.Scan(&r.OfferID, &r.ItemID, &r.Bundle, &r.Price, &r.Count, &r.CurrencyID)
	return r, wrapNoRows(err)
}

func (s *Store) H5ShopReceipt(rid int64, requestID string) (H5ShopOrder, error) {
	return scanH5ShopReceipt(s.db.QueryRow(h5ShopReceiptQuery, rid, requestID))
}

// BuyH5Shop commits the receipt, purchase count, debit, and delivery together.
// Taking the write lock with the receipt insert also serializes competing purchases
// before they read the shared limits. A retry returns the original receipt.
func (s *Store) BuyH5Shop(rid int64, requestID string, r H5ShopOrder, day, fromDay, toDay, limit int64) (H5ShopOrder, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return H5ShopOrder{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO h5_shop_purchases(rid,request_id,offer_id,item_id,bundle,price,count,currency_id)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(rid,request_id) DO NOTHING`,
		rid, requestID, r.OfferID, r.ItemID, r.Bundle, r.Price, r.Count, r.CurrencyID)
	if err != nil {
		return H5ShopOrder{}, err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return H5ShopOrder{}, err
	}
	if inserted == 0 {
		saved, err := scanH5ShopReceipt(tx.QueryRow(h5ShopReceiptQuery, rid, requestID))
		if err != nil {
			return H5ShopOrder{}, err
		}
		if saved != r {
			return H5ShopOrder{}, ErrShopOrder
		}
		return saved, nil
	}
	spend := map[int64]int64{r.CurrencyID: r.Price * r.Count}
	delivery := map[int64]int64{r.ItemID: r.Bundle * r.Count}
	if err := shopBuyTx(tx, rid, r.OfferID, day, r.Count, fromDay, toDay, limit, spend, nil, nil, delivery); err != nil {
		return H5ShopOrder{}, err
	}
	return r, tx.Commit()
}
