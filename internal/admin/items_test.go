package admin

import (
	"strings"
	"testing"
)

func itemIDs(t *testing.T, res resp) []int64 {
	t.Helper()
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	var ids []int64
	for _, it := range res.JSON()["items"].([]any) {
		ids = append(ids, int64(it.(map[string]any)["base_id"].(float64)))
	}
	return ids
}

func TestSearchItems(t *testing.T) {
	e := newEnv(t)
	viewer, _, _ := e.tokens()

	// by name: 生命金卡 is 10034, and the entry says what the game does with it
	res := e.do(viewer, "GET", "/api/items?q=%E7%94%9F%E5%91%BD%E9%87%91%E5%8D%A1", nil) // 生命金卡
	ids := itemIDs(t, res)
	if len(ids) == 0 || ids[0] != 10034 {
		t.Fatalf("by name: %v", ids)
	}
	it := res.JSON()["items"].([]any)[0].(map[string]any)
	if it["name"] != "生命金卡" || it["sellable"] != true || it["in_backpack"] != true || it["currency"] != false {
		t.Fatalf("entry: %v", it)
	}

	// by id prefix: sorted, and capped by limit
	ids = itemIDs(t, e.do(viewer, "GET", "/api/items?q=1003&limit=3", nil))
	if len(ids) != 3 || ids[0] != 10030 {
		t.Fatalf("by id prefix: %v", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("not sorted: %v", ids)
		}
	}

	// currencies are flagged, equipment is not in the backpack
	for _, c := range []struct {
		q                  string
		currency, backpack bool
	}{{"1", true, false}, {"40112", false, false}} {
		got := e.do(viewer, "GET", "/api/items?q="+c.q+"&limit=1", nil).JSON()["items"].([]any)
		if len(got) != 1 {
			t.Fatalf("%s: %v", c.q, got)
		}
		m := got[0].(map[string]any)
		if m["currency"] != c.currency || m["in_backpack"] != c.backpack {
			t.Fatalf("%s: %v", c.q, m)
		}
	}

	// nothing to search for, nothing found, a bad limit falls back to the default
	for _, q := range []string{"", "%20", "no-such-item-name-xyz"} {
		if ids := itemIDs(t, e.do(viewer, "GET", "/api/items?q="+q, nil)); len(ids) != 0 {
			t.Fatalf("q=%q: %v", q, ids)
		}
	}
	if ids := itemIDs(t, e.do(viewer, "GET", "/api/items?q=1&limit=100000", nil)); len(ids) != 30 {
		t.Fatalf("limit out of range must fall back to 30: %d", len(ids))
	}
	// login is required
	if res := e.do("", "GET", "/api/items?q=1", nil); res.Status != 401 {
		t.Fatalf("no token: %d", res.Status)
	}
}

func TestMailItemAttachmentsAreChecked(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	mail := func(itemID int64) resp {
		return e.do(operator, "POST", "/api/mails", map[string]any{"scope": "direct", "role_ids": []int64{r.RID}, "subject": "s",
			"attachments": []map[string]any{{"kind": "item", "item_id": itemID, "num": 1}}})
	}
	if res := mail(10034); res.Status != 201 && res.Status != 200 {
		t.Fatalf("a known item: %d %s", res.Status, res.Body)
	}
	for _, c := range []struct {
		id   int64
		want string
	}{{99999999, "unknown item"}, {1, "is a currency"}, {3, "is a currency"}} {
		res := mail(c.id)
		if res.Status != 422 || !strings.Contains(res.Err(), c.want) {
			t.Fatalf("item %d: %d %q, want 422 containing %q", c.id, res.Status, res.Err(), c.want)
		}
	}
	// nothing was created by the refused mails
	if n := len(e.do(operator, "GET", "/api/mails", nil).JSON()["items"].([]any)); n != 1 {
		t.Fatalf("mails: %d", n)
	}
}

// A list that is empty must be [] and not null: the web pages call .map on it (the mail
// and notice pages broke on a fresh database).
func TestEmptyListsAreArraysNotNull(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	for _, path := range []string{"/api/servers", "/api/accounts", "/api/roles", "/api/notices", "/api/mails", "/api/audit?limit=1&offset=1000", "/api/admins", "/api/items?q=zzz"} {
		res := e.do(super, "GET", path, nil)
		if res.Status != 200 {
			t.Fatalf("%s: %d %s", path, res.Status, res.Body)
		}
		items, present := res.JSON()["items"]
		if _, isList := items.([]any); !present || !isList {
			t.Errorf("%s: items is %T (%v), want an array", path, items, items)
		}
	}
}

func TestMailListNamesTheItemsItCarries(t *testing.T) {
	e := newEnv(t)
	_, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	// no mails, no names: still an object, not null
	if got, ok := e.do(operator, "GET", "/api/mails", nil).JSON()["item_names"].(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("empty item_names: %v", got)
	}
	send := e.do(operator, "POST", "/api/mails", map[string]any{"scope": "direct", "role_ids": []int64{r.RID}, "subject": "s",
		"attachments": []map[string]any{{"kind": "item", "item_id": 10034, "num": 3}, {"kind": "asset", "asset": "gold", "num": 5}}})
	if send.Status != 201 && send.Status != 200 {
		t.Fatalf("%d %s", send.Status, send.Body)
	}
	names, _ := e.do(operator, "GET", "/api/mails", nil).JSON()["item_names"].(map[string]any)
	if len(names) != 1 || names["10034"] != "生命金卡" {
		t.Fatalf("item_names: %v", names)
	}
}

func TestBroadcast(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	body := func(msg string) map[string]any { return map[string]any{"msg": msg} }

	if res := e.do(viewer, "POST", "/api/broadcast", body("hi")); res.Status != 403 || len(e.gw.broadcasts) != 0 {
		t.Fatalf("a viewer must not broadcast: %d %v", res.Status, e.gw.broadcasts)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("字", 101), "a\x00b"} {
		if res := e.do(operator, "POST", "/api/broadcast", body(bad)); res.Status != 422 {
			t.Fatalf("msg %q: %d %s", bad, res.Status, res.Body)
		}
	}
	if len(e.gw.broadcasts) != 0 {
		t.Fatalf("a refused message reached the gateway: %v", e.gw.broadcasts)
	}

	// the text goes through URL encoding and arrives as it was, trimmed
	msg := "维护通知 & 补偿 100% 请查收?x=1"
	res := e.do(operator, "POST", "/api/broadcast", body("  "+msg+"  "))
	if res.Status != 200 || res.JSON()["pushed"].(float64) != 4 || len(e.gw.broadcasts) != 1 || e.gw.broadcasts[0] != msg {
		t.Fatalf("broadcast: %d %s %q", res.Status, res.Body, e.gw.broadcasts)
	}
	found := false
	for _, a := range e.auditActions() {
		found = found || a == "broadcast"
	}
	if !found {
		t.Fatalf("not audited: %v", e.auditActions())
	}
	// the gateway being down is an error here: the point of the call is the push
	e.gw.down = true
	if res := e.do(operator, "POST", "/api/broadcast", body("hi")); res.Status != 502 {
		t.Fatalf("gateway down: %d %s", res.Status, res.Body)
	}
}
