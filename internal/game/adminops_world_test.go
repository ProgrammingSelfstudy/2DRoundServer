package game

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestWorldLevelGatewayOverridePushLoginAndExpiry(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	h := opsTestHandler(e)
	c, role := e.enter("world-level", "世界等级")
	chargeBarrier(c)
	if err := e.st.SetRoleLevel(role.RID, 35); err != nil {
		t.Fatal(err)
	}
	put := func(value int, until int64) {
		t.Helper()
		w := opsRequest(h, "PUT", "/ops/worldlev?srv=symlf_1", AdminWorldLevelRequest{AdminOperation: AdminOperation{Reason: "世界等级回归", AdminID: 1, AdminName: "super"}, Override: value, Until: until})
		if w.Code != 200 {
			t.Fatalf("PUT: %d %s", w.Code, w.Body)
		}
		var out struct {
			World  store.WorldLevelInfo `json:"world_level"`
			Pushed int                  `json:"pushed"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Pushed != 1 {
			t.Fatalf("push count: %s", w.Body)
		}
		if c.waitFor(10905).Int("world_lev") != int64(out.World.Effective) {
			t.Fatal("PUT push disagrees with response")
		}
	}
	until := actNow.Add(time.Minute).Unix()
	put(88, until)
	c.send(10905, proto.Message{})
	if m := c.waitFor(10905); m.Int("world_lev") != 88 {
		t.Fatal("10905 ignored active override")
	}
	before, _, _ := e.st.GlobalModule(role.SrvID, store.WorldLevelModule)
	for _, path := range []string{"/ops/worldlev?srv=symlf_1", "/ops/global?srv=symlf_1&key=worldlev", "/ops/overview?srv=symlf_1"} {
		w := opsRequest(h, "GET", path, nil)
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
	}
	// Re-enter the same real TCP role: login (10192) must use the same override.
	_, token := e.account("world-level")
	c.send(1164, loginArgs("world-level", token))
	c.waitFor(1164)
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if m := c.waitFor(10192); m.Int("world_lev") != 88 {
		t.Fatalf("login ignored override: %v", m)
	}
	chargeBarrier(c)
	e.setNow(actNow.Add(time.Minute))
	c.send(10905, proto.Message{})
	if m := c.waitFor(10905); m.Int("world_lev") != 35 {
		t.Fatalf("expired 10905: %v", m)
	}
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": role.SrvID})
	if m := c.waitFor(10192); m.Int("world_lev") != 35 {
		t.Fatalf("expired login: %v", m)
	}
	chargeBarrier(c)
	after, _, _ := e.st.GlobalModule(role.SrvID, store.WorldLevelModule)
	if !bytes.Equal(before, after) {
		t.Fatal("GET/login/10905 expiry rewrote configuration")
	}
	w := opsRequest(h, "GET", "/ops/worldlev?srv=symlf_1", nil)
	var info store.WorldLevelInfo
	json.Unmarshal(w.Body.Bytes(), &info)
	if info.Active || info.Computed != 35 || info.Effective != 35 || info.Override != 88 {
		t.Fatalf("expired info: %s", w.Body)
	}
	put(99, 0)
	put(0, 0)
	logs, _, _ := e.st.ListAudit(100, 0)
	count := 0
	for _, l := range logs {
		if l.Action == "server.worldlev" {
			count++
			if !bytes.Contains([]byte(l.DetailJSON), []byte("before")) || !bytes.Contains([]byte(l.DetailJSON), []byte("reason")) {
				t.Fatal("audit before/after/reason missing")
			}
		}
	}
	if count != 3 {
		t.Fatalf("audit count %d", count)
	}
	for _, body := range []AdminWorldLevelRequest{{AdminOperation: AdminOperation{Reason: "invalid"}, Override: -1}, {AdminOperation: AdminOperation{Reason: "invalid"}, Override: 201}, {AdminOperation: AdminOperation{Reason: "invalid"}, Override: 80, Until: -1}, {Override: 80}} {
		old, _, _ := e.st.GlobalModule(role.SrvID, store.WorldLevelModule)
		w := opsRequest(h, "PUT", "/ops/worldlev?srv=symlf_1", body)
		saved, _, _ := e.st.GlobalModule(role.SrvID, store.WorldLevelModule)
		if w.Code != 422 || !bytes.Equal(old, saved) {
			t.Fatalf("invalid PUT changed state: %d", w.Code)
		}
	}
}
