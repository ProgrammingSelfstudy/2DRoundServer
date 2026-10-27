package game

import (
	"encoding/json"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func panelSpec(t *testing.T, panel int64) string {
	b, err := json.Marshal(ActivitySpec{Title: "测试", PanelType: panel, SortVal: 1, TimeStr: "活动期间"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLevelUpGifts(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("lg1", "礼包人")
	c2, role2 := e.enter("lg2", "礼包人二")
	c.send(21200, proto.Message{})
	if r := c.waitFor(21200); len(r.Arr("gifts")) != 0 {
		t.Fatalf("closed: %v", r)
	}
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 9015, BID: 91060, Status: 1, StartAt: dayStart(actNow).Unix(), EndAt: dayStart(actNow).Unix() + 86400*7, Spec: panelSpec(t, lgPanelType)})
	if err := e.st.SetRoleLevel(role.RID, 30); err != nil {
		t.Fatal(err)
	}
	c.send(21200, proto.Message{})
	g := c.waitFor(21200).Arr("gifts")
	if len(g) != 6 || g[0].Int("status") != 1 || g[1].Int("status") != 1 || g[2].Int("status") != 0 {
		t.Fatalf("by level 30: %v", g)
	}
	c.send(21201, proto.Message{"id": 3})
	if r := c.waitFor(21201); r.Int("code") != 0 {
		t.Fatalf("a level not reached: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(21201, proto.Message{"id": 2})
	if r := c.waitFor(21201); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	g = c.waitFor(21200).Arr("gifts")
	if g[1].Int("status") != 2 || g[1].Int("num") != 1 {
		t.Fatalf("after: %v", g)
	}
	if after, _ := e.st.RoleAssets(role.RID); after[1] != before[1]+100000 {
		t.Fatalf("coins %d -> %d", before[1], after[1])
	}
	c.send(21201, proto.Message{"id": 2})
	if r := c.waitFor(21201); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	// the count is the server's: a second role sees it, and a gift with all taken is gone
	if err := e.st.SetRoleLevel(role2.RID, 60); err != nil {
		t.Fatal(err)
	}
	w := &lgWorld{}
	if _, err := e.st.LoadGlobal(role.SrvID, lgModule, w); err != nil {
		t.Fatal(err)
	}
	w.Taken[6] = 10
	if err := e.st.SaveGlobal(role.SrvID, lgModule, w); err != nil {
		t.Fatal(err)
	}
	c2.send(21200, proto.Message{})
	if g := c2.waitFor(21200).Arr("gifts"); g[1].Int("num") != 1 || g[5].Int("num") != 10 {
		t.Fatalf("the server's counts: %v", g)
	}
	c2.send(21201, proto.Message{"id": 6})
	if r := c2.waitFor(21201); r.Int("code") != 0 {
		t.Fatalf("sold out: %v", r)
	}
}

func TestTimeCollect(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("tc1", "收集人")
	e.setNow(time.Date(2026, 10, 27, 13, 0, 0, 0, dayZone))
	c.send(16698, proto.Message{"id": 1})
	if r := c.waitFor(16698); r.Int("code") != 0 {
		t.Fatalf("closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 31817, BID: 91061, Status: 1, StartAt: start, EndAt: start + 86400*7, Spec: panelSpec(t, 16)})
	c.send(16697, proto.Message{})
	if r := c.waitFor(16697); r.Int("code") != 1 || r.Int("camp_id") != 31817 || r.Int("id") != 1 {
		t.Fatalf("open: %v", r)
	}
	c.send(16698, proto.Message{"id": 1})
	if r := c.waitFor(16698); r.Int("code") != 1 {
		t.Fatalf("take: %v", r)
	}
	c.send(16698, proto.Message{"id": 1})
	if r := c.waitFor(16698); r.Int("code") != 0 {
		t.Fatalf("twice a day: %v", r)
	}
	c.send(16697, proto.Message{})
	if r := c.waitFor(16697); r.Int("code") != 0 {
		t.Fatalf("after the take: %v", r)
	}
	e.setNow(time.Date(2026, 10, 28, 9, 0, 0, 0, dayZone))
	c.send(16697, proto.Message{})
	if r := c.waitFor(16697); r.Int("code") != 0 {
		t.Fatalf("before the hours: %v", r)
	}
	e.setNow(time.Date(2026, 10, 28, 12, 0, 0, 0, dayZone))
	c.send(16698, proto.Message{"id": 1})
	if r := c.waitFor(16698); r.Int("code") != 1 {
		t.Fatalf("next day: %v", r)
	}
}
