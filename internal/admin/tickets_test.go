package admin

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/store"
)

func TestAdminTickets(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("p1", "symlf_1", "甲")
	id, err := e.st.FileTicket(store.Ticket{RID: role.RID, SrvID: "symlf_1", Title: "卡了"}, "进不去", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/tickets/" + strconv.FormatInt(id, 10) + "/reply"
	if res := e.do(viewer, "POST", path, map[string]any{"content": "好的"}); res.Status != 403 {
		t.Fatalf("viewer replies: %d", res.Status)
	}
	if res := e.do(operator, "POST", path, map[string]any{"content": "  "}); res.Status != 422 {
		t.Fatalf("blank: %d", res.Status)
	}
	if res := e.do(operator, "POST", "/api/tickets/999/reply", map[string]any{"content": "好的"}); res.Status != 404 {
		t.Fatalf("unknown: %d", res.Status)
	}
	if res := e.do(operator, "POST", path, map[string]any{"content": "请更新"}); res.Status != 200 {
		t.Fatalf("reply: %d %s", res.Status, res.Body)
	}
	res := e.do(viewer, "GET", "/api/tickets", nil)
	if res.Status != 200 || !strings.Contains(string(res.Body), "请更新") || !strings.Contains(string(res.Body), "进不去") {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	if tk, _ := e.st.TicketByID(id); tk.State != store.TicketAnswered {
		t.Fatalf("state %d", tk.State)
	}
}
