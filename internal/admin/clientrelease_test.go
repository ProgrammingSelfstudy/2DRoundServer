package admin

import (
	"strings"
	"testing"
)

// The gateway's WebSocket address is a client setting of its platform (the login service hands it to the client in /client/config):
// only a ws:// or wss:// address is taken, an empty value removes it, and a key nothing reads is refused.
func TestClientSettingWSURL(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	set := func(token, key, value string) int {
		return e.do(token, "PUT", "/api/client/config", map[string]any{"platform": "h5", "key": key, "value": value}).Status
	}
	if code := set(operator, "ws_url", "wss://gw.example.com/ws"); code != 403 {
		t.Fatalf("operator sets ws_url: %d", code)
	}
	for _, bad := range []string{"https://gw.example.com/ws", "gw.example.com/ws", "wss://", "wss://gw.example.com/a b"} {
		if code := set(super, "ws_url", bad); code != 422 {
			t.Fatalf("%q: %d", bad, code)
		}
	}
	if code := set(super, "ws_addr", "wss://gw.example.com/ws"); code != 422 {
		t.Fatalf("unknown key: %d", code)
	}
	for _, good := range []string{"ws://192.168.1.5:8802/ws", "wss://gw.example.com/ws"} {
		if code := set(super, "ws_url", good); code != 200 {
			t.Fatalf("%q: %d", good, code)
		}
	}
	if res := e.do(viewer, "GET", "/api/client?platform=h5", nil); res.Status != 200 || !strings.Contains(string(res.Body), `"ws_url":"wss://gw.example.com/ws"`) {
		t.Fatalf("read back: %d %s", res.Status, res.Body)
	}
	if code := set(super, "ws_url", ""); code != 200 {
		t.Fatalf("clear: %d", code)
	}
	if res := e.do(viewer, "GET", "/api/client?platform=h5", nil); strings.Contains(string(res.Body), "ws_url") {
		t.Fatalf("still set after clearing: %s", res.Body)
	}
}
