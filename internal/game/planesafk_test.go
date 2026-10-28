package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func pfEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("up", "探险者")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{}) // the starter hero
	c.waitFor(11000)
	c.send(28601, proto.Message{})
	if r := c.waitFor(28601); r.Int("flag") != 1 {
		t.Fatalf("28601: %v", r)
	}
	return e, c, role
}

func (e *env) pfState(t *testing.T, role *store.Role) *pfState {
	st := &pfState{}
	// the handler saves its state after it answered, so the first look may come early
	for i := 0; ; i++ {
		ok, err := e.st.LoadState(role.RID, pfModule, st)
		if err == nil && ok {
			return st
		}
		if err != nil || i == 50 {
			t.Fatalf("state: %v %v", ok, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// setTile makes a tile of the board hold an event.
func (e *env) setTile(t *testing.T, role *store.Role, line, index, evt int64) {
	st := e.pfState(t, role)
	tile := st.tile(line, index)
	tile.Evt = evt
	if et := pfCfg.events[evt].Type; et == pfEvtMonster || et == pfEvtGuard {
		tile.Power = 500
	}
	if err := e.st.SaveState(role.RID, pfModule, st); err != nil {
		t.Fatal(err)
	}
}

func TestPlanesafkNeedsLevel30AndBuildsTheBoard(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("up", "新人")
	c.send(28601, proto.Message{})
	if r := c.waitFor(28601); r.Int("flag") != 0 || r.Str("msg") == "" {
		t.Fatalf("below 30: %v", r)
	}
	e.st.SetRoleLevel(role.RID, 30)
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	c.send(28601, proto.Message{})
	c.waitFor(28601)
	c.send(28603, proto.Message{})
	b := c.waitFor(28603)
	if b.Int("floor") != 1 || b.Int("line") != 1 || b.Int("index") != 3 || b.Int("map_id") != 2001 || len(b.Arr("tile_list")) != 1+9*5+1 {
		t.Fatalf("board: floor=%d line=%d index=%d tiles=%d", b.Int("floor"), b.Int("line"), b.Int("index"), len(b.Arr("tile_list")))
	}
	for _, tile := range b.Arr("tile_list") {
		if tile.Int("line") == 11 && tile.Int("evt_id") != 711101 {
			t.Fatalf("boss line: %v", tile)
		}
		if tile.Int("line") > 1 && tile.Int("line") < 11 && tile.Int("evt_id") == 0 {
			t.Fatalf("an empty tile: %v", tile)
		}
	}
	c.send(28602, proto.Message{})
	if r := c.waitFor(28602); r.Int("update_time") <= time.Now().Unix() {
		t.Fatalf("28602: %v", r)
	}
	c.send(28613, proto.Message{})
	if r := c.waitFor(28613).Arr("partners"); len(r) != 1 || r[0].Int("hp_per") != 100 || r[0].Int("flag") != 0 {
		t.Fatalf("roster: %v", r)
	}
	c.send(28612, proto.Message{})
	if f := c.waitFor(28612); len(f.Arr("pos_info")) != 1 {
		t.Fatalf("default formation: %v", f)
	}
}

func TestPlanesafkStepsSpringCrossAndMoves(t *testing.T) {
	e, c, role := pfEnv(t)
	st := e.pfState(t, role)
	st.Heroes[0].HP = 40
	e.st.SaveState(role.RID, pfModule, st)
	e.setTile(t, role, 2, 1, pfEvtID(1, 1, 2, 3))                                                   // a spring
	c.send(28600, proto.Message{"line": 2, "index": 5, "action": 1, "ext_list": []proto.Message{}}) // too far
	if r := c.waitFor(28600); r.Int("code") != 0 {
		t.Fatalf("far tile: %v", r)
	}
	c.send(28600, proto.Message{"line": 3, "index": 2, "action": 1}) // a line ahead
	if r := c.waitFor(28600); r.Int("code") != 0 {
		t.Fatalf("skipped a line: %v", r)
	}
	e.setTile(t, role, 2, 2, pfEvtID(1, 1, 2, 3))
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 1, "ext_list": []proto.Message{}})
	if n := c.waitFor(12741); n.Str("msg") != "使用成功，泉水从眼前消失..." {
		t.Fatalf("spring notice: %v", n)
	}
	hp := c.waitFor(28609).Arr("partners")
	if len(hp) != 1 || hp[0].Int("hp_per") != 70 { // the spring gives 30 per cent, and only the heroes that changed are listed
		t.Fatalf("hp after the spring: %v", hp)
	}
	upd := c.waitFor(28606) // the tiles come before the answer (capture original-2026-10-27)
	if upd.Int("line") != 2 || upd.Int("index") != 2 {
		t.Fatalf("28606: %v", upd)
	}
	if r := c.waitFor(28600); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("spring: %v", r)
	}
	hidden := 0
	for _, tile := range upd.Arr("tile_list") {
		if tile.Int("line") == 2 && tile.Int("is_hide") == 1 {
			hidden++
		}
	}
	if hidden != 4 {
		t.Fatalf("other tiles of the line: %d hidden", hidden)
	}
	// the next line: index 3 is next to index 2; index 5 is not
	c.send(28600, proto.Message{"line": 3, "index": 5, "action": 1})
	if r := c.waitFor(28600); r.Int("code") != 0 {
		t.Fatalf("not adjacent: %v", r)
	}
	e.setTile(t, role, 3, 3, pfEvtID(1, 1, 3, 4)) // a cross: nobody fell, so the weakest is made whole
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 1})
	if hp := c.waitFor(28609).Arr("partners"); hp[0].Int("hp_per") != 100 {
		t.Fatalf("cross: %v", hp)
	}
}

func TestPlanesafkFightRelicAndBoss(t *testing.T) {
	e, c, role := pfEnv(t)
	hero := e.pfState(t, role).Heroes[0].PID
	e.setTile(t, role, 2, 2, pfEvtID(1, 1, 2, 1))
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 0})
	info := c.waitFor(28607)
	// the strength is in guards_power; power is 0 (capture original-2026-10-27)
	if info.Int("power") != 0 || info.Int("guards_power") <= 0 || len(info.Arr("guards")) == 0 || info.Str("name") == "" {
		t.Fatalf("28607: %v", info)
	}
	c.waitFor(28600)
	fight := proto.Message{"line": 2, "index": 2, "action": 1, "ext_list": []proto.Message{
		{"type": pfExtFormation, "val1": 1, "val2": 0}, {"type": pfExtOwnHero, "val1": 1, "val2": hero}}}
	c.send(28600, fight)
	// the line's other tiles are hidden before the fight starts, in a 28606 of their own
	if h := c.waitFor(28606); len(h.Arr("tile_list")) == 0 || h.Arr("tile_list")[0].Int("is_hide") != 1 || h.Int("line") != 1 {
		t.Fatalf("hide at the start: %v", h)
	}
	if r := c.waitFor(28600); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatPlanes {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(28608)
	if res.Int("line") != 2 || res.Int("index") != 2 || len(res.Arr("hurt_statistics")) != 1 {
		t.Fatalf("28608: %v", res)
	}
	c.waitFor(28609)
	if res.Int("result") != 1 {
		t.Skip("the fight was lost; relic not tested")
	}
	if len(res.Arr("award_list")) == 0 {
		t.Fatalf("a win pays: %v", res)
	}
	// the tile became the relic event and the role stayed put (capture original-2026-10-27); asking the tile shows the options
	upd := c.waitFor(28606)
	if upd.Int("line") != 1 {
		t.Fatalf("the role moved: %v", upd)
	}
	for _, tile := range upd.Arr("tile_list") {
		if tile.Int("line") == 2 && tile.Int("index") == 2 && tile.Int("evt_id") < 3021 {
			t.Fatalf("the tile is no relic: %v", tile)
		}
	}
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 0})
	opts := c.waitFor(28614)
	if len(opts.Arr("ext_list")) != 3 || opts.Int("line") != 2 {
		t.Fatalf("relic options: %v", opts)
	}
	c.waitFor(28600)
	pick := opts.Arr("ext_list")[1].Int("val1")
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 1, "ext_list": []proto.Message{{"type": pfExtBuff, "val1": pick, "val2": 0}}})
	moved := c.waitFor(28606) // the role steps onto the tile before the answer, whose msg is empty
	if moved.Int("line") != 2 || moved.Int("index") != 2 {
		t.Fatalf("not moved: %v", moved)
	}
	if r := c.waitFor(28600); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("choose relic: %v", r)
	}
	c.send(28620, proto.Message{})
	if b := c.waitFor(28620).Arr("buffs"); len(b) != 1 || b[0].Int("buff_id") != pick {
		t.Fatalf("relics: %v", b)
	}
	c.send(28625, proto.Message{})
	if l := c.waitFor(28625).Arr("item_list"); len(l) == 0 {
		t.Fatalf("loot: %v", l)
	}
	// the boss: wins mark the floor cleared, give points and open the next floor
	e.setTile(t, role, 11, 3, pfEvtID(1, 1, 11, 1))
	st := e.pfState(t, role)
	st.Line, st.Index = 10, 2
	st.Chosen = map[string]int64{"10": 2}
	e.st.SaveState(role.RID, pfModule, st)
	c.send(28600, proto.Message{"line": 11, "index": 3, "action": 1, "ext_list": []proto.Message{
		{"type": pfExtFormation, "val1": 1}, {"type": pfExtOwnHero, "val1": 1, "val2": hero}}})
	c.waitFor(28600)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	boss := c.waitFor(28608)
	c.waitFor(28609)
	c.settle()
	st = e.pfState(t, role)
	if boss.Int("result") == 1 && (!st.Cleared["1"] || st.Points != 100) {
		t.Fatalf("boss won but state says %+v", st)
	}
}

func TestPlanesafkLeaseShopAndFloors(t *testing.T) {
	e, c, role := pfEnv(t)
	e.setTile(t, role, 2, 2, pfEvtID(1, 1, 2, 5))
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 0})
	lease := c.waitFor(28622)
	if len(lease.Arr("planes_load_partner")) != 4 {
		t.Fatalf("hire list: %v", lease)
	}
	c.waitFor(28600)
	c.send(28623, proto.Message{"pos": 2})
	if h := c.waitFor(28623); h.Int("pos") != 2 || h.Int("bid") == 0 || h.Int("hp") == 0 {
		t.Fatalf("28623: %v", h)
	}
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 1, "ext_list": []proto.Message{{"type": pfExtHire, "val1": 9}}}) // no such place
	if r := c.waitFor(28600); r.Int("code") != 0 {
		t.Fatalf("hired nobody: %v", r)
	}
	c.send(28600, proto.Message{"line": 2, "index": 2, "action": 1, "ext_list": []proto.Message{{"type": pfExtHire, "val1": 2}}})
	// the notice, the moved role, the whole roster (the hired hero has flag 1) and then the answer (capture original-2026-10-27)
	if n := c.waitFor(12741); n.Str("msg") == "" {
		t.Fatalf("notice: %v", n)
	}
	if mv := c.waitFor(28606); mv.Int("line") != 2 || mv.Int("index") != 2 {
		t.Fatalf("not moved: %v", mv)
	}
	if r := c.waitFor(28613).Arr("partners"); len(r) != 2 || r[1].Int("flag") != 1 {
		t.Fatalf("roster with a hired hero: %v", r)
	}
	if r := c.waitFor(28600); r.Int("code") != 1 || r.Str("msg") != "" {
		t.Fatalf("hire answer: %v", r)
	}
	// the merchant
	e.setTile(t, role, 3, 3, pfEvtID(1, 1, 3, 6))
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 0})
	sh := c.waitFor(28621)
	if len(sh.Arr("item_list")) != 8 || sh.Int("is_select") != 0 {
		t.Fatalf("shop: %v", sh)
	}
	c.waitFor(28600)
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 1, "ext_list": []proto.Message{{"type": pfExtShopEnter}}})
	if sh := c.waitFor(28621); sh.Int("is_select") != 1 {
		t.Fatalf("entered: %v", sh)
	}
	c.waitFor(28600)
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 1, "ext_list": []proto.Message{{"type": pfExtShopBuy, "val1": 1}}}) // no gems
	if r := c.waitFor(28600); r.Int("code") != 0 {
		t.Fatalf("bought without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 5000})
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 1, "ext_list": []proto.Message{{"type": pfExtShopBuy, "val1": 1}}})
	if sh := c.waitFor(28621); sh.Arr("item_list")[0].Int("is_flag") != 1 {
		t.Fatalf("bought: %v", sh)
	}
	c.waitFor(28600)
	c.send(28600, proto.Message{"line": 3, "index": 3, "action": 1, "ext_list": []proto.Message{{"type": pfExtShopLeave}}})
	if upd := c.waitFor(28606); upd.Int("line") != 3 {
		t.Fatalf("after the merchant: %v", upd)
	}
	c.waitFor(28600)
	// floors: the next floor needs the boss down
	c.send(28604, proto.Message{"floor": 2, "difficulty": 1})
	if r := c.waitFor(28604); r.Int("flag") != 0 {
		t.Fatalf("next floor before the boss: %v", r)
	}
	c.settle()
	st := e.pfState(t, role)
	st.Cleared["1"] = true
	e.st.SaveState(role.RID, pfModule, st)
	c.send(28605, proto.Message{"floor": 1})
	if r := c.waitFor(28605); r.Int("flag") != 1 {
		t.Fatalf("floor chest: %v", r)
	}
	c.send(28605, proto.Message{"floor": 1})
	if r := c.waitFor(28605); r.Int("flag") != 0 {
		t.Fatalf("chest twice: %v", r)
	}
	c.send(28604, proto.Message{"floor": 2, "difficulty": 1})
	if r := c.waitFor(28604); r.Int("flag") != 1 {
		t.Fatalf("next floor: %v", r)
	}
	if b := c.waitFor(28603); b.Int("floor") != 2 || b.Int("map_id") != 2002 || b.Int("line") != 1 {
		t.Fatalf("floor 2: %v", b)
	}
	c.send(28604, proto.Message{"floor": 3, "difficulty": 2}) // not cleared yet anyway
	if r := c.waitFor(28604); r.Int("flag") != 0 {
		t.Fatalf("hard floor: %v", r)
	}
}

func TestPlanesafkWarOrder(t *testing.T) {
	e, c, role := pfEnv(t)
	c.send(28616, proto.Message{})
	o := c.waitFor(28616)
	if o.Int("period") != 1 || o.Int("win_count") != 0 || o.Int("lev") != 1 || len(o.Arr("list")) == 0 {
		t.Fatalf("28616: %v", o)
	}
	c.send(28617, proto.Message{"id": 1}) // level 1 needs 0 points: free
	if r := c.waitFor(28617); r.Int("flag") != 1 {
		t.Fatalf("level 1: %v", r)
	}
	c.send(28617, proto.Message{"id": 1})
	if r := c.waitFor(28617); r.Int("flag") != 0 {
		t.Fatalf("level 1 twice: %v", r)
	}
	c.send(28617, proto.Message{"id": 2}) // 400 points
	if r := c.waitFor(28617); r.Int("flag") != 0 {
		t.Fatalf("level 2 early: %v", r)
	}
	c.settle()
	st := e.pfState(t, role)
	st.Points = 450
	e.st.SaveState(role.RID, pfModule, st)
	c.send(28617, proto.Message{"id": 2})
	if r := c.waitFor(28617); r.Int("flag") != 1 {
		t.Fatalf("level 2: %v", r)
	}
	c.send(28616, proto.Message{})
	if o := c.waitFor(28616); o.Int("lev") != 2 || o.Arr("list")[1].Int("award_status") != 1 {
		t.Fatalf("after claims: %v", o)
	}
	c.send(28619, proto.Message{})
	if r := c.waitFor(28619); r.Int("is_pop") != 0 {
		t.Fatalf("28619: %v", r)
	}
}
