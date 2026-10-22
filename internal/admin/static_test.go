package admin

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func get(t *testing.T, e *env, path string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestWebUIIsServedWithStrictHeaders(t *testing.T) {
	e := newEnv(t)
	res, body := get(t, e, "/")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") || !strings.Contains(body, `src="/app.js"`) {
		t.Fatalf("index: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	csp := res.Header.Get("Content-Security-Policy")
	for _, must := range []string{"script-src 'self'", "default-src 'none'", "frame-ancestors 'none'", "connect-src 'self'"} {
		if !strings.Contains(csp, must) {
			t.Errorf("CSP lacks %q: %s", must, csp)
		}
	}
	for _, never := range []string{"unsafe-inline", "unsafe-eval", "*", "http:", "https:"} {
		if strings.Contains(csp, never) {
			t.Errorf("CSP must not contain %q: %s", never, csp)
		}
	}
	if res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("Referrer-Policy") != "no-referrer" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", res.Header)
	}
	if strings.Contains(strings.ToLower(body), "<script>") || strings.Contains(body, "onclick=") {
		t.Error("index.html must not contain inline script")
	}
	for path, ct := range map[string]string{"/app.js": "text/javascript", "/style.css": "text/css"} {
		r, _ := get(t, e, path)
		if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), ct) {
			t.Errorf("%s: %d %q", path, r.StatusCode, r.Header.Get("Content-Type"))
		}
	}
}

func TestWebUIRevalidatesWithETag(t *testing.T) {
	e := newEnv(t)
	res, _ := get(t, e, "/app.js")
	etag := res.Header.Get("ETag")
	if etag == "" || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("etag=%q cache=%q", etag, res.Header.Get("Cache-Control"))
	}
	if r, _ := get(t, e, "/app.js", "If-None-Match", etag); r.StatusCode != 304 {
		t.Fatalf("a matching ETag must give 304, got %d", r.StatusCode)
	}
	if r, _ := get(t, e, "/app.js", "If-None-Match", `"stale"`); r.StatusCode != 200 {
		t.Fatalf("a stale ETag must give the file: %d", r.StatusCode)
	}
}

func TestWebUIServesFilesOnly(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/pages/", "/pages", "/web/", "/web/index.html", "/nothing.js", "/%2e%2e/server.go", "/../../etc/passwd", "/index.html/", "//", "/static.go"} {
		r, body := get(t, e, p)
		if p == "//" || p == "/index.html/" { // these clean to the index page: acceptable
			continue
		}
		if r.StatusCode != 404 {
			t.Errorf("%s -> %d %.60q", p, r.StatusCode, body)
		}
	}
	// the API is unaffected, and an unknown API path is a JSON 404, not the UI
	if r, _ := get(t, e, "/api/me"); r.StatusCode != 401 {
		t.Fatalf("/api/me: %d", r.StatusCode)
	}
	r, body := get(t, e, "/api/does-not-exist")
	if r.StatusCode != 404 || !strings.Contains(r.Header.Get("Content-Type"), "json") || !strings.Contains(body, "error") {
		t.Fatalf("unknown API path: %d %q %s", r.StatusCode, r.Header.Get("Content-Type"), body)
	}
	req, _ := http.NewRequest("POST", e.ts.URL+"/", nil)
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 405 {
		t.Fatalf("POST /: %d", res.StatusCode)
	}
}
