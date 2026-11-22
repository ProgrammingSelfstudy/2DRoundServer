package store

import (
	"errors"
	"testing"
)

func TestStoneSweepRejectsStalePriceAndInsufficientFunds(t *testing.T) {
	s := open(t)
	rid := moduleRole(t, s).RID
	const day = 20261122
	loot := map[int64]int64{1: 100}
	if err := s.TakeStoneAttempt(rid, 1, day, 0, 3, 0, loot, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.TakeStoneAttempt(rid, 1, day, 0, 3, 0, loot, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale count: %v", err)
	}
	if err := s.TakeStoneAttempt(rid, 1, day, 1, 3, 0, loot, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.TakeStoneAttempt(rid, 1, day, 2, 3, 50, loot, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("no diamonds: %v", err)
	}
	counts, err := s.StoneCounts(rid, day)
	if err != nil || counts[1] != 2 {
		t.Fatalf("counts: %v, %v", counts, err)
	}
	balances, err := s.RoleAssets(rid)
	if err != nil || balances[1] != 200 || balances[3] != 0 {
		t.Fatalf("balances: %v, %v", balances, err)
	}
	if err := s.TakeStoneAttempt(rid, 1, day+1, 0, 3, 0, loot, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.TakeStoneAttempt(rid, 1, day, 0, 3, 0, loot, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired attempt: %v", err)
	}
	counts, err = s.StoneCounts(rid, day+1)
	if err != nil || counts[1] != 1 {
		t.Fatalf("counts after rollover: %v, %v", counts, err)
	}
}

func TestStoneFirstClearRollsBackWithFailedLoot(t *testing.T) {
	s := open(t)
	rid := moduleRole(t, s).RID
	assets := map[int64]int64{1: 100}
	firstItems := map[int64]int64{10001: 5}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_first_loot BEFORE INSERT ON role_items
		WHEN NEW.item_id=10001 BEGIN SELECT RAISE(ABORT, 'first loot unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if first, err := s.ClearStoneDungeon(rid, 101, assets, nil, nil, firstItems); err == nil || first {
		t.Fatalf("clear with failed loot: %v, %v", first, err)
	}
	if clears, err := s.StoneCleared(rid); err != nil || len(clears) != 0 {
		t.Fatalf("rolled-back clear: %v, %v", clears, err)
	}
	if ledger, err := s.RoleAssets(rid); err != nil || ledger[1] != 0 {
		t.Fatalf("rolled-back reward: %v, %v", ledger, err)
	}

	if _, err := s.db.Exec(`DROP TRIGGER fail_first_loot`); err != nil {
		t.Fatal(err)
	}
	if first, err := s.ClearStoneDungeon(rid, 101, assets, nil, nil, firstItems); err != nil || !first {
		t.Fatalf("first clear retry: %v, %v", first, err)
	}
	if first, err := s.ClearStoneDungeon(rid, 101, assets, nil, nil, firstItems); err != nil || first {
		t.Fatalf("repeat clear: %v, %v", first, err)
	}
	if ledger, err := s.RoleItems(rid); err != nil || ledger[10001] != 5 {
		t.Fatalf("first-clear payout: %v, %v", ledger, err)
	}
	if ledger, err := s.RoleAssets(rid); err != nil || ledger[1] != 200 {
		t.Fatalf("regular payouts: %v, %v", ledger, err)
	}
}

func TestExpeditionProgressAndRewardsRejectStaleSettlement(t *testing.T) {
	s := open(t)
	rid := moduleRole(t, s).RID
	const module = "heroexpedit"
	if err := s.SaveState(rid, module, map[string]int{"Guard": 7}); err != nil {
		t.Fatal(err)
	}
	before, _, err := s.RoleModule(rid, module)
	if err != nil {
		t.Fatal(err)
	}
	change := ModuleStateChange{RID: rid, Previous: before, Next: map[string]int{"Guard": 9}, GainAssets: map[int64]int64{1: 70500, 25: 101}}
	if err := s.CommitModuleState(module, change); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitModuleState(module, change); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate settlement: %v", err)
	}
	balances, err := s.RoleAssets(rid)
	if err != nil || balances[1] != 70500 || balances[25] != 101 {
		t.Fatalf("balances: %v, %v", balances, err)
	}
	after, _, err := s.RoleModule(rid, module)
	if err != nil || string(after) != `{"Guard":9}` {
		t.Fatalf("state: %s, %v", after, err)
	}
}

func TestExpeditionHireRollsBackWhenLenderChanges(t *testing.T) {
	s := open(t)
	borrower := moduleRole(t, s)
	lender, err := s.CreateRole(borrower.AccountID, borrower.SrvID, "lender", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	const module = "heroexpedit"
	if err := s.SaveState(lender.RID, module, map[string]int{"Borrowed": 1}); err != nil {
		t.Fatal(err)
	}
	next := []ModuleStateChange{
		{RID: borrower.RID, Next: map[string]int{"Hired": 1}},
		{RID: lender.RID, Previous: []byte(`{"Borrowed":0}`), Next: map[string]int{"Borrowed": 2}, GainAssets: map[int64]int64{14: 10}},
	}
	if err := s.CommitModuleState(module, next...); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale hire: %v", err)
	}
	if _, exists, err := s.RoleModule(borrower.RID, module); err != nil || exists {
		t.Fatalf("borrower's hire was committed: %v, %v", exists, err)
	}
	if ledger, err := s.RoleAssets(lender.RID); err != nil || ledger[14] != 0 {
		t.Fatalf("stale hire paid friendship points: %v, %v", ledger, err)
	}
	next[1].Previous = []byte(`{"Borrowed":1}`)
	if err := s.CommitModuleState(module, next...); err != nil {
		t.Fatal(err)
	}
	if data, _, err := s.RoleModule(borrower.RID, module); err != nil || string(data) != `{"Hired":1}` {
		t.Fatalf("borrower's hire: %s, %v", data, err)
	}
	if data, _, err := s.RoleModule(lender.RID, module); err != nil || string(data) != `{"Borrowed":2}` {
		t.Fatalf("lender's counter: %s, %v", data, err)
	}
	if ledger, err := s.RoleAssets(lender.RID); err != nil || ledger[14] != 10 {
		t.Fatalf("lender's reward: %v, %v", ledger, err)
	}
}
