package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLoginDayClaimsAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login-day.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	if err := first.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	account, err := first.FindOrCreateAccount(PlatformH5, "welfare")
	if err != nil {
		t.Fatal(err)
	}
	role, err := first.CreateRole(account.ID, "symlf_1", "领取测试", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SaveState(role.RID, "loginday", map[string][]int64{"taken": {1}}); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if _, err := second.ClaimLoginDay(role.RID, 1, map[int64]int64{3: 100}, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing receipt rejected with %v, want conflict", err)
	}

	start := make(chan struct{})
	results := make(chan error, 3)
	for _, claim := range []struct {
		store *Store
		day   int64
	}{{first, 2}, {second, 2}, {second, 3}} {
		go func() {
			<-start
			_, err := claim.store.ClaimLoginDay(role.RID, claim.day, map[int64]int64{3: 10}, map[int64]int64{10403: 1})
			results <- err
		}()
	}
	close(start)
	var succeeded, conflicts int
	for range 3 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	state, err := first.LoginDay(role.RID)
	if err != nil || succeeded != 2 || conflicts != 1 || len(state.Taken) != 3 || !state.Has(1) || !state.Has(2) || !state.Has(3) {
		t.Fatalf("successes=%d conflicts=%d state=%+v err=%v", succeeded, conflicts, state, err)
	}
	assets, err := second.RoleAssets(role.RID)
	if err != nil || assets[3] != 20 {
		t.Fatalf("asset ledger: %v (%v)", assets, err)
	}
	items, err := second.RoleItems(role.RID)
	if err != nil || items[10403] != 2 {
		t.Fatalf("item ledger: %v (%v)", items, err)
	}
}

func TestLoginDayFailedCreditRollsBackReceipt(t *testing.T) {
	e := newMailEnv(t)
	if _, err := e.s.db.Exec(`CREATE TRIGGER reject_login_reward BEFORE INSERT ON role_items BEGIN SELECT RAISE(ABORT, 'test ledger failure'); END`); err != nil {
		t.Fatal(err)
	}
	assets, items := map[int64]int64{3: 10}, map[int64]int64{10403: 1}
	if _, err := e.s.ClaimLoginDay(e.a.RID, 1, assets, items); err == nil {
		t.Fatal("claim should fail at the item ledger")
	}
	var receipt LoginDayState
	if found, err := e.s.LoadState(e.a.RID, LoginDayStateKey, &receipt); err != nil || found {
		t.Fatalf("receipt persisted despite rollback: %+v (found=%v, err=%v)", receipt, found, err)
	}
	if got, err := e.s.RoleAssets(e.a.RID); err != nil || got[3] != 0 {
		t.Fatalf("asset credited despite rollback: %v (%v)", got, err)
	}
	if got, err := e.s.RoleItems(e.a.RID); err != nil || got[10403] != 0 {
		t.Fatalf("item credited despite rollback: %v (%v)", got, err)
	}
	if _, err := e.s.db.Exec(`DROP TRIGGER reject_login_reward`); err != nil {
		t.Fatal(err)
	}
	if state, err := e.s.ClaimLoginDay(e.a.RID, 1, assets, items); err != nil || !state.Has(1) {
		t.Fatalf("retry did not record the receipt: %+v (%v)", state, err)
	}
	if got, err := e.s.RoleAssets(e.a.RID); err != nil || got[3] != 10 {
		t.Fatalf("retry asset ledger: %v (%v)", got, err)
	}
	if got, err := e.s.RoleItems(e.a.RID); err != nil || got[10403] != 1 {
		t.Fatalf("retry item ledger: %v (%v)", got, err)
	}
}
