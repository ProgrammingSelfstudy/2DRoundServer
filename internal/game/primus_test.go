package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestPrimus(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("pr1", "霸主人")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		t.Fatal(err)
	}
	c.send(20701, proto.Message{})
	r := c.waitFor(20701)
	if len(r.Arr("list")) != 6 || r.Arr("list")[0].Int("status") != 0 || r.Arr("list")[0].Int("unit_id") != 81011 {
		t.Fatalf("20701: %v", r)
	}
	c.send(20702, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(20702); r.Int("code") != 0 { // not in the arena yet
		t.Fatalf("without an arena place: %v", r)
	}
	if _, err := e.st.ArenaPlayerOf(role.RID, 1000, actNow.Unix()); err != nil {
		t.Fatal(err)
	}
	c.send(20702, proto.Message{"pos": 1, "num": 2})
	if r := c.waitFor(20702); r.Int("code") != 0 {
		t.Fatalf("a wrong evolution count: %v", r)
	}
	c.send(20702, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(20702); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20705)
	if res.Int("pos") != 1 || res.Int("num1") != 0 {
		t.Fatalf("20705: %v", res)
	}
	won := res.Int("num2") == 1
	c.send(20703, proto.Message{"pos": 1})
	if r := c.waitFor(20703); len(r.Arr("list")) != 1 || r.Arr("list")[0].Int("rid") != role.RID {
		t.Fatalf("20703: %v", r)
	}
	c.send(20700, proto.Message{})
	r = c.waitFor(20700)
	if won && r.Int("time") != 0 || !won && r.Int("time") != actNow.Unix()+1200 {
		t.Fatalf("20700 (won %v): %v", won, r)
	}
	if !won {
		c.send(20702, proto.Message{"pos": 2, "num": 1})
		if r := c.waitFor(20702); r.Int("code") != 0 {
			t.Fatalf("while cooling: %v", r)
		}
		return
	}
	c.send(20701, proto.Message{})
	if r := c.waitFor(20701); r.Arr("list")[0].Int("rid") != role.RID {
		t.Fatalf("holder: %v", r)
	}
	c.send(20702, proto.Message{"pos": 1, "num": 1})
	if r := c.waitFor(20702); r.Int("code") != 0 {
		t.Fatalf("own seat: %v", r)
	}
}
