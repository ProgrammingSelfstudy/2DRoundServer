package admin

import "testing"

func TestAnalyticsEndpoint(t *testing.T) {
	e := newEnv(t)
	viewer, _, _ := e.tokens()
	if r := e.do("", "GET", "/api/analytics", nil); r.Status != 401 {
		t.Fatalf("without a token: %d", r.Status)
	}
	j := e.do(viewer, "GET", "/api/analytics", nil).JSON()
	if days, _ := j["days"].([]any); len(days) != 14 {
		t.Fatalf("default window: %v", j["days"])
	}
	if days, _ := e.do(viewer, "GET", "/api/analytics?days=3", nil).JSON()["days"].([]any); len(days) != 3 {
		t.Fatalf("days=3 gave %d days", len(days))
	}
	for _, bad := range []string{"0", "91", "-1", "x"} {
		if r := e.do(viewer, "GET", "/api/analytics?days="+bad, nil); r.Status != 400 {
			t.Fatalf("days=%s: %d, want 400", bad, r.Status)
		}
	}
}
