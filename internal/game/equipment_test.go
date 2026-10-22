package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestEquipmentExtracted(t *testing.T) {
	if len(equipmentTable) != 92 {
		t.Fatalf("pieces: %d", len(equipmentTable))
	}
	if w := equipmentTable[40101]; w.Slot != 1 || w.Attr["atk"] != 43 {
		t.Fatalf("40101: %+v", w)
	}
	for id, e := range equipmentTable {
		for k := range e.Attr {
			if _, ok := attrIDs[k]; !ok {
				t.Errorf("piece %d has attribute %q with no id", id, k)
			}
		}
	}
}

func TestWearAndTakeOffEquipment(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	hero := heroes[0] // 10101: 33 atk, 165 hp at level 1
	// two swords of one kind, a better sword and an armor
	e.give(c, role, map[int64]int64{40101: 2, 40102: 1, 40201: 1})

	c.send(10501, proto.Message{})
	bag := c.recv(10501).Arr("item_list")
	if len(bag) != 4 || bag[0].Int("id") != 40101001 || bag[1].Int("id") != 40101002 || bag[0].Int("base_id") != 40101 ||
		bag[0].Arr("main_attr")[0].Int("attr_id") != 101 || bag[0].Arr("main_attr")[0].Int("attr_val") != 43 {
		t.Fatalf("bag: %v", bag)
	}

	equip := func(item int64) proto.Message {
		c.send(11010, proto.Message{"partner_id": hero.PartnerID, "item_id": item})
		return c.recv(11010)
	}
	if r := equip(12345678); r.Int("result") != 0 {
		t.Fatalf("not a piece: %v", r)
	}
	if r := equip(40103001); r.Int("result") != 0 { // a real piece the role does not hold
		t.Fatalf("not held: %v", r)
	}
	if r := equip(40101001); r.Int("result") != 1 {
		t.Fatalf("wear: %v", r)
	}
	h := c.recv(11012)
	if h.Int("partner_id") != hero.PartnerID || h.Int("atk") != 33+43 || len(h.Arr("eqms")) != 1 || h.Arr("eqms")[0].Int("base_id") != 40101 || h.Arr("eqms")[0].Int("type") != 1 {
		t.Fatalf("11012: %v", h)
	}
	if bag := c.recv(10501).Arr("item_list"); len(bag) != 3 {
		t.Fatalf("bag after wearing: %d", len(bag))
	}
	// the list now shows the worn piece
	c.send(11026, proto.Message{"partner_ids": []proto.Message{{"partner_id": hero.PartnerID}}})
	if d := c.recv(11026).Arr("partners"); d[0].Int("atk") != 76 || len(d[0].Arr("eqms")) != 1 {
		t.Fatalf("11026: %v", d)
	}
	// another sword takes the slot and the first one goes back to the bag
	if r := equip(40102001); r.Int("result") != 1 {
		t.Fatalf("replace: %v", r)
	}
	c.recv(11012)
	c.recv(10501)
	got, _ := e.st.RoleItems(role.RID)
	if got[40101] != 2 || got[40102] != 0 {
		t.Fatalf("ledger after replacing: %v", got)
	}
	// armor in slot 3 adds hp; taking it off removes it
	if r := equip(40201001); r.Int("result") != 1 {
		t.Fatalf("armor: %v", r)
	}
	if h := c.recv(11012); h.Int("hp") != 165+322 || len(h.Arr("eqms")) != 2 {
		t.Fatalf("armor 11012: %v", h)
	}
	c.recv(10501)
	c.send(11011, proto.Message{"partner_id": hero.PartnerID, "pos_id": 3})
	if r := c.recv(11011); r.Int("result") != 1 {
		t.Fatalf("take off: %v", r)
	}
	if h := c.recv(11012); h.Int("hp") != 165 || len(h.Arr("eqms")) != 1 {
		t.Fatalf("after taking off: %v", h)
	}
	c.recv(10501)
	c.send(11011, proto.Message{"partner_id": hero.PartnerID, "pos_id": 3})
	if r := c.recv(11011); r.Int("result") != 0 {
		t.Fatalf("empty slot: %v", r)
	}
	// the tips window sends the id of the worn piece (its eqms id), not the slot
	c.send(11011, proto.Message{"partner_id": hero.PartnerID, "pos_id": 40102})
	if r := c.recv(11011); r.Int("result") != 1 {
		t.Fatalf("take off by piece id: %v", r)
	}
	if h := c.recv(11012); len(h.Arr("eqms")) != 0 {
		t.Fatalf("after taking off by id: %v", h)
	}
	c.recv(10501)
}

func TestConsumedHeroGivesItsEquipmentBack(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10501, Star: 5}, {BID: 10405, Star: 5},
		{BID: 10401, Star: 5}, {BID: 10402, Star: 5}, {BID: 10403, Star: 5}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	if err := e.st.SellItems(role.RID, nil, nil, map[int64]int64{40101: 1}); err != nil { // puts a sword in the ledger
		t.Fatal(err)
	}
	if err := e.st.EquipPiece(role.RID, hs[1].PartnerID, 1, 40101); err != nil {
		t.Fatal(err)
	}
	consumed := []int64{hs[1].PartnerID, hs[2].PartnerID, hs[3].PartnerID, hs[4].PartnerID, hs[5].PartnerID}
	if err := e.st.AscendPartner(role.RID, hs[0].PartnerID, 5, consumed, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.RoleItems(role.RID); got[40101] != 1 {
		t.Fatalf("the sword must be back in the ledger: %v", got)
	}
}

// Claiming equipment from a mail also sends the equipment bag (10501) right after the
// backpack (10500): the client keeps its own copy and its equipment window said
// "背包中无可穿戴装备" on a device until the bag was sent again.
func TestEquipmentBagFollowsAClaim(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{
		{Kind: "item", ItemID: 40105, Num: 1}, {Kind: "item", ItemID: 40205, Num: 1}, {Kind: "item", ItemID: 10003, Num: 5},
	}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	// the order matters to the client only in that both bags come after the claim
	if _, bp := c.recvNext(); bp == nil {
		t.Fatal("no backpack")
	}
	cmd, bag := c.recvNext()
	if cmd != 10501 {
		t.Fatalf("after the backpack the server sent %d, want 10501", cmd)
	}
	pieces := bag.Arr("item_list")
	if len(pieces) != 2 || pieces[0].Int("base_id") != 40105 || pieces[1].Int("base_id") != 40205 {
		t.Fatalf("equipment bag: %v", pieces)
	}
}

// The hero panel's one-key buttons send item_id 0 (11010) and pos_id 0 (11011): wear the
// best piece of the bag in every slot, and take every piece off.
func TestOneKeyWearAndTakeOff(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	hero := heroes[0]

	oneKey := func(cmd uint16, key string) proto.Message {
		c.send(cmd, proto.Message{"partner_id": hero.PartnerID, key: 0})
		return c.recv(cmd)
	}
	// nothing to wear and nothing worn
	if r := oneKey(11010, "item_id"); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("one-key wear with an empty bag: %v", r)
	}
	if r := oneKey(11011, "pos_id"); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("one-key take-off with nothing on: %v", r)
	}

	// a plain and a better sword (221 attack), an armor, boots: the better sword and the others go on
	e.give(c, role, map[int64]int64{40101: 1, 40105: 1, 40201: 1, 40301: 1})
	if r := oneKey(11010, "item_id"); r.Int("result") != 1 {
		t.Fatalf("one-key wear: %v", r)
	}
	got, _ := e.st.PartnerByID(role.RID, hero.PartnerID)
	worn := map[int64]int64{}
	for _, eq := range got.Equips {
		worn[eq.Slot] = eq.BaseID
	}
	if len(worn) != 3 || worn[1] != 40105 || worn[equipmentTable[40201].Slot] != 40201 || worn[equipmentTable[40301].Slot] != 40301 {
		t.Fatalf("worn: %v", worn)
	}
	// 11012 (the new attributes) and 10501 (the bag) follow
	if m := c.recv(11012); m.Int("partner_id") != hero.PartnerID || m.Int("atk") != 33+221 { // the level-1 starter has 33 attack, the sword adds 221
		t.Fatalf("11012: %v", m)
	}
	if bag := c.recv(10501).Arr("item_list"); len(bag) != 1 || bag[0].Int("base_id") != 40101 {
		t.Fatalf("only the unused plain sword stays in the bag: %v", bag)
	}
	// the same again: nothing better to wear
	if r := oneKey(11010, "item_id"); r.Int("result") != 0 {
		t.Fatalf("one-key wear again: %v", r)
	}

	// take everything off: all pieces are back in the bag
	if r := oneKey(11011, "pos_id"); r.Int("result") != 1 {
		t.Fatalf("one-key take-off: %v", r)
	}
	c.recv(11012)
	if bag := c.recv(10501).Arr("item_list"); len(bag) != 4 {
		t.Fatalf("bag after take-off: %v", bag)
	}
	got, _ = e.st.PartnerByID(role.RID, hero.PartnerID)
	if len(got.Equips) != 0 {
		t.Fatalf("still wearing %v", got.Equips)
	}
}
