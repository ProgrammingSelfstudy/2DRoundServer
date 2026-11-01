package game

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"2droundserver/internal/admincoverage"
)

func TestAdminCoverageRegistryComplete(t *testing.T) {
	root := filepath.Join("..", "..")
	inventory, err := admincoverage.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "ADMIN_COVERAGE.md"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := admincoverage.ParseRegistry(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := admincoverage.Check(inventory, registry); err != nil {
		t.Fatal(err)
	}
	live := map[string]AdminModule{}
	for _, m := range AdminModules() {
		id := m.Scope + ":" + m.Key
		if _, duplicate := live[id]; duplicate {
			t.Fatalf("duplicate runtime registration %s", id)
		}
		live[id] = m
		if m.New == nil || reflect.ValueOf(m.New()).Kind() != reflect.Pointer || m.Editable || len(m.Resets) != 0 {
			t.Fatalf("invalid constructor or phase-1 write capability on %s", id)
		}
		if m.Title == "" || m.Window == "" {
			t.Fatalf("missing runtime metadata for %s", id)
		}
	}
	for _, record := range registry {
		if record.Scope == "table" {
			continue
		}
		id := record.Scope + ":" + record.Key
		if _, ok := live[id]; !ok {
			t.Errorf("coverage document has no runtime registration: %s", id)
		}
		delete(live, id)
	}
	for id := range live {
		t.Errorf("runtime registration missing from coverage document: %s", id)
	}
	counts := map[string]int{}
	for _, entry := range inventory.Modules {
		counts[entry.Scope]++
	}
	t.Logf("%d persistence calls; role=%d global=%d tables=%d; all module constants and literal/prefix names registered",
		inventory.StateCalls, counts["role"], counts["global"], len(inventory.Tables))
}
