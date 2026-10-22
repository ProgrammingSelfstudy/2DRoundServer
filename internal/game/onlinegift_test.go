package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func progress(c *client) (secs int64, claimed []int64) {
	c.send(10926, proto.Message{})
	r := c.recv(10926)
	for _, e := range r.Arr("list") {
		claimed = append(claimed, e.Int("time"))
	}
	return r.Int("time"), claimed
}

// claimStep claims the step that needs the given online seconds. The client sends the
// step's number (its index in the table), which is what the real device was seen to do.
func claimStep(c *client, seconds int64) proto.Message {
	index := int64(99) // not a step
	for _, st := range onlineSteps {
		if st.Time == seconds {
			index = int64(st.Index)
		}
	}
	c.send(10927, proto.Message{"time": index})
	return c.recv(10927)
}

// waitOnlineSeconds waits until the disconnect hook has stored the role's time.
func (e *env) waitOnlineSeconds(rid int64, day, want int64) {
	e.t.Helper()
	for i := 0; i < 200; i++ {
		if n, _ := e.st.OnlineSeconds(rid, day); n == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	n, _ := e.st.OnlineSeconds(rid, day)
	e.t.Fatalf("online seconds = %d, want %d", n, want)
}

func TestOnlineGiftStepsAndClaims(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")

	if secs, claimed := progress(c); secs != 0 || len(claimed) != 0 {
		t.Fatalf("at the start: %d %v", secs, claimed)
	}
	// not online long enough yet
	if r := claimStep(c, 60); r.Int("code") != 0 || r.Str("msg") != "在线时间不足" || r.Int("time") != 60 {
		t.Fatalf("too early: %v", r)
	}
	e.advance(70 * time.Second)
	if secs, _ := progress(c); secs != 70 {
		t.Fatalf("after 70 s: %d", secs)
	}

	// step 60 pays 5000 gold (currency 3) and refreshes the resources and the backpack
	r := claimStep(c, 60)
	if r.Int("code") != 1 || r.Str("msg") == "" || r.Int("time") != 60 {
		t.Fatalf("claim: %v", r)
	}
	if as := c.recv(10302); as.Str("gold") != "5000" {
		t.Fatalf("assets after the claim: %v", as)
	}
	c.recv(10500)
	if _, claimed := progress(c); !same(claimed, 60) {
		t.Fatalf("claimed: %v", claimed)
	}
	// once per day, and only steps that exist
	if r := claimStep(c, 60); r.Int("code") != 0 || r.Str("msg") != "已经领取过了" {
		t.Fatalf("second claim: %v", r)
	}
	if r := claimStep(c, 150); r.Int("code") != 0 || r.Str("msg") != "在线时间不足" {
		t.Fatalf("step 150 at 70 s: %v", r)
	}
	if r := claimStep(c, 61); r.Int("code") != 0 || r.Str("msg") != "奖励不存在" || r.Int("time") != 99 {
		t.Fatalf("no such step: %v", r)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[3] != 5000 {
		t.Fatalf("the gold was paid once: %v", got)
	}

	// an item step (530 s: item 26301) lands in the backpack
	e.advance(500 * time.Second)
	if r := claimStep(c, 530); r.Int("code") != 1 {
		t.Fatalf("claim 530: %v", r)
	}
	c.recv(10302)
	bag := c.recv(10500).Arr("item_list")
	found := false
	for _, it := range bag {
		found = found || (it.Int("base_id") == 26301 && it.Int("quantity") == 1)
	}
	if !found {
		t.Fatalf("item 26301 missing from %v", bag)
	}
}

func TestOnlineTimeAddsUpAcrossLogins(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	c, role := e.enter("u1", "甲")
	day := dayKey(time.Unix(0, e.clk.Load()))

	e.advance(100 * time.Second)
	c.c.Close()
	e.waitOnlineSeconds(role.RID, day, 100)
	e.advance(time.Hour) // offline time does not count

	c2, _ := e.dialSameRole("u1", role)
	e.advance(10 * time.Second)
	if secs, _ := progress(c2); secs != 110 {
		t.Fatalf("100 s before, 10 s now: %d", secs)
	}

	// logging in again while online replaces the session and keeps what was earned
	e.advance(20 * time.Second)
	old := e.gw.Hub.Online(role.RID)
	c3, _ := e.dialSameRole("u1", role)
	c2.recv(10310)
	c2.expectClosed()
	time.Sleep(150 * time.Millisecond)
	// the old session's disconnect hook may run after the new login has started counting;
	// it must not stop that count (run it once more here to be sure it is late)
	e.online.Disconnected(old)
	e.advance(5 * time.Second)
	if secs, _ := progress(c3); secs != 135 {
		t.Fatalf("after replacing the session: %d", secs)
	}
}

func TestOnlineGiftNewDay(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 23, 59, 0, 0, dayZone)) // a minute before midnight
	c, _ := e.enter("u1", "甲")
	e.advance(30 * time.Second)
	if secs, _ := progress(c); secs != 30 {
		t.Fatalf("before midnight: %d", secs)
	}
	if r := claimStep(c, 60); r.Int("code") != 0 {
		t.Fatalf("not enough yet: %v", r)
	}
	e.advance(60 * time.Second) // 00:00:30 next day: only the 30 s after midnight count
	if secs, claimed := progress(c); secs != 30 || len(claimed) != 0 {
		t.Fatalf("after midnight: %d %v", secs, claimed)
	}
	e.advance(40 * time.Second) // 70 s into the new day
	if r := claimStep(c, 60); r.Int("code") != 1 {
		t.Fatalf("claim on the new day: %v", r)
	}
	c.recv(10302)
	c.recv(10500)
	// a day later (still online) the clock starts again at midnight and nothing is claimed
	e.advance(24 * time.Hour)
	if secs, claimed := progress(c); secs != 70 || len(claimed) != 0 {
		t.Fatalf("a day later: %d %v", secs, claimed)
	}
	if r := claimStep(c, 60); r.Int("code") != 1 {
		t.Fatalf("the same step on the next day: %v", r)
	}
}

func TestOnlineGiftBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(10926, proto.Message{})
	c.send(10927, proto.Message{"time": 60})
	c.expectNothing()
}

func TestOnlineGiftStore(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	// a new day starts over instead of adding to yesterday
	e.st.AddOnlineSeconds(role.RID, 20261019, 500)
	e.st.AddOnlineSeconds(role.RID, 20261019, 100)
	if n, _ := e.st.OnlineSeconds(role.RID, 20261019); n != 600 {
		t.Fatalf("same day: %d", n)
	}
	e.st.AddOnlineSeconds(role.RID, 20261020, 40)
	if n, _ := e.st.OnlineSeconds(role.RID, 20261020); n != 40 {
		t.Fatalf("next day: %d", n)
	}
	if n, _ := e.st.OnlineSeconds(role.RID, 20261019); n != 0 {
		t.Fatalf("the old day is gone: %d", n)
	}
	// a second claim of a step pays nothing
	if err := e.st.ClaimOnlineGift(role.RID, 20261020, 60, map[int64]int64{3: 5000}, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ClaimOnlineGift(role.RID, 20261020, 60, map[int64]int64{3: 5000}, nil); err != store.ErrConflict {
		t.Fatalf("err = %v", err)
	}
	if got, _ := e.st.RoleAssets(role.RID); got[3] != 5000 {
		t.Fatalf("paid twice: %v", got)
	}
}

func TestOnlineGiftTableIsComplete(t *testing.T) {
	if len(onlineSteps) != 12 || onlineSteps[0].Time != 60 || onlineSteps[11].Time != 14400 {
		t.Fatalf("steps: %v", onlineSteps)
	}
	for _, s := range onlineSteps {
		for _, it := range s.Items {
			if _, known := itemTable[it[0]]; !known {
				t.Errorf("step %d pays unknown id %d", s.Index, it[0])
			}
		}
	}
}
