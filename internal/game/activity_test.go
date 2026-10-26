package game

import (
	"encoding/json"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

var actNow = time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone)

func actSpecJSON(t *testing.T, daily bool) string {
	b, err := json.Marshal(ActivitySpec{Title: "累计召唤", PanelType: 2, SortVal: 1, CliType: 0, Daily: daily, TimeStr: "活动期间",
		Aims: []ActivityAim{
			{ID: 1, Str: "召唤 3 次", Event: evtRecruit, Target: 3, Rewards: [][2]int64{{3, 100}}},
			{ID: 2, Str: "登录奖励", Rewards: [][2]int64{{10408, 2}}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *env) addActivity(t *testing.T, a store.Activity) *store.Activity {
	t.Helper()
	if msg := ValidateActivity(&a); msg != "" {
		t.Fatalf("validate: %s", msg)
	}
	row, err := e.st.CreateActivity(a)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestActivityWindowAndList(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ac1", "活动人")
	c.send(16601, proto.Message{})
	if r := c.waitFor(16601); len(r.Arr("holiday_list")) != 0 {
		t.Fatalf("an empty schedule: %v", r)
	}
	soon := e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 7001, BID: 91001, Status: 1, StartAt: actNow.Unix() + 3600, EndAt: actNow.Unix() + 7200, Spec: actSpecJSON(t, false)})
	e.addActivity(t, store.Activity{SrvID: "other_1", CampID: 7002, BID: 91002, Status: 1, Spec: actSpecJSON(t, false)})
	e.addActivity(t, store.Activity{CampID: 7003, BID: 91003, Status: 0, Spec: actSpecJSON(t, false)})
	e.addActivity(t, store.Activity{CampID: 7004, BID: 91004, Status: 1, EndAt: actNow.Unix() - 1, Spec: actSpecJSON(t, false)})
	all := e.addActivity(t, store.Activity{CampID: 7005, BID: 91005, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(16601, proto.Message{})
	r := c.waitFor(16601)
	l := r.Arr("holiday_list")
	if len(l) != 1 || l[0].Int("bid") != all.BID || l[0].Int("camp_id") != 7005 || l[0].Int("panel_type") != 2 || l[0].Str("title") != "累计召唤" || l[0].Int("remain_sec") != actForever {
		t.Fatalf("list: %v", r)
	}
	if _, _, open := ActivityOpen(e.st, role.SrvID, 7001, actNow); open {
		t.Fatal("an activity before its window is open")
	}
	e.setNow(actNow.Add(90 * time.Minute))
	c.send(16601, proto.Message{})
	l = c.waitFor(16601).Arr("holiday_list")
	if len(l) != 2 {
		t.Fatalf("list inside the window: %v", l)
	}
	for _, x := range l {
		if x.Int("bid") == soon.BID && x.Int("remain_sec") != 1800 {
			t.Fatalf("remain: %v", x)
		}
	}
	if start, end, open := ActivityOpen(e.st, role.SrvID, 7001, actNow.Add(90*time.Minute)); !open || start != soon.StartAt || end != soon.EndAt {
		t.Fatalf("ActivityOpen: %d %d %v", start, end, open)
	}
}

func TestActivityAimsAndClaim(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ac2", "领奖人")
	a := e.addActivity(t, store.Activity{CampID: 7010, BID: 91010, Status: 1, Spec: actSpecJSON(t, false)})
	c.send(16603, proto.Message{"bid": a.BID})
	d := c.waitFor(16603)
	aims := d.Arr("aim_list")
	if len(aims) != 2 || aims[0].Int("status") != 0 || aims[1].Int("status") != 1 || len(aims[0].Arr("item_list")) != 1 {
		t.Fatalf("detail: %v", d)
	}
	c.send(16602, proto.Message{})
	if r := c.waitFor(16602).Arr("holiday_list"); len(r) != 1 || r[0].Int("can_get_num") != 1 {
		t.Fatalf("red: %v", r)
	}
	c.send(16604, proto.Message{"bid": a.BID, "aim": 1})
	if r := c.waitFor(16604); r.Int("code") != 0 {
		t.Fatalf("claimed before the count: %v", r)
	}
	c.send(16604, proto.Message{"bid": a.BID, "aim": 2})
	if r := c.waitFor(16604); r.Int("code") != 1 {
		t.Fatalf("claim: %v", r)
	}
	if r := c.waitFor(16603); r.Arr("aim_list")[1].Int("status") != 2 {
		t.Fatalf("detail after the claim: %v", r)
	}
	if r := c.waitFor(16606); r.Int("bid") != a.BID || r.Int("can_get_num") != 0 {
		t.Fatalf("red after the claim: %v", r)
	}
	c.send(16604, proto.Message{"bid": a.BID, "aim": 2})
	if r := c.waitFor(16604); r.Int("code") != 0 {
		t.Fatalf("claimed twice: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[10408] != 2 {
		t.Fatalf("items %v", items)
	}

	// a real summon feeds the count of aim 1
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{10408: 10}); err != nil {
		t.Fatal(err)
	}
	c.send(23231, proto.Message{"times": 1, "recruit_type": eliteScrollType})
	c.waitFor(23230) // the page comes last of the draw's frames
	st := actState{}
	if _, err := e.st.LoadState(role.RID, actModule, &st); err != nil {
		t.Fatal(err)
	}
	if got := st.Camps[7010].Progress[1]; got != 1 {
		t.Fatalf("the draw was not counted: %+v", st.Camps[7010])
	}
}

func TestActivityEventDrivesProgress(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ac3", "累计人")
	a := e.addActivity(t, store.Activity{CampID: 7020, BID: 91020, Status: 1, Spec: actSpecJSON(t, true)})
	feed := func(n int64) {
		sess := e.gw.Hub.Online(role.RID)
		if sess == nil {
			t.Fatal("not online")
		}
		if err := activityEvent(sess, e.st, role.RID, evtRecruit, n); err != nil {
			t.Fatal(err)
		}
	}
	feed(2)
	c.send(16603, proto.Message{"bid": a.BID})
	d := c.waitFor(16603)
	if got := d.Arr("aim_list")[0]; got.Int("status") != 0 || got.Str("aim_str") != "召唤 3 次 (2/3)" {
		t.Fatalf("progress: %v", got)
	}
	feed(5)
	if r := c.waitFor(16606); r.Int("bid") != a.BID || r.Int("can_get_num") != 2 {
		t.Fatalf("push when an aim is ready: %v", r)
	}
	c.send(16604, proto.Message{"bid": a.BID, "aim": 1})
	if r := c.waitFor(16604); r.Int("code") != 1 {
		t.Fatalf("claim: %v", r)
	}
	// a daily activity starts over on the next day
	e.setNow(actNow.Add(24 * time.Hour))
	c.send(16603, proto.Message{"bid": a.BID})
	for {
		d = c.waitFor(16603)
		if len(d.Arr("aim_list")) == 2 && d.Arr("aim_list")[0].Int("status") == 0 {
			break
		}
	}
	if d.Arr("aim_list")[1].Int("status") != 1 {
		t.Fatalf("the next day: %v", d)
	}
	c.send(16687, proto.Message{"bid": a.BID})
	if r := c.waitFor(16687); r.Int("code") != 1 {
		t.Fatalf("16687: %v", r)
	}
	c.send(16620, proto.Message{"id_list": []proto.Message{{"id": a.BID}, {"id": 1234}}})
	if r := c.waitFor(16620); len(r.Arr("holiday_list")) != 1 {
		t.Fatalf("16620: %v", r)
	}
}

func TestActivityWatchTellsAboutChanges(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ac4", "监听人")
	stop := make(chan struct{})
	defer close(stop)
	go WatchActivities(e.st, e.gw.Hub, 20*time.Millisecond, func() time.Time { return time.Unix(0, e.clk.Load()) }, stop)
	time.Sleep(100 * time.Millisecond) // a first look at the empty schedule
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: 7030, BID: 91030, Status: 1, Spec: actSpecJSON(t, false)})
	if r := c.waitFor(16607); r.Int("type") != 0 {
		t.Fatalf("16607: %v", r)
	}
}
