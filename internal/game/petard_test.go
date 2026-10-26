package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestPetardFlow(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	night := time.Date(2026, time.October, 3, 20, 0, 0, 0, dayZone)
	e.setNow(night)
	a, ra := e.enter("pa", "放烟花的")
	b, rb := e.enter("pb", "抢红包的")
	cfg := petardCfg.Const
	a.send(27001, proto.Message{"id": cfg["meteor_bid"], "num": 1})
	if r := a.waitFor(27001); r.Int("code") != 0 {
		t.Fatalf("without the activity: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: ra.SrvID, CampID: petardCamp, BID: 93037, Status: 1, Spec: actSpecJSON(t, false)})
	a.send(27000, proto.Message{})
	r := a.waitFor(27000)
	if r.Int("score") != 0 || r.Int("lev_score") != 100000 || len(r.Arr("score_award")) != 7 {
		t.Fatalf("open: %v", r)
	}
	a.send(27001, proto.Message{"id": cfg["meteor_bid"], "num": 1})
	if r := a.waitFor(27001); r.Int("code") != 0 {
		t.Fatalf("no meteor: %v", r)
	}
	grantItems(t, e, a, ra, nil, map[int64]int64{cfg["meteor_bid"]: 20, cfg["firework_bid"]: 3})
	a.send(27001, proto.Message{"id": cfg["meteor_bid"], "num": 20})
	if r := a.waitFor(27001); r.Int("code") != 1 || r.Int("flag") != 0 {
		t.Fatalf("meteors: %v", r)
	}
	if r := a.waitFor(27000); r.Int("score") != 20 {
		t.Fatalf("heat after the meteors: %v", r)
	}
	as, _ := e.st.RoleAssets(ra.RID)
	if as[1] < 20*20000 {
		t.Fatalf("the meteors give back berries: %v", as)
	}
	// a cannon sends a red bag; it can only be fired at night
	e.setNow(time.Date(2026, time.October, 3, 12, 0, 0, 0, dayZone))
	a.send(27001, proto.Message{"id": cfg["firework_bid"], "num": 1})
	if r := a.waitFor(27001); r.Int("code") != 0 {
		t.Fatalf("a cannon by day: %v", r)
	}
	e.setNow(night)
	a.send(27001, proto.Message{"id": cfg["firework_bid"], "num": 2})
	if r := a.waitFor(27001); r.Int("code") != 1 || r.Int("flag") != 2 {
		t.Fatalf("cannons: %v", r)
	}
	if r := b.waitFor(27008); r.Int("flag") != 1 || r.Str("msg") == "" {
		t.Fatalf("the others see it: %v", r)
	}
	b.waitFor(27004)
	b.send(27002, proto.Message{})
	l := b.waitFor(27002)
	if len(l.Arr("red_packet_list")) != 2 || l.Arr("red_packet_list")[0].Int("status") != 1 || l.Int("max_num") != 8 {
		t.Fatalf("list: %v", l)
	}
	id := l.Arr("red_packet_list")[0].Int("red_packet_id")
	b.send(27003, proto.Message{"red_packet_id": id})
	o := b.waitFor(27003)
	if o.Int("code") != 1 || len(o.Arr("get_red_packet_list")) != 1 || o.Int("max_num") != 10 || o.Str("send_name") != ra.Name {
		t.Fatalf("open: %v", o)
	}
	got := o.Arr("get_red_packet_list")[0].Arr("item")[0].Int("num")
	if got < petardMinPart || got > petardPackets {
		t.Fatalf("share: %d", got)
	}
	bs, _ := e.st.RoleAssets(rb.RID)
	if bs[3] != got {
		t.Fatalf("paid %d, ledger %v", got, bs)
	}
	b.send(27003, proto.Message{"red_packet_id": id}) // again: only the list
	if o := b.waitFor(27003); o.Int("code") != 1 || len(o.Arr("get_red_packet_list")) != 1 {
		t.Fatalf("view: %v", o)
	}
	if bs2, _ := e.st.RoleAssets(rb.RID); bs2[3] != got {
		t.Fatalf("paid twice: %v", bs2)
	}
	b.send(27006, proto.Message{})
	if h := b.waitFor(27006); len(h.Arr("get_red_packet_list")) != 1 {
		t.Fatalf("news: %v", h)
	}
	// the heat awards: 160 / 1000 of 100000 = 16000
	a.send(27007, proto.Message{"id": 1})
	if r := a.waitFor(27007); r.Int("code") != 0 {
		t.Fatalf("not enough heat: %v", r)
	}
	w := &petardWorld{}
	if _, err := e.st.LoadGlobal(ra.SrvID, petardModule, w); err != nil {
		t.Fatal(err)
	}
	w.Heat = 20000
	if err := e.st.SaveGlobal(ra.SrvID, petardModule, w); err != nil {
		t.Fatal(err)
	}
	a.send(27000, proto.Message{})
	if r := a.waitFor(27000); r.Arr("score_award")[0].Int("status") != 1 {
		t.Fatalf("ready: %v", r)
	}
	a.send(27007, proto.Message{"id": 1})
	if r := a.waitFor(27007); r.Int("code") != 1 {
		t.Fatalf("award: %v", r)
	}
	a.send(27007, proto.Message{"id": 1})
	if r := a.waitFor(27007); r.Int("code") != 0 {
		t.Fatalf("award twice: %v", r)
	}
}
