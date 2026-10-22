package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestBackpackListsOnlyNormalItems(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")

	c.send(10500, proto.Message{})
	if r := c.recv(10500); r.Int("volume") != bagVolume || len(r.Arr("item_list")) != 0 {
		t.Fatalf("empty backpack: %v", r)
	}

	// 10020 and 10033 are normal items, 40112 is equipment, 100201 a home item,
	// 99999999 is not in the client's item table
	m := e.sendMail(store.Mail{Subject: "items", Attachments: []store.Attachment{
		{Kind: "item", ItemID: 10033, Num: 3}, {Kind: "item", ItemID: 10020, Num: 5}, {Kind: "item", ItemID: 40112, Num: 1},
		{Kind: "item", ItemID: 100201, Num: 1}, {Kind: "item", ItemID: 99999999, Num: 1}, {Kind: "asset", ItemID: 3, Num: 7}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	bag := c.recv(10500) // claiming refreshes the backpack
	items := bag.Arr("item_list")
	if len(items) != 2 {
		t.Fatalf("backpack: %v", bag)
	}
	if a, b := items[0], items[1]; a.Int("id") != 10020 || a.Int("base_id") != 10020 || a.Int("quantity") != 5 || a.Int("pos") != 1 ||
		b.Int("id") != 10033 || b.Int("quantity") != 3 || b.Int("pos") != 2 {
		t.Fatalf("items must be sorted by base id, one stack each: %v", items)
	}

	// a second claim of other items shows up in the next full list, and the list is complete
	m2 := e.sendMail(store.Mail{Subject: "more", Attachments: []store.Attachment{{Kind: "item", ItemID: 10020, Num: 2}}}, role.RID)
	c.send(10801, proto.Message{"id": m2.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	items = c.recv(10500).Arr("item_list")
	if len(items) != 2 || items[0].Int("quantity") != 7 {
		t.Fatalf("stack must add up: %v", items)
	}

	// the equipment bag holds the one piece of equipment (40112) given above
	c.send(10501, proto.Message{})
	if r := c.recv(10501); r.Int("volume") != bagVolume || len(r.Arr("item_list")) != 1 || r.Arr("item_list")[0].Int("base_id") != 40112 {
		t.Fatalf("10501: %v", r)
	}
	// the other bags answer with a capacity and nothing in them
	for _, cmd := range []uint16{10502, 10503} {
		c.send(cmd, proto.Message{})
		if r := c.recv(cmd); r.Int("volume") != bagVolume || len(r.Arr("item_list")) != 0 {
			t.Fatalf("%d: %v", cmd, r)
		}
	}

	// after a new login the same backpack comes back
	c2, _ := e.dialSameRole("u1", role)
	c2.send(10500, proto.Message{})
	if items := c2.recv(10500).Arr("item_list"); len(items) != 2 || items[0].Int("quantity") != 7 {
		t.Fatalf("backpack after relogin: %v", items)
	}
}

func TestBackpackBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(10500, proto.Message{})
	c.expectNothing()
}

func TestItemTableCoversTheClientsItems(t *testing.T) {
	if len(itemTable) != 4466 {
		t.Fatalf("items: %d, the client declares 4466", len(itemTable))
	}
	for id, want := range map[int64]bool{10020: true, 10033: true, 100201: false, 40112: false, 1: false, 1010001: false} {
		info, ok := itemTable[id]
		if !ok || goesToBackpack(info.Type) != want {
			t.Errorf("item %d: known=%v type=%d, backpack want %v", id, ok, info.Type, want)
		}
	}
}
