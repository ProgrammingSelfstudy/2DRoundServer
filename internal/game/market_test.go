package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestGoldMarket(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "商人")
	c.send(23500, proto.Message{"catalg": 4})
	list := c.waitFor(23500)
	if list.Int("catalg") != 4 || len(list.Arr("goods")) != 5 || list.Arr("goods")[0].Int("base_id") != 10151 || list.Arr("goods")[0].Int("cur_price") != 8000 {
		t.Fatalf("other tab: %v", list)
	}
	c.send(23500, proto.Message{"catalg": 0})
	if all := c.waitFor(23500); len(all.Arr("goods")) < 80 {
		t.Fatalf("all tabs: %d goods", len(all.Arr("goods")))
	}
	c.send(23501, proto.Message{"base_id": 10151, "num": 1})
	if r := c.waitFor(23501); r.Int("flag") != 0 {
		t.Fatalf("no coins: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{1: 100000})
	c.send(23501, proto.Message{"base_id": 10151, "num": 3})
	if r := c.waitFor(23501); r.Int("flag") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.send(23501, proto.Message{"base_id": 10151, "num": 3}) // 5 a week
	if r := c.waitFor(23501); r.Int("flag") != 0 {
		t.Fatalf("limit: %v", r)
	}
	c.send(23520, proto.Message{})
	if r := c.waitFor(23520); len(r.Arr("limit_data")) != 1 || r.Arr("limit_data")[0].Int("item_id") != 10151 || r.Arr("limit_data")[0].Int("count") != 3 {
		t.Fatalf("counts: %v", r)
	}
	ledger, err := e.st.RoleAssets(role.RID)
	if err != nil || ledger[1] != 100000-3*8000 {
		t.Fatalf("coins: %v %v", ledger, err)
	}
	c.send(23516, proto.Message{"base_ids": []proto.Message{{"base_id": 10151}, {"base_id": 1}}})
	if r := c.waitFor(23516); len(r.Arr("market_price")) != 1 || r.Arr("market_price")[0].Int("source") != 2 || r.Arr("market_price")[0].Int("price") != 8000 {
		t.Fatalf("prices: %v", r)
	}
	c.send(23502, proto.Message{"id": 10151, "num": 2})
	if r := c.waitFor(23502); r.Int("flag") != 1 {
		t.Fatalf("sell: %v", r)
	}
	ledger, _ = e.st.RoleAssets(role.RID)
	if ledger[1] != 100000-3*8000+2*7200 {
		t.Fatalf("coins after selling: %v", ledger)
	}
	c.send(23502, proto.Message{"id": 10151, "num": 2}) // only one left
	if r := c.waitFor(23502); r.Int("flag") != 0 {
		t.Fatalf("sell more than held: %v", r)
	}
}
