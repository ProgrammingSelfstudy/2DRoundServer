package admincoverage

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Record is one manually reviewed row in docs/ADMIN_COVERAGE.md. The document is
// the phase-0 registry; this is deliberately not the phase-1 runtime registry.
type Record struct {
	Entry
	Title, Current, Planned, Reason string
}

var sourceLink = regexp.MustCompile(`\]\(\.\./(internal/[^)#]+)\)`)

func ParseRegistry(markdown string) ([]Record, error) {
	var records []Record
	seen := map[string]bool{}
	for line, text := range strings.Split(markdown, "\n") {
		cells := strings.Split(strings.TrimSpace(text), "|")
		if len(cells) < 3 {
			continue
		}
		scope := strings.TrimSpace(cells[1])
		if scope != "role" && scope != "global" && scope != "table" {
			continue
		}
		if len(cells) != 9 {
			return nil, fmt.Errorf("coverage row %d: need seven columns", line+1)
		}
		for i := 1; i <= 7; i++ {
			cells[i] = strings.TrimSpace(cells[i])
			if cells[i] == "" || cells[i] == "—" || cells[i] == "-" {
				return nil, fmt.Errorf("coverage row %d: column %d is empty", line+1, i)
			}
		}
		key := strings.Trim(cells[2], "`")
		id := scope + ":" + key
		if seen[id] {
			return nil, fmt.Errorf("coverage row %d: duplicate %s", line+1, id)
		}
		seen[id] = true
		var sources []string
		for _, link := range sourceLink.FindAllStringSubmatch(cells[7], -1) {
			sources = append(sources, link[1])
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("coverage row %d: missing repository source link", line+1)
		}
		slices.Sort(sources)
		records = append(records, Record{Entry{scope, key, sources}, cells[3], cells[4], cells[5], cells[6]})
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("coverage registry is empty")
	}
	return records, nil
}

// Check demands an exact (scope, key) inventory, including the whole dynamic
// prefix, not arbitrary wildcard matches. Stale entries and source links fail too.
func Check(inventory Inventory, records []Record) error {
	documented := map[string]Record{}
	declared := map[string]bool{}
	for _, record := range records {
		id := record.Scope + ":" + record.Key
		if _, exists := documented[id]; exists {
			return fmt.Errorf("duplicate registry entry %s", id)
		}
		documented[id] = record
		if record.Scope != "table" {
			declared[record.Key] = true
		}
	}
	var problems []string
	for _, entry := range append(slices.Clone(inventory.Modules), inventory.Tables...) {
		id := entry.Scope + ":" + entry.Key
		record, exists := documented[id]
		if !exists {
			problems = append(problems, "missing "+id+" ("+strings.Join(entry.Sources, ", ")+")")
		} else if !slices.Equal(entry.Sources, record.Sources) {
			problems = append(problems, "source links differ for "+id+"; expected "+strings.Join(entry.Sources, ", "))
		}
		delete(documented, id)
	}
	for id := range documented {
		problems = append(problems, "stale or wrong-scope entry "+id)
	}
	for _, entry := range inventory.Declarations {
		if !declared[entry.Key] {
			problems = append(problems, "unregistered module constant "+entry.Key+" ("+strings.Join(entry.Sources, ", ")+")")
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("admin coverage registry incomplete:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}
