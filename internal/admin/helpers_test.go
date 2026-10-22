package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"2droundserver/internal/store"
)

// gatewayStub plays the gateway's control endpoint and records what the API asks of it.
type gatewayStub struct {
	mu                        sync.Mutex
	pushes                    []string
	kicks                     []string
	refreshes                 []string
	heroPushes                []string // the heroes= of each refresh
	refreshed                 bool     // what POST /push/refresh answers
	noticePushes, noticeDrops []string
	broadcasts                []string
	pushed                    int
	down                      bool
	onlineRID                 string // the role GET /online?rid= reports as online
	kickedAnyone              bool   // what POST /kick answers
}

func (g *gatewayStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/push/mail":
		g.pushes = append(g.pushes, r.URL.Query().Get("id"))
		json.NewEncoder(w).Encode(map[string]int{"pushed": g.pushed})
	case "/push/notice":
		g.noticePushes = append(g.noticePushes, r.URL.Query().Get("id"))
		json.NewEncoder(w).Encode(map[string]int{"pushed": g.pushed, "dropped": 0})
	case "/push/notice/remove":
		g.noticeDrops = append(g.noticeDrops, r.URL.Query().Get("id"))
		json.NewEncoder(w).Encode(map[string]int{"dropped": 3})
	case "/push/broadcast":
		g.broadcasts = append(g.broadcasts, r.URL.Query().Get("msg"))
		json.NewEncoder(w).Encode(map[string]int{"pushed": 4})
	case "/push/refresh":
		g.refreshes = append(g.refreshes, r.URL.Query().Get("rid"))
		g.heroPushes = append(g.heroPushes, r.URL.Query().Get("heroes"))
		json.NewEncoder(w).Encode(map[string]bool{"refreshed": g.refreshed})
	case "/kick":
		g.kicks = append(g.kicks, r.URL.Query().Get("rid"))
		json.NewEncoder(w).Encode(map[string]bool{"kicked": g.kickedAnyone})
	case "/online":
		if rid := r.URL.Query().Get("rid"); rid != "" {
			json.NewEncoder(w).Encode(map[string]bool{"online": rid == g.onlineRID})
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"online": 7})
	default:
		http.NotFound(w, r)
	}
}

type env struct {
	t     *testing.T
	st    *store.Store
	s     *Server
	ts    *httptest.Server
	gw    *gatewayStub
	gwSrv *httptest.Server
	clock atomic.Int64 // unix nanoseconds
	logs  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, gw: &gatewayStub{kickedAnyone: true}, logs: &bytes.Buffer{}}
	e.clock.Store(time.Unix(1_800_000_000, 0).UnixNano())
	e.gwSrv = httptest.NewServer(e.gw)
	e.s = &Server{
		Store: st, Secret: []byte("test-secret-0123456789-0123456789-abcdef"), TTL: time.Hour, GatewayURL: e.gwSrv.URL,
		Log: log.New(e.logs, "", 0), Now: func() time.Time { return time.Unix(0, e.clock.Load()) },
	}
	e.ts = httptest.NewServer(e.s.Handler())
	t.Cleanup(func() { e.ts.Close(); e.gwSrv.Close(); st.Close() })
	return e
}

func (e *env) advance(d time.Duration) { e.clock.Add(int64(d)) }

// admin creates an admin directly in the store (cheap bcrypt) and returns its id.
func (e *env) admin(name, role, password string) int64 {
	e.t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	a, err := e.st.CreateAdmin(name, string(h), role)
	if err != nil {
		e.t.Fatal(err)
	}
	return a.ID
}

type resp struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r resp) JSON() map[string]any {
	var m map[string]any
	json.Unmarshal(r.Body, &m)
	return m
}

func (r resp) Err() string { s, _ := r.JSON()["error"].(string); return s }

func (e *env) do(token, method, path string, body any) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Body: raw, Header: res.Header}
}

// login returns a token or fails the test.
func (e *env) login(user, pass string) string {
	e.t.Helper()
	r := e.do("", "POST", "/api/login", map[string]string{"username": user, "password": pass})
	if r.Status != 200 {
		e.t.Fatalf("login %s: %d %s", user, r.Status, r.Body)
	}
	return r.JSON()["token"].(string)
}

// tokens returns one token per role, creating the admins.
func (e *env) tokens() (viewer, operator, super string) {
	e.admin("viewer1", "viewer", "viewerpass1")
	e.admin("operator1", "operator", "operatorpass1")
	e.admin("super1", "superadmin", "superpass123")
	return e.login("viewer1", "viewerpass1"), e.login("operator1", "operatorpass1"), e.login("super1", "superpass123")
}

func (e *env) seedServer(id string) {
	e.t.Helper()
	if err := e.st.UpsertServer(store.Server{SrvID: id, Name: id, Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) seedRole(account, srv, name string) *store.Role {
	e.t.Helper()
	a, err := e.st.FindOrCreateAccount("symlf", account)
	if err != nil {
		e.t.Fatal(err)
	}
	r, err := e.st.CreateRole(a.ID, srv, name, 0, 1, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) auditActions() []string {
	rows, _, err := e.st.ListAudit(200, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
