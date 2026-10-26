package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func ymEnv(t *testing.T) (*env, *client, *store.Role, int64, int64) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ym", "年兽人")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: ymCamp, BID: 93032, Status: 1, StartAt: start, EndAt: start + 20*86400, Spec: actSpecJSON(t, false)})
	return e, c, role, start, heroes[0].PartnerID
}

// ymGo puts the role next to a tile of the kind and returns it.
func (e *env) ymTile(t *testing.T, c *client, role *store.Role, evt int64) int64 {
	t.Helper()
	c.send(28201, proto.Message{})
	m := c.waitFor(28201)
	for _, tl := range m.Arr("tile_list") {
		if tl.Int("evtid") == evt {
			d := &ymRole{}
			if _, err := e.st.LoadState(role.RID, ymModule, d); err != nil {
				t.Fatal(err)
			}
			d.Pos = tl.Int("index")
			d.Start = dayStart(actNow).Unix()
			if err := e.st.SaveState(role.RID, ymModule, d); err != nil {
				t.Fatal(err)
			}
			return tl.Int("index")
		}
	}
	t.Fatalf("no tile %d in %v", evt, m)
	return 0
}

func fightExt(hero int64) []proto.Message {
	return []proto.Message{{"type": 1, "val1": 1, "val2": 0}, {"type": 3, "val1": 1, "val2": hero}}
}

func (e *env) ymWorld(t *testing.T, role *store.Role) *ymWorld {
	w := &ymWorld{}
	if _, err := e.st.LoadGlobal(role.SrvID, ymModule, w); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestYearMonsterBaseMapAndMove(t *testing.T) {
	e, c, role, start, _ := ymEnv(t)
	c.send(28200, proto.Message{})
	b := c.waitFor(28200)
	if b.Int("camp_id") != 1 || b.Int("val") != 0 || b.Int("max_val") != 1800 || b.Int("flag") != 0 {
		t.Fatalf("28200: %v", b)
	}
	m := c.waitFor(28201)
	kinds := map[int64]int{}
	for _, tl := range m.Arr("tile_list") {
		kinds[tl.Int("evtid")]++
	}
	if m.Int("map_id") != 100001 || kinds[10001] != 1 || kinds[19001] != 1 || kinds[19051] != 1 || kinds[10101] != 1 || kinds[10801] != 3 || kinds[10301] != 1 || kinds[10501] != 5 || kinds[10701] != 0 {
		t.Fatalf("28201: %v %v", kinds, m.Int("index"))
	}
	// one step to a road cell next to the start, none to a far one
	x, y := plXY(m.Int("index"))
	var near, far int64
	for _, cell := range ymCells {
		cx, cy := plXY(cell)
		d := plDistance(x, y, cx, cy)
		if d == 1 && near == 0 {
			if _, ok := e.ymEvtAt(m, cell); !ok {
				near = cell
			}
		}
		if d > 5 && far == 0 {
			far = cell
		}
	}
	c.send(28202, proto.Message{"index": far})
	if r := c.waitFor(28202); r.Int("code") != 0 {
		t.Fatalf("far: %v", r)
	}
	c.send(28202, proto.Message{"index": plIndex(1, 1)})
	if r := c.waitFor(28202); r.Int("code") != 0 {
		t.Fatalf("a wall: %v", r)
	}
	c.send(28202, proto.Message{"index": near})
	if r := c.waitFor(28202); r.Int("code") != 1 || r.Int("index") != near {
		t.Fatalf("step: %v (from %v to %d)", r, m.Int("index"), near)
	}
	c.settle()
	d := &ymRole{}
	e.st.LoadState(role.RID, ymModule, d)
	if d.Pos != near || d.Start != start {
		t.Fatalf("state: %+v", d)
	}
}

func (e *env) ymEvtAt(m proto.Message, cell int64) (int64, bool) {
	for _, tl := range m.Arr("tile_list") {
		if tl.Int("index") == cell {
			return tl.Int("evtid"), true
		}
	}
	return 0, false
}

func TestYearMonsterClosedWithoutActivity(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, _ := e.enter("ym0", "无活动")
	c.send(28200, proto.Message{})
	if b := c.waitFor(28200); b.Int("camp_id") != 0 {
		t.Fatalf("28200: %v", b)
	}
	c.send(28211, proto.Message{"num": 1})
	if r := c.waitFor(28211); r.Int("flag") != 0 {
		t.Fatalf("submit: %v", r)
	}
	c.send(28203, proto.Message{"index": 1001, "action": 1})
	if r := c.waitFor(28203); r.Int("code") != 0 {
		t.Fatalf("act: %v", r)
	}
}

func TestYearMonsterPickUpAndCakes(t *testing.T) {
	e, c, role, _, _ := ymEnv(t)
	idx := e.ymTile(t, c, role, 10501)
	c.send(28203, proto.Message{"index": idx, "action": 1})
	if r := c.waitFor(28203); r.Int("code") != 1 || r.Int("index") != idx {
		t.Fatalf("pick: %v", r)
	}
	c.settle()
	if items, _ := e.st.RoleItems(role.RID); items[80351] != 1 {
		t.Fatalf("cake: %v", items)
	}
	c.send(28203, proto.Message{"index": idx, "action": 1})
	if r := c.waitFor(28203); r.Int("code") != 0 {
		t.Fatalf("pick again: %v", r)
	}
	// the tile is gone from the map
	c.send(28201, proto.Message{})
	for _, tl := range c.waitFor(28201).Arr("tile_list") {
		if tl.Int("index") == idx {
			t.Fatalf("still there: %v", tl)
		}
	}
	// a notice and an NPC only talk
	b := e.ymTile(t, c, role, 10101)
	c.send(28203, proto.Message{"index": b, "action": 0})
	c.waitFor(28203)
	if r := c.waitFor(28205); r.Int("index") != b || r.Arr("ext_list")[0].Int("type") != 1 || r.Arr("ext_list")[0].Int("val1") != 10101 {
		t.Fatalf("board: %v", r)
	}
	// the cakes are handed in, ten progress each
	grantItems(t, e, c, role, nil, map[int64]int64{80351: 20})
	c.send(28211, proto.Message{"num": 22})
	if r := c.waitFor(28211); r.Int("flag") != 0 {
		t.Fatalf("more than owned: %v", r)
	}
	c.send(28211, proto.Message{"num": 20})
	if r := c.waitFor(28211); r.Int("flag") != 1 {
		t.Fatalf("submit: %v", r)
	}
	if b := c.waitFor(28200); b.Int("val") != 200 {
		t.Fatalf("28200: %v", b)
	}
}

func TestYearMonsterRedRain(t *testing.T) {
	e, c, role, _, _ := ymEnv(t)
	grantItems(t, e, c, role, nil, map[int64]int64{80351: 50})
	c.send(28211, proto.Message{"num": 50})
	c.waitFor(28211)
	b := c.waitFor(28200)
	if b.Int("val") != 500 || b.Int("last_red_pacekt_num") != 50 || b.Int("max_red_packet_num") != 50 {
		t.Fatalf("28200: %v", b)
	}
	idx := e.ymTile(t, c, role, 10701)
	for i := 1; i <= 5; i++ {
		c.send(28203, proto.Message{"index": idx, "action": 1})
		c.waitFor(28203)
		r := c.waitFor(28214)
		if r.Int("flag") != 1 || r.Int("role_num") != int64(i) || r.Int("last_num") != int64(50-i) || len(r.Arr("information")) != i {
			t.Fatalf("take %d: %v", i, r)
		}
	}
	c.send(28203, proto.Message{"index": idx, "action": 1})
	c.waitFor(28203)
	if r := c.waitFor(28214); r.Int("flag") != 0 {
		t.Fatalf("over the day's limit: %v", r)
	}
	c.send(28223, proto.Message{})
	if r := c.waitFor(28223); r.Int("flag") != 0 {
		t.Fatalf("28223: %v", r)
	}
	if assets, _ := e.st.RoleAssets(role.RID); assets[3] < 50 {
		t.Fatalf("diamonds: %v", assets)
	}
}

func TestYearMonsterTimerBossWakesFightsAndRanks(t *testing.T) {
	e, c, role, _, hero := ymEnv(t)
	idx := e.ymTile(t, c, role, 19001)
	c.send(28203, proto.Message{"index": idx, "action": 0})
	if r := c.waitFor(28203); r.Int("code") != 0 {
		t.Fatalf("a sleeping monster: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{80351: 180})
	c.send(28211, proto.Message{"num": 500})
	if r := c.waitFor(28211); r.Int("flag") != 1 {
		t.Fatalf("submit: %v", r)
	}
	b := c.waitFor(28200)
	if b.Int("val") != 1800 || b.Int("flag") != 1 || b.Int("end_time") != actNow.Unix()+86400 {
		t.Fatalf("28200: %v", b)
	}
	c.send(28211, proto.Message{"num": 1})
	if r := c.waitFor(28211); r.Int("flag") != 0 {
		t.Fatalf("submit while awake: %v", r)
	}
	idx = e.ymTile(t, c, role, 19003)
	c.send(28203, proto.Message{"index": idx, "action": 0})
	c.waitFor(28203)
	if r := c.waitFor(28215); r.Int("type") != 12 || r.Int("combat_time") != 2 || r.Int("buy_time") != 3 || r.Int("last_time") != 86400 || r.Int("index") != idx || r.Int("unit_id") != 97001 {
		t.Fatalf("28215: %v", r)
	}
	// a fight without playing it (the skip box)
	skip := append(fightExt(hero), proto.Message{"type": 4, "val1": 1, "val2": 1})
	c.send(28203, proto.Message{"index": idx, "action": 1, "ext_list": skip})
	if r := c.waitFor(28203); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	res := c.waitFor(28209)
	if res.Int("type") != 12 || res.Int("all_dps") <= 0 || res.Int("result") != 1 || len(res.Arr("reward")) == 0 || res.Int("evt_index") != idx {
		t.Fatalf("28209: %v", res)
	}
	dmg := res.Int("all_dps")
	// a fight with the battle
	c.send(28203, proto.Message{"index": idx, "action": 1, "ext_list": fightExt(hero)})
	c.waitFor(28203)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res2 := c.waitFor(28209)
	c.settle()
	// no fight left: buy one
	c.send(28203, proto.Message{"index": idx, "action": 1, "ext_list": skip})
	if r := c.waitFor(28203); r.Int("code") != 0 {
		t.Fatalf("no fights left: %v", r)
	}
	c.send(28212, proto.Message{"type": 1})
	if r := c.waitFor(28212); r.Int("flag") != 0 {
		t.Fatalf("buy without diamonds: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 500}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	c.send(28212, proto.Message{"type": 1})
	if r := c.waitFor(28212); r.Int("flag") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.send(28224, proto.Message{})
	if r := c.waitFor(28224); r.Int("timer_flag") != 1 || r.Int("gold_flag") != 0 {
		t.Fatalf("28224: %v", r)
	}
	c.send(28213, proto.Message{"type": 1, "num": 10})
	rk := c.waitFor(28213)
	list := rk.Arr("dps_list")
	if len(list) != 1 || list[0].Int("rank") != 1 || list[0].Int("dps") != dmg+res2.Int("all_dps") || list[0].Str("name") != "年兽人" {
		t.Fatalf("28213: %v (%d + %d)", rk, dmg, res2.Int("all_dps"))
	}
	// a day later it sleeps again and the bar starts from nothing
	e.setNow(actNow.Add(25 * time.Hour))
	c.send(28200, proto.Message{})
	if b := c.waitFor(28200); b.Int("val") != 0 || b.Int("flag") != 0 {
		t.Fatalf("asleep: %v", b)
	}
}

func TestYearMonsterSmallMonsterAndGoldMonster(t *testing.T) {
	e, c, role, start, hero := ymEnv(t)
	idx := e.ymTile(t, c, role, 10801)
	c.send(28203, proto.Message{"index": idx, "action": 0})
	c.waitFor(28203)
	if r := c.waitFor(28210); r.Int("unit_id") != 97004 || r.Int("power") <= 0 {
		t.Fatalf("28210: %v", r)
	}
	// the hero hits for more than the monster's hp or not: either way the result comes; a win takes the tile
	skip := append(fightExt(hero), proto.Message{"type": 4, "val1": 1, "val2": 1})
	c.send(28203, proto.Message{"index": idx, "action": 1, "ext_list": skip})
	c.waitFor(28203)
	res := c.waitFor(28209)
	if res.Int("type") != 1 || (res.Int("result") != 1 && res.Int("result") != 2) {
		t.Fatalf("28209: %v", res)
	}
	// the gold monster sleeps until the last two days of the activity
	g := e.ymTile(t, c, role, 19051)
	c.send(28203, proto.Message{"index": g, "action": 0})
	if r := c.waitFor(28203); r.Int("code") != 0 {
		t.Fatalf("gold asleep: %v", r)
	}
	e.setNow(time.Unix(start+19*86400+3600, 0))
	g = e.ymTile(t, c, role, 19053)
	c.send(28224, proto.Message{})
	if r := c.waitFor(28224); r.Int("gold_flag") != 1 {
		t.Fatalf("28224: %v", r)
	}
	c.send(28203, proto.Message{"index": g, "action": 0})
	c.waitFor(28203)
	if r := c.waitFor(28215); r.Int("type") != 13 || r.Int("combat_time") != 4 || r.Int("buy_time") != 6 || r.Int("unit_id") != 97003 {
		t.Fatalf("28215: %v", r)
	}
	c.send(28203, proto.Message{"index": g, "action": 1, "ext_list": skip})
	c.waitFor(28203)
	if r := c.waitFor(28209); r.Int("type") != 13 || r.Int("all_dps") <= 0 {
		t.Fatalf("gold result: %v", r)
	}
	c.settle()
	// the activity is over: the first place gets the mail
	e.setNow(time.Unix(start+20*86400+3600, 0))
	c.send(28200, proto.Message{})
	c.waitFor(28200)
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mails {
		found = found || m.Subject == "巅峰BIG·MOM排名奖励"
	}
	if !found {
		t.Fatalf("no rank mail: %+v", mails)
	}
}

func TestYearMonsterExchangeFacesAndFireworks(t *testing.T) {
	e, c, role, _, _ := ymEnv(t)
	c.send(28217, proto.Message{})
	r := c.waitFor(28217)
	if len(r.Arr("collection")) != 18 || r.Arr("collection")[0].Int("last_num") != 1 || r.Arr("collection")[0].Int("flag") != 1 {
		t.Fatalf("28217: %v", r)
	}
	c.send(28216, proto.Message{"id": 6, "num": 1})
	if r := c.waitFor(28216); r.Int("flag") != 0 {
		t.Fatalf("exchange without sweets: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{80301: 3})
	for i := 0; i < 3; i++ {
		c.send(28216, proto.Message{"id": 6, "num": 1})
		if r := c.waitFor(28216); r.Int("flag") != 1 {
			t.Fatalf("exchange %d: %v", i, r)
		}
	}
	if assets, _ := e.st.RoleAssets(role.RID); assets[22] != 30000 {
		t.Fatalf("assets: %v", assets)
	}
	c.send(28220, proto.Message{})
	if r := c.waitFor(28220); len(r.Arr("face")) != 3 || r.Arr("face")[2].Int("face_id") != 6 {
		t.Fatalf("default faces: %v", r)
	}
	c.send(28219, proto.Message{"face": []proto.Message{{"order": 1, "face_id": 4}}})
	if r := c.waitFor(28219); r.Int("code") != 1 {
		t.Fatalf("28219: %v", r)
	}
	c.send(28220, proto.Message{})
	if r := c.waitFor(28220); len(r.Arr("face")) != 1 || r.Arr("face")[0].Int("face_id") != 4 {
		t.Fatalf("faces: %v", r)
	}
	c.send(28221, proto.Message{})
	if r := c.waitFor(28221); r.Int("code") != 0 {
		t.Fatalf("firework without one: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{80352: 1})
	c.send(28221, proto.Message{})
	if r := c.waitFor(28221); r.Int("code") != 1 || len(r.Arr("reward")) != 1 {
		t.Fatalf("firework: %v", r)
	}
	other, orole := e.enter("ym2", "路过的人")
	_ = orole
	other.send(28200, proto.Message{})
	other.waitFor(28200)
	c.send(28222, proto.Message{})
	if r := c.waitFor(28222); len(r.Arr("player")) != 1 || r.Arr("player")[0].Str("name") != "路过的人" {
		t.Fatalf("28222: %v", r)
	}
}
