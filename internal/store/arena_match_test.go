package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestArenaH5NearUsesRealmPlatformAndEligibleRoster(t *testing.T) {
	s := open(t)
	for _, id := range []string{"symlf_1", "symlf_2"} {
		if err := s.EnsureServer(srv(id)); err != nil {
			t.Fatal(err)
		}
	}
	players := []struct {
		name, realm, platform string
		score                 int64
		star                  int
		banned                bool
	}{
		{"self", "symlf_1", PlatformH5, 1000, 5, false},
		{"far", "symlf_1", PlatformH5, 1050, 5, false},
		{"close", "symlf_1", PlatformH5, 980, 6, false},
		{"low-star", "symlf_1", PlatformH5, 1000, 4, false},
		{"banned", "symlf_1", PlatformH5, 1000, 5, true},
		{"original", "symlf_1", "symlf", 1000, 5, false},
		{"other-realm", "symlf_2", PlatformH5, 1000, 5, false},
	}
	rids := map[string]int64{}
	for _, p := range players {
		account, err := s.FindOrCreateAccount(p.platform, p.name)
		if err != nil {
			t.Fatal(err)
		}
		role, err := s.CreateRole(account.ID, p.realm, p.name, 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		rids[p.name] = role.RID
		if _, err := s.ArenaPlayerOf(role.RID, p.score, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GrantStarterPartners(role.RID, []PartnerSeed{{BID: 70501, Star: p.star}}); err != nil {
			t.Fatal(err)
		}
		if p.banned {
			if err := s.SetAccountStatus(account.ID, 0, "test"); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := s.ArenaH5Near("symlf_1", rids["self"], 1000, 5, 3)
	want := []ArenaRow{{RID: rids["close"], Score: 980}, {RID: rids["far"], Score: 1050}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("nearby eligible opponents: %v, want %v, error %v", got, want, err)
	}
}

func TestArenaMatchRefreshAndCommitUseTheCurrentSlot(t *testing.T) {
	s := open(t)
	if err := s.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	account, err := s.FindOrCreateAccount(PlatformH5, "arena")
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.CreateRole(account.ID, "symlf_1", "arena", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	rid := role.RID
	if _, err := s.ArenaPlayerOf(rid, 1000, 1); err != nil {
		t.Fatal(err)
	}
	old := ArenaSlot{Idx: 1, OppRID: 4000000000, Info: `{"Name":"old"}`}
	current := old
	current.Info = `{"Name":"old","RefreshTime":103}`
	if err := s.InitArenaSlots(rid, []ArenaSlot{old}); err != nil {
		t.Fatal(err)
	}
	if err := s.InitArenaSlots(rid, []ArenaSlot{current}); !errors.Is(err, ErrConflict) {
		t.Fatalf("initialization replaced an existing list: %v", err)
	}
	if err := s.RefreshArenaSlots(rid, 100, 103, []ArenaSlot{current, current}); !isUnique(err) {
		t.Fatalf("expected the duplicate slot to fail: %v", err)
	}
	if got, err := s.ArenaSlots(rid); err != nil || !reflect.DeepEqual(got, []ArenaSlot{old}) {
		t.Fatalf("a failed refresh replaced the list: %v %v", got, err)
	}
	if got, err := s.ArenaPlayerOf(rid, 1000, 1); err != nil || got.RefTime != 0 {
		t.Fatalf("a failed refresh advanced the cooldown: %v %v", got, err)
	}
	if err := s.RefreshArenaSlots(rid, 100, 103, []ArenaSlot{current}); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshArenaSlots(rid, 102, 105, []ArenaSlot{old}); !errors.Is(err, ErrConflict) {
		t.Fatalf("refresh during cooldown: %v", err)
	}
	if err := s.CommitArenaMatch(rid, old, func(*ArenaMatchTx) error {
		t.Error("a stale slot reached settlement")
		return nil
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale match: %v", err)
	}
	settle := func(tx *ArenaMatchTx) error {
		a, err := tx.ArenaPlayerOf(rid, 1000, 1)
		if err != nil {
			return err
		}
		a.Battles++
		if err := tx.SaveArenaPlayer(a); err != nil {
			return err
		}
		return tx.CreditReward(rid, map[int64]int64{1: 10}, nil)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- s.CommitArenaMatch(rid, current, settle)
		}()
	}
	close(start)
	var commits, conflicts int
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			commits++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Errorf("commit: %v", err)
		}
	}
	if commits != 1 || conflicts != 1 {
		t.Fatalf("concurrent settlement: %d commits, %d conflicts", commits, conflicts)
	}
	if a, err := s.ArenaPlayerOf(rid, 1000, 1); err != nil || a.Battles != 1 || a.RefTime != 103 {
		t.Fatalf("standing: %+v %v", a, err)
	}
	if assets, err := s.RoleAssets(rid); err != nil || assets[1] != 10 {
		t.Fatalf("reward: %v %v", assets, err)
	}
	current.Status = 1
	if got, err := s.ArenaSlots(rid); err != nil || !reflect.DeepEqual(got, []ArenaSlot{current}) {
		t.Fatalf("settled slot: %v %v", got, err)
	}
}
