package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

func TestModuleAPIReadPermissionsAndNoWrites(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("modules", "symlf_1", "模块测试")
	base := fmt.Sprintf("/api/roles/%d/modules", role.RID)
	seed := map[string]any{"Large": int64(9007199254740993), "FutureField": []any{nil, "原样保留"}}
	for _, key := range []string{"monthcard", "firstcharge_first_new", "legacy_unknown"} {
		if err := e.st.SaveState(role.RID, key, seed); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := e.st.RoleModules(role.RID)
	auditBefore := e.auditActions()
	for _, token := range []string{viewer, operator, super} {
		for _, path := range []string{"/api/modules", base, base + "/monthcard", base + "/firstcharge_first_new", base + "/daycharge"} {
			if got := e.do(token, "GET", path, nil); got.Status != 200 {
				t.Fatalf("%s: %d %s", path, got.Status, got.Body)
			}
			if got := e.do("", "GET", path, nil); got.Status != 401 {
				t.Fatalf("unauthed %s: %d", path, got.Status)
			}
		}
		for _, key := range []string{"missing", "firstcharge_arbitrary", "firstcharge_*", "guildmarket:1"} {
			if got := e.do(token, "GET", base+"/"+key, nil); got.Status != 404 {
				t.Fatalf("unknown key %q: %d", key, got.Status)
			}
		}
	}
	var catalog struct{ Items []moduleInfo }
	if err := json.Unmarshal(e.do(viewer, "GET", "/api/modules", nil).Body, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != len(game.AdminModules()) {
		t.Fatal("catalog incomplete")
	}
	ranks := map[string]bool{"arenateam": true, "arenamany": true, "crossarena": true, "elitematch": true, "guildwar": true, "daysrank": true, "arenachamp": true, "crosschamp": true, "ladder": true, "endless_rank": true, "practisetower_rank": true, "sandybeach": true, "sweet": true, "yearmonster": true, "termbegins": true, "homeworld_board": true}
	for _, m := range catalog.Items {
		charge := m.Scope == "role" && (m.Key == "firstcharge_*" || m.Key == "daycharge" || m.Key == "monthcard" || m.Key == "monthfund" || m.Key == "limitgift" || m.Key == "cyclegift")
		if m.Editable || m.HasRank != (m.Scope == "global" && ranks[m.Key]) || m.Resets == nil || m.HasView != (charge || m.Scope == "global") || (len(m.Resets) > 0) != charge {
			t.Fatalf("unexpected phase-3 capabilities: %+v", m)
		}
	}
	got := e.do(viewer, "GET", base+"/monthcard", nil)
	if !bytes.Contains(got.Body, []byte("9007199254740993")) || !bytes.Contains(got.Body, []byte("FutureField")) {
		t.Fatal("raw data lost precision or unknown fields")
	}
	if e.do(viewer, "GET", base+"/daycharge", nil).JSON()["exists"] != false {
		t.Fatal("missing state initialized")
	}
	var list struct {
		Items []struct {
			Key        string
			Registered bool
		}
	}
	json.Unmarshal(e.do(viewer, "GET", base, nil).Body, &list)
	if len(list.Items) != 3 || list.Items[1].Key != "legacy_unknown" || list.Items[1].Registered {
		t.Fatalf("existing/unregistered rows not faithfully listed: %+v", list)
	}
	after, _ := e.st.RoleModules(role.RID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(e.auditActions(), auditBefore) {
		t.Fatal("GET changed state or audit")
	}
	if got := e.do(super, "PUT", base+"/monthcard", seed); got.Status != 405 {
		t.Fatalf("JSON edit route should not exist: %d", got.Status)
	}
	if got := e.do(viewer, "GET", "/api/servers/symlf_1/modules", nil); got.Status != 502 {
		t.Fatalf("global data must require the gateway: %d", got.Status)
	}
	if got := e.do(viewer, "GET", "/api/roles/9999999/modules", nil); got.Status != 404 {
		t.Fatalf("missing role: %d", got.Status)
	}
}

func TestModuleResetRealRouteFailsClosed(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("reset", "symlf_1", "重置测试")
	if err := e.st.SaveState(role.RID, "daycharge", map[string]any{"Day": 1, "Sum": 123}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := e.st.RoleModule(role.RID, "daycharge")
	auditBefore := e.auditActions()
	path := fmt.Sprintf("/api/roles/%d/modules/daycharge/reset", role.RID)
	body := map[string]any{"item": "all", "reason": "人工核对"}
	for _, tc := range []struct {
		tok    string
		status int
	}{{"", 401}, {viewer, 403}, {operator, 403}, {super, 422}} {
		if got := e.do(tc.tok, "POST", path, body); got.Status != tc.status {
			t.Fatalf("permission/whitelist: %d %s", got.Status, got.Body)
		}
	}
	e.gw.mu.Lock()
	e.gw.onlineRID = strconv.FormatInt(role.RID, 10)
	e.gw.mu.Unlock()
	if got := e.do(super, "POST", path, body); got.Status != 409 {
		t.Fatalf("online reset: %d", got.Status)
	}
	e.gw.mu.Lock()
	e.gw.down = true
	e.gw.mu.Unlock()
	if got := e.do(super, "POST", path, body); got.Status != 502 {
		t.Fatalf("gateway down: %d", got.Status)
	}
	for _, reason := range []string{"", "   ", strings.Repeat("字", 201), "\n原因"} {
		if got := e.do(super, "POST", path, map[string]string{"item": "all", "reason": reason}); got.Status != 422 {
			t.Fatalf("invalid reason: %d", got.Status)
		}
	}
	for _, bad := range []string{`{"item":"all","reason":"测试","extra":1}`, `{"item":"all","reason":"测试"} {}`} {
		if got := e.do(super, "POST", path, bad); got.Status != 400 {
			t.Fatalf("invalid body: %d", got.Status)
		}
	}
	after, _, _ := e.st.RoleModule(role.RID, "daycharge")
	if !bytes.Equal(before, after) || !reflect.DeepEqual(e.auditActions(), auditBefore) {
		t.Fatal("rejected reset wrote data/audit")
	}
}

func TestModuleResetUnknownOnlineStatus(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("unknownonline", "symlf_1", "状态未知")
	path := fmt.Sprintf("/api/roles/%d/modules/monthcard/reset", role.RID)
	for _, reply := range []string{`{}`, `{"online":null}`, `{"online":"false"}`, `null`, `not-json`} {
		t.Run(reply, func(t *testing.T) {
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, reply) }))
			defer gw.Close()
			e.s.GatewayURL = gw.URL
			if got := e.do(super, "POST", path, map[string]string{"item": "all", "reason": "核对"}); got.Status != 502 {
				t.Fatalf("unknown online accepted: %d", got.Status)
			}
		})
	}
	if rows, _ := e.st.RoleModules(role.RID); len(rows) != 0 {
		t.Fatal("unknown status initialized state")
	}
}

func TestModuleResetMechanismAndAudit(t *testing.T) {
	e := newEnv(t)
	e.seedServer("symlf_1")
	role := e.seedRole("mechanism", "symlf_1", "机制测试")
	aid := e.admin("reset_admin", "superadmin", "resetpassword")
	admin, err := e.st.AdminByID(aid)
	if err != nil {
		t.Fatal(err)
	}
	type state struct{ Day, Count int64 }
	m := game.AdminModule{Key: "test_mechanism", Scope: "role", New: func() any { return &state{} }, Resets: []game.AdminReset{{Item: "today", Title: "当日", Apply: func(_ *store.Store, _ int64, value any, _ time.Time) error { value.(*state).Count = 0; return nil }}, {Item: "all", Title: "整行"}}}
	call := func(item, reason string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"item": item, "reason": reason})
		req := httptest.NewRequest("POST", "/reset", bytes.NewReader(body))
		w := httptest.NewRecorder()
		e.s.resetModule(w, req, &principal{Admin: admin}, role, "test_mechanism", m)
		return w
	}
	if err := e.st.SaveState(role.RID, "test_mechanism", state{Day: 3, Count: 9}); err != nil {
		t.Fatal(err)
	}
	if got := call("today", strings.Repeat("字", 200)); got.Code != 200 {
		t.Fatalf("whitelisted reset: %d %s", got.Code, got.Body)
	}
	var result state
	e.st.LoadState(role.RID, "test_mechanism", &result)
	if result.Count != 0 || result.Day != 3 {
		t.Fatalf("wrong reset: %+v", result)
	}
	logs, total, err := e.st.ListAudit(10, 0)
	if err != nil || total != 1 {
		t.Fatalf("missing reset audit: %v", err)
	}
	var detail map[string]any
	json.Unmarshal([]byte(logs[0].DetailJSON), &detail)
	if logs[0].Action != "role.module.reset" || detail["key"] != "test_mechanism" || detail["item"] != "today" || detail["reason"] != strings.Repeat("字", 200) || detail["diff"] == nil {
		t.Fatal("audit missing reason/key/item/diff")
	}
	if got := call("all", "删除测试行"); got.Code != 200 {
		t.Fatalf("explicit all: %d", got.Code)
	}
	if _, exists, err := e.st.RoleModule(role.RID, "test_mechanism"); err != nil || exists {
		t.Fatal("explicit all did not delete")
	}
	if got := call("all", "重复检查"); got.Code != 404 {
		t.Fatalf("missing state should not be recreated: %d", got.Code)
	}
	if rows, _ := e.st.RoleModules(role.RID); len(rows) != 0 {
		t.Fatal("missing state initialized by reset")
	}
	// This descriptor is never added to game.AdminModules; production remains read-only.
	if _, ok := game.FindAdminModule("role", "test_mechanism"); ok {
		t.Fatal("test whitelist leaked into production")
	}
}
