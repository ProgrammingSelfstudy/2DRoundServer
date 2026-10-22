package admin

import (
	"fmt"
	"testing"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// stock puts items and currencies in a role's ledgers the way the game does: a
// mail the role claims.
func (e *env) stock(rid int64, atts ...store.Attachment) {
	e.t.Helper()
	m, err := e.st.CreateMail(store.Mail{Subject: "s", Attachments: atts}, []int64{rid})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.st.ClaimMail(rid, m.ID); err != nil {
		e.t.Fatal(err)
	}
}

func TestRoleDetail(t *testing.T) {
	e := newEnv(t)
	viewer, _, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	e.stock(r.RID,
		store.Attachment{Kind: "asset", ItemID: 3, Num: 120}, store.Attachment{Kind: "asset", ItemID: 1, Num: 5000},
		store.Attachment{Kind: "item", ItemID: 10034, Num: 3}, store.Attachment{Kind: "item", ItemID: 40112, Num: 1})
	e.gw.onlineRID = fmt.Sprint(r.RID)

	got := e.do(viewer, "GET", fmt.Sprintf("/api/roles/%d", r.RID), nil)
	if got.Status != 200 {
		t.Fatalf("%d %s", got.Status, got.Body)
	}
	j := got.JSON()
	role := j["role"].(map[string]any)
	if role["name"] != "路飞" || j["account"] != "p1" || j["online"] != true || j["banned"] != false {
		t.Fatalf("detail: %v", j)
	}
	assets := j["assets"].([]any)
	if len(assets) != 2 {
		t.Fatalf("assets: %v", assets)
	}
	if a := assets[0].(map[string]any); a["label"].(float64) != 1 || a["name"] != "coin" || a["val"].(float64) != 5000 {
		t.Fatalf("assets are sorted by label with names: %v", assets)
	}
	items := j["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items: %v", items)
	}
	// 10034 is listed in the game's backpack, 40112 (equipment) is not
	if i := items[0].(map[string]any); i["base_id"].(float64) != 10034 || i["name"] != "生命金卡" || i["num"].(float64) != 3 || i["listed"] != true {
		t.Fatalf("item 0: %v", i)
	}
	if i := items[1].(map[string]any); i["base_id"].(float64) != 40112 || i["name"] != "大关刀" || i["listed"] != false {
		t.Fatalf("item 1: %v", i)
	}

	// offline: the stub reports a different role as online
	e.gw.onlineRID = "999"
	if j := e.do(viewer, "GET", fmt.Sprintf("/api/roles/%d", r.RID), nil).JSON(); j["online"] != false {
		t.Fatalf("offline: %v", j)
	}
	// gateway down: the detail still comes, with online null and the reason
	e.gw.down = true
	if res := e.do(viewer, "GET", fmt.Sprintf("/api/roles/%d", r.RID), nil); res.Status != 200 || res.JSON()["online"] != nil || res.JSON()["online_error"] == nil {
		t.Fatalf("gateway down: %d %s", res.Status, res.Body)
	}
	e.gw.down = false

	if res := e.do(viewer, "GET", "/api/roles/999999", nil); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}
	if res := e.do(viewer, "GET", "/api/roles/abc", nil); res.Status != 400 {
		t.Fatalf("bad id: %d", res.Status)
	}
	if res := e.do("", "GET", fmt.Sprintf("/api/roles/%d", r.RID), nil); res.Status != 401 {
		t.Fatalf("no token: %d", res.Status)
	}

	// an empty role has empty lists, not null
	r2 := e.seedRole("p2", "symlf_1", "索隆")
	j = e.do(viewer, "GET", fmt.Sprintf("/api/roles/%d", r2.RID), nil).JSON()
	if a, ok := j["assets"].([]any); !ok || len(a) != 0 {
		t.Fatalf("assets: %v", j["assets"])
	}
	if a, ok := j["items"].([]any); !ok || len(a) != 0 {
		t.Fatalf("items: %v", j["items"])
	}
}

func TestKickRole(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	path := fmt.Sprintf("/api/roles/%d/kick", r.RID)

	if res := e.do(viewer, "POST", path, nil); res.Status != 403 {
		t.Fatalf("a viewer must not kick: %d", res.Status)
	}
	if len(e.gw.kicks) != 0 {
		t.Fatalf("refused call reached the gateway: %v", e.gw.kicks)
	}
	res := e.do(operator, "POST", path, nil)
	if res.Status != 200 || res.JSON()["kicked"] != true || len(e.gw.kicks) != 1 || e.gw.kicks[0] != fmt.Sprint(r.RID) {
		t.Fatalf("kick: %d %s %v", res.Status, res.Body, e.gw.kicks)
	}
	// kicking is not banning
	if got, _ := e.st.RoleByID(r.RID); got.Status != 1 {
		t.Fatalf("kick banned the role: %+v", got)
	}
	// an offline role: the call works and says nobody was kicked
	e.gw.kickedAnyone = false
	if res := e.do(operator, "POST", path, nil); res.Status != 200 || res.JSON()["kicked"] != false {
		t.Fatalf("offline kick: %d %s", res.Status, res.Body)
	}
	if res := e.do(operator, "POST", "/api/roles/999999/kick", nil); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}
	// the gateway being down is an error here: the point of the call is the kick
	e.gw.down = true
	if res := e.do(operator, "POST", path, nil); res.Status != 502 {
		t.Fatalf("gateway down: %d %s", res.Status, res.Body)
	}
	acts := e.auditActions()
	n := 0
	for _, a := range acts {
		if a == "role.kick" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("two successful kicks must be audited, got %d in %v", n, acts)
	}
}

func TestSetRoleLevel(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	path := fmt.Sprintf("/api/roles/%d/level", r.RID)

	if res := e.do(viewer, "POST", path, map[string]any{"lev": 30}); res.Status != 403 {
		t.Fatalf("a viewer must not set levels: %d", res.Status)
	}
	if got, _ := e.st.RoleByID(r.RID); got.Lev != 1 {
		t.Fatalf("a refused call changed the level: %d", got.Lev)
	}
	for _, lev := range []int{0, -3, game.MaxRoleLevel() + 1} {
		if res := e.do(operator, "POST", path, map[string]any{"lev": lev}); res.Status != 422 {
			t.Fatalf("lev %d: %d %s", lev, res.Status, res.Body)
		}
	}
	if res := e.do(operator, "POST", "/api/roles/999999/level", map[string]any{"lev": 5}); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}

	// without kick: the level changes, nobody is disconnected
	res := e.do(operator, "POST", path, map[string]any{"lev": 30})
	if res.Status != 200 || res.JSON()["lev"].(float64) != 30 || res.JSON()["was"].(float64) != 1 {
		t.Fatalf("set: %d %s", res.Status, res.Body)
	}
	if got, _ := e.st.RoleByID(r.RID); got.Lev != 30 || len(e.gw.kicks) != 0 {
		t.Fatalf("level %d, kicks %v", got.Lev, e.gw.kicks)
	}
	// the top level is allowed, and with kick the role is disconnected so it sees the change
	res = e.do(operator, "POST", path, map[string]any{"lev": game.MaxRoleLevel(), "kick": true})
	if res.Status != 200 || len(e.gw.kicks) != 1 || e.gw.kicks[0] != fmt.Sprint(r.RID) {
		t.Fatalf("with kick: %d %s %v", res.Status, res.Body, e.gw.kicks)
	}
	n := 0
	for _, a := range e.auditActions() {
		if a == "role.level" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("both changes must be audited, got %d: %v", n, e.auditActions())
	}
}

func TestSetRoleVIP(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	path := fmt.Sprintf("/api/roles/%d/vip", r.RID)

	if res := e.do(viewer, "POST", path, map[string]any{"vip_lev": 5}); res.Status != 403 {
		t.Fatalf("a viewer must not set VIP: %d", res.Status)
	}
	if got, _ := e.st.RoleByID(r.RID); got.VIPLev != 0 {
		t.Fatalf("a refused call changed the VIP level: %d", got.VIPLev)
	}
	for _, v := range []int{-1, game.MaxVIPLevel + 1} {
		if res := e.do(operator, "POST", path, map[string]any{"vip_lev": v}); res.Status != 422 {
			t.Fatalf("vip %d: %d %s", v, res.Status, res.Body)
		}
	}
	if res := e.do(operator, "POST", "/api/roles/999999/vip", map[string]any{"vip_lev": 5}); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}
	res := e.do(operator, "POST", path, map[string]any{"vip_lev": 5})
	if res.Status != 200 || res.JSON()["vip_lev"].(float64) != 5 || res.JSON()["was"].(float64) != 0 {
		t.Fatalf("set: %d %s", res.Status, res.Body)
	}
	if got, _ := e.st.RoleByID(r.RID); got.VIPLev != 5 || len(e.gw.kicks) != 0 {
		t.Fatalf("vip %d, kicks %v", got.VIPLev, e.gw.kicks)
	}
	// back to none, with a kick so the role sees it
	res = e.do(operator, "POST", path, map[string]any{"vip_lev": 0, "kick": true})
	if res.Status != 200 || len(e.gw.kicks) != 1 || e.gw.kicks[0] != fmt.Sprint(r.RID) {
		t.Fatalf("with kick: %d %s %v", res.Status, res.Body, e.gw.kicks)
	}
	n := 0
	for _, a := range e.auditActions() {
		if a == "role.vip" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("both changes must be audited, got %d: %v", n, e.auditActions())
	}
	// the detail shows it
	d := e.do(operator, "GET", fmt.Sprintf("/api/roles/%d", r.RID), nil)
	if d.Status != 200 {
		t.Fatalf("detail: %d", d.Status)
	}
}

func TestGrantRole(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	path := fmt.Sprintf("/api/roles/%d/grant", r.RID)
	body := func(g ...map[string]any) map[string]any { return map[string]any{"grants": g} }
	gold := map[string]any{"kind": "asset", "asset": "gold", "num": 500}
	item := map[string]any{"kind": "item", "item_id": 10401, "num": 3}

	if res := e.do(viewer, "POST", path, body(gold)); res.Status != 403 {
		t.Fatalf("a viewer must not grant: %d", res.Status)
	}
	bad := map[string]map[string]any{
		"nothing":       {"grants": []any{}},
		"zero":          body(map[string]any{"kind": "asset", "asset": "gold", "num": 0}),
		"unknown asset": body(map[string]any{"kind": "asset", "asset": "nope", "num": 1}),
		"unknown item":  body(map[string]any{"kind": "item", "item_id": 99999999, "num": 1}),
		"bad kind":      body(map[string]any{"kind": "hero", "item_id": 1, "num": 1}),
	}
	for name, b := range bad {
		if res := e.do(operator, "POST", path, b); res.Status != 422 {
			t.Fatalf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	if res := e.do(operator, "POST", "/api/roles/999999/grant", body(gold)); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}
	if as, _ := e.st.RoleAssets(r.RID); len(as) != 0 {
		t.Fatalf("refused calls changed the ledger: %v", as)
	}

	// offline role: stored, the gateway says nobody was online
	res := e.do(operator, "POST", path, body(gold, item, gold))
	if res.Status != 200 || res.JSON()["refreshed"] != false {
		t.Fatalf("grant: %d %s", res.Status, res.Body)
	}
	label, _ := game.AssetLabelID("gold")
	as, _ := e.st.RoleAssets(r.RID)
	is, _ := e.st.RoleItems(r.RID)
	if as[label] != 1000 || is[10401] != 3 { // the same currency twice adds up
		t.Fatalf("ledgers: %v %v", as, is)
	}
	if len(e.gw.refreshes) != 1 || e.gw.refreshes[0] != fmt.Sprint(r.RID) {
		t.Fatalf("refresh asks: %v", e.gw.refreshes)
	}
	// the online role is refreshed
	e.gw.refreshed = true
	if res := e.do(operator, "POST", path, body(item)); res.Status != 200 || res.JSON()["refreshed"] != true {
		t.Fatalf("online: %d %s", res.Status, res.Body)
	}
	// the gateway down: the grant still stands and the answer says why it was not shown live
	e.gw.down = true
	res = e.do(operator, "POST", path, body(item))
	if res.Status != 200 || res.JSON()["push_error"] == nil {
		t.Fatalf("gateway down: %d %s", res.Status, res.Body)
	}
	if is, _ := e.st.RoleItems(r.RID); is[10401] != 9 {
		t.Fatalf("items after three grants of 3: %v", is)
	}
	n := 0
	for _, a := range e.auditActions() {
		if a == "role.grant" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("three grants must be audited, got %d: %v", n, e.auditActions())
	}
}

func TestGrantHeroes(t *testing.T) {
	e := newEnv(t)
	viewer, operator, _ := e.tokens()
	e.seedServer("symlf_1")
	r := e.seedRole("p1", "symlf_1", "路飞")
	path := fmt.Sprintf("/api/roles/%d/heroes", r.RID)
	body := func(h ...map[string]any) map[string]any { return map[string]any{"heroes": h} }
	hero := map[string]any{"bid": 10501, "star": 5}

	if res := e.do(viewer, "POST", path, body(hero)); res.Status != 403 {
		t.Fatalf("a viewer must not grant: %d", res.Status)
	}
	bad := map[string]map[string]any{
		"nothing":      {"heroes": []any{}},
		"unknown hero": body(map[string]any{"bid": 1, "star": 1}),
		"unknown star": body(map[string]any{"bid": 10501, "star": 1}),
		"too many":     body(map[string]any{"bid": 10501, "star": 5, "num": 21}),
	}
	for name, b := range bad {
		if res := e.do(operator, "POST", path, b); res.Status != 422 {
			t.Fatalf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	if res := e.do(operator, "POST", "/api/roles/999999/heroes", body(hero)); res.Status != 404 {
		t.Fatalf("unknown role: %d", res.Status)
	}
	if hs, _ := e.st.PartnersOf(r.RID); len(hs) != 0 {
		t.Fatalf("refused calls gave heroes: %v", hs)
	}
	res := e.do(operator, "POST", path, body(hero, map[string]any{"bid": 10401, "star": 4, "num": 2}))
	if res.Status != 200 || res.JSON()["refreshed"] != false {
		t.Fatalf("grant: %d %s", res.Status, res.Body)
	}
	hs, _ := e.st.PartnersOf(r.RID)
	if len(hs) != 3 || hs[0].BID != 10501 || hs[0].Star != 5 || hs[2].BID != 10401 || hs[2].Lev != 1 {
		t.Fatalf("heroes: %+v", hs)
	}
	want := fmt.Sprintf("%d,%d,%d", hs[0].PartnerID, hs[1].PartnerID, hs[2].PartnerID)
	if len(e.gw.heroPushes) != 1 || e.gw.heroPushes[0] != want {
		t.Fatalf("push asks %v, want %s", e.gw.heroPushes, want)
	}
	found := false
	for _, a := range e.auditActions() {
		found = found || a == "role.heroes"
	}
	if !found {
		t.Fatalf("audit: %v", e.auditActions())
	}
}
