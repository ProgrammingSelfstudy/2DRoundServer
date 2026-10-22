package admin

import (
	"fmt"
	"strings"
	"testing"
)

func srvBody(id string) map[string]any {
	return map[string]any{"srv_id": id, "name": "测试服", "platform": "symlf", "zone_id": 2, "host": "10.0.0.2", "port": 8800, "status": 1, "group_id": 1}
}

// TestRoleMatrix pins who may call what: viewer reads, operator also writes
// content and bans, only superadmin manages servers, admins and the audit log.
func TestRoleMatrix(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "角色")

	type call struct {
		method, path string
		body         any
		viewer, op   int
		super        int
	}
	mail := map[string]any{"scope": "broadcast", "subject": "s"}
	notice := map[string]any{"title": "t", "content": "c", "status": 1}
	calls := []call{
		{"GET", "/api/servers", nil, 200, 200, 200},
		{"GET", "/api/accounts", nil, 200, 200, 200},
		{"GET", "/api/roles", nil, 200, 200, 200},
		{"GET", "/api/notices", nil, 200, 200, 200},
		{"GET", "/api/mails", nil, 200, 200, 200},
		{"GET", "/api/stats", nil, 200, 200, 200},
		{"GET", "/api/meta", nil, 200, 200, 200},
		{"GET", "/api/online", nil, 200, 200, 200},
		{"GET", "/api/admins", nil, 403, 403, 200},
		{"GET", "/api/audit", nil, 403, 403, 200},
		{"POST", "/api/admins", map[string]string{"username": "newadmin", "password": "password1", "role": "viewer"}, 403, 403, 201},
		{"POST", "/api/servers", srvBody("symlf_9"), 403, 403, 201},
		{"DELETE", "/api/servers/symlf_9", nil, 403, 403, 200},
		{"POST", "/api/mails", mail, 403, 201, 201},
		{"POST", "/api/notices", notice, 403, 201, 201},
		{"POST", fmt.Sprintf("/api/roles/%d/ban", r.RID), map[string]any{}, 403, 200, 200},
		{"POST", fmt.Sprintf("/api/roles/%d/unban", r.RID), nil, 403, 200, 200},
		{"POST", fmt.Sprintf("/api/accounts/%d/ban", r.AccountID), map[string]any{}, 403, 200, 200},
		{"POST", fmt.Sprintf("/api/accounts/%d/unban", r.AccountID), nil, 403, 200, 200},
	}
	for _, c := range calls {
		for who, want := range map[string]struct {
			tok  string
			code int
		}{"viewer": {viewer, c.viewer}, "operator": {operator, c.op}, "superadmin": {super, c.super}} {
			if c.method == "DELETE" && strings.HasPrefix(c.path, "/api/servers/") {
				e.seedServer("symlf_9") // every caller finds the server it tries to delete
			}
			got := e.do(want.tok, c.method, c.path, c.body)
			if got.Status != want.code {
				t.Errorf("%s %s as %s: got %d, want %d (%s)", c.method, c.path, who, got.Status, want.code, got.Body)
			}
			// the calls above create things; make a repeated create valid again
			if c.method == "POST" && c.path == "/api/servers" && got.Status == 201 {
				e.do(super, "DELETE", "/api/servers/symlf_9", nil)
			}
		}
		if c.method == "POST" && c.path == "/api/admins" {
			// created once by the superadmin; a second identical create must conflict, not 500
			if got := e.do(super, c.method, c.path, c.body); got.Status != 409 {
				t.Errorf("duplicate admin: %d", got.Status)
			}
		}
	}
	for _, c := range calls { // nothing is reachable without a token
		if got := e.do("", c.method, c.path, c.body); got.Status != 401 {
			t.Errorf("%s %s without a token: %d", c.method, c.path, got.Status)
		}
	}
}

func TestAdminManagement(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	supers, _ := e.st.ListAdmins()
	var selfID, viewerID int64
	for _, a := range supers {
		switch a.Username {
		case "super1":
			selfID = a.ID
		case "viewer1":
			viewerID = a.ID
		}
	}
	for name, body := range map[string]map[string]string{
		"short name":     {"username": "ab", "password": "password1", "role": "viewer"},
		"odd characters": {"username": "bad name!", "password": "password1", "role": "viewer"},
		"short password": {"username": "newone", "password": "short", "role": "viewer"},
		"unknown role":   {"username": "newone", "password": "password1", "role": "root"},
	} {
		if r := e.do(super, "POST", "/api/admins", body); r.Status != 422 {
			t.Errorf("%s: %d", name, r.Status)
		}
	}
	// you cannot lock yourself out, and the last superadmin stays
	if r := e.do(super, "PATCH", fmt.Sprintf("/api/admins/%d", selfID), map[string]any{"role": "viewer"}); r.Status != 409 {
		t.Fatalf("self-demotion: %d", r.Status)
	}
	if r := e.do(super, "PATCH", fmt.Sprintf("/api/admins/%d", selfID), map[string]any{"status": 0}); r.Status != 409 {
		t.Fatalf("self-disable: %d", r.Status)
	}
	// promote the viewer, then another superadmin may demote the first one
	if r := e.do(super, "PATCH", fmt.Sprintf("/api/admins/%d", viewerID), map[string]any{"role": "superadmin"}); r.Status != 200 {
		t.Fatalf("promote: %d %s", r.Status, r.Body)
	}
	second := e.login("viewer1", "viewerpass1")
	if r := e.do(second, "PATCH", fmt.Sprintf("/api/admins/%d", selfID), map[string]any{"status": 0}); r.Status != 200 {
		t.Fatalf("a second superadmin may disable the first: %d %s", r.Status, r.Body)
	}
	if r := e.do(super, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("the disabled admin's token still works: %d", r.Status)
	}
	if r := e.do(second, "PATCH", "/api/admins/99999", map[string]any{"status": 0}); r.Status != 404 {
		t.Fatalf("unknown admin: %d", r.Status)
	}
	if r := e.do(second, "PATCH", fmt.Sprintf("/api/admins/%d", viewerID), map[string]any{"role": "viewer"}); r.Status != 409 {
		t.Fatalf("self-demotion of the last superadmin: %d", r.Status)
	}
	// a password reset by another admin invalidates the target's tokens
	opTok := e.login("operator1", "operatorpass1")
	var opID int64
	list, _ := e.st.ListAdmins()
	for _, a := range list {
		if a.Username == "operator1" {
			opID = a.ID
		}
	}
	if r := e.do(second, "PATCH", fmt.Sprintf("/api/admins/%d", opID), map[string]any{"password": "reset-by-admin-1"}); r.Status != 200 {
		t.Fatalf("reset: %d %s", r.Status, r.Body)
	}
	if r := e.do(opTok, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("token survived a password reset: %d", r.Status)
	}
	e.login("operator1", "reset-by-admin-1")
	// the audit trail never holds a password
	rows, _, _ := e.st.ListAudit(200, 0)
	for _, row := range rows {
		if strings.Contains(row.DetailJSON, "reset-by-admin") || strings.Contains(row.DetailJSON, "password1") {
			t.Fatalf("a password reached the audit log: %s", row.DetailJSON)
		}
	}
}

func TestServers(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	bad := map[string]func(m map[string]any){
		"bad srv_id":      func(m map[string]any) { m["srv_id"] = "no spaces" },
		"empty name":      func(m map[string]any) { m["name"] = "  " },
		"port 0":          func(m map[string]any) { m["port"] = 0 },
		"port too big":    func(m map[string]any) { m["port"] = 70000 },
		"bad status":      func(m map[string]any) { m["status"] = 2 },
		"no host":         func(m map[string]any) { m["host"] = "" },
		"group 0":         func(m map[string]any) { m["group_id"] = 0 },
		"unknown main":    func(m map[string]any) { m["main_srv_id"] = "ghost_1" },
		"negative opentm": func(m map[string]any) { m["open_time"] = -1 },
	}
	for name, mut := range bad {
		b := srvBody("symlf_2")
		mut(b)
		if r := e.do(super, "POST", "/api/servers", b); r.Status != 422 {
			t.Errorf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	if r := e.do(super, "POST", "/api/servers", srvBody("symlf_2")); r.Status != 201 {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	if r := e.do(super, "POST", "/api/servers", srvBody("symlf_2")); r.Status != 409 {
		t.Fatalf("duplicate: %d", r.Status)
	}
	upd := srvBody("symlf_2")
	upd["name"], upd["status"] = "改名了", 0
	if r := e.do(super, "PUT", "/api/servers/symlf_2", upd); r.Status != 200 || r.JSON()["name"] != "改名了" || r.JSON()["status"].(float64) != 0 {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	if r := e.do(super, "PUT", "/api/servers/symlf_3", srvBody("symlf_3")); r.Status != 404 {
		t.Fatalf("update of a missing server: %d", r.Status)
	}
	mismatch := srvBody("symlf_9")
	if r := e.do(super, "PUT", "/api/servers/symlf_2", mismatch); r.Status != 422 {
		t.Fatalf("id mismatch: %d", r.Status)
	}
	// the list reflects the maintenance flag the client reads
	list := e.do(super, "GET", "/api/servers", nil).JSON()["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["status"].(float64) != 0 {
		t.Fatalf("list: %v", list)
	}
	e.seedRole("p1", "symlf_2", "有角色")
	if r := e.do(super, "DELETE", "/api/servers/symlf_2", nil); r.Status != 409 {
		t.Fatalf("deleting a server that has roles: %d", r.Status)
	}
	e.seedServer("symlf_5")
	if r := e.do(super, "DELETE", "/api/servers/symlf_5", nil); r.Status != 200 {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := e.do(super, "DELETE", "/api/servers/symlf_5", nil); r.Status != 404 {
		t.Fatalf("second delete: %d", r.Status)
	}
	acts := e.auditActions()
	for _, want := range []string{"server.create", "server.update", "server.delete"} {
		if !has(acts, want) {
			t.Errorf("audit lacks %s: %v", want, acts)
		}
	}
}

func TestAccountsAndRolesBanAndKick(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r1 := e.seedRole("alice", "symlf_1", "爱丽丝")
	e.seedRole("alice", "symlf_1", "爱丽丝二号")
	r2 := e.seedRole("bob", "symlf_1", "鲍勃")

	list := e.do(operator, "GET", "/api/accounts?q=ali", nil).JSON()
	if list["total"].(float64) != 1 || list["items"].([]any)[0].(map[string]any)["role_count"].(float64) != 2 {
		t.Fatalf("account search: %v", list)
	}
	if strings.Contains(fmt.Sprint(list), "password") {
		t.Fatalf("password hash exposed: %v", list)
	}
	rl := e.do(operator, "GET", "/api/roles?q=鲍勃", nil).JSON()
	if rl["total"].(float64) != 1 || rl["items"].([]any)[0].(map[string]any)["account"] != "bob" {
		t.Fatalf("role search: %v", rl)
	}
	if got := e.do(operator, "GET", "/api/roles?srv_id=symlf_2", nil).JSON(); got["total"].(float64) != 0 {
		t.Fatalf("server filter: %v", got)
	}

	// banning an account kicks every role it owns and revokes its login tokens
	tok, _ := e.st.IssueToken(r1.AccountID, 3600)
	if r := e.do(operator, "POST", fmt.Sprintf("/api/accounts/%d/ban", r1.AccountID), map[string]any{"reason": "外挂"}); r.Status != 200 {
		t.Fatalf("ban: %d %s", r.Status, r.Body)
	}
	if len(e.gw.kicks) != 2 {
		t.Fatalf("both roles of the account must be kicked: %v", e.gw.kicks)
	}
	if _, err := e.st.AccountByToken(tok); err == nil {
		t.Fatal("a banned account keeps a working game token")
	}
	if acc, _ := e.st.AccountByID(r1.AccountID); acc.Status != 0 || acc.BanReason != "外挂" {
		t.Fatalf("account: %+v", acc)
	}
	if r := e.do(operator, "POST", fmt.Sprintf("/api/accounts/%d/unban", r1.AccountID), nil); r.Status != 200 {
		t.Fatalf("unban: %d", r.Status)
	}
	if acc, _ := e.st.AccountByID(r1.AccountID); acc.Status != 1 || acc.BanReason != "" {
		t.Fatalf("account after unban: %+v", acc)
	}
	if r := e.do(operator, "POST", "/api/accounts/9999/ban", map[string]any{}); r.Status != 404 {
		t.Fatalf("unknown account: %d", r.Status)
	}
	if r := e.do(operator, "POST", fmt.Sprintf("/api/accounts/%d/ban", r2.AccountID), map[string]any{"reason": strings.Repeat("长", 201)}); r.Status != 422 {
		t.Fatalf("long reason: %d", r.Status)
	}

	e.gw.kicks = nil
	if r := e.do(operator, "POST", fmt.Sprintf("/api/roles/%d/ban", r2.RID), nil); r.Status != 200 {
		t.Fatalf("role ban: %d %s", r.Status, r.Body)
	}
	if len(e.gw.kicks) != 1 || e.gw.kicks[0] != fmt.Sprint(r2.RID) {
		t.Fatalf("kick: %v", e.gw.kicks)
	}
	if role, _ := e.st.RoleByID(r2.RID); role.Status != 0 {
		t.Fatal("role not banned")
	}
	e.do(operator, "POST", fmt.Sprintf("/api/roles/%d/unban", r2.RID), nil)
	if role, _ := e.st.RoleByID(r2.RID); role.Status != 1 {
		t.Fatal("role not unbanned")
	}
	if r := e.do(operator, "POST", "/api/roles/9999/ban", nil); r.Status != 404 {
		t.Fatalf("unknown role: %d", r.Status)
	}
	// with the gateway down the ban still holds
	e.gw.down = true
	if r := e.do(operator, "POST", fmt.Sprintf("/api/roles/%d/ban", r2.RID), nil); r.Status != 200 {
		t.Fatalf("ban with the gateway down: %d", r.Status)
	}
	if role, _ := e.st.RoleByID(r2.RID); role.Status != 0 {
		t.Fatal("ban lost when the gateway was down")
	}
	acts := e.auditActions()
	for _, want := range []string{"account.ban", "account.unban", "role.ban", "role.unban"} {
		if !has(acts, want) {
			t.Errorf("audit lacks %s", want)
		}
	}
}

func TestNotices(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	good := map[string]any{"title": "维护公告", "content": "今晚维护", "status": 1, "start_at": 100, "end_at": 200, "srv_id": "symlf_1"}
	bad := map[string]func(m map[string]any){
		"no title":       func(m map[string]any) { m["title"] = "" },
		"long title":     func(m map[string]any) { m["title"] = strings.Repeat("长", 101) },
		"no content":     func(m map[string]any) { m["content"] = "" },
		"bad status":     func(m map[string]any) { m["status"] = 5 },
		"end before":     func(m map[string]any) { m["end_at"] = 50 },
		"unknown server": func(m map[string]any) { m["srv_id"] = "ghost_1" },
		"negative":       func(m map[string]any) { m["start_at"] = -1 },
	}
	for name, mut := range bad {
		b := map[string]any{}
		for k, v := range good {
			b[k] = v
		}
		mut(b)
		if r := e.do(operator, "POST", "/api/notices", b); r.Status != 422 {
			t.Errorf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	r := e.do(operator, "POST", "/api/notices", good)
	if r.Status != 201 || r.JSON()["created_by"] != "operator1" {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	id := int(r.JSON()["id"].(float64))
	good["title"], good["end_at"] = "改了", 0
	if r := e.do(operator, "PUT", fmt.Sprintf("/api/notices/%d", id), good); r.Status != 200 || r.JSON()["title"] != "改了" {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	if r := e.do(operator, "PUT", "/api/notices/999", good); r.Status != 404 {
		t.Fatalf("update missing: %d", r.Status)
	}
	if l := e.do(operator, "GET", "/api/notices", nil).JSON(); l["total"].(float64) != 1 {
		t.Fatalf("list: %v", l)
	}
	if r := e.do(operator, "DELETE", fmt.Sprintf("/api/notices/%d", id), nil); r.Status != 200 {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := e.do(operator, "DELETE", fmt.Sprintf("/api/notices/%d", id), nil); r.Status != 404 {
		t.Fatalf("delete twice: %d", r.Status)
	}
	if r := e.do(operator, "GET", "/api/notices/abc", nil); r.Status != 404 && r.Status != 405 {
		t.Fatalf("bad id route: %d", r.Status)
	}
}
