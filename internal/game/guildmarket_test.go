package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGuildMarket(t *testing.T) {
	e := newEnv(t, 1000*time.Hour)
	a, ra := e.enter("gm1", "放入者")
	b, rb := e.enter("gm2", "兑换者")
	o, _ := e.enter("gm3", "外人")
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: ra.RID, Lev: 1}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.JoinGuild(gid, rb.RID, 1, 15); err != nil {
		t.Fatal(err)
	}
	// a role without a guild has no treasury
	o.send(26900, proto.Message{})
	if r := o.waitFor(26900); r.Int("flag") != 0 {
		t.Fatalf("no guild: %v", r)
	}

	e.addItems(t, ra, map[int64]int64{24902: 5, 10403: 5})
	put := func(c *client, base, qty, storage int64) proto.Message {
		c.send(26901, proto.Message{"id": base, "quantity": qty, "storage": storage})
		return c.waitFor(26901)
	}
	if r := put(a, 10403, 1, gmBackpack); r.Int("flag") != 0 { // not for players to put
		t.Fatalf("a good players cannot put: %v", r)
	}
	if r := put(a, 24902, 9, gmBackpack); r.Int("flag") != 0 { // only 5 in the bag
		t.Fatalf("more than owned: %v", r)
	}
	if r := put(a, 24902, 1, 3); r.Int("flag") != 0 { // the equipment bag
		t.Fatalf("equipment bag: %v", r)
	}
	if r := put(a, 24902, 3, gmBackpack); r.Int("flag") != 1 {
		t.Fatalf("put: %v", r)
	}
	if got := e.assetOf(t, ra.RID, 36); got != 18 { // 6 coins a piece
		t.Fatalf("coins for the put: %d", got)
	}
	if r := b.waitFor(26904); len(r.Arr("item_list")) != 1 || r.Arr("item_list")[0].Int("num") != 3 {
		t.Fatalf("push to the member: %v", r)
	}

	b.send(26900, proto.Message{})
	r := b.waitFor(26900)
	if l := r.Arr("item_list"); len(l) != 1 || l[0].Int("base_id") != 24902 || l[0].Int("num") != 3 || len(l[0].Arr("end_time")) != 1 {
		t.Fatalf("26900: %v", r)
	}
	buy := func(c *client, base, qty int64) proto.Message {
		c.send(26902, proto.Message{"id": base, "quantity": qty})
		return c.waitFor(26902)
	}
	if r := buy(b, 24902, 2); r.Int("flag") != 0 {
		t.Fatalf("no coins: %v", r)
	}
	e.grantAssets(b, rb, map[int64]int64{36: 100})
	if r := buy(b, 24902, 4); r.Int("flag") != 0 {
		t.Fatalf("more than the stock: %v", r)
	}
	if r := buy(b, 24902, 2); r.Int("flag") != 1 {
		t.Fatalf("buy: %v", r)
	}
	if got := e.assetOf(t, rb.RID, 36); got != 100-2*12 {
		t.Fatalf("coins after the buy: %d", got)
	}
	b.send(26903, proto.Message{})
	if l := b.waitFor(26903).Arr("board_list"); len(l) != 2 || l[0].Int("operation") != gmOpPut || l[1].Int("operation") != gmOpBuy {
		t.Fatalf("board: %v", l)
	}

	// the batch leaves at 05:00 of the day after its end
	e.advance(time.Duration(31*24+6) * time.Hour)
	b.send(26900, proto.Message{})
	if r := b.waitFor(26900); len(r.Arr("item_list")) != 0 {
		t.Fatalf("expired goods are still there: %v", r)
	}
}
