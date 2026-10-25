package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestRoleMisc(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c2, other := e.enter("u2", "乙")

	// facts about the server and the role
	c.send(10905, proto.Message{})
	if m := c.recv(10905); m.Int("world_lev") != 1 { // two level-1 roles
		t.Fatalf("world level: %v", m)
	}
	c.send(10380, proto.Message{})
	if m := c.recv(10380); m.Int("reg_time") != role.CreatedAt || m.Int("open_time") == 0 {
		t.Fatalf("10380: %v", m)
	}
	c.send(10303, proto.Message{})
	if m := c.recv(10303); m.Int("klb") != 0 {
		t.Fatalf("klb: %v", m)
	}
	c.send(10395, proto.Message{"age": 30})
	if m := c.recv(10395); m.Int("status") != 1 {
		t.Fatalf("age: %v", m)
	}
	c.send(10960, proto.Message{})
	if m := c.recv(10960); m.Int("code") != 1 {
		t.Fatalf("real name: %v", m)
	}
	c.send(10947, proto.Message{"charge_id": 1, "notify_url": ""})
	if m := c.recv(10947); m.Int("code") != 0 {
		t.Fatalf("recharge: %v", m)
	}

	// worship: once a day for each role and kind, and the target counts
	worship := func(kind int64) proto.Message {
		c.send(10316, proto.Message{"type": kind, "rid": other.RID, "srv_id": "symlf_1", "idx": 1})
		return c.recv(10316)
	}
	if r := worship(1); r.Int("code") != 1 || r.Int("rid") != other.RID {
		t.Fatalf("worship: %v", r)
	}
	if r := worship(1); r.Int("code") != 0 {
		t.Fatalf("again today: %v", r)
	}
	if r := worship(2); r.Int("code") != 1 { // another kind
		t.Fatalf("other kind: %v", r)
	}
	c2.send(10317, proto.Message{})
	if m := c2.recv(10317); m.Int("worship") != 2 {
		t.Fatalf("worshipped twice: %v", m)
	}
	e.advance(25 * time.Hour)
	if r := worship(1); r.Int("code") != 1 {
		t.Fatalf("next day: %v", r)
	}
	c.send(10316, proto.Message{"type": 1, "rid": 99999, "srv_id": "symlf_1", "idx": 1})
	if r := c.recv(10316); r.Int("code") != 0 {
		t.Fatalf("no such role: %v", r)
	}

	// role models: the one the face gives, and the ones the heroes open
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 4}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	c.send(10345, proto.Message{})
	m := c.recv(10345)
	var ids []int64
	for _, x := range m.Arr("list") {
		ids = append(ids, x.Int("id"))
	}
	if m.Int("use_id") != 110401 || len(ids) != 1 || ids[0] != 110401 {
		t.Fatalf("models: use %d list %v", m.Int("use_id"), ids)
	}
	use := func(id int64) proto.Message {
		c.send(10346, proto.Message{"id": id})
		return c.recv(10346)
	}
	if r := use(110402); r.Int("code") != 0 {
		t.Fatalf("not owned: %v", r)
	}
	if r := use(110401); r.Int("code") != 1 {
		t.Fatalf("use: %v", r)
	}
	if cfg, _ := e.st.SettingsOf(role.RID); cfg.LookID != 110401 {
		t.Fatalf("not saved: %+v", cfg)
	}

	// redeem codes
	if err := e.st.CreateRedeemCode(store.RedeemCode{Code: "hello1", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 41, Num: 7}, {Kind: store.AttachItem, ItemID: 16001, Num: 2}}, MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateRedeemCode(store.RedeemCode{Code: "OLD2", Attachments: []store.Attachment{{Kind: store.AttachItem, ItemID: 16001, Num: 1}}, ExpiresAt: time.Now().Unix() - 100}); err != nil {
		t.Fatal(err)
	}
	redeem := func(c *client, code string) proto.Message {
		c.send(10945, proto.Message{"card_id": code})
		return c.recv(10945)
	}
	if r := redeem(c, "nope"); r.Int("code") != 0 {
		t.Fatalf("unknown: %v", r)
	}
	if r := redeem(c, "OLD2"); r.Int("code") != 0 {
		t.Fatalf("expired: %v", r)
	}
	c.send(10945, proto.Message{"card_id": " Hello1 "}) // case and blanks do not matter
	c.recv(10302)
	c.recv(10500)
	if r := c.recv(10945); r.Int("code") != 1 {
		t.Fatalf("redeem: %v", r)
	}
	if r := redeem(c, "HELLO1"); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	if r := redeem(c2, "HELLO1"); r.Int("code") != 0 { // used up
		t.Fatalf("used up: %v", r)
	}
	c.send(10303, proto.Message{})
	if m := c.recv(10303); m.Int("klb") != 7 {
		t.Fatalf("klb after the code: %v", m)
	}

	// red points seen
	c.send(10986, proto.Message{"id": 2})
	c.send(10985, proto.Message{})
	if m := c.recv(10985); len(m.Arr("id_list")) != 1 || m.Arr("id_list")[0].Int("id") != 2 {
		t.Fatalf("red points: %v", m)
	}

	// reports
	c.send(12770, proto.Message{"rid": other.RID, "srv_id": "symlf_1", "type": 3, "msg": "广告", "history": []proto.Message{}})
	if r := c.recv(12770); r.Int("code") != 1 {
		t.Fatalf("report: %v", r)
	}
	c.send(12770, proto.Message{"rid": role.RID, "srv_id": "symlf_1", "type": 3, "msg": "自己", "history": []proto.Message{}})
	if r := c.recv(12770); r.Int("code") != 0 {
		t.Fatalf("report oneself: %v", r)
	}
	if rs, _ := e.st.Reports(10); len(rs) != 1 || rs[0].TargetRID != other.RID || rs[0].Msg != "广告" {
		t.Fatalf("stored: %+v", rs)
	}
	c.send(12771, proto.Message{"rid": other.RID, "srv_id": "symlf_1"})
	if m := c.recv(12771); len(m.Arr("history")) != 0 {
		t.Fatalf("history: %v", m)
	}
}
