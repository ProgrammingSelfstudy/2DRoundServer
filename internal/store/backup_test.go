package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openFile opens a real database file (a backup is made from the file, not :memory:).
func openFile(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "live", "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestBackupHoldsTheDataAndIsPrivate(t *testing.T) {
	s, dir := openFile(t)
	a, err := s.FindOrCreateAccount("symlf", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureServer(Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRole(a.ID, "symlf_1", "路飞", 1, 2, 0); err != nil {
		t.Fatal(err)
	}

	path, err := s.Backup(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("a backup holds every account: mode %v, want 0600", info.Mode().Perm())
	}
	if d, _ := os.Stat(filepath.Dir(path)); d.Mode().Perm() != 0o700 {
		t.Fatalf("backup directory mode %v, want 0700", d.Mode().Perm())
	}

	// the copy opens as a database and has the rows (and the migrations)
	b, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var name string
	if err := b.QueryRow(`SELECT name FROM roles`).Scan(&name); err != nil || name != "路飞" {
		t.Fatalf("role in the backup: %q %v", name, err)
	}
	var migrations int
	b.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrations)
	if migrations == 0 {
		t.Fatal("the backup has no migrations table content")
	}
	// the backup is independent: later changes do not reach it
	if _, err := s.CreateRole(a.ID, "symlf_1", "索隆", 1, 2, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	b.QueryRow(`SELECT COUNT(*) FROM roles`).Scan(&n)
	if n != 1 {
		t.Fatalf("roles in the old backup: %d", n)
	}
}

func TestBackupNamesDoNotCollide(t *testing.T) {
	s, dir := openFile(t)
	fixed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		p, err := s.Backup(filepath.Join(dir, "b"))
		if err != nil {
			t.Fatal(err)
		}
		if seen[p] {
			t.Fatalf("%s written twice", p)
		}
		seen[p] = true
	}
	all, _ := ListBackups(filepath.Join(dir, "b"))
	if len(all) != 3 || !strings.HasPrefix(all[0].Name, "2droundserver-20261019-") {
		t.Fatalf("backups: %v", all)
	}
	// the newest first, also inside one second: "<stamp>-3.db" was made after "<stamp>.db"
	want := []string{"2droundserver-20261019-120000-3.db", "2droundserver-20261019-120000-2.db", "2droundserver-20261019-120000.db"}
	for i, w := range want {
		if all[i].Name != w {
			t.Fatalf("order: %v, want %v", all, want)
		}
	}
	// and pruning keeps the newest of them, not the one that sorts last as text
	if n, err := PruneBackups(filepath.Join(dir, "b"), 1); err != nil || n != 2 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if left, _ := ListBackups(filepath.Join(dir, "b")); len(left) != 1 || left[0].Name != want[0] {
		t.Fatalf("left: %v, want only %s", left, want[0])
	}
}

func TestPruneKeepsTheNewestAndOnlyBackups(t *testing.T) {
	_, dir := openFile(t)
	b := filepath.Join(dir, "b")
	os.MkdirAll(b, 0o700)
	for _, n := range []string{"2droundserver-20261015-010101.db", "2droundserver-20261016-010101.db", "2droundserver-20261017-010101.db", "2droundserver-20261018-010101.db"} {
		os.WriteFile(filepath.Join(b, n), []byte("x"), 0o600)
	}
	// things that are not backups must survive, whatever their names look like
	for _, n := range []string{"notes.txt", "2droundserver-20261001-000000.db.bak", "other-20250101.db"} {
		os.WriteFile(filepath.Join(b, n), []byte("keep me"), 0o600)
	}
	os.Mkdir(filepath.Join(b, "2droundserver-20260101-000000.db"), 0o700) // a directory with a backup's name

	n, err := PruneBackups(b, 2)
	if err != nil || n != 2 {
		t.Fatalf("pruned %d (%v), want 2", n, err)
	}
	all, _ := ListBackups(b)
	if len(all) != 2 || all[0].Name != "2droundserver-20261018-010101.db" || all[1].Name != "2droundserver-20261017-010101.db" {
		t.Fatalf("left: %v", all)
	}
	for _, n := range []string{"notes.txt", "2droundserver-20261001-000000.db.bak", "other-20250101.db", "2droundserver-20260101-000000.db"} {
		if _, err := os.Stat(filepath.Join(b, n)); err != nil {
			t.Fatalf("%s was deleted: %v", n, err)
		}
	}
	if n, _ := PruneBackups(b, 0); n != 0 { // keep < 1 deletes nothing
		t.Fatalf("keep 0 pruned %d", n)
	}
	if n, _ := PruneBackups(filepath.Join(dir, "missing"), 3); n != 0 {
		t.Fatalf("a missing directory pruned %d", n)
	}
}

func TestBackupNowAndDue(t *testing.T) {
	s, dir := openFile(t)
	b := filepath.Join(dir, "b")
	now := time.Now()
	if !BackupDue(b, 24*time.Hour, now) {
		t.Fatal("no backup yet: one is due")
	}
	for i := 0; i < 4; i++ {
		s.now = func() time.Time { return now.Add(time.Duration(i) * time.Second) }
		if _, _, err := s.BackupNow(b, 3); err != nil {
			t.Fatal(err)
		}
	}
	if all, _ := ListBackups(b); len(all) != 3 {
		t.Fatalf("kept %d, want 3", len(all))
	}
	if BackupDue(b, time.Hour, time.Now()) {
		t.Fatal("a backup was just made: none is due")
	}
	if !BackupDue(b, time.Hour, time.Now().Add(2*time.Hour)) {
		t.Fatal("two hours later one is due")
	}
}
