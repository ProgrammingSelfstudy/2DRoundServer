package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGuildRedbag(t *testing.T) {
	e := newEnv(t, 1000*time.Hour)
	a, ra := e.enter("ra", "发红包的")
	b, rb := e.enter("rb", "抢红包的")
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "红包联盟", LeaderRID: ra.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.JoinGuild(gid, rb.RID, 2, 30); err != nil {
		t.Fatal(err)
	}
	a.send(13535, proto.Message{"type": 2, "num": 1, "loss_type": 1})
	if r := a.waitFor(13535); r.Int("code") != 0 {
		t.Fatalf("without the item: %v", r)
	}
	grantItems(t, e, a, ra, nil, map[int64]int64{10801: 2})
	a.send(13535, proto.Message{"type": 2, "num": 1, "loss_type": 0})
	if r := a.waitFor(13535); r.Int("code") != 0 {
		t.Fatalf("for money: %v", r)
	}
	a.send(13535, proto.Message{"type": 2, "num": 1, "loss_type": 1})
	if r := a.waitFor(13535); r.Int("code") != 1 || r.Int("day_send_num") != 1 {
		t.Fatalf("send: %v", r)
	}
	as, _ := e.st.RoleAssets(ra.RID)
	if as[3] != 500 { // the sender's reward: 500 diamonds
		t.Fatalf("reward: %v", as)
	}
	push := b.waitFor(13534)
	if push.Int("type") != 1 || len(push.Arr("list")) != 1 || push.Arr("list")[0].Int("max_num") != 10 {
		t.Fatalf("push: %v", push)
	}
	id := push.Arr("list")[0].Int("id")
	b.send(13534, proto.Message{})
	if r := b.waitFor(13534); r.Int("type") != 0 || len(r.Arr("list")) != 1 || r.Arr("list")[0].Int("flag") != 0 {
		t.Fatalf("list: %v", r)
	}
	b.send(13536, proto.Message{"id": id})
	r := b.waitFor(13536)
	if r.Int("code") != 1 || r.Int("val") < 1 || r.Int("val") > 100 || r.Int("day_recv_num") != 1 {
		t.Fatalf("open: %v", r)
	}
	got := r.Int("val")
	b.send(13536, proto.Message{"id": id})
	if r := b.waitFor(13536); r.Int("code") != 0 {
		t.Fatalf("open twice: %v", r)
	}
	b.send(13540, proto.Message{"id": id})
	if d := b.waitFor(13540); len(d.Arr("list")) != 1 || d.Arr("list")[0].Int("val") != got || d.Int("val") != 500 {
		t.Fatalf("detail: %v", d)
	}
	a.send(13545, proto.Message{})
	if rk := a.waitFor(13545); len(rk.Arr("list")) != 1 || rk.Arr("list")[0].Int("price") != 50 {
		t.Fatalf("rank: %v", rk)
	}
	a.send(13546, proto.Message{})
	if l := a.waitFor(13546).Arr("list"); len(l) != 1 || l[0].Int("id") != 2 || l[0].Int("num") != 1 {
		t.Fatalf("sent today: %v", l)
	}
	a.send(13523, proto.Message{})
	if r := a.waitFor(13523); r.Int("day_send_num") != 1 {
		t.Fatalf("13523: %v", r)
	}
	// a day later the bag cannot be opened any more
	e.setNow(time.Now().Add(25 * time.Hour))
	b.send(13536, proto.Message{"id": id})
	if r := b.waitFor(13536); r.Int("code") != 0 {
		t.Fatalf("expired: %v", r)
	}
}

func TestRedbagShares(t *testing.T) {
	left, shares := int64(500), int64(10)
	total := int64(0)
	for shares > 0 {
		v := redbagShare(left, shares)
		if v < 1 || v > left-(shares-1) {
			t.Fatalf("share %d of %d left with %d shares", v, left, shares)
		}
		total += v
		left -= v
		shares--
	}
	if total != 500 || left != 0 {
		t.Fatalf("total %d left %d", total, left)
	}
}
