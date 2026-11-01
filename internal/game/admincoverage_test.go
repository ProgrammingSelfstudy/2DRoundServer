package game

import (
	"os"
	"path/filepath"
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
	counts := map[string]int{}
	for _, entry := range inventory.Modules {
		counts[entry.Scope]++
	}
	t.Logf("%d persistence calls; role=%d global=%d tables=%d; all module constants and literal/prefix names registered",
		inventory.StateCalls, counts["role"], counts["global"], len(inventory.Tables))
}
