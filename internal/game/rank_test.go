package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestCampaignAndPowerRankings(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	_, c3 := e.enter("c", "丙")
	atDungeon(t, e, b.RID, 10050, false) // b is furthest, a and c have not fought
	// a has one strong hero, c two weak ones, b none
	if _, err := e.st.GrantStarterPartners(a.RID, []store.PartnerSeed{{BID: 10501, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GrantStarterPartners(c3.RID, []store.PartnerSeed{{BID: 10101, Star: 1}, {BID: 10101, Star: 1}}); err != nil {
		t.Fatal(err)
	}

	ask := func(c *client, typ int64) proto.Message {
		c.send(12900, proto.Message{"type": typ, "start": 1, "num": 100})
		return c.recv(12900)
	}
	r := ask(cb, rankDrama)
	rows := r.Arr("rank_list")
	if len(rows) != 3 || rows[0].Int("rid") != b.RID || rows[0].Int("val1") != 10050 || r.Int("my_idx") != 1 || r.Int("my_val1") != 10050 || r.Int("len") != 3 {
		t.Fatalf("campaign rank: %v", r)
	}
	if rows[1].Int("val1") != 10010 || rows[0].Int("idx") != 1 || rows[2].Int("idx") != 3 {
		t.Fatalf("campaign rows: %v", rows)
	}

	r = ask(ca, rankPower)
	rows = r.Arr("rank_list")
	if len(rows) != 3 || rows[0].Int("rid") != a.RID || rows[1].Int("rid") != c3.RID || rows[2].Int("rid") != b.RID {
		t.Fatalf("power order: %v", rows)
	}
	if rows[2].Int("val1") != 0 || rows[0].Int("val1") <= rows[1].Int("val1") || r.Int("my_idx") != 1 {
		t.Fatalf("power values: %v", r)
	}
	// a window of the list, and a type nobody fills
	ca.send(12900, proto.Message{"type": rankPower, "start": 2, "num": 1})
	if w := ca.recv(12900); len(w.Arr("rank_list")) != 1 || w.Arr("rank_list")[0].Int("rid") != c3.RID || w.Int("my_idx") != 1 {
		t.Fatalf("window: %v", w)
	}
	if o := ask(ca, 99); len(o.Arr("rank_list")) != 0 || o.Int("my_idx") != 0 {
		t.Fatalf("unknown type: %v", o)
	}
	// the preview holds the first place of both rankings
	ca.send(12902, proto.Message{})
	first := ca.recv(12902).Arr("rank_list")
	if len(first) != 2 || first[0].Int("type") != rankDrama || first[0].Int("rid") != b.RID || first[1].Int("rid") != a.RID {
		t.Fatalf("12902: %v", first)
	}
}
