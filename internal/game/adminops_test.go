package game

import (
	"2droundserver/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func opsRequest(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func opsTestHandler(e *env) http.Handler { e.mail.Now = e.gw.Now; return e.mail.AdminHandler() }
func globalBytes(t *testing.T, st *store.Store, srv string) map[string]string {
	t.Helper()
	keys, err := st.GlobalModuleKeys(srv, "")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, k := range keys {
		v, _, err := st.GlobalModule(srv, k)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = string(v)
	}
	return out
}
func TestAdminGlobalReadsUseLiveOwnersAndNeverRoll(t *testing.T) {
	e := newEnv(t, time.Hour)
	e.setNow(actNow)
	h := opsTestHandler(e)
	for _, m := range AdminModules() {
		if m.Scope != "global" {
			continue
		}
		key := m.Key
		if strings.HasSuffix(key, "*") {
			key = strings.TrimSuffix(key, "*") + "1"
		}
		state := m.New()
		if err := e.st.SaveGlobal("symlf_1", key, state); err != nil {
			t.Fatal(err)
		}
	}
	// A due state must stay due after every GET; no loader may settle it.
	if err := e.st.SaveGlobal("symlf_1", drModule, &drWorld{}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(globalBytes(t, e.st, "symlf_1"))
	for _, m := range AdminModules() {
		if m.Scope != "global" {
			continue
		}
		key := m.Key
		if strings.HasSuffix(key, "*") {
			key = strings.TrimSuffix(key, "*") + "1"
		}
		for _, path := range []string{"/ops/global?srv=symlf_1&key=" + key, "/ops/season?srv=symlf_1&key=" + key} {
			w := opsRequest(h, "GET", path, nil)
			if w.Code != 200 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body)
			}
		}
		if m.Rank != nil {
			w := opsRequest(h, "GET", "/ops/rank?srv=symlf_1&key="+key+"&limit=2", nil)
			if w.Code != 200 {
				t.Fatalf("rank %s: %d %s", key, w.Code, w.Body)
			}
		}
	}
	for _, path := range []string{"/ops/global?srv=symlf_1", "/ops/overview?srv=symlf_1"} {
		w := opsRequest(h, "GET", path, nil)
		if w.Code != 200 {
			t.Fatalf("list/overview: %d %s", w.Code, w.Body)
		}
	}
	after, _ := json.Marshal(globalBytes(t, e.st, "symlf_1"))
	if !bytes.Equal(before, after) {
		t.Fatal("GET saved defaults, rolled or settled global state")
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/ops/global?srv=other&key=daysrank", 404}, {"/ops/global?srv=symlf_1&key=firstcharge_first", 404}, {"/ops/global?srv=symlf_1&key=guildmarket:01", 404}, {"/ops/rank?srv=symlf_1&key=dial", 405}, {"/ops/rank?srv=symlf_1&key=ladder&limit=-1", 422}} {
		if w := opsRequest(h, "GET", tc.path, nil); w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
}
func TestAdminGlobalReadWaitsForActualServiceLock(t *testing.T) {
	e := newEnv(t, time.Hour)
	h := opsTestHandler(e)
	for _, key := range []string{atModule, amModule, caModule, eliteModule, guildWarModule, champModule, drModule, "monopoly_g1", sbModule} {
		_, o, err := globalOwner(e.st, key)
		if err != nil {
			t.Fatal(err)
		}
		o.mu.Lock()
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- opsRequest(h, "GET", "/ops/global?srv=symlf_1&key="+key, nil) }()
		select {
		case <-done:
			o.mu.Unlock()
			t.Fatalf("%s read outside owning lock", key)
		case <-time.After(15 * time.Millisecond):
		}
		o.mu.Unlock()
		select {
		case w := <-done:
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
		case <-time.After(time.Second):
			t.Fatal("read stayed locked")
		}
	}
	// A different Store cannot borrow this gateway's service instance/lock.
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, _, err := globalOwner(st, atModule); err == nil {
		t.Fatal("unregistered owner silently synthesized")
	}
}
func TestAdminGlobalRanksReuseExistingOrdering(t *testing.T) {
	e := newEnv(t, time.Hour)
	b := &amBoard{Roles: map[int64]*amRole{3: {RID: 3, Score: 200, Do: 1}, 2: {RID: 2, Score: 200, Do: 1}, 1: {RID: 1, Score: 300, Do: 1}, 4: {RID: 4, Score: 500}}}
	if err := e.st.SaveGlobal("symlf_1", amModule, b); err != nil {
		t.Fatal(err)
	}
	m, _ := FindAdminModule("global", amModule)
	rows, err := m.Rank(e.st, "symlf_1", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := b.ranked()
	if len(rows) != 2 || rows[0].Data.(*amRole).RID != want[0].RID || rows[1].Data.(*amRole).RID != want[1].RID {
		t.Fatal("rank uses different scoring/qualification/ties")
	}
}
