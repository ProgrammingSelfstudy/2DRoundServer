package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// give puts items in the role's backpack the way the game does: through a mail
// the player claims. It drains the replies that follow.
func (e *env) give(c *client, role *store.Role, items map[int64]int64) {
	e.t.Helper()
	var atts []store.Attachment
	for id, n := range items {
		atts = append(atts, store.Attachment{Kind: "item", ItemID: id, Num: n})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	c.recv(10302)
	c.recv(10500)
}

func sale(lines ...[2]int64) proto.Message {
	args := []proto.Message{}
	for _, l := range lines {
		args = append(args, proto.Message{"id": l[0], "bid": l[0], "num": l[1]})
	}
	return proto.Message{"storage": 1, "args": args}
}

func TestSellItems(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// 10034 sells for 50000 coin (asset 1) each, 16001 for 2200 of item 10450,
	// 10020 cannot be sold, 40112 is equipment (not in the backpack)
	e.give(c, role, map[int64]int64{10034: 3, 16001: 2, 10020: 1, 40112: 1})

	// preview changes nothing
	c.send(10521, sale([2]int64{10034, 2}, [2]int64{16001, 1}))
	pv := c.recv(10521).Arr("items")
	if len(pv) != 2 || pv[0].Int("bid") != 1 || pv[0].Int("num") != 100000 || pv[1].Int("bid") != 10450 || pv[1].Int("num") != 2200 {
		t.Fatalf("preview: %v", pv)
	}
	if got, _ := e.st.RoleItems(role.RID); got[10034] != 3 {
		t.Fatalf("preview must not sell: %v", got)
	}

	// the same item on two lines is added up
	c.send(10522, sale([2]int64{10034, 1}, [2]int64{10034, 1}, [2]int64{16001, 1}))
	if r := c.recv(10522); r.Int("flag") != 1 || r.Str("msg") == "" {
		t.Fatalf("sell: %v", r)
	}
	if as := c.recv(10302); as.Str("coin") != "100000" {
		t.Fatalf("coin after selling: %v", as)
	}
	bag := c.recv(10500).Arr("item_list")
	left := map[int64]int64{}
	for _, it := range bag {
		left[it.Int("base_id")] = it.Int("quantity")
	}
	if len(bag) != 4 || left[10034] != 1 || left[16001] != 1 || left[10450] != 2200 || left[10020] != 1 {
		t.Fatalf("backpack after selling: %v", left)
	}

	// selling the last piece removes the stack
	c.send(10522, sale([2]int64{10034, 1}))
	c.recv(10522)
	c.recv(10302)
	for _, it := range c.recv(10500).Arr("item_list") {
		if it.Int("base_id") == 10034 {
			t.Fatalf("sold-out stack still listed: %v", it)
		}
	}
}

func TestSellRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	e.give(c, role, map[int64]int64{10034: 2, 10020: 1, 40112: 1})
	bad := func(msg string, req proto.Message) {
		t.Helper()
		c.send(10522, req)
		if r := c.recv(10522); r.Int("flag") != 0 || r.Str("msg") != msg {
			t.Fatalf("want %q, got %v", msg, r)
		}
		// the preview of the same request is empty
		c.send(10521, req)
		if r := c.recv(10521); len(r.Arr("items")) != 0 {
			t.Fatalf("preview of a refused sale: %v", r)
		}
	}
	bad("物品数量不足", sale([2]int64{10034, 3}))
	bad("物品数量不足", sale([2]int64{10034, 2}, [2]int64{10034, 1})) // added up: 3 > 2
	bad("该物品不可出售", sale([2]int64{10020, 1}))
	bad("物品不存在", sale([2]int64{40112, 1})) // equipment is not a backpack item
	bad("物品不存在", sale([2]int64{16001, 1})) // not owned
	bad("物品不存在", sale([2]int64{99999999, 1}))
	bad("物品不存在", sale([2]int64{10034, 0}))
	bad("物品不存在", proto.Message{"storage": 1, "args": []proto.Message{{"id": 1, "bid": 10034, "num": 1}}})
	bad("请选择要出售的物品", sale())
	bad("该背包暂不支持出售", proto.Message{"storage": 3, "args": sale([2]int64{10034, 1})["args"]})

	// nothing was sold, nothing was paid
	got, _ := e.st.RoleItems(role.RID)
	as, _ := e.st.RoleAssets(role.RID)
	if got[10034] != 2 || got[10020] != 1 || len(as) != 0 {
		t.Fatalf("refused sales changed the ledger: %v %v", got, as)
	}
}

func TestSellBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(10522, sale([2]int64{10034, 1}))
	c.send(10521, sale([2]int64{10034, 1}))
	c.expectNothing()
}

func TestSellItemsStoreIsAllOrNothing(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "item", ItemID: 10034, Num: 2}, {Kind: "item", ItemID: 16001, Num: 1}}}, role.RID)
	if _, _, err := e.st.ClaimMail(role.RID, m.ID); err != nil {
		t.Fatal(err)
	}
	// the second line asks for more than is held: nothing may change
	err := e.st.SellItems(role.RID, []store.ItemSale{{ItemID: 10034, Num: 2}, {ItemID: 16001, Num: 5}}, map[int64]int64{1: 100000}, nil)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	got, _ := e.st.RoleItems(role.RID)
	as, _ := e.st.RoleAssets(role.RID)
	if got[10034] != 2 || got[16001] != 1 || len(as) != 0 {
		t.Fatalf("a failed sale changed the ledger: %v %v", got, as)
	}
}
