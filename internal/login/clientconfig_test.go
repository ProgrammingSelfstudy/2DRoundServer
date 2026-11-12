package login

import (
	"encoding/json"
	"testing"

	"2droundserver/internal/store"
)

func TestClientConfigServesLiveRelease(t *testing.T) {
	s, ts := newTestServer(t)
	if code, _ := get(t, "GET", ts.URL+"/client/config"); code != 400 {
		t.Fatalf("a missing platform must be 400, got %d", code)
	}
	s.Store.SetClientConfig("*", "cdn_base", "https://cdn.example/res")
	s.Store.SetClientConfig("wechat", "switch.pay", "off")
	s.Store.SetClientConfig("h5", "ws_url", "wss://gw.example/ws")
	s.Store.SetClientConfig("wechat", "min_version", "1.2.10")
	rel, err := s.Store.CreateClientRelease(store.ClientRelease{Platform: "wechat", Version: "7", ManifestURL: "https://cdn.example/m7.json", Whitelist: "tester1"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.SetClientReleaseStatus(rel.ID, store.ReleasePublished, 200)

	read := func(query string) map[string]any {
		_, body := get(t, "GET", ts.URL+"/client/config?"+query)
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return m
	}
	m := read("platform=wechat&ver=1.2.9")
	if m["cdn_base"] != "https://cdn.example/res" || m["force_update"] != true || m["switches"].(map[string]any)["pay"] != "off" {
		t.Fatalf("%v", m)
	}
	if m["release"] != nil {
		t.Fatalf("a whitelisted release must be hidden from others: %v", m["release"])
	}
	// the gateway address is a setting of its platform: H5 gets it, a platform without one gets "" and works it out itself
	if h5 := read("platform=h5"); h5["ws_url"] != "wss://gw.example/ws" || m["ws_url"] != "" {
		t.Fatalf("ws_url: h5 %v, wechat %v", h5["ws_url"], m["ws_url"])
	}
	m = read("platform=wechat&ver=1.2.10&account=tester1")
	if m["force_update"] != false || m["release"].(map[string]any)["version"] != "7" {
		t.Fatalf("%v", m)
	}
	s.Store.SetClientReleaseStatus(rel.ID, store.ReleaseRolledBack, 300)
	if m = read("platform=wechat&account=tester1"); m["release"] != nil {
		t.Fatalf("a rolled back release must not be served: %v", m["release"])
	}
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.2.9", "1.2.10", true}, {"1.2.10", "1.2.9", false}, {"", "1.0", true}, {"2", "2.0.0", false}} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q,%q)=%v", c.a, c.b, got)
		}
	}
}
