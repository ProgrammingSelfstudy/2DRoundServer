package admin

import (
	"fmt"
	"testing"
)

func TestNoticeChangesAreRelayedToTheGateway(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.gw.pushed = 2
	body := map[string]any{"title": "维护公告", "content": "今晚维护", "status": 1}

	r := e.do(operator, "POST", "/api/notices", body)
	if r.Status != 201 {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	j := r.JSON()
	id := int(j["id"].(float64))
	// the notice's own fields stay at the top level and the push result sits beside them
	if j["title"] != "维护公告" || j["created_by"] != "operator1" || j["pushed"].(float64) != 2 || j["push_error"] != nil {
		t.Fatalf("create reply: %v", j)
	}
	if len(e.gw.noticePushes) != 1 || e.gw.noticePushes[0] != fmt.Sprint(id) {
		t.Fatalf("gateway pushes: %v", e.gw.noticePushes)
	}

	body["title"], body["status"] = "改过了", 0 // hiding goes through the same push; the gateway decides show or drop
	r = e.do(operator, "PUT", fmt.Sprintf("/api/notices/%d", id), body)
	if r.Status != 200 || r.JSON()["title"] != "改过了" || r.JSON()["pushed"].(float64) != 2 {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	if len(e.gw.noticePushes) != 2 {
		t.Fatalf("update was not pushed: %v", e.gw.noticePushes)
	}

	r = e.do(operator, "DELETE", fmt.Sprintf("/api/notices/%d", id), nil)
	if r.Status != 200 || r.JSON()["ok"] != true || r.JSON()["pushed"].(float64) != 3 { // the stub says it dropped 3
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	if len(e.gw.noticeDrops) != 1 || e.gw.noticeDrops[0] != fmt.Sprint(id) {
		t.Fatalf("remove pushes: %v", e.gw.noticeDrops)
	}
}

func TestNoticeSavedEvenWhenTheGatewayIsDown(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.gw.down = true
	r := e.do(operator, "POST", "/api/notices", map[string]any{"title": "t", "content": "c", "status": 1})
	j := r.JSON()
	if r.Status != 201 || j["pushed"] != nil || j["push_error"] == nil || j["id"] == nil {
		t.Fatalf("create with the gateway down: %d %s", r.Status, r.Body)
	}
	id := int(j["id"].(float64))
	if l := e.do(operator, "GET", "/api/notices", nil).JSON(); l["total"].(float64) != 1 {
		t.Fatalf("not stored: %v", l)
	}
	r = e.do(operator, "DELETE", fmt.Sprintf("/api/notices/%d", id), nil)
	if r.Status != 200 || r.JSON()["push_error"] == nil || r.JSON()["pushed"] != nil {
		t.Fatalf("delete with the gateway down: %d %s", r.Status, r.Body)
	}
	if l := e.do(operator, "GET", "/api/notices", nil).JSON(); l["total"].(float64) != 0 {
		t.Fatalf("not deleted: %v", l)
	}
	// a refused request must not reach the gateway
	e.gw.down = false
	before := len(e.gw.noticePushes)
	e.do(operator, "POST", "/api/notices", map[string]any{"title": "", "content": "c", "status": 1})
	e.do(operator, "PUT", "/api/notices/9999", map[string]any{"title": "t", "content": "c", "status": 1})
	e.do(operator, "DELETE", "/api/notices/9999", nil)
	if len(e.gw.noticePushes) != before || len(e.gw.noticeDrops) != 0 {
		t.Fatalf("failed requests were relayed: %v %v", e.gw.noticePushes, e.gw.noticeDrops)
	}
}
