package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
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
