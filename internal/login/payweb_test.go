package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"2droundserver/internal/store"
)

// The client's iceBirdOnPay1 (capture 2026-11-05): POST pay_web on the server list's `host`; the body is the 快乐币 left.
func TestPayWeb(t *testing.T) {
	s, ts := newTestServer(t)
	if err := s.Store.EnsureServer(s.Cfg.DefaultServer()); err != nil {
		t.Fatal(err)
	}
	acc, err := s.Store.CreateAccount("symlf", "webpayer", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.Store.CreateRole(acc.ID, "symlf_1", "直付人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Exchange(role.RID, nil, nil, map[int64]int64{41: 100}, nil); err != nil {
		t.Fatal(err)
	}
	// a fake gateway: the role is online, and the push is recorded
	var mu sync.Mutex
	online, pushes := true, []string{}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/online":
			w.Write([]byte(`{"online":` + map[bool]string{true: "true", false: "false"}[online] + `}`))
		case "/push/charge":
			pushes = append(pushes, r.URL.RawQuery)
			w.Write([]byte(`{"refreshed":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	s.Cfg.Gateway.AdminAddr = strings.TrimPrefix(gw.URL, "http://")

	pay := func(q string) (int, string) {
		return get(t, "POST", ts.URL+"/api.php/pf/sygame/pay_web/?"+q)
	}
	base := "srv_id=symlf_1&role_id=" + itoa(role.RID) + "&name=webpayer"
	code, body := pay(base + "&money=30&productId=4")
	if code != 200 || body != "70" {
		t.Fatalf("pay: %d %q (the body is the 快乐币 left)", code, body)
	}
	if a, _ := s.Store.RoleAssets(role.RID); a[3] != 300 || a[41] != 70 {
		t.Fatalf("assets: %v", a)
	}
	mu.Lock()
	if len(pushes) != 1 || !strings.Contains(pushes[0], "paid=300") || !strings.Contains(pushes[0], "mail=") || !strings.Contains(pushes[0], "rid="+itoa(role.RID)) {
		t.Fatalf("pushes: %v", pushes)
	}
	mu.Unlock()

	// refusals are plain messages for the client's toast, and nothing is taken
	for name, c := range map[string]struct{ q, want string }{
		"too few":      {base + "&money=128&productId=21", "快乐币不足"},
		"wrong money":  {base + "&money=6&productId=4", "该商品暂时无法购买"},
		"unknown":      {base + "&money=30&productId=99999", "该商品暂时无法购买"},
		"klb product":  {base + "&money=38&productId=30001", "该商品暂时无法购买"},
		"other name":   {"srv_id=symlf_1&role_id=" + itoa(role.RID) + "&name=someoneelse&money=30&productId=4", "账号与角色不符"},
		"other server": {"srv_id=symlf_9&role_id=" + itoa(role.RID) + "&name=webpayer&money=30&productId=4", "账号与角色不符"},
		"no role":      {"srv_id=symlf_1&role_id=99999&name=webpayer&money=30&productId=4", "角色不存在"},
		"junk":         {"srv_id=symlf_1&role_id=x&money=y&productId=z", "充值参数错误"},
	} {
		if code, body := pay(c.q); code != 200 || body != c.want {
			t.Fatalf("%s: %d %q, want %q", name, code, body, c.want)
		}
	}
	if a, _ := s.Store.RoleAssets(role.RID); a[3] != 300 || a[41] != 70 {
		t.Fatalf("a refused payment changed the assets: %v", a)
	}

	// not online: refused (the original had no such check; without it an account name alone would spend the 快乐币)
	mu.Lock()
	online = false
	mu.Unlock()
	if code, body := pay(base + "&money=30&productId=4"); code != 200 || body != "请在游戏内充值" {
		t.Fatalf("offline: %d %q", code, body)
	}
	if code, _ := get(t, "GET", ts.URL+"/api.php/pf/sygame/pay_web/?"+base+"&money=30&productId=4"); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", code)
	}
}

// The server list's `host` is where the client sends pay_web: the login service's own address, `ip` stays the game's.
func TestServerListHostIsThePayAddress(t *testing.T) {
	s, ts := newTestServer(t)
	s.Cfg.LoginHTTP.PublicBase = "http://192.168.1.9:8080"
	_, body := get(t, "GET", ts.URL+"/api/serverlist?account=a")
	if !strings.Contains(body, `host="192.168.1.9:8080"`) || !strings.Contains(body, `ip="127.0.0.1"`) {
		t.Fatalf("server list: %s", body)
	}
}

// A red bag (products 501 to 503) goes through pay_web: refused with the reason without a guild; with one it is sent and the push
// carries the bag's id (capture 2026-11-06).
func TestPayWebRedbag(t *testing.T) {
	s, ts := newTestServer(t)
	if err := s.Store.EnsureServer(s.Cfg.DefaultServer()); err != nil {
		t.Fatal(err)
	}
	acc, _ := s.Store.CreateAccount("symlf", "bagger", "x", "")
	role, err := s.Store.CreateRole(acc.ID, "symlf_1", "发包人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Exchange(role.RID, nil, nil, map[int64]int64{41: 100}, nil); err != nil {
		t.Fatal(err)
	}
	var pushes []string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/online":
			w.Write([]byte(`{"online":true}`))
		case "/push/charge":
			pushes = append(pushes, r.URL.RawQuery)
			w.Write([]byte(`{"refreshed":true}`))
		}
	}))
	defer gw.Close()
	s.Cfg.Gateway.AdminAddr = strings.TrimPrefix(gw.URL, "http://")
	q := "srv_id=symlf_1&role_id=" + itoa(role.RID) + "&name=bagger&money=12&productId=501"
	if code, body := get(t, "POST", ts.URL+"/api.php/pf/sygame/pay_web/?"+q); code != 200 || body != "你还没有加入公会" {
		t.Fatalf("without a guild: %d %q", code, body)
	}
	if _, err := s.Store.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "红包联盟", LeaderRID: role.RID}, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, "POST", ts.URL+"/api.php/pf/sygame/pay_web/?"+q); code != 200 || body != "88" {
		t.Fatalf("with a guild: %d %q", code, body)
	}
	if len(pushes) != 1 || !strings.Contains(pushes[0], "redbag=1") {
		t.Fatalf("pushes: %v", pushes)
	}
	if a, _ := s.Store.RoleAssets(role.RID); a[3] != 120 || a[10] != 550 || a[41] != 88 {
		t.Fatalf("assets: %v", a)
	}
}
