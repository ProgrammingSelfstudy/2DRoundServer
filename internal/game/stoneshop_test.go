package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestStoneShop(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ss1", "神秘人")
	c.send(16689, proto.Message{"id": 3, "num": 1})
	if r := c.waitFor(16689); r.Int("code") != 0 {
		t.Fatalf("closed: %v", r)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 9028, BID: ssBID, Status: 1, StartAt: start, EndAt: start + 86400*7, Spec: panelSpec(t, 16)})
	c.send(16688, proto.Message{})
	if r := c.waitFor(16688); len(r.Arr("buy_info")) != 26 {
		t.Fatalf("info: %v", r)
	}
	c.send(16689, proto.Message{"id": 3, "num": 1})
	if r := c.waitFor(16689); r.Int("code") != 0 {
		t.Fatalf("without the item: %v", r)
	}
	e.addItems(t, role, map[int64]int64{99899: 250})
	c.send(16689, proto.Message{"id": 3, "num": 2})
	if r := c.waitFor(16689); r.Int("code") != 1 || r.Int("num") != 2 {
		t.Fatalf("buy: %v", r)
	}
	r := c.waitFor(16688)
	for _, b := range r.Arr("buy_info") {
		if b.Int("id") == 3 && (b.Int("day_num") != 2 || b.Int("all_num") != 2) {
			t.Fatalf("counts: %v", b)
		}
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[99899] != 50 {
		t.Fatalf("the item: %v", items)
	}
	assets, _ := e.st.RoleAssets(role.RID)
	if assets[10006]+items[10006] != 260 {
		t.Fatalf("the award: assets %v items %v", assets[10006], items[10006])
	}
	// the day count starts again, the activity count does not
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(16688, proto.Message{})
	for _, b := range c.waitFor(16688).Arr("buy_info") {
		if b.Int("id") == 3 && (b.Int("day_num") != 0 || b.Int("all_num") != 2) {
			t.Fatalf("next day: %v", b)
		}
	}
}
