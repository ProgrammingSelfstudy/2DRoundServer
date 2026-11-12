package login

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"2droundserver/internal/config"
	"2droundserver/internal/store"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{Store: st, Cfg: config.Default(), Log: log.New(io.Discard, "", 0)}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func get(t *testing.T, method, url string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestURLConfigAndServerList(t *testing.T) {
	_, ts := newTestServer(t)
	code, body := get(t, "GET", ts.URL+"/update/config/url_config_demo.lua?build_ver=0")
	if code != 200 || !strings.Contains(body, "URL_PATH_ALL") || !strings.Contains(body, "function get_servers_url") {
		t.Fatalf("%d %s", code, body)
	}
	code, body = get(t, "GET", ts.URL+"/api/serverlist?account=a")
	if code != 200 || !strings.Contains(body, `srv_id="symlf_1"`) || !strings.Contains(body, "port=8800") || !strings.Contains(body, "default_zone") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestMakeTokenIssuesUsableToken(t *testing.T) {
	s, ts := newTestServer(t)
	// the client's patched URL carries a filler "a=&" parameter
	code, body := get(t, "POST", ts.URL+"/index.php/userlogin/yxmmdasba?a=&username=test001&timestamp=1")
	if code != 200 || !strings.HasPrefix(body, "{code=1, msg=") {
		t.Fatalf("%d %s", code, body)
	}
	tok := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(body, "{code=1, msg="), "}"), `"`)
	acc, err := s.Store.AccountByToken(tok)
	if err != nil || acc.Account != "test001" {
		t.Fatalf("token not valid: %v %v", acc, err)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	_, ts := newTestServer(t)
	if code, _ := get(t, "GET", ts.URL+"/update/version2.txt"); code != 404 {
		t.Fatalf("want 404 got %d", code)
	}
}

func TestPlatformLoginAndRegister(t *testing.T) {
	s, ts := newTestServer(t)
	login := func(u, p string) string {
		_, b := get(t, "POST", ts.URL+"/index.php/userlogin/userLogins2?&&&username="+u+"&password="+p+"&action=login&acc_idfa=")
		return b
	}
	// auto_register on (default): unknown account is created with this password
	tokenOf := func(b string) string {
		if !strings.HasPrefix(b, "{code=1, msg=") {
			t.Fatalf("not a success reply: %s", b)
		}
		return strings.Trim(strings.TrimSuffix(strings.TrimPrefix(b, "{code=1, msg="), "}"), `"`)
	}
	tok := tokenOf(login("80566037", "123456"))
	if acc, err := s.Store.AccountByToken(tok); err != nil || acc.Account != "80566037" {
		t.Fatalf("login token not usable: %v %v", acc, err)
	}
	tokenOf(login("80566037", "123456"))
	if b := login("80566037", "wrongpw"); b != `{msg="207"}` {
		t.Fatalf("wrong password: %s", b)
	}
	reg := func(u, p string) string {
		_, b := get(t, "POST", ts.URL+"/index.php/userlogin/userRegs?&&&username="+u+"&password="+p)
		return b
	}
	if b := reg("ab", "123456"); b != `{msg="203"}` {
		t.Fatal(b)
	}
	if b := reg("player01", "123"); b != `{msg="204"}` {
		t.Fatal(b)
	}
	if b := reg("player01", "123456"); b != `{msg="205"}` {
		t.Fatal(b)
	}
	if b := reg("player01", "123456"); b != `{msg="200"}` {
		t.Fatal(b)
	}
}

// The H5 client adds client=h5 to its account calls and gets accounts of its own (store.PlatformH5); the same name under the original
// client's platform is another account, and a call without client=h5 never sees the H5 ones.
func TestH5AccountsLiveOnTheirOwnPlatform(t *testing.T) {
	s, ts := newTestServer(t)
	post := func(path, q string) string {
		_, b := get(t, "POST", ts.URL+path+"?&&&"+q)
		return b
	}
	if b := post("/index.php/userlogin/userRegs", "username=h5player&password=123456&client=h5"); b != `{msg="205"}` {
		t.Fatal(b)
	}
	if a, err := s.Store.AccountByName(store.PlatformH5, "h5player"); err != nil || a.Platform != store.PlatformH5 {
		t.Fatalf("not registered on the H5 platform: %v %v", a, err)
	}
	if _, err := s.Store.AccountByName("symlf", "h5player"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the H5 account leaked onto the original platform: %v", err)
	}
	// the same name is free on the original platform, and the H5 one is still taken
	if b := post("/index.php/userlogin/userRegs", "username=h5player&password=123456"); b != `{msg="205"}` {
		t.Fatal(b)
	}
	if b := post("/index.php/userlogin/userRegs", "username=h5player&password=123456&client=h5"); b != `{msg="200"}` {
		t.Fatal(b)
	}
	// a login with client=h5 reaches the H5 account (and its password), an auto-registered unknown name lands on the H5 platform too
	if b := post("/index.php/userlogin/userLogins2", "username=h5player&password=wrongpw&client=h5"); b != `{msg="207"}` {
		t.Fatal(b)
	}
	if b := post("/index.php/userlogin/userLogins2", "username=newbie01&password=123456&client=h5"); !strings.HasPrefix(b, "{code=1, msg=") {
		t.Fatal(b)
	}
	if a, err := s.Store.AccountByName(store.PlatformH5, "newbie01"); err != nil || a.Platform != store.PlatformH5 {
		t.Fatalf("auto-registered on the wrong platform: %v %v", a, err)
	}
	if _, err := s.Store.AccountByName("symlf", "newbie01"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("auto-registered H5 account on the original platform: %v", err)
	}
}

func TestPaddedClientPathsAreAccepted(t *testing.T) {
	_, ts := newTestServer(t)
	for _, p := range []string{"/", "/////log", "///////userLogin", "/////update/config/url_config_demo.lua"} {
		if code, _ := get(t, "GET", ts.URL+p); code != 200 {
			t.Errorf("%s -> %d", p, code)
		}
	}
	if _, b := get(t, "POST", ts.URL+"/index.php/userlogin/ageverify?&&&username=x&agestatus=1"); b != `{msg="200"}` {
		t.Fatal(b)
	}
}

func TestServerListComesFromDatabase(t *testing.T) {
	s, ts := newTestServer(t)
	s.Store.UpsertServer(store.Server{SrvID: "symlf_1", Name: "一区", Platform: "symlf", ZoneID: 1, Host: "10.0.0.5", Port: 8800, Status: 1, IsRecommend: true, GroupID: 1})
	s.Store.UpsertServer(store.Server{SrvID: "symlf_2", Name: "二区", Platform: "symlf", ZoneID: 2, Host: "10.0.0.6", Port: 8801, Status: 0, MainSrvID: "symlf_1", GroupID: 1})
	_, body := get(t, "GET", ts.URL+"/api/serverlist?account=a")
	for _, want := range []string{`name="一区"`, `name="二区"`, `ip="10.0.0.6"`, "port=8801", "is_maintain=true", "main_zone_id=1", "default_zone={srv_id=\"symlf_1\""} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	// editing a server is visible on the next request
	s.Store.UpsertServer(store.Server{SrvID: "symlf_2", Name: "二区", Platform: "symlf", ZoneID: 2, Host: "10.0.0.6", Port: 8801, Status: 1, GroupID: 1})
	_, body = get(t, "GET", ts.URL+"/api/serverlist?account=a")
	if strings.Contains(body, "is_maintain=true") {
		t.Fatalf("admin edit not visible: %s", body)
	}
}

// The token call (yxmmdasba) creates the account on first use too: with client=h5 it is an H5 account
// and the token belongs to it, without the parameter it is the original client's account of the same
// name. Both hand out distinct tokens that resolve to their own account.
func TestH5TokenBelongsToTheH5Account(t *testing.T) {
	s, ts := newTestServer(t)
	token := func(q string) string {
		t.Helper()
		_, b := get(t, "POST", ts.URL+"/index.php/userlogin/yxmmdasba?a=&username=tokuser&timestamp=1"+q)
		const head, tail = `{code=1, msg="`, `"}`
		if !strings.HasPrefix(b, head) || !strings.HasSuffix(b, tail) {
			t.Fatalf("no token in %q (query %q)", b, q)
		}
		return strings.TrimSuffix(strings.TrimPrefix(b, head), tail)
	}

	h5 := token("&client=h5")
	acc, err := s.Store.AccountByToken(h5)
	if err != nil || acc.Platform != store.PlatformH5 || acc.Account != "tokuser" {
		t.Fatalf("the H5 token resolves to %+v, %v", acc, err)
	}
	// no original account yet: the call with client=h5 must not have created one
	if _, err := s.Store.AccountByName("symlf", "tokuser"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an H5 token call created an original-platform account: %v", err)
	}

	orig := token("")
	oacc, err := s.Store.AccountByToken(orig)
	if err != nil || oacc.Platform != "symlf" || oacc.ID == acc.ID {
		t.Fatalf("the plain token resolves to %+v, %v (H5 account %d)", oacc, err, acc.ID)
	}
	// a second call finds the account again instead of creating another, a token per call
	if again := token("&client=h5"); again == h5 {
		t.Fatal("the same token was issued twice")
	} else if a2, err := s.Store.AccountByToken(again); err != nil || a2.ID != acc.ID {
		t.Fatalf("second H5 token resolves to %+v, %v", a2, err)
	}
	// an H5 account that is banned gets no token, and that does not touch the original one
	if err := s.Store.SetAccountStatus(acc.ID, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if _, b := get(t, "POST", ts.URL+"/index.php/userlogin/yxmmdasba?a=&username=tokuser&client=h5"); b != `{code=0, msg="account unavailable"}` {
		t.Fatalf("banned H5 account: %s", b)
	}
	token("")
}
