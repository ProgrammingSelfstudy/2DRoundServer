package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"2droundserver/internal/admincoverage"
)

func TestAdminTableRegistrySchemaCoverageAndSafety(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tables.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc, err := os.ReadFile("../../docs/ADMIN_COVERAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	records, err := admincoverage.ParseRegistry(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, cfg := range adminTableRegistry {
		if seen[cfg.Name] || cfg.Title == "" || cfg.Group == "" || cfg.Owner == "" || cfg.order == "" {
			t.Fatalf("invalid config %+v", cfg)
		}
		seen[cfg.Name] = true
		for _, col := range cfg.Columns {
			if strings.Contains(col, "password") || strings.Contains(col, "token") || (cfg.Name == "chat_offline" && col == "msg") || (cfg.Name == "replays" && col == "data") {
				t.Fatalf("sensitive column %s.%s", cfg.Name, col)
			}
		}
		id := any(int64(1))
		if cfg.Scope == "server" {
			id = "s1"
		}
		if _, err := s.AdminTableRows(cfg.Scope, cfg.Name, id, 50, 0); err != nil {
			t.Fatalf("schema %s: %v", cfg.Name, err)
		}
	}
	for name, reason := range adminTableExclusions {
		if seen[name] || reason == "" {
			t.Fatal(name)
		}
		seen[name] = true
	}
	for _, r := range records {
		if r.Scope != "table" {
			continue
		}
		if !seen[r.Key] {
			t.Fatalf("unregistered table %s", r.Key)
		}
		delete(seen, r.Key)
		marker := "| table | `" + r.Key + "` |"
		for _, line := range strings.Split(string(doc), "\n") {
			if strings.HasPrefix(line, marker) {
				if reason, ok := adminTableExclusions[r.Key]; ok {
					if !strings.Contains(line, reason) {
						t.Fatal("exclusion mismatch", r.Key)
					}
				} else if !strings.Contains(line, "表视图注册") {
					t.Fatal("missing documented view", r.Key)
				}
			}
		}
	}
	if len(seen) != 0 {
		t.Fatal("undocumented tables", seen)
	}
	for _, name := range []string{"admins", "accounts", "audit_logs", "login_tokens", "schema_migrations", "module_state", "global_state", "pay_orders"} {
		for _, scope := range []string{"role", "guild", "server"} {
			if _, err := s.AdminTableRows(scope, name, 1, 50, 0); err != ErrNotFound {
				t.Fatal("excluded accessible", name)
			}
		}
	}
}
func TestAdminTableRowsIsolationPagingJoinsAndNoWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tables.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sqls := []string{
		`INSERT INTO accounts(id,platform,account,created_at) VALUES(1,'x','a',0)`,
		`INSERT INTO servers(srv_id,name,platform,zone_id,host,created_at) VALUES('s1','s','x',1,'localhost',0),('s2','s','x',2,'localhost',0)`,
		`INSERT INTO roles(rid,account_id,srv_id,name,created_at) VALUES(1,1,'s1','a',0),(2,1,'s1','b',0)`,
		`INSERT INTO partners(partner_id,rid,bid,star,created_at) VALUES(11,1,10,1,0),(22,2,10,1,0)`,
		`INSERT INTO hero_equips VALUES(11,1,100),(22,1,200)`,
		`INSERT INTO partner_talents VALUES(11,1,101),(22,1,201)`,
		`INSERT INTO mails(id,subject,content,created_at) VALUES(1,'subject','body',0)`,
		`INSERT INTO role_mails(mail_id,rid) VALUES(1,1)`,
		`INSERT INTO chat_offline(from_rid,to_rid,len,msg,sent_at) VALUES(2,1,6,'SECRET',0)`,
		`INSERT INTO replays(srv_id,combat_type,a_rid,b_rid,time,meta,data) VALUES('s1',1,1,2,0,'{}','SCRIPT'),('s2',1,2,3,0,'{}','SCRIPT')`,
		`INSERT INTO module_state VALUES(1,'x','{}')`, `INSERT INTO global_state VALUES('s1','x','{}')`,
	}
	for _, q := range sqls {
		if _, err = s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for i := 1; i <= 7; i++ {
		if _, err = s.db.Exec(`INSERT INTO role_items VALUES(1,?,?)`, i, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec(`INSERT INTO variety_shop(rid,day,free_left,goods) VALUES(1,0,0,?)`, strings.Repeat("字", 2000)); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		all := map[string][]map[string]any{}
		for _, cfg := range adminTableRegistry {
			id := any(1)
			if cfg.Scope == "server" {
				id = "s1"
			}
			p, e := s.AdminTableRows(cfg.Scope, cfg.Name, id, 200, 0)
			if e != nil {
				t.Fatal(e)
			}
			all[cfg.Name] = p.Rows
		}
		b, _ := json.Marshal(all)
		return string(b)
	}
	before := snapshot()
	counts := func() map[string]int64 {
		out := map[string]int64{}
		rows, e := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			names = append(names, n)
		}
		rows.Close()
		for _, n := range names {
			var count int64
			if e := s.db.QueryRow(`SELECT COUNT(*) FROM "` + n + `"`).Scan(&count); e != nil {
				t.Fatal(e)
			}
			out[n] = count
		}
		return out
	}
	initial := counts()
	for _, name := range []string{"hero_equips", "partner_talents", "role_mails", "chat_offline"} {
		p, e := s.AdminTableRows("role", name, 1, 50, 0)
		if e != nil || p.Total != 1 {
			t.Fatal(name, p, e)
		}
		b, _ := json.Marshal(p)
		if strings.Contains(string(b), "SECRET") {
			t.Fatal("chat body exposed")
		}
		if name == "role_mails" && p.Rows[0]["subject"] != "subject" {
			t.Fatal("missing mail subject")
		}
	}
	p, e := s.AdminTableRows("server", "replays", "s1", 50, 0)
	if e != nil || p.Total != 1 {
		t.Fatal(p, e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "SCRIPT") {
		t.Fatal("replay script exposed")
	}
	ids := []int64{}
	for offset := 0; offset < 7; offset += 2 {
		p, e := s.AdminTableRows("role", "role_items", 1, 2, offset)
		if e != nil || p.Total != 7 {
			t.Fatal(p, e)
		}
		for _, r := range p.Rows {
			ids = append(ids, r["item_id"].(int64))
		}
	}
	if !reflect.DeepEqual(ids, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal(ids)
	}
	p, e = s.AdminTableRows("role", "variety_shop", 1, 50, 0)
	if e != nil || len(p.Rows[0]["goods"].(string)) > 4096 || len(p.Rows[0]["truncated"].([]string)) != 1 {
		t.Fatal(p, e)
	}
	if before != snapshot() || !reflect.DeepEqual(initial, counts()) {
		t.Fatal("read changed database")
	}
}
