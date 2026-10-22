package game

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// enter logs an account in, creates a role for it and enters the game with it.
func (e *env) enter(account, roleName string) (*client, *store.Role) {
	e.t.Helper()
	a, tok := e.account(account)
	role, err := e.st.CreateRole(a.ID, "symlf_1", roleName, 0, 1, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	c := e.dial(true)
	c.send(1164, loginArgs(account, tok))
	c.recv(1164)
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": "symlf_1"})
	if r := c.recv(10192); r.Int("code") != 1 {
		e.t.Fatalf("enter: %v", r)
	}
	return c, role
}

func (e *env) sendMail(m store.Mail, rids ...int64) *store.Mail {
	e.t.Helper()
	out, err := e.st.CreateMail(m, rids)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// expectNothing proves the server sent no reply to the previous request: the next
// frame is the heartbeat answer.
func (c *client) expectNothing() {
	c.t.Helper()
	c.send(1169, proto.Message{})
	if r := c.recv(1169); r.Int("time") == 0 {
		c.t.Fatal(r)
	}
}

func TestMailListDetailReadClaimDelete(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")

	c.send(10800, proto.Message{})
	if r := c.recv(10800); len(r.Arr("mail")) != 0 {
		t.Fatalf("no mail yet: %v", r)
	}

	m := e.sendMail(store.Mail{Subject: "奖励", Content: "欢迎", FromName: "客服", ExpireAt: time.Now().Unix() + 3600,
		Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 100}, {Kind: "asset", ItemID: 1, Num: 5000}, {Kind: "item", ItemID: 40112, Num: 2}}}, role.RID)

	c.send(10800, proto.Message{})
	list := c.recv(10800).Arr("mail")
	if len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	b := list[0]
	if b.Int("id") != m.ID || b.Str("srv_id") != "symlf_1" || b.Str("subject") != "奖励" || b.Str("from_name") != "客服" ||
		b.Int("status") != 0 || b.Int("has_items") != 1 || b.Int("type") != 1 || b.Int("send_time") == 0 || b.Int("time_out") == 0 || b.Int("read_time") != 0 {
		t.Fatalf("brief: %v", b)
	}

	c.send(10806, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	d := c.recv(10806)
	if d.Str("content") != "欢迎" || len(d.Arr("assets")) != 2 || len(d.Arr("items")) != 1 ||
		d.Arr("assets")[0].Int("label") != 3 || d.Arr("assets")[0].Int("val") != 100 ||
		d.Arr("items")[0].Int("base_id") != 40112 || d.Arr("items")[0].Int("quantity") != 2 {
		t.Fatalf("detail: %v", d)
	}

	c.send(10805, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	if r := c.recv(10805); r.Int("code") != 1 || r.Int("read_time") == 0 || r.Int("id") != m.ID {
		t.Fatalf("read: %v", r)
	}

	// claim: reply, then the refreshed assets (gold is a string on the wire)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	if r := c.recv(10801); r.Int("code") != 1 || r.Str("msg") == "" {
		t.Fatalf("claim: %v", r)
	}
	as := c.recv(10302)
	if as.Str("gold") != "100" || as.Str("coin") != "5000" {
		t.Fatalf("assets after claim: %v", as)
	}
	// the backpack is refreshed too; 40112 is equipment, which the backpack does not list
	if bag := c.recv(10500); len(bag.Arr("item_list")) != 0 {
		t.Fatalf("backpack after claiming equipment: %v", bag)
	}
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	if r := c.recv(10801); r.Int("code") != 0 {
		t.Fatalf("second claim must fail: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[40112] != 2 {
		t.Fatalf("item ledger: %v", items)
	}

	// after the claim the mail shows as claimed and carries no attachments any more
	c.send(10806, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	d = c.recv(10806)
	if d.Int("status") != 2 || d.Int("has_items") != 0 || len(d.Arr("assets")) != 0 || len(d.Arr("items")) != 0 {
		t.Fatalf("claimed detail: %v", d)
	}

	c.send(10804, proto.Message{"ids": []proto.Message{{"id": m.ID, "srv_id": "symlf_1"}}})
	if r := c.recv(10804); len(r.Arr("ids")) != 1 || r.Arr("ids")[0].Int("id") != m.ID {
		t.Fatalf("delete: %v", r)
	}
	c.send(10800, proto.Message{})
	if r := c.recv(10800); len(r.Arr("mail")) != 0 {
		t.Fatalf("deleted mail still listed: %v", r)
	}
	// the refreshed assets survive a new login
	c2, _ := e.dialSameRole("u1", role)
	c2.send(10300, proto.Message{})
	c2.recv(10301)
	if a2 := c2.recv(10302); a2.Str("gold") != "100" {
		t.Fatalf("10302 after relogin: %v", a2)
	}
}

// dialSameRole logs the same account in again and enters the given role.
func (e *env) dialSameRole(account string, role *store.Role) (*client, *store.Role) {
	e.t.Helper()
	a, _ := e.st.FindOrCreateAccount("symlf", account)
	tok, _ := e.st.IssueToken(a.ID, 3600)
	c := e.dial(true)
	c.send(1164, loginArgs(account, tok))
	c.recv(1164)
	c.send(10192, proto.Message{"rid": role.RID, "srv_id": "symlf_1"})
	if r := c.recv(10192); r.Int("code") != 1 {
		e.t.Fatalf("re-enter: %v", r)
	}
	return c, role
}

func TestMailClaimAllAndDeleteRules(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	g1 := e.sendMail(store.Mail{Subject: "g1", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 10}}}, role.RID)
	g2 := e.sendMail(store.Mail{Subject: "g2", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 20}}}, role.RID)
	plain := e.sendMail(store.Mail{Subject: "plain"}, role.RID)

	// nothing is deletable while unread / unclaimed
	c.send(10804, proto.Message{"ids": []proto.Message{{"id": g1.ID, "srv_id": "symlf_1"}, {"id": plain.ID, "srv_id": "symlf_1"}}})
	if r := c.recv(10804); len(r.Arr("ids")) != 0 {
		t.Fatalf("unread mail deleted: %v", r)
	}
	c.send(10802, proto.Message{})
	r := c.recv(10802)
	if len(r.Arr("ids")) != 2 || r.Arr("ids")[0].Str("srv_id") != "symlf_1" || r.Arr("ids")[0].Int("read_time") == 0 {
		t.Fatalf("claim all: %v", r)
	}
	if as := c.recv(10302); as.Str("gold") != "30" {
		t.Fatalf("assets: %v", as)
	}
	c.recv(10500)                  // refreshed backpack
	c.send(10802, proto.Message{}) // nothing left: a reply with an empty list and no asset refresh
	if r := c.recv(10802); len(r.Arr("ids")) != 0 || r.Str("msg") == "" {
		t.Fatalf("second claim all: %v", r)
	}
	// claimed mails are deletable, the unread plain one is not
	c.send(10804, proto.Message{"ids": []proto.Message{{"id": g1.ID, "srv_id": "symlf_1"}, {"id": g2.ID, "srv_id": "symlf_1"}, {"id": plain.ID, "srv_id": "symlf_1"}}})
	if r := c.recv(10804); len(r.Arr("ids")) != 2 {
		t.Fatalf("delete claimed: %v", r)
	}
}

func TestMailIsolationAndBadRequests(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, mine := e.enter("u1", "甲")
	_, other := e.enter("u2", "乙")
	theirs := e.sendMail(store.Mail{Subject: "secret", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 99}}}, other.RID)

	c.send(10806, proto.Message{"id": theirs.ID, "srv_id": "symlf_1"})
	c.expectNothing() // someone else's mail: no detail
	c.send(10805, proto.Message{"id": theirs.ID, "srv_id": "symlf_1"})
	if r := c.recv(10805); r.Int("code") != 0 {
		t.Fatalf("read of another role's mail: %v", r)
	}
	c.send(10801, proto.Message{"id": theirs.ID, "srv_id": "symlf_1"})
	if r := c.recv(10801); r.Int("code") != 0 {
		t.Fatalf("claim of another role's mail: %v", r)
	}
	if assets, _ := e.st.RoleAssets(mine.RID); len(assets) != 0 {
		t.Fatalf("granted to the wrong role: %v", assets)
	}
	// a wrong srv_id never matches
	m := e.sendMail(store.Mail{Subject: "m", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 1}}}, mine.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_2"})
	if r := c.recv(10801); r.Int("code") != 0 {
		t.Fatalf("wrong srv_id: %v", r)
	}
	// deleting someone else's mail removes nothing
	c.send(10804, proto.Message{"ids": []proto.Message{{"id": theirs.ID, "srv_id": "symlf_1"}}})
	if r := c.recv(10804); len(r.Arr("ids")) != 0 {
		t.Fatalf("delete of another role's mail: %v", r)
	}
	if l, _ := e.st.MailsForRole(other); len(l) != 1 {
		t.Fatal("the other role lost its mail")
	}
}

func TestMailRequestsBeforeRoleLoginAreIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164) // account only, no role entered
	for _, cmd := range []uint16{10800, 10802} {
		c.send(cmd, proto.Message{})
		c.expectNothing()
	}
	c.send(10805, proto.Message{"id": 1, "srv_id": "symlf_1"})
	c.expectNothing()
}

func TestPushMailReachesOnlineRolesOnce(t *testing.T) {
	e := newEnv(t, time.Minute)
	c1, r1 := e.enter("u1", "甲")
	c2, _ := e.enter("u2", "乙")
	a3, _ := e.account("u3")
	offline, _ := e.st.CreateRole(a3.ID, "symlf_1", "丙", 0, 1, 0)

	// direct mail to the first role and the offline one
	m := e.sendMail(store.Mail{Subject: "direct", Attachments: []store.Attachment{{Kind: "asset", ItemID: 3, Num: 1}}}, r1.RID, offline.RID)
	n, err := e.mail.PushMail(m.ID)
	if err != nil || n != 1 {
		t.Fatalf("direct push: %d %v", n, err)
	}
	p := c1.recv(10803).Arr("mail")
	if len(p) != 1 || p[0].Int("id") != m.ID || p[0].Str("subject") != "direct" || p[0].Int("has_items") != 1 || p[0].Int("status") != 0 {
		t.Fatalf("pushed mail: %v", p)
	}
	c2.expectNothing() // the second role is not a recipient

	// broadcast: both online roles get it, the offline one finds it when it lists
	time.Sleep(1100 * time.Millisecond) // the broadcast must be created after the roles (second resolution)
	b := e.sendMail(store.Mail{Scope: store.MailBroadcast, Subject: "all"})
	if n, err := e.mail.PushMail(b.ID); err != nil || n != 2 {
		t.Fatalf("broadcast push: %d %v", n, err)
	}
	c1.recv(10803)
	c2.recv(10803)
	// listing afterwards shows each broadcast exactly once
	c1.send(10800, proto.Message{})
	if l := c1.recv(10800).Arr("mail"); len(l) != 2 {
		t.Fatalf("online role must have the direct mail and one copy of the broadcast: %v", l)
	}
	if l, _ := e.st.MailsForRole(offline); len(l) != 2 { // direct + broadcast, delivered lazily
		t.Fatalf("offline role: %+v", l)
	}
	// pushing the same broadcast again must not create a duplicate row
	e.mail.PushMail(b.ID)
	c1.recv(10803)
	c1.send(10800, proto.Message{})
	if l := c1.recv(10800).Arr("mail"); len(l) != 2 {
		t.Fatalf("duplicate after a second push: %d", len(l))
	}
	_ = fmt.Sprint
}

func TestAdminHandlerPushAndOnline(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	srv := httptest.NewServer(e.mail.AdminHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/online")
	if err != nil {
		t.Fatal(err)
	}
	var on map[string]int
	json.NewDecoder(resp.Body).Decode(&on)
	resp.Body.Close()
	if on["online"] != 1 {
		t.Fatalf("online: %v", on)
	}

	m := e.sendMail(store.Mail{Subject: "via http"}, role.RID)
	resp, _ = http.Post(fmt.Sprintf("%s/push/mail?id=%d", srv.URL, m.ID), "", nil)
	var pushed map[string]int
	json.NewDecoder(resp.Body).Decode(&pushed)
	resp.Body.Close()
	if resp.StatusCode != 200 || pushed["pushed"] != 1 {
		t.Fatalf("push: %d %v", resp.StatusCode, pushed)
	}
	if p := c.recv(10803).Arr("mail"); len(p) != 1 || p[0].Str("subject") != "via http" {
		t.Fatalf("pushed: %v", p)
	}

	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/push/mail", 400}, {"POST", "/push/mail?id=abc", 400}, {"POST", "/push/mail?id=99999", 404}, {"GET", "/push/mail?id=1", 405}} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != tc.want {
			t.Errorf("%s %s -> %d, want %d", tc.method, tc.path, r.StatusCode, tc.want)
		}
	}
}

func TestAssetLabels(t *testing.T) {
	if id, ok := AssetLabelID("gold"); !ok || id != 3 {
		t.Fatalf("gold: %d %v", id, ok)
	}
	if id, ok := AssetLabelID("coin"); !ok || id != 1 {
		t.Fatalf("coin: %d %v", id, ok)
	}
	if _, ok := AssetLabelID("no_such_currency"); ok {
		t.Fatal("unknown name resolved")
	}
	// a label with no field in 10302 stays in the ledger and does not break the message
	r := &store.Role{RID: 1, Lev: 5}
	msg := roleAssets(r, map[int64]int64{3: 40, 22: 7, 5: 9, 19: 3 /* arena_coin: not a 10302 field */})
	if msg.Str("gold") != "40" || msg.Str("hero_exp") != "7" || msg.Int("exp") != 9 || msg.Int("lev") != 5 {
		t.Fatalf("assets message: %v", msg)
	}
}

func TestAdminHandlerKickDisconnectsAnOnlineRole(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	srv := httptest.NewServer(e.mail.AdminHandler())
	defer srv.Close()

	post := func(path string) (int, map[string]bool) {
		resp, err := http.Post(srv.URL+path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]bool
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := post("/kick?rid=999999"); code != 200 || out["kicked"] {
		t.Fatalf("an offline role is not kicked: %d %v", code, out)
	}
	if code, _ := post("/kick"); code != 400 {
		t.Fatalf("missing rid: %d", code)
	}
	if code, out := post(fmt.Sprintf("/kick?rid=%d", role.RID)); code != 200 || !out["kicked"] {
		t.Fatalf("kick: %d %v", code, out)
	}
	off := c.recv(10310) // the client is told why it was disconnected, then dropped
	if off.Int("is_show") != 1 || off.Str("msg") == "" {
		t.Fatalf("10310: %v", off)
	}
	c.expectClosed()
	deadline := time.Now().Add(2 * time.Second)
	for e.gw.Hub.Online(role.RID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("the kicked role is still online")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControlEndpointOnlineByRole(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	h := httptest.NewServer(e.mail.AdminHandler())
	defer h.Close()
	get := func(q string) (int, map[string]any) {
		resp, err := http.Get(h.URL + "/online" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	if code, m := get(fmt.Sprintf("?rid=%d", role.RID)); code != 200 || m["online"] != true {
		t.Fatalf("online role: %d %v", code, m)
	}
	if code, m := get("?rid=999999"); code != 200 || m["online"] != false {
		t.Fatalf("offline role: %d %v", code, m)
	}
	if code, _ := get("?rid=abc"); code != 400 {
		t.Fatalf("bad rid: %d", code)
	}
	if code, m := get(""); code != 200 || m["online"] != float64(1) {
		t.Fatalf("count: %d %v", code, m)
	}
}

func TestAdminHandlerRefreshResendsTheLedgers(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	srv := httptest.NewServer(e.mail.AdminHandler())
	defer srv.Close()
	post := func(path string) (int, map[string]bool) {
		resp, err := http.Post(srv.URL+path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]bool
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, _ := post("/push/refresh"); code != 400 {
		t.Fatalf("missing rid: %d", code)
	}
	if code, out := post("/push/refresh?rid=999999"); code != 200 || out["refreshed"] {
		t.Fatalf("an offline role is not refreshed: %d %v", code, out)
	}
	// a grant behind the session's back (the admin page), then the refresh
	if err := e.st.Exchange(role.RID, nil, nil, map[int64]int64{3: 77}, map[int64]int64{10401: 4}); err != nil {
		t.Fatal(err)
	}
	if code, out := post(fmt.Sprintf("/push/refresh?rid=%d", role.RID)); code != 200 || !out["refreshed"] {
		t.Fatalf("refresh: %d %v", code, out)
	}
	if a := c.recv(10302); a.Str("gold") != "77" { // gold travels as a string
		t.Fatalf("10302: %v", a)
	}
	bag := c.recv(10500).Arr("item_list")
	if len(bag) != 1 || bag[0].Int("base_id") != 10401 || bag[0].Int("quantity") != 4 {
		t.Fatalf("10500: %v", bag)
	}
	// heroes given behind the session's back arrive as 11001
	got, err := e.st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{{BID: 10501, Star: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if code, out := post(fmt.Sprintf("/push/refresh?rid=%d&heroes=%d,x,999999", role.RID, got[0].PartnerID)); code != 200 || !out["refreshed"] {
		t.Fatalf("refresh with heroes: %d %v", code, out)
	}
	c.recv(10302)
	c.recv(10500)
	if hs := c.recv(11001).Arr("partners"); len(hs) != 1 || hs[0].Int("partner_id") != got[0].PartnerID || hs[0].Int("bid") != 10501 {
		t.Fatalf("11001: %v", hs)
	}
}

func TestRoleAssetsCarryEnergyMax(t *testing.T) {
	m := roleAssets(&store.Role{Lev: 1}, map[int64]int64{7: 100})
	if m["energy_max"] != energyMax || m["energy"] != int64(100) {
		t.Fatalf("energy %v / %v", m["energy"], m["energy_max"])
	}
	if energyMax != 10000 {
		t.Fatal("the client's dungeon_const energy_max is 10000")
	}
}
