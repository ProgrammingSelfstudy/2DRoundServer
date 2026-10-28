package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestTitleCompose(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	const title, material = 92101, 52101
	step := titleCompose.Compound[title][0][3]
	if step.Rate != 1000 { // three pieces always succeed in the client's table; the test relies on it
		t.Skip("rate", step.Rate)
	}
	compose := func(n int64) proto.Message {
		c.send(23304, proto.Message{"item_id": title, "item_num": n, "expends": []proto.Message{}})
		return c.until(23304)
	}
	if r := compose(3); r.Int("result") != 0 { // nothing to pay with
		t.Fatalf("no materials: %v", r)
	}
	if r := compose(9); r.Int("result") != 0 {
		t.Fatalf("a count the table does not have: %v", r)
	}
	e.give(c, role, map[int64]int64{material: 10})
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 1, Num: 10000000}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.until(10801)
	if r := compose(3); r.Int("result") != 1 || r.Int("flag") != 1 {
		t.Fatalf("compose: %v", r)
	}
	if left, _ := e.st.RoleItems(role.RID); left[material] != 7 {
		t.Fatalf("materials: %v", left)
	}
	c.send(23300, proto.Message{})
	var found bool
	for _, h := range c.until(23300).Arr("honor") {
		if h.Int("base_id") == title {
			found = true
			if h.Int("lev") != 1 {
				t.Fatalf("level: %v", h)
			}
		}
	}
	if !found {
		t.Fatal("title not given")
	}
	// the next level has its own row
	next := titleCompose.Compound[title][1]
	if len(next) == 0 {
		t.Fatal("no row for level 1")
	}
}
