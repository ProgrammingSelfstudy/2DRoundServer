package admin

import (
	"2droundserver/internal/game"
	"2droundserver/internal/gateway"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGlobalReadProxyPermissionsRoutingAndNoFallback(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	var mu sync.Mutex
	calls := []string{}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"gateway": true, "path": r.URL.Path, "srv": r.URL.Query().Get("srv"), "key": r.URL.Query().Get("key")})
	}))
	defer gw.Close()
	e.s.GatewayURL = gw.URL
	paths := []string{"/api/servers/symlf_1/modules", "/api/servers/symlf_1/modules/daysrank", "/api/servers/symlf_1/modules/daysrank/rank?limit=3", "/api/servers/symlf_1/modules/daysrank/season", "/api/servers/symlf_1/overview"}
	auditBefore := e.auditActions()
	for _, path := range paths {
		for _, token := range []string{viewer, operator, super} {
			r := e.do(token, "GET", path, nil)
			if r.Status != 200 || r.JSON()["gateway"] != true || r.JSON()["srv"] != "symlf_1" {
				t.Fatalf("proxy %s: %d %s", path, r.Status, r.Body)
			}
		}
		if r := e.do("", "GET", path, nil); r.Status != 401 {
			t.Fatal("public global API")
		}
	}
	beforeCalls := len(calls)
	if r := e.do(viewer, "GET", "/api/servers/symlf_1/modules/unknown", nil); r.Status != 404 {
		t.Fatal("unknown module accepted")
	}
	if len(calls) != beforeCalls {
		t.Fatal("invalid key reached gateway")
	}
	for _, upstream := range []struct {
		code, want int
		body       string
	}{{503, 502, `{"error":"down"}`}, {500, 502, `{"error":"broken"}`}, {200, 502, `invalid`}, {405, 405, `{"error":"unsupported"}`}, {404, 404, `{"error":"missing"}`}} {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(upstream.code)
			w.Write([]byte(upstream.body))
		}))
		e.s.GatewayURL = bad.URL
		if r := e.do(viewer, "GET", paths[1], nil); r.Status != upstream.want {
			t.Fatalf("upstream %d: %d", upstream.code, r.Status)
		}
		bad.Close()
	}
	e.s.GatewayURL = "http://127.0.0.1:1"
	for _, path := range paths {
		if r := e.do(viewer, "GET", path, nil); r.Status != 502 {
			t.Fatalf("fallback for %s: %d", path, r.Status)
		}
	}
	a, _ := json.Marshal(auditBefore)
	b, _ := json.Marshal(e.auditActions())
	if !bytes.Equal(a, b) {
		t.Fatal("GET wrote audit")
	}
}

func TestSettlementAPIRequiresManageAndForwardsTrustedActor(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	calls := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var op game.AdminOperation
		if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.URL.Path != "/ops/season/settle" || op.Reason != "补结算" || op.AdminID == 0 || op.AdminName == "" || op.IP == "" {
			t.Errorf("bad authenticated envelope: %+v", op)
		}
		json.NewEncoder(w).Encode(map[string]any{"settled": false, "reason": "not_due"})
	}))
	defer gw.Close()
	e.s.GatewayURL = gw.URL
	for _, key := range []string{"arenateam", "elitematch", "crossarena", "arenamany", "daysrank", "guildwar", "arenachamp", "crosschamp"} {
		path := "/api/servers/symlf_1/seasons/" + key + "/settle"
		for _, tc := range []struct {
			token  string
			status int
		}{{"", 401}, {viewer, 403}, {operator, 403}, {super, 200}} {
			r := e.do(tc.token, "POST", path, map[string]string{"reason": "补结算"})
			if r.Status != tc.status {
				t.Fatalf("%s permission: %d %s", key, r.Status, r.Body)
			}
		}
	}
	if calls != 8 {
		t.Fatalf("unauthorized operation reached gateway: %d", calls)
	}
	for _, reason := range []string{"", " ", strings.Repeat("字", 201), "bad\n"} {
		if r := e.do(super, "POST", "/api/servers/symlf_1/seasons/daysrank/settle", map[string]string{"reason": reason}); r.Status != 422 {
			t.Fatal("invalid reason accepted")
		}
	}
	if calls != 8 {
		t.Fatal("invalid reason reached gateway")
	}
	e.s.GatewayURL = "http://127.0.0.1:1"
	if r := e.do(super, "POST", "/api/servers/symlf_1/seasons/daysrank/settle", map[string]string{"reason": "补结算"}); r.Status != 502 {
		t.Fatal("gateway failure must be 502")
	}
}
func TestSettlementAPIWithRealGatewayCommitsOnePayoutAndAudit(t *testing.T) {
	e := newEnv(t)
	viewer, _, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("settle-real-gateway", "symlf_1", "到期角色")
	sv, _ := e.st.Server("symlf_1")
	sv.OpenTime = e.s.Now().Add(-20 * 24 * time.Hour).Unix()
	if err := e.st.UpsertServer(*sv); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ArenaPlayerOf(role.RID, 1000, e.s.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	router := gateway.NewRouter()
	game.RegisterDaysRank(router, e.st)
	gw := httptest.NewServer((&game.MailService{Store: e.st, SrvID: "symlf_1", Hub: gateway.NewHub(), Now: e.s.Now}).AdminHandler())
	defer gw.Close()
	e.s.GatewayURL = gw.URL
	path := "/api/servers/symlf_1/seasons/daysrank/settle"
	first := e.do(super, "POST", path, map[string]string{"reason": "补做到期奖励"})
	if first.Status != 200 || first.JSON()["settled"] != true {
		t.Fatalf("real settle: %d %s", first.Status, first.Body)
	}
	_, n, _ := e.st.ListMails(100, 0)
	if n == 0 {
		t.Fatal("real handler paid nothing")
	}
	if r := e.do(super, "POST", path, map[string]string{"reason": "重复确认"}); r.Status != 200 || r.JSON()["reason"] != "already" {
		t.Fatalf("repeat: %s", r.Body)
	}
	_, again, _ := e.st.ListMails(100, 0)
	if again != n {
		t.Fatal("repeat mailed twice")
	}
	logs, _, _ := e.st.ListAudit(100, 0)
	count := 0
	for _, l := range logs {
		if l.Action == "server.season.settle" {
			count++
			if l.AdminName == "" || !strings.Contains(l.DetailJSON, "reason") {
				t.Fatal("missing audit actor/reason")
			}
		}
	}
	if count != 2 {
		t.Fatal("one gateway audit required per accepted request")
	}
	// Viewer sees settled state through the same real gateway.
	if r := e.do(viewer, "GET", "/api/servers/symlf_1/modules/daysrank/season", nil); r.Status != 200 || r.JSON()["settled"] != true {
		t.Fatalf("read settled state: %d %s", r.Status, r.Body)
	}
}
