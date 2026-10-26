package admin

import (
	"fmt"
	"strings"
	"testing"
)

func TestActivitySchedule(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	spec := func() map[string]any {
		return map[string]any{"title": "累计召唤", "panel_type": 2, "sort_val": 1, "daily": false,
			"aims": []map[string]any{{"id": 1, "str": "召唤 10 次", "event": "evt_recruit", "target": 10, "rewards": [][]int{{3, 100}}}}}
	}
	good := func() map[string]any {
		return map[string]any{"srv_id": "symlf_1", "camp_id": 7001, "bid": 91001, "status": 1, "start_at": 100, "end_at": 0, "spec": spec()}
	}
	bad := map[string]func(m map[string]any){
		"no camp":        func(m map[string]any) { m["camp_id"] = 0 },
		"bad status":     func(m map[string]any) { m["status"] = 3 },
		"end before":     func(m map[string]any) { m["end_at"] = 50 },
		"unknown server": func(m map[string]any) { m["srv_id"] = "ghost_1" },
		"no spec":        func(m map[string]any) { delete(m, "spec") },
		"no title":       func(m map[string]any) { s := spec(); s["title"] = ""; m["spec"] = s },
		"no panel":       func(m map[string]any) { s := spec(); s["panel_type"] = 0; m["spec"] = s },
		"unknown field":  func(m map[string]any) { s := spec(); s["colour"] = "red"; m["spec"] = s },
		"dup aim":        func(m map[string]any) { s := spec(); a := s["aims"].([]map[string]any); s["aims"] = append(a, a[0]); m["spec"] = s },
		"unknown item": func(m map[string]any) {
			s := spec()
			s["aims"].([]map[string]any)[0]["rewards"] = [][]int{{99999999, 1}}
			m["spec"] = s
		},
		"no reward": func(m map[string]any) { s := spec(); s["aims"].([]map[string]any)[0]["rewards"] = [][]int{}; m["spec"] = s },
	}
	for name, mut := range bad {
		b := good()
		mut(b)
		if r := e.do(operator, "POST", "/api/activities", b); r.Status != 422 {
			t.Errorf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	if r := e.do(viewer, "POST", "/api/activities", good()); r.Status != 403 {
		t.Fatalf("viewer created: %d", r.Status)
	}
	r := e.do(operator, "POST", "/api/activities", good())
	if r.Status != 201 || r.JSON()["created_by"] != "operator1" || r.JSON()["spec"].(map[string]any)["title"] != "累计召唤" {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	id := int(r.JSON()["id"].(float64))
	b := good()
	b["status"] = 0
	b["spec"].(map[string]any)["title"] = strings.Repeat("长", 3)
	if r := e.do(operator, "PUT", fmt.Sprintf("/api/activities/%d", id), b); r.Status != 200 || r.JSON()["status"].(float64) != 0 {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	if r := e.do(operator, "PUT", "/api/activities/999", good()); r.Status != 404 {
		t.Fatalf("update missing: %d", r.Status)
	}
	if l := e.do(viewer, "GET", "/api/activities?srv_id=symlf_1", nil).JSON(); l["total"].(float64) != 1 || len(l["items"].([]any)) != 1 {
		t.Fatalf("list: %v", l)
	}
	if l := e.do(viewer, "GET", "/api/activities?srv_id=other_1", nil).JSON(); l["total"].(float64) != 0 {
		t.Fatalf("list of another server: %v", l)
	}
	if r := e.do(operator, "DELETE", fmt.Sprintf("/api/activities/%d", id), nil); r.Status != 200 {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := e.do(operator, "DELETE", fmt.Sprintf("/api/activities/%d", id), nil); r.Status != 404 {
		t.Fatalf("delete twice: %d", r.Status)
	}
}
