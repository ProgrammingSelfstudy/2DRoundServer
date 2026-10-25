package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func grantItems(t *testing.T, e *env, c *client, role *store.Role, assets map[int64]int64, items map[int64]int64) {
	t.Helper()
	var atts []store.Attachment
	for id, n := range assets {
		atts = append(atts, store.Attachment{Kind: store.AttachAsset, ItemID: id, Num: n})
	}
	for id, n := range items {
		atts = append(atts, store.Attachment{Kind: store.AttachItem, ItemID: id, Num: n})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.waitFor(10801)
	c.waitFor(10500)
}

func TestForgeCompose(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "铁匠")
	c.send(11080, proto.Message{"base_id": 12345, "num": 1})
	if r := c.waitFor(11080); r.Int("result") != 0 {
		t.Fatalf("unknown item: %v", r)
	}
	c.send(11080, proto.Message{"base_id": 40102, "num": 1})
	if r := c.waitFor(11080); r.Int("result") != 0 {
		t.Fatalf("nothing to compose with: %v", r)
	}
	grantItems(t, e, c, role, map[int64]int64{1: 100000}, map[int64]int64{40101: 10})
	c.send(11080, proto.Message{"base_id": 40102, "num": 2}) // 6 pieces and 2112 coin
	if r := c.waitFor(11080); r.Int("result") != 1 {
		t.Fatalf("compose: %v", r)
	}
	items, _ := e.st.RoleItems(role.RID)
	assets, _ := e.st.RoleAssets(role.RID)
	if items[40101] != 4 || items[40102] != 2 || assets[1] != 100000-2*1056 {
		t.Fatalf("ledger: %v %v", items, assets)
	}
	// what composing everything up to 40103 would make: 1 more 40102 from 3 left? 4/3=1, then 3 40102 -> 1 40103
	c.send(11079, proto.Message{"base_id": 40103})
	plan := c.waitFor(11079)
	if plan.Int("coin") != 1056+2323 || len(plan.Arr("list")) != 2 || plan.Arr("list")[0].Int("bid") != 40102 || plan.Arr("list")[0].Int("num") != 1 || plan.Arr("list")[1].Int("bid") != 40103 {
		t.Fatalf("plan: %v", plan)
	}
	c.send(11081, proto.Message{"base_id": 40103})
	if r := c.waitFor(11081); r.Int("result") != 1 {
		t.Fatalf("compose all: %v", r)
	}
	items, _ = e.st.RoleItems(role.RID)
	if items[40101] != 1 || items[40102] != 0 || items[40103] != 1 {
		t.Fatalf("ledger after composing all: %v", items)
	}
	c.send(11081, proto.Message{"base_id": 40103}) // nothing left to do
	if r := c.waitFor(11081); r.Int("result") != 0 {
		t.Fatalf("nothing more: %v", r)
	}
	c.send(11082, proto.Message{})
	rec := c.waitFor(11082)
	if len(rec.Arr("logs")) != 2 || rec.Arr("logs")[0].Int("coin") != 1056+2323 || len(rec.Arr("logs")[0].Arr("items")) != 2 {
		t.Fatalf("records: %v", rec)
	}
	// not enough coin: the plan shrinks
	grantItems(t, e, c, role, nil, map[int64]int64{40201: 3})
	if err := e.st.Exchange(role.RID, map[int64]int64{1: assets[1] - 1056 - 2323 - 1}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.send(11079, proto.Message{"base_id": 40203})
	if p := c.waitFor(11079); len(p.Arr("list")) != 0 {
		t.Fatalf("plan without coin: %v", p)
	}
}
