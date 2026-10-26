package game

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func keySet(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			out[prefix+k] = true
			keySet(c, prefix+k+".", out)
		}
	case []any:
		for _, c := range x {
			keySet(c, prefix+"[].", out)
		}
	}
}

// TestCaptureLoginShapes sends the requests of the login of a capture of the original server (testdata/captures/original-2026-10-26)
// to a new role and compares the answers: the names of the fields must be the same (the fields inside lists are left out, a new role
// has empty lists), and the countdowns the original gives as seconds left must be seconds left here too.
func TestCaptureLoginShapes(t *testing.T) {
	f, err := os.Open("../../testdata/captures/original-2026-10-26/login-main-city.jsonl")
	if err != nil {
		t.Skip("no capture")
	}
	defer f.Close()
	type row struct {
		Dir string         `json:"dir"`
		Cmd uint16         `json:"cmd"`
		Msg map[string]any `json:"msg"`
	}
	var rows []row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<26), 1<<26)
	for sc.Scan() {
		var r row
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			rows = append(rows, r)
		}
	}
	real := map[uint16]map[string]any{}
	for _, r := range rows {
		if r.Dir == "s2c" && real[r.Cmd] == nil && r.Msg != nil {
			real[r.Cmd] = r.Msg
		}
	}
	e := newEnv(t, 100*time.Hour)
	c, _ := e.enter("cap", "对比")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	seen := map[uint16]bool{}
	compared := 0
	for _, r := range rows {
		if r.Dir != "c2s" || seen[r.Cmd] || r.Cmd < 10300 || r.Cmd > 30000 {
			continue
		}
		seen[r.Cmd] = true
		if !e.gw.Router.Has(r.Cmd) || real[r.Cmd] == nil {
			continue
		}
		body := proto.Message{}
		for k, v := range r.Msg {
			switch x := v.(type) {
			case float64:
				body[k] = int64(x)
			case string:
				body[k] = x
			}
		}
		c.send(r.Cmd, body)
		got := c.waitFor(r.Cmd)
		compared++
		a, b := map[string]bool{}, map[string]bool{}
		keySet(map[string]any(got), "", a)
		keySet(real[r.Cmd], "", b)
		var diff []string
		for k := range b {
			if !a[k] && !strings.Contains(k, "[]") {
				diff = append(diff, "missing "+k)
			}
		}
		for k := range a {
			if !b[k] && !strings.Contains(k, "[]") {
				diff = append(diff, "extra "+k)
			}
		}
		sort.Strings(diff)
		if len(diff) > 0 {
			t.Errorf("%d: %s", r.Cmd, strings.Join(diff, ", "))
		}
		// countdowns: the original sends seconds left, which the client counts down itself
		for _, k := range []string{"state_time", "next_time"} {
			if rv, ok := real[r.Cmd][k].(float64); ok && rv < 1e9 && got.Int(k) >= 1e9 {
				t.Errorf("%d.%s must be seconds left, got %d", r.Cmd, k, got.Int(k))
			}
		}
		if rv, ok := real[r.Cmd]["end_time"].(float64); ok && rv < 1e9 && got.Int("end_time") >= 1e9 && (r.Cmd == 24905 || r.Cmd == 27220) {
			t.Errorf("%d.end_time must be seconds left, got %d", r.Cmd, got.Int("end_time"))
		}
	}
	if compared < 100 {
		t.Fatalf("only %d requests compared", compared)
	}
	if r := real[27902]; r["period"].(float64) != 0 {
		t.Fatalf("capture: %v", r)
	}
}
