package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestH5ShopConcurrentOrdersAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	a, err := st.FindOrCreateAccount(PlatformH5, "shopqa")
	if err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(a.ID, "symlf_1", "商城测试", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{1: 100000}, nil); err != nil {
		t.Fatal(err)
	}
	order := H5ShopOrder{OfferID: -10152, ItemID: 10152, CurrencyID: 1, Bundle: 1, Price: 20000, Count: 1}
	const day = 20261021
	race := func(a, b string) (error, error) {
		t.Helper()
		start := make(chan struct{})
		result := make(chan error, 2)
		for _, key := range []string{a, b} {
			go func() {
				<-start
				_, err := st.BuyH5Shop(role.RID, key, order, day, day, day, 2)
				result <- err
			}()
		}
		close(start)
		return <-result, <-result
	}
	if first, second := race("same", "same"); first != nil || second != nil {
		t.Fatalf("retries: %v, %v", first, second)
	}
	first, second := race("one", "two")
	if !((first == nil && errors.Is(second, ErrShopLimit)) || (second == nil && errors.Is(first, ErrShopLimit))) {
		t.Fatalf("last unit: %v, %v", first, second)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	if got, err := reopened.H5ShopReceipt(role.RID, "same"); err != nil || got != order {
		t.Fatalf("reopened receipt: %v, %v", got, err)
	}
	changed := order
	changed.Count++
	if _, err := reopened.BuyH5Shop(role.RID, "same", changed, day, day, day, 2); !errors.Is(err, ErrShopOrder) {
		t.Fatalf("reused ID with a different count: %v", err)
	}
	assets, err := reopened.RoleAssets(role.RID)
	if err != nil || assets[1] != 60000 {
		t.Fatalf("debits: %v, %v", assets, err)
	}
	items, err := reopened.RoleItems(role.RID)
	if err != nil || items[10152] != 2 {
		t.Fatalf("deliveries: %v, %v", items, err)
	}
	bought, err := reopened.ShopBought(role.RID, day, day)
	if err != nil || bought[order.OfferID] != 2 {
		t.Fatalf("purchase count: %v, %v", bought, err)
	}
}

func TestH5ShopRollsBackWhenDeliveryFails(t *testing.T) {
	e := newMailEnv(t)
	if err := e.s.Exchange(e.a.RID, nil, nil, map[int64]int64{1: 40000}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.db.Exec(`CREATE TRIGGER reject_shop_delivery BEFORE INSERT ON role_items
		BEGIN SELECT RAISE(ABORT, 'delivery failed'); END`); err != nil {
		t.Fatal(err)
	}
	order := H5ShopOrder{OfferID: -10152, ItemID: 10152, CurrencyID: 1, Bundle: 1, Price: 20000, Count: 1}
	const day = 20261021
	if _, err := e.s.BuyH5Shop(e.a.RID, "failed", order, day, day, day, 1); err == nil {
		t.Fatal("delivery should fail")
	}
	if _, err := e.s.H5ShopReceipt(e.a.RID, "failed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed purchase must leave no receipt: %v", err)
	}
	if assets, err := e.s.RoleAssets(e.a.RID); err != nil || assets[1] != 40000 {
		t.Fatalf("debit should roll back: %v, %v", assets, err)
	}
	if bought, err := e.s.ShopBought(e.a.RID, day, day); err != nil || bought[order.OfferID] != 0 {
		t.Fatalf("quota should roll back: %v, %v", bought, err)
	}
	if _, err := e.s.db.Exec(`DROP TRIGGER reject_shop_delivery`); err != nil {
		t.Fatal(err)
	}
	if got, err := e.s.BuyH5Shop(e.a.RID, "failed", order, day, day, day, 1); err != nil || got != order {
		t.Fatalf("retry after delivery recovered: %v, %v", got, err)
	}
	if items, err := e.s.RoleItems(e.a.RID); err != nil || items[10152] != 1 {
		t.Fatalf("retried delivery: %v, %v", items, err)
	}
}
