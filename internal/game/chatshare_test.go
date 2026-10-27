package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestChatShare(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("a", "甲")
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachItem, ItemID: 40101, Num: 2}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.waitFor(10801)
	c.waitFor(10500)

	c.send(10535, proto.Message{"type": 4, "id": 40101*pieceIDFactor + 2, "partner_id": 0, "code": 7})
	r := c.waitFor(10535)
	if r.Int("flag") != 1 || r.Int("base_id") != 40101 || r.Int("count") != 2 || r.Int("code") != 7 || r.Int("share_id") != 40101*pieceIDFactor+2 {
		t.Fatalf("share a piece: %v", r)
	}
	c.send(10535, proto.Message{"type": 4, "id": 40102, "code": 1})
	if r := c.waitFor(10535); r.Int("flag") != 0 || r.Str("msg") == "" {
		t.Fatalf("share what the role lacks: %v", r)
	}
	c.send(10535, proto.Message{"type": 1, "id": 99999999, "code": 1})
	if r := c.waitFor(10535); r.Int("flag") != 0 {
		t.Fatalf("share a non-item: %v", r)
	}
	// anybody can look at an item
	c.send(10536, proto.Message{"id": 40101*pieceIDFactor + 1, "srv_id": "symlf_1"})
	d := c.waitFor(10536)
	if d.Int("base_id") != 40101 || len(d.Arr("main_attr")) == 0 {
		t.Fatalf("10536: %v", d)
	}
}
