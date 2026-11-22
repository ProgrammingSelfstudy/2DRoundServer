package login

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"2droundserver/internal/config"
	"2droundserver/internal/store"
)

func TestAccountAuthTransportsAndNoStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var logs bytes.Buffer
	s := &Server{Store: st, Cfg: config.Default(), Log: log.New(&logs, "", 0)}
	handler := s.Handler()

	call := func(method, target, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: status %d, cache %q", method, r.URL.Path, w.Code, w.Header().Get("Cache-Control"))
		}
		if status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != http.MethodPost {
			t.Fatal("H5 method rejection did not advertise POST")
		}
		return w.Body.String()
	}

	const loginPath = "/index.php/userlogin/userLogins2"
	const regPath = "/index.php/userlogin/userRegs"
	const password = "test+p&ss=密"
	form := url.Values{"client": {"h5"}, "username": {"testh5"}, "password": {password}}
	if reply := call("POST", regPath, form.Encode(), 200); reply != `{msg="205"}` {
		t.Fatal("H5 registration failed")
	}
	reply := call("POST", loginPath, form.Encode(), 200)
	token, ok := strings.CutPrefix(reply, `{code=1, msg="`)
	if !ok || !strings.HasSuffix(token, `"}`) {
		t.Fatal("form login did not issue a token")
	}
	account, err := st.AccountByToken(strings.TrimSuffix(token, `"}`))
	if err != nil || account.Platform != store.PlatformH5 || account.Account != "testh5" {
		t.Fatal("form login issued a token for the wrong account")
	}
	form.Del("client")
	if reply := call("POST", loginPath+"?client=h5", form.Encode(), 200); !strings.HasPrefix(reply, `{code=1, msg="`) {
		t.Fatal("a query selector with body credentials did not issue a token")
	}
	form.Set("client", "h5")
	form.Set("password", "incorrect")
	if reply := call("POST", loginPath, form.Encode(), 200); reply != `{msg="207"}` {
		t.Fatal("wrong password was not rejected")
	}

	for _, path := range []string{loginPath, regPath} {
		call("GET", path+"?"+form.Encode(), "", 405)
		call("POST", path+"?"+form.Encode(), "", 400)
		call("POST", path+"?password=not-for-logs", form.Encode(), 400)
		call("POST", path+"?username=other", form.Encode(), 400)
		call("POST", path+"?client=other", form.Encode(), 400)
		call("POST", path+"?client=other&client=h5", form.Encode(), 400)
		jsonRequest := httptest.NewRequest("POST", path+"?client=h5", strings.NewReader(`{"username":"badform","password":"123456"}`))
		jsonRequest.Header.Set("Content-Type", "application/json")
		jsonReply := httptest.NewRecorder()
		handler.ServeHTTP(jsonReply, jsonRequest)
		if jsonReply.Code != http.StatusBadRequest || jsonReply.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s accepted an unsupported H5 content type: %d", path, jsonReply.Code)
		}
		call("POST", path, "client=h5&username=badform&password=%XX", 400)
		call("POST", path, "client=h5&username=badform&username=another&password=123456", 400)
		call("POST", path, "client=h5&username=badform&password=123456&password=654321", 400)
		call("POST", path, "client=h5&username=badform&password=", 400)
		call("POST", path, "client=h5&username=badform&password="+strings.Repeat("a", 73), 400)
		call("POST", path, "client=h5&username=badform&password="+strings.Repeat("a", 8<<10), 413)
	}
	if _, err := st.AccountByName(store.PlatformH5, "badform"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed form created an account: %v", err)
	}

	// Native clients send a query on both GET and POST and may pad it.
	const native = "?&&&username=testh5&password=native-secret"
	if reply := call("GET", regPath+native, "", 200); reply != `{msg="205"}` {
		t.Fatal("native registration failed")
	}
	for _, method := range []string{"GET", "POST"} {
		if reply := call(method, loginPath+native, "", 200); !strings.HasPrefix(reply, `{code=1, msg="`) {
			t.Fatalf("native %s login failed", method)
		}
	}
	for _, body := range []string{"ignored=%XX", "ignored=" + strings.Repeat("a", 8<<10)} {
		if reply := call("POST", loginPath+native, body, 200); !strings.HasPrefix(reply, `{code=1, msg="`) {
			t.Fatal("an ignored non-H5 form changed native query login")
		}
	}
	if reply := call("POST", "/index.php/userlogin/yxmmdasba?username=testh5", "", 200); !strings.HasPrefix(reply, `{code=1, msg="`) {
		t.Fatal("native token issuance changed for a password-backed account")
	}
	call("GET", "/index.php/userlogin/yxmmdasba", "", 200)
	if !strings.Contains(logs.String(), "POST "+loginPath+" from ") ||
		strings.Contains(logs.String(), "?") || strings.Contains(logs.String(), "native-secret") {
		t.Fatal("account request log did not contain the path alone")
	}
}

func TestH5TokenRequestValidation(t *testing.T) {
	s := newTestLogin(t)
	st := s.Store
	handler := s.Handler()
	const endpoint = "/index.php/userlogin/yxmmdasba"
	for _, tc := range []struct {
		name, method, query, form string
		status                    int
	}{
		{"query POST", "POST", "a=&username=tokuser&client=h5", "", 200},
		{"query GET", "GET", "username=tokuser&client=h5", "", 200},
		{"encoded form", "POST", "", "client=h5&username=tokuser", 200},
		{"query selector", "POST", "client=h5", "username=tokuser", 200},
		{"wrong method", "PUT", "client=h5&username=badform", "", 405},
		{"two usernames", "GET", "client=h5&username=badform&username=other", "", 400},
		{"two sources", "POST", "username=badform", "client=h5&username=other", 400},
		{"query password", "POST", "client=h5&username=badform&password=123456", "", 400},
		{"form password", "POST", "", "client=h5&username=badform&password=123456", 400},
		{"invalid username encoding", "GET", "client=h5&username=%FF", "", 400},
		{"oversize query", "GET", "client=h5&username=badform&padding=" + strings.Repeat("a", 8<<10), "", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := recordFormRequest(handler, tc.method, endpoint+"?"+tc.query, tc.form)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d, cache %q, reply %q", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
			}
			if tc.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET, POST" {
				t.Fatal("H5 method rejection did not advertise GET and POST")
			}
			if tc.status != http.StatusOK {
				return
			}
			account, err := st.AccountByToken(loginToken(t, w))
			if err != nil || account.Platform != store.PlatformH5 || account.Account != "tokuser" {
				t.Fatalf("the token resolved to the wrong account: %v", err)
			}
		})
	}
	if _, err := st.AccountByName(store.PlatformH5, "badform"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid H5 token request created an account: %v", err)
	}
	for _, user := range []string{"tokuser", "badform"} {
		if _, err := st.AccountByName(platform, user); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("H5 token request created a native account for %s: %v", user, err)
		}
	}
}

func TestH5PasswordEnrollmentRevokesBootstrapTokens(t *testing.T) {
	s := newTestLogin(t)
	handler := s.Handler()
	const tokenPath = "/index.php/userlogin/yxmmdasba?client=h5&username=firstpass"
	const loginPath = "/index.php/userlogin/userLogins2?client=h5"
	bootstrap := loginToken(t, recordFormRequest(handler, "POST", tokenPath, ""))
	guest, err := s.Store.AccountByToken(bootstrap)
	if err != nil || guest.Platform != store.PlatformH5 {
		t.Fatalf("bootstrap account: %v", err)
	}

	// Two simultaneous first logins must agree on one account password.
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, pass := range []string{"first-password", "second-password"} {
		go func() {
			<-start
			form := url.Values{"username": {guest.Account}, "password": {pass}}
			results <- recordFormRequest(handler, "POST", loginPath, form.Encode())
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.Body.String() == `{msg="207"}` {
		first, second = second, first
	}
	if second.Code != http.StatusOK || second.Body.String() != `{msg="207"}` {
		t.Fatalf("the losing password should be refused: %d %q", second.Code, second.Body.String())
	}
	if a, err := s.Store.AccountByToken(loginToken(t, first)); err != nil || a.ID != guest.ID || a.PasswordHash == "" {
		t.Fatalf("the winning password should log into the original account: %v", err)
	}
	if _, err := s.Store.AccountByToken(bootstrap); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bootstrap token survived enrollment: %v", err)
	}
	if w := recordFormRequest(handler, "POST", tokenPath, ""); w.Code != http.StatusOK || w.Body.String() != `{code=0, msg="account unavailable"}` {
		t.Fatalf("a password-backed account must use password login: %d %q", w.Code, w.Body.String())
	}
}

func TestH5BootstrapRespectsAutoRegister(t *testing.T) {
	s := newTestLogin(t)
	s.Cfg.LoginHTTP.AutoRegister = false
	handler := s.Handler()
	const tokenPath = "/index.php/userlogin/yxmmdasba?username=guest01"
	if w := recordFormRequest(handler, "POST", tokenPath+"&client=h5", ""); w.Code != http.StatusOK || w.Body.String() != `{code=0, msg="account unavailable"}` {
		t.Fatalf("H5 auto-register is off: %d %q", w.Code, w.Body.String())
	}
	if _, err := s.Store.AccountByName(store.PlatformH5, "guest01"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("H5 auto-register created an account: %v", err)
	}
	// The native username-only route has always provisioned accounts on first use.
	if a, err := s.Store.AccountByToken(loginToken(t, recordFormRequest(handler, "POST", tokenPath, ""))); err != nil || a.Platform != platform {
		t.Fatalf("native bootstrap behavior changed: %v", err)
	}
}

func recordFormRequest(handler http.Handler, method, target, form string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(form))
	if form != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func loginToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	token, ok := strings.CutPrefix(w.Body.String(), `{code=1, msg="`)
	if w.Code != http.StatusOK || !ok || !strings.HasSuffix(token, `"}`) {
		t.Fatalf("expected a login token, got %d %q", w.Code, w.Body.String())
	}
	return strings.TrimSuffix(token, `"}`)
}
