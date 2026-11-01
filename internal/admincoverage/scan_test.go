package admincoverage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"internal/game", "internal/store/migrations"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "internal/game/modules.go"), []byte("package game\n"+source), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScanConstantsLiteralsPrefixesAndScopedFields(t *testing.T) {
	root := fixture(t, `
const baseModule = "base"
const aliasModule string = baseModule
type match struct { module string }
type other struct { module string }
func newMatch() *match { return &match{} }
func setup() { c := newMatch(); c.module = aliasModule; d := &match{module: "cross"}; _ = d; o := &other{module:"unrelated"}; _ = o }
func (c *match) load() { st.LoadState(1,c.module,nil); st.SaveGlobal("s",c.module,nil) }
func guildKey(gid int) string { return "guild:" + strconv.Itoa(gid) }
func load(suffix string) { st.LoadState(1,"literal",nil); st.SaveState(1,"first_"+suffix,nil); st.LoadGlobal("s",guildKey(1),nil) }
// st.LoadState(1,"comment_only",nil)
`)
	if err := os.WriteFile(filepath.Join(root, "internal/game/ignored_test.go"), []byte(`package game;func test(){st.LoadState(1,"test_only",nil)}`), 0600); err != nil {
		t.Fatal(err)
	}
	i, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range i.Modules {
		keys = append(keys, e.Scope+":"+e.Key)
	}
	want := []string{"global:base", "global:cross", "global:guild:*", "role:base", "role:cross", "role:first_*", "role:literal"}
	if !slices.Equal(keys, want) || i.StateCalls != 5 {
		t.Fatalf("got %v (%d calls), want %v (5 calls)", keys, i.StateCalls, want)
	}
}

func TestScanFailsOnOpaqueModuleExpressions(t *testing.T) {
	for _, expression := range []string{"key", "c.module", "buildKey()", `"prefix"+key+"suffix"`} {
		t.Run(expression, func(t *testing.T) {
			_, err := Scan(fixture(t, "func load(){st.LoadState(1,"+expression+",nil)}"))
			if err == nil || !strings.Contains(err.Error(), "LoadState module") {
				t.Fatalf("opaque module was not rejected: %v", err)
			}
		})
	}
}

func TestScanLocalConstantsAndShadowing(t *testing.T) {
	i, err := Scan(fixture(t, `func load(){ const localModule = "local"; const unusedModule = "new_unused"; st.LoadState(1,localModule,nil) }`))
	if err != nil || len(i.Modules) != 1 || i.Modules[0].Key != "local" {
		t.Fatalf("local module constant not resolved: %+v, %v", i, err)
	}
	if err := Check(i, nil); err == nil || !strings.Contains(err.Error(), "unregistered module constant new_unused") {
		t.Fatalf("unused local module must be registered too: %v", err)
	}
	_, err = Scan(fixture(t, `const sharedModule="old"; func load(){sharedModule:=opaque(); st.LoadState(1,sharedModule,nil)}`))
	if err == nil {
		t.Fatal("opaque local variable silently resolved to the shadowed package constant")
	}
}

func TestScanIncludesStoreWrappersWithSeparateNamespaces(t *testing.T) {
	root := fixture(t, `const sharedModule="game";func load(){st.LoadState(1,sharedModule,nil);st.LoadState(1,"common",nil)}`)
	store := `package store;const sharedModule="store";func load(){st.LoadState(1,sharedModule,nil);st.SaveState(1,sharedModule,nil);st.LoadState(1,"common",nil)}`
	if err := os.WriteFile(filepath.Join(root, "internal/store/wrapper.go"), []byte(store), 0600); err != nil {
		t.Fatal(err)
	}
	i, err := Scan(root)
	if err != nil || len(i.Modules) != 3 || i.StateCalls != 5 || i.Modules[0].Key != "common" || len(i.Modules[0].Sources) != 2 || i.Modules[1].Key != "game" || i.Modules[2].Key != "store" {
		t.Fatalf("store wrapper missed or package namespaces mixed: %+v, %v", i, err)
	}
}

func TestScanTablesIncludesSQLAndBootstrapWithoutComments(t *testing.T) {
	root := fixture(t, `const unusedModule = "not_registered"`)
	sql := "-- CREATE TABLE comment_only(x);\nCREATE TABLE `fresh_table`(x);\n/* CREATE TABLE fake(x); */"
	if err := os.WriteFile(filepath.Join(root, "internal/store/migrations/001.sql"), []byte(sql), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/store/store.go"), []byte("package store;var sql = `CREATE TABLE IF NOT EXISTS schema_migrations(x)`"), 0600); err != nil {
		t.Fatal(err)
	}
	i, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(i.Tables) != 2 || i.Tables[0].Key != "fresh_table" || i.Tables[1].Key != "schema_migrations" {
		t.Fatalf("wrong table inventory: %+v", i.Tables)
	}
	if err := Check(i, nil); err == nil || !strings.Contains(err.Error(), "unregistered module constant not_registered") || !strings.Contains(err.Error(), "missing table:fresh_table") {
		t.Fatalf("new table/unused module constant must fail the gate: %v", err)
	}
}

func TestRegistryRequiresExactScopePrefixSourcesAndMetadata(t *testing.T) {
	row := "| role | `first_*` | 首充/21000 | 未接 | 查看 | 缺少API | [modules.go](../internal/game/modules.go) |"
	records, err := ParseRegistry(row)
	if err != nil {
		t.Fatal(err)
	}
	i := Inventory{Modules: []Entry{{"role", "first_*", []string{"internal/game/modules.go"}}}}
	if err := Check(i, records); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{row + "\n" + row, strings.Replace(row, "缺少API", "", 1), strings.Replace(row, "../internal/game/modules.go", "elsewhere", 1)} {
		if _, err := ParseRegistry(bad); err == nil {
			t.Fatalf("malformed registry accepted: %s", bad)
		}
	}
	for _, bad := range []string{strings.Replace(row, "role", "global", 1), strings.Replace(row, "first_*", "*", 1), strings.ReplaceAll(row, "modules.go", "other.go")} {
		r, err := ParseRegistry(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := Check(i, r); err == nil {
			t.Fatalf("incorrect inventory accepted: %s", bad)
		}
	}
	added := i
	added.Modules = append(slices.Clone(i.Modules), Entry{"role", "new_literal", []string{"internal/game/new.go"}})
	if err := Check(added, records); err == nil || !strings.Contains(err.Error(), "missing role:new_literal") {
		t.Fatalf("new literal must fail: %v", err)
	}
}
