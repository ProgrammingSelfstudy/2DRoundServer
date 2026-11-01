package admin

import (
	"2droundserver/internal/store"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestTableAPIReadPermissionsScopesAndPagination(t *testing.T) {
	e := newEnv(t)
	v, o, a := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("tableapi", "symlf_1", "只读表")
	base := fmt.Sprintf("/api/roles/%d/tables", role.RID)
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "testguild", LeaderRID: role.RID}, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Inspect the isolated shared-memory schema before/after every HTTP read.
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snapshot := func() string {
		t.Helper()
		names := []string{}
		rs, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
		if err != nil {
			t.Fatal(err)
		}
		for rs.Next() {
			var n string
			rs.Scan(&n)
			names = append(names, n)
		}
		rs.Close()
		all := map[string][]map[string]any{}
		for _, n := range names {
			rs, err := db.Query(`SELECT * FROM "` + n + `"`)
			if err != nil {
				t.Fatal(err)
			}
			cols, _ := rs.Columns()
			items := []map[string]any{}
			for rs.Next() {
				values := make([]any, len(cols))
				ptr := make([]any, len(cols))
				for i := range values {
					ptr[i] = &values[i]
				}
				if err := rs.Scan(ptr...); err != nil {
					t.Fatal(err)
				}
				row := map[string]any{}
				for i, v := range values {
					row[cols[i]] = v
				}
				items = append(items, row)
			}
			rs.Close()
			all[n] = items
		}
		b, _ := json.Marshal(all)
		return string(b)
	}
	if err := e.st.SaveState(role.RID, "test_read_only", map[string]int{"n": 3}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveGlobal("symlf_1", "test_read_only", map[string]int{"n": 4}); err != nil {
		t.Fatal(err)
	}
	before := snapshot()
	paths := []string{"/api/tables", base}
	for _, cfg := range store.AdminTables() {
		switch cfg.Scope {
		case "role":
			paths = append(paths, base+"/"+cfg.Name)
		case "guild":
			paths = append(paths, fmt.Sprintf("/api/guilds/%d/tables/%s", gid, cfg.Name))
		case "server":
			paths = append(paths, "/api/servers/symlf_1/tables/"+cfg.Name)
		}
	}
	for _, tok := range []string{v, o, a} {
		for _, path := range paths {
			if got := e.do(tok, "GET", path, nil); got.Status != 200 {
				t.Fatalf("%s: %d %s", path, got.Status, got.Body)
			}
			if got := e.do("", "GET", path, nil); got.Status != 401 {
				t.Fatalf("unauth %s: %d", path, got.Status)
			}
		}
	}
	for _, q := range []string{"limit=0", "limit=201", "limit=-1", "limit=abc", "offset=-1", "offset=abc", "limit=", "offset=", "offset=9999999999999999999999999", "limit=1&limit=2"} {
		if got := e.do(v, "GET", base+"/partners?"+q, nil); got.Status != 422 {
			t.Fatalf("%s: %d", q, got.Status)
		}
	}
	for _, q := range []string{"limit=200&offset=0", "limit=1&offset=99999"} {
		if got := e.do(v, "GET", base+"/partners?"+q, nil); got.Status != 200 {
			t.Fatalf("%s: %d", q, got.Status)
		}
	}
	for _, path := range []string{base + "/guild_logs", base + "/admins", base + "/login_tokens", base + "/missing", base + "/partners%27", "/api/guilds/999999/tables/guild_logs", "/api/roles/999999/tables", "/api/roles/999999/tables/partners", "/api/servers/missing/tables/replays", "/api/servers/symlf_1/tables/partners"} {
		if got := e.do(v, "GET", path, nil); got.Status != 404 {
			t.Fatalf("%s: %d %s", path, got.Status, got.Body)
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("HTTP reads changed database")
	}
	// No mutation routes exist for registered table views.
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if got := e.do(a, method, base+"/partners", map[string]any{}); got.Status != 405 {
			t.Fatalf("write method %s: %d", method, got.Status)
		}
	}
}
