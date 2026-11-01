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
	charge := map[string]bool{"firstcharge_*": true, "daycharge": true, "monthcard": true, "monthfund": true, "limitgift": true, "cyclegift": true}
	ranks := map[string]bool{"arenateam": true, "arenamany": true, "crossarena": true, "elitematch": true, "guildwar": true, "daysrank": true, "arenachamp": true, "crosschamp": true, "ladder": true, "endless_rank": true, "practisetower_rank": true, "sandybeach": true, "sweet": true, "yearmonster": true, "termbegins": true, "homeworld_board": true}
	for _, m := range AdminModules() {
		id := m.Scope + ":" + m.Key
		if _, duplicate := live[id]; duplicate {
			t.Fatalf("duplicate runtime registration %s", id)
		}
		live[id] = m
		allowed := m.Scope == "role" && charge[m.Key]
		if m.New == nil || reflect.ValueOf(m.New()).Kind() != reflect.Pointer || m.Editable || (m.Rank != nil) != (m.Scope == "global" && ranks[m.Key]) || (len(m.Resets) > 0) != allowed || (m.View != nil) != (allowed || m.Scope == "global") {
			t.Fatalf("invalid constructor or unauthorized phase-3 capability on %s", id)
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
