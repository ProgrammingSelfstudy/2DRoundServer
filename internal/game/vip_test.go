package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestVIPTable(t *testing.T) {
	if len(vipTable.Price[0]) != 1 || vipTable.Price[0][0][1] != 0 || vipTable.Price[1][0] != [2]int64{3, 238} || vipTable.Items[30][0] != [2]int64{3, 200000} {
		t.Fatalf("table: %v %v", vipTable.Price[:2], vipTable.Items[30])
	}
}

func TestVIPGifts(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	buy := func(lev int64) proto.Message {
		c.send(16711, proto.Message{"lev": lev})
		return c.recv(16711)
	}
	c.send(16710, proto.Message{})
	if r := c.recv(16710); len(r.Arr("list")) != 0 {
		t.Fatalf("nothing bought: %v", r)
	}
	if r := buy(1); r.Int("code") != 0 { // VIP 0
		t.Fatalf("level too low: %v", r)
	}
	if r := buy(99); r.Int("code") != 0 {
		t.Fatalf("unknown level: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	// a bought gift: the new list first, then the answer with an empty msg (as the original)
	c.send(16711, proto.Message{"lev": 0}) // free
	if r := c.recv(16710); len(r.Arr("list")) != 1 || r.Arr("list")[0].Int("lev") != 0 {
		t.Fatalf("list: %v", r)
	}
	if r := c.recv(16711); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("level 0: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	after, _ := e.st.RoleAssets(role.RID)
	items, _ := e.st.RoleItems(role.RID)
	if after[3]+items[3] != before[3]+200000 {
		t.Fatalf("gift: before %v after %v items %v", before, after, items)
	}
	if r := buy(0); r.Int("code") != 0 { // once
		t.Fatalf("twice: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 1); err != nil {
		t.Fatal(err)
	}
	c.send(16711, proto.Message{"lev": 1})
	c.recv(16710)
	if r := c.recv(16711); r.Int("code") != 1 {
		t.Fatalf("level 1: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	if r := buy(2); r.Int("code") != 0 {
		t.Fatalf("level 2 above VIP 1: %v", r)
	}
}
