package store

// VarietyShop is the saved state of a role's tavern shop.
type VarietyShop struct {
	Day, FreeLeft int64
	Goods         string // JSON, owned by the game package
}

// VarietyShop returns the role's tavern shop, or ErrNotFound when it never opened it.
func (s *Store) VarietyShop(rid int64) (*VarietyShop, error) {
	v := VarietyShop{}
	err := s.db.QueryRow(`SELECT day,free_left,goods FROM variety_shop WHERE rid=?`, rid).Scan(&v.Day, &v.FreeLeft, &v.Goods)
	if err != nil {
		return nil, wrapNoRows(err)
	}
	return &v, nil
}

// SaveVarietyShop replaces the role's tavern shop.
func (s *Store) SaveVarietyShop(rid int64, v VarietyShop) error {
	_, err := s.db.Exec(`INSERT INTO variety_shop(rid,day,free_left,goods) VALUES(?,?,?,?)
		ON CONFLICT(rid) DO UPDATE SET day=excluded.day, free_left=excluded.free_left, goods=excluded.goods`, rid, v.Day, v.FreeLeft, v.Goods)
	return err
}

// VarietyBuy replaces the shelf goods oldGoods by newGoods (a good marked as bought) and
// takes the price and gives the item, in one transaction. It returns ErrConflict, changing
// nothing, when the shelf is no longer oldGoods or the role cannot pay.
func (s *Store) VarietyBuy(rid int64, oldGoods, newGoods string, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE variety_shop SET goods=? WHERE rid=? AND goods=?`, newGoods, rid, oldGoods)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}
