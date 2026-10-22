package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAPI(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	dir := filepath.Join(t.TempDir(), "backups")

	// not configured: refused for everyone allowed to ask, and nothing is written
	for _, c := range []struct{ method, path string }{{"GET", "/api/backups"}, {"POST", "/api/backups"}} {
		if res := e.do(super, c.method, c.path, nil); res.Status != 409 {
			t.Fatalf("%s %s unconfigured: %d %s", c.method, c.path, res.Status, res.Body)
		}
	}
	e.s.BackupDir, e.s.BackupKeep = dir, 2

	// only a superadmin: the copy holds every account
	for _, tok := range []string{viewer, operator} {
		for _, c := range []struct{ method, path string }{{"GET", "/api/backups"}, {"POST", "/api/backups"}} {
			if res := e.do(tok, c.method, c.path, nil); res.Status != 403 {
				t.Fatalf("%s %s: %d", c.method, c.path, res.Status)
			}
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a refused request made the backup directory")
	}

	if res := e.do(super, "GET", "/api/backups", nil); res.Status != 200 || len(res.JSON()["items"].([]any)) != 0 {
		t.Fatalf("empty list: %d %s", res.Status, res.Body)
	}
	e.seedServer("symlf_1")
	e.seedRole("p1", "symlf_1", "路飞")

	var names []string
	for i := 0; i < 3; i++ {
		res := e.do(super, "POST", "/api/backups", nil)
		if res.Status != 200 || res.JSON()["size"].(float64) <= 0 {
			t.Fatalf("backup %d: %d %s", i, res.Status, res.Body)
		}
		names = append(names, res.JSON()["name"].(string))
		if info, err := os.Stat(filepath.Join(dir, names[i])); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("file %s: %v %v", names[i], info, err)
		}
	}
	// keep = 2: the oldest is gone, and the list says what is left, newest first
	list := e.do(super, "GET", "/api/backups", nil).JSON()
	items := list["items"].([]any)
	if len(items) != 2 || list["keep"].(float64) != 2 {
		t.Fatalf("list: %v", list)
	}
	if items[0].(map[string]any)["name"] != names[2] || items[1].(map[string]any)["name"] != names[1] {
		t.Fatalf("order: %v, made %v", items, names)
	}
	if _, err := os.Stat(filepath.Join(dir, names[0])); !os.IsNotExist(err) {
		t.Fatalf("the oldest backup is still there: %v", err)
	}
	n := 0
	for _, a := range e.auditActions() {
		if a == "backup.create" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("three backups must be audited, got %d: %v", n, e.auditActions())
	}
}
