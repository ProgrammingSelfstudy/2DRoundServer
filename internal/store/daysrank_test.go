package store

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDaysRankConcurrentClaimsAndCreditRollback(t *testing.T) {
	first, dir := openFile(t)
	role := moduleRole(t, first)
	second, err := Open(filepath.Join(dir, "live", "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if err := first.SaveState(role.RID, DaysRankKey, DaysRankRole{Claimed: []int64{1002}}); err != nil {
		t.Fatal(err)
	}
	claim := func(st *Store, id int64) error {
		return st.ClaimDaysRankTask(role.RID, id, map[int64]int64{1: 10}, map[int64]int64{10102: 2}, func(*Store) error { return nil })
	}
	if _, err := first.db.Exec(`CREATE TRIGGER reject_daysrank_reward BEFORE INSERT ON role_items BEGIN SELECT RAISE(ABORT, 'test ledger failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := claim(first, 1001); err == nil {
		t.Fatal("credit should have failed")
	}
	var state DaysRankRole
	if _, err := second.LoadState(role.RID, DaysRankKey, &state); err != nil || !slices.Equal(state.Claimed, []int64{1002}) {
		t.Fatalf("receipt survived rollback: %v (%v)", state, err)
	}
	if assets, err := second.RoleAssets(role.RID); err != nil || assets[1] != 0 {
		t.Fatalf("partial credit survived rollback: %v (%v)", assets, err)
	}
	if _, err := first.db.Exec(`DROP TRIGGER reject_daysrank_reward`); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, st := range []*Store{first, second} {
		go func() {
			<-start
			results <- claim(st, 1001)
		}()
	}
	close(start)
	var credited, refused int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			credited++
		case errors.Is(err, ErrConflict):
			refused++
		default:
			t.Fatal(err)
		}
	}
	if credited != 1 || refused != 1 {
		t.Fatalf("claims: credited=%d refused=%d", credited, refused)
	}
	if _, err := second.LoadState(role.RID, DaysRankKey, &state); err != nil || !slices.Equal(state.Claimed, []int64{1002, 1001}) {
		t.Fatalf("recorded receipts: %v (%v)", state, err)
	}
	if assets, err := second.RoleAssets(role.RID); err != nil || assets[1] != 10 {
		t.Fatalf("asset ledger: %v (%v)", assets, err)
	}
	if items, err := second.RoleItems(role.RID); err != nil || items[10102] != 2 {
		t.Fatalf("item ledger: %v (%v)", items, err)
	}
}

func TestDaysRankMailAndFrozenStateCommitTogether(t *testing.T) {
	first, dir := openFile(t)
	role := moduleRole(t, first)
	second, err := Open(filepath.Join(dir, "live", "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	type snapshot struct{ Settled bool }
	interrupt := errors.New("settlement interrupted")
	settle := func(st *Store, fail bool) error {
		return st.DaysRankSettlement(role.SrvID, func(tx *Store) error {
			var state snapshot
			if _, err := tx.LoadGlobal(role.SrvID, DaysRankKey, &state); err != nil || state.Settled {
				return err
			}
			if _, err := tx.CreateMail(Mail{SrvID: role.SrvID, Subject: "rank reward"}, []int64{role.RID}); err != nil {
				return err
			}
			if fail {
				return interrupt
			}
			return tx.SaveGlobal(role.SrvID, DaysRankKey, snapshot{Settled: true})
		})
	}
	if err := settle(first, true); !errors.Is(err, interrupt) {
		t.Fatal(err)
	}
	if mails, err := second.MailsForRole(role); err != nil || len(mails) != 0 {
		t.Fatalf("partial payout remained: %v (%v)", mails, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, st := range []*Store{first, second} {
		go func() {
			<-start
			results <- settle(st, false)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var state snapshot
	if _, err := second.LoadGlobal(role.SrvID, DaysRankKey, &state); err != nil || !state.Settled {
		t.Fatalf("missing frozen state: %v (%v)", state, err)
	}
	if mails, err := second.MailsForRole(role); err != nil || len(mails) != 1 || mails[0].Subject != "rank reward" {
		t.Fatalf("expected one payout: %v (%v)", mails, err)
	}
}

func TestCampaignReplayPreservesRankArrivalTime(t *testing.T) {
	st := open(t)
	role := moduleRole(t, st)
	arrived := time.Unix(100, 0)
	st.now = func() time.Time { return arrived }
	state := Drama{RID: role.RID, DunID: 10300, MaxDunID: 10300}
	if err := st.SaveDrama(state); err != nil {
		t.Fatal(err)
	}
	st.now = func() time.Time { return arrived.Add(time.Minute) }
	state.DunID = 10290
	if err := st.ClearDungeon(state, nil, nil); err != nil {
		t.Fatal(err)
	}
	if drama, err := st.Drama(role.RID); err != nil || drama.UpdatedAt != arrived.Unix() {
		t.Fatalf("replay changed arrival time: %v (%v)", drama, err)
	}
}
