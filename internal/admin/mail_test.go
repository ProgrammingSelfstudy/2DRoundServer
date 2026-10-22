package admin

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/store"
)

func TestSendDirectMail(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r1 := e.seedRole("a", "symlf_1", "甲")
	r2 := e.seedRole("b", "symlf_1", "乙")
	e.gw.pushed = 1

	body := map[string]any{
		"scope": "direct", "role_ids": []int64{r1.RID, r2.RID, r1.RID}, // a duplicate id is harmless
		"subject": "奖励", "content": "请领取", "from_name": "客服", "type": 1, "expire_days": 7,
		"attachments": []map[string]any{{"kind": "asset", "asset": "gold", "num": 100}, {"kind": "asset", "asset": "coin", "num": 5000}, {"kind": "item", "item_id": 40112, "num": 2}},
	}
	r := e.do(operator, "POST", "/api/mails", body)
	if r.Status != 201 {
		t.Fatalf("send: %d %s", r.Status, r.Body)
	}
	j := r.JSON()
	mail := j["mail"].(map[string]any)
	if j["pushed"].(float64) != 1 || j["push_error"] != nil || mail["created_by"] != "operator1" || mail["scope"] != "direct" {
		t.Fatalf("response: %v", j)
	}
	if want := e.clock.Load()/1e9 + 7*86400; int64(mail["expire_at"].(float64)) != want {
		t.Fatalf("expire_at %v, want %d", mail["expire_at"], want)
	}
	atts := mail["attachments"].([]any)
	if len(atts) != 3 || atts[0].(map[string]any)["item_id"].(float64) != 3 || atts[1].(map[string]any)["item_id"].(float64) != 1 {
		t.Fatalf("assets must be stored by label (gold=3, coin=1): %v", atts)
	}
	if len(e.gw.pushes) != 1 || e.gw.pushes[0] != fmt.Sprint(int64(mail["id"].(float64))) {
		t.Fatalf("gateway push: %v", e.gw.pushes)
	}
	rows, _ := e.st.MailsForRole(r1)
	if len(rows) != 1 || rows[0].Subject != "奖励" || len(rows[0].Attachments) != 3 {
		t.Fatalf("role 1 mail: %+v", rows)
	}
	list := e.do(operator, "GET", "/api/mails", nil).JSON()
	if list["total"].(float64) != 1 || list["items"].([]any)[0].(map[string]any)["recipients"].(float64) != 2 {
		t.Fatalf("sent list: %v", list)
	}
}

func TestSendBroadcastMailAndGatewayDown(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.do(operator, "POST", "/api/mails", map[string]any{"scope": "broadcast", "srv_id": "symlf_1", "subject": "公告", "content": "x"})
	if r.Status != 201 || r.JSON()["mail"].(map[string]any)["srv_id"] != "symlf_1" {
		t.Fatalf("broadcast: %d %s", r.Status, r.Body)
	}
	e.gw.down = true
	r = e.do(operator, "POST", "/api/mails", map[string]any{"scope": "broadcast", "subject": "留存"})
	j := r.JSON()
	if r.Status != 201 || j["pushed"] != nil || j["push_error"] == nil {
		t.Fatalf("with the gateway down the mail is stored and the failure reported: %d %s", r.Status, r.Body)
	}
	if list := e.do(operator, "GET", "/api/mails", nil).JSON(); list["total"].(float64) != 2 {
		t.Fatalf("both mails are stored: %v", list)
	}
}

func TestSendMailValidation(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("a", "symlf_1", "甲")
	base := func() map[string]any {
		return map[string]any{"scope": "direct", "role_ids": []int64{role.RID}, "subject": "s", "content": "c"}
	}
	att := func(a ...map[string]any) []map[string]any { return a }
	cases := map[string]func(m map[string]any){
		"no scope":              func(m map[string]any) { delete(m, "scope") },
		"bad scope":             func(m map[string]any) { m["scope"] = "everyone" },
		"direct without roles":  func(m map[string]any) { m["role_ids"] = []int64{} },
		"direct unknown role":   func(m map[string]any) { m["role_ids"] = []int64{role.RID, 99999} },
		"too many roles":        func(m map[string]any) { ids := make([]int64, maxRecipients+1); m["role_ids"] = ids },
		"broadcast with roles":  func(m map[string]any) { m["scope"] = "broadcast" },
		"broadcast unknown srv": func(m map[string]any) { m["scope"] = "broadcast"; m["role_ids"] = []int64{}; m["srv_id"] = "ghost_1" },
		"empty subject":         func(m map[string]any) { m["subject"] = " " },
		"long subject":          func(m map[string]any) { m["subject"] = strings.Repeat("长", 61) },
		"long content":          func(m map[string]any) { m["content"] = strings.Repeat("长", 2001) },
		"bad type":              func(m map[string]any) { m["type"] = 2 },
		"long sender":           func(m map[string]any) { m["from_name"] = strings.Repeat("长", 21) },
		"both expiries":         func(m map[string]any) { m["expire_days"] = 3; m["expire_at"] = e.clock.Load()/1e9 + 99999 },
		"expiry in the past":    func(m map[string]any) { m["expire_at"] = e.clock.Load()/1e9 - 5 },
		"expire_days too large": func(m map[string]any) { m["expire_days"] = 400 },
		"negative expire_days":  func(m map[string]any) { m["expire_days"] = -1 },
		"unknown asset name": func(m map[string]any) {
			m["attachments"] = att(map[string]any{"kind": "asset", "asset": "diamonds", "num": 1})
		},
		"unknown asset label": func(m map[string]any) {
			m["attachments"] = att(map[string]any{"kind": "asset", "item_id": 9999, "num": 1})
		},
		"zero amount": func(m map[string]any) {
			m["attachments"] = att(map[string]any{"kind": "asset", "asset": "gold", "num": 0})
		},
		"huge amount": func(m map[string]any) {
			m["attachments"] = att(map[string]any{"kind": "asset", "asset": "gold", "num": maxAmount + 1})
		},
		"item without id":     func(m map[string]any) { m["attachments"] = att(map[string]any{"kind": "item", "num": 1}) },
		"bad attachment kind": func(m map[string]any) { m["attachments"] = att(map[string]any{"kind": "pet", "item_id": 1, "num": 1}) },
		"too many attachments": func(m map[string]any) {
			var many []map[string]any
			for i := 0; i < maxAttachments+1; i++ {
				many = append(many, map[string]any{"kind": "item", "item_id": 1, "num": 1})
			}
			m["attachments"] = many
		},
	}
	for name, mut := range cases {
		b := base()
		mut(b)
		if r := e.do(operator, "POST", "/api/mails", b); r.Status != 422 {
			t.Errorf("%s: got %d (%s), want 422", name, r.Status, r.Body)
		}
	}
	if total := e.do(operator, "GET", "/api/mails", nil).JSON()["total"].(float64); total != 0 {
		t.Fatalf("a rejected request must not store anything, %v mails exist", total)
	}
	// the error of a missing role says which one
	b := base()
	b["role_ids"] = []int64{role.RID, 4242}
	if r := e.do(operator, "POST", "/api/mails", b); !strings.Contains(r.Err(), "4242") {
		t.Fatalf("the message must name the missing role: %s", r.Err())
	}
	// the same request with the faults removed works
	if r := e.do(operator, "POST", "/api/mails", base()); r.Status != 201 {
		t.Fatalf("a valid request: %d %s", r.Status, r.Body)
	}
}

func TestAuditTrail(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	e.do(operator, "POST", "/api/mails", map[string]any{"scope": "broadcast", "subject": "审计"})
	e.do(operator, "POST", "/api/notices", map[string]any{"title": "t", "content": "c", "status": 1})
	e.do(viewer, "POST", "/api/mails", map[string]any{"scope": "broadcast", "subject": "denied"}) // refused: no entry

	r := e.do(super, "GET", "/api/audit", nil)
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var mine []map[string]any
	for _, it := range r.JSON()["items"].([]any) {
		m := it.(map[string]any)
		mine = append(mine, m)
		if m["ip"] == "" || m["created_at"].(float64) == 0 || m["admin_name"] == "" {
			t.Errorf("incomplete entry: %v", m)
		}
	}
	acts := e.auditActions()
	for _, want := range []string{"admin.login", "mail.send", "notice.create"} {
		if !has(acts, want) {
			t.Errorf("audit lacks %s: %v", want, acts)
		}
	}
	for _, m := range mine {
		if m["action"] == "mail.send" && m["admin_name"] != "operator1" {
			t.Errorf("mail.send recorded under %v", m["admin_name"])
		}
		if m["target"] == "denied" || strings.Contains(fmt.Sprint(m["detail"]), "denied") {
			t.Errorf("a refused request was audited: %v", m)
		}
	}
	// paging is bounded
	r = e.do(super, "GET", "/api/audit?limit=100000", nil)
	if n := len(r.JSON()["items"].([]any)); n > 200 {
		t.Fatalf("limit not capped: %d", n)
	}
	if r := e.do(super, "GET", "/api/audit?limit=1&offset=1", nil); len(r.JSON()["items"].([]any)) != 1 {
		t.Fatalf("paging: %s", r.Body)
	}
	if r := e.do(super, "GET", "/api/audit?limit=-5&offset=-9", nil); r.Status != 200 {
		t.Fatalf("negative paging values: %d", r.Status)
	}
}

func TestOnlineAndMeta(t *testing.T) {
	e := newEnv(t)
	viewer, _, _ := e.tokens()
	if j := e.do(viewer, "GET", "/api/online", nil).JSON(); j["online"].(float64) != 7 {
		t.Fatalf("online: %v", j)
	}
	e.gw.down = true
	j := e.do(viewer, "GET", "/api/online", nil)
	if j.Status != 200 || j.JSON()["online"] != nil || j.JSON()["error"] == nil {
		t.Fatalf("a gateway that is down is reported, not a failure: %d %s", j.Status, j.Body)
	}
	meta := e.do(viewer, "GET", "/api/meta", nil).JSON()
	found := false
	for _, a := range meta["assets"].([]any) {
		m := a.(map[string]any)
		if m["name"] == "gold" && m["label"].(float64) == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("meta lacks gold=3: %v", meta)
	}
	st := e.do(viewer, "GET", "/api/stats", nil).JSON()
	if st["servers"] == nil || st["roles"] == nil {
		t.Fatalf("stats: %v", st)
	}
	_ = store.MailDirect
	_ = time.Second
}
