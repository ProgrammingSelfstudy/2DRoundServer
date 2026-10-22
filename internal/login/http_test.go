package login

import (
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
