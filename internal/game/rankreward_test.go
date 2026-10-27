package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestRankRewardList(t *testing.T) {
	e := newEnv(t, time.Hour)
	e.setNow(actNow)
	c, _ := e.enter("rr1", "榜客")
	c.send(16650, proto.Message{"bid": sweetCamp})
	r := c.waitFor(16650)
	tiers := r.Arr("rank_award")
	if len(tiers) != len(sweetCfg.Ranks) || tiers[0].Int("rank1") != sweetCfg.Ranks[0].Min || len(tiers[0].Arr("award")) == 0 || r.Int("rank") != 0 {
		t.Fatalf("sweet: %v", r)
	}
	c.send(16650, proto.Message{"bid": sbCamp})
	if r := c.waitFor(16650); len(r.Arr("rank_award")) != len(sbCfg.Ranks) {
		t.Fatalf("sandybeach: %v", r)
	}
	c.send(16650, proto.Message{"bid": 1})
	if r := c.waitFor(16650); len(r.Arr("rank_award")) != 0 {
		t.Fatalf("an unknown activity: %v", r)
	}
}
