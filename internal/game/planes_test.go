package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func plEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("uq", "秘境客")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	e.plSync = func() {
		c.send(23112, proto.Message{}) // the bag: nothing else here asks for it
		c.waitFor(23112)
	}
	c.send(23105, proto.Message{"id": 1})
	if r := c.waitFor(23105); r.Int("flag") != 1 || r.Int("floor") != 1 {
		t.Fatalf("23105: %v", r)
	}
	return e, c, role
}

func (e *env) plState(t *testing.T, role *store.Role) *plState {
	if e.plSync != nil {
		e.plSync()
	}
	st := &plState{}
	if ok, err := e.st.LoadState(role.RID, plModule, st); err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	return st
}

// put places an event on the cell `steps` cells down the road from the start and puts the role next to it.
func (e *env) plPut(t *testing.T, role *store.Role, floor int64, evt int64, power int64) int64 {
	st := e.plState(t, role)
	f := st.floorOf(floor)
	cells := plCells(plMapID(st.Dun, floor))
	var spot int64
	for _, c := range cells[8:] {
		if f.tile(c) == nil {
			spot = c
			break
		}
	}
	f.Tiles = append(f.Tiles, plTile{Index: spot, Evt: evt, Power: power})
	f.Pos = spot
	st.Floor = floor
	if err := e.st.SaveState(role.RID, plModule, st); err != nil {
		t.Fatal(err)
	}
	return spot
}

func TestPlanesLevelAndBase(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("uq", "新人")
	c.send(23105, proto.Message{"id": 1})
	if r := c.waitFor(23105); r.Int("flag") != 0 || r.Str("msg") == "" {
		t.Fatalf("below 30: %v", r)
	}
	e.st.SetRoleLevel(role.RID, 30)
	c.send(23100, proto.Message{})
	b := c.waitFor(23100)
	if b.Int("dun_id") != 0 || len(b.Arr("ids")) != 1 || b.Arr("ids")[0].Int("id") != 1 || b.Int("update_time") <= time.Now().Unix() {
		t.Fatalf("23100: %v", b)
	}
	c.send(23105, proto.Message{"id": 2}) // wants dungeon 1 at full progress, level 45 and power
	if r := c.waitFor(23105); r.Int("flag") != 0 {
		t.Fatalf("dungeon 2: %v", r)
	}
}

func TestPlanesMapAndMoves(t *testing.T) {
	e, c, role := plEnv(t)
	c.send(23106, proto.Message{"floor": 1})
	if r := c.waitFor(23106); r.Int("flag") != 1 {
		t.Fatalf("23106: %v", r)
	}
	m := c.waitFor(23101)
	st := e.plState(t, role)
	if m.Int("floor") != 1 || m.Int("map_id") != 10101 || m.Int("index") != st.Floors[0].Pos || len(m.Arr("tile_list")) < 30 || len(m.Arr("walk_tile")) != 1 {
		t.Fatalf("23101: floor=%d map=%d index=%d tiles=%d walk=%d", m.Int("floor"), m.Int("map_id"), m.Int("index"), len(m.Arr("tile_list")), len(m.Arr("walk_tile")))
	}
	for _, tile := range m.Arr("tile_list") {
		if tile.Int("evtid") == 0 {
			t.Fatalf("an empty event in the list: %v", tile)
		}
	}
	// one step along the road
	x, y := plXY(st.Floors[0].Pos)
	mp := plCfg.maps[10101]
	var next int64
	for _, c := range plCells(10101) {
		cx, cy := plXY(c)
		if c != st.Floors[0].Pos && plDistance(x, y, cx, cy) == 1 && mp.walkable(cx, cy) {
			next = c
			break
		}
	}
	if next == 0 {
		t.Fatal("no neighbour on the road")
	}
	c.send(23103, proto.Message{"index": next})
	if r := c.waitFor(23103); r.Int("code") != 1 || r.Int("index") != next {
		t.Fatalf("step: %v", r)
	}
	c.send(23103, proto.Message{"index": plIndex(1, 1)}) // off the road
	if r := c.waitFor(23103); r.Int("code") != 0 {
		t.Fatalf("off the road: %v", r)
	}
	cells := plCells(10101)
	c.send(23103, proto.Message{"index": cells[len(cells)-1]}) // the far end
	if r := c.waitFor(23103); r.Int("code") != 0 {
		t.Fatalf("far away: %v", r)
	}
	c.settle()
	if st := e.plState(t, role); st.Floors[0].Pos != next || len(st.Floors[0].Walked) != 2 {
		t.Fatalf("position: %+v", st.Floors[0])
	}
}

func TestPlanesEventsSpringBoxBoardPortalAndGroups(t *testing.T) {
	e, c, role := plEnv(t)
	st := e.plState(t, role)
	st.Heroes[0].HP = 40
	e.st.SaveState(role.RID, plModule, st)
	spring := e.plPut(t, role, 1, 1014102, 0) // a spring of group 1
	// two more tiles of the same group next to it: choosing the spring removes them
	st = e.plState(t, role)
	f := st.floorOf(1)
	var mates []int64
	for _, tl := range f.Tiles {
		if plCfg.events[tl.Evt].Group == 1 && tl.Index != spring {
			mates = append(mates, tl.Index)
		}
	}
	if len(mates) == 0 {
		t.Fatal("no group mates on the board")
	}
	c.send(23104, proto.Message{"index": spring, "action": 1})
	hp := c.waitFor(23119).Arr("partners")
	if len(hp) == 0 || hp[0].Int("hp_per") != 70 {
		t.Fatalf("spring: %v", hp)
	}
	if r := c.waitFor(23104); r.Int("code") != 1 {
		t.Fatalf("23104: %v", r)
	}
	upd := c.waitFor(23102)
	gone := 0
	for _, tl := range upd.Arr("tile_list") {
		if tl.Int("evtid") == 0 {
			gone++
		}
	}
	if gone != len(mates) {
		t.Fatalf("group mates not removed: %d of %d: %v", gone, len(mates), upd)
	}
	c.settle()
	c.send(23104, proto.Message{"index": spring, "action": 1}) // already done
	if r := c.waitFor(23104); r.Int("code") != 0 {
		t.Fatalf("done twice: %v", r)
	}
	// a chest
	box := e.plPut(t, role, 1, 2001, 0)
	before, _ := e.st.RoleAssets(role.RID)
	c.send(23104, proto.Message{"index": box, "action": 1})
	c.waitFor(23104)
	c.waitFor(23102)
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] <= before[1] {
		t.Fatalf("a chest gave no beli: %d -> %d", before[1], after[1])
	}
	// a board
	b := e.plPut(t, role, 1, 1011201, 0)
	c.send(23104, proto.Message{"index": b, "action": 0})
	if r := c.waitFor(23108); r.Int("dun_id") != 1 || r.Arr("ext_list")[0].Int("type") != 8 || r.Arr("ext_list")[0].Int("val1") != 1001 {
		t.Fatalf("board: %v", r)
	}
	// the portal to the second map
	pt := e.plPut(t, role, 1, 1011301, 0)
	c.send(23104, proto.Message{"index": pt, "action": 1})
	c.waitFor(23104)
	if m := c.waitFor(23101); m.Int("floor") != 2 || m.Int("map_id") != 10201 {
		t.Fatalf("portal: %v", m)
	}
}

func TestPlanesFightRelicHireAndFirstReward(t *testing.T) {
	e, c, role := plEnv(t)
	hero := e.plState(t, role).Heroes[0].PID
	spot := e.plPut(t, role, 1, 1015101, 500)
	c.send(23104, proto.Message{"index": spot, "action": 0})
	if info := c.waitFor(23109); info.Int("power") != 500 || len(info.Arr("guards")) == 0 {
		t.Fatalf("23109: %v", info)
	}
	c.waitFor(23104)
	c.send(23104, proto.Message{"index": spot, "action": 1, "ext_list": []proto.Message{
		{"type": plExtFormation, "val1": 1}, {"type": plExtOwnHero, "val1": 1, "val2": hero}}})
	if r := c.waitFor(23104); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatPlanesOld {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(23110)
	if res.Int("evt_index") != spot || len(res.Arr("hurt_statistics")) != 1 {
		t.Fatalf("23110: %v", res)
	}
	c.waitFor(23119)
	c.settle()
	st := e.plState(t, role)
	if len(res.Arr("award_list")) > 0 && st.Val != 1 {
		t.Fatalf("a win must count: %+v", st.Val)
	}
	// relics
	rel := e.plPut(t, role, 1, 1014405, 0)
	c.send(23104, proto.Message{"index": rel, "action": 0})
	opts := c.waitFor(23108)
	if len(opts.Arr("ext_list")) != 3 || opts.Arr("ext_list")[0].Int("type") != 7 {
		t.Fatalf("relics: %v", opts)
	}
	c.waitFor(23104)
	pick := opts.Arr("ext_list")[0].Int("val1")
	c.send(23104, proto.Message{"index": rel, "action": 1, "ext_list": []proto.Message{{"type": plExtBuff, "val1": pick}}})
	if r := c.waitFor(23104); r.Int("code") != 1 {
		t.Fatalf("take relic: %v", r)
	}
	c.waitFor(23102)
	c.send(23118, proto.Message{})
	if b := c.waitFor(23118).Arr("buffs"); len(b) != 1 || b[0].Int("buff_id") != pick {
		t.Fatalf("buffs: %v", b)
	}
	// hire
	hire := e.plPut(t, role, 1, 1014104, 0)
	c.send(23104, proto.Message{"index": hire, "action": 0})
	if l := c.waitFor(23107).Arr("load_partner"); len(l) != 4 {
		t.Fatalf("hire list: %v", l)
	}
	c.waitFor(23104)
	c.send(23116, proto.Message{"pos": 3})
	if h := c.waitFor(23116); h.Int("pos") != 3 || h.Int("hp") == 0 {
		t.Fatalf("23116: %v", h)
	}
	c.send(23104, proto.Message{"index": 0, "action": 1, "ext_list": []proto.Message{{"type": plExtHire, "val1": 3}}}) // the hire window sends no index
	c.waitFor(23119)
	if r := c.waitFor(23104); r.Int("code") != 1 {
		t.Fatalf("hire: %v", r)
	}
	c.send(23115, proto.Message{})
	if r := c.waitFor(23115).Arr("partners"); len(r) != 2 || r[1].Int("flag") != 1 {
		t.Fatalf("roster: %v", r)
	}
	// first clear reward
	c.send(23117, proto.Message{"dun_id": 1})
	if r := c.waitFor(23117); r.Int("flag") != 0 {
		t.Fatalf("reward before clearing: %v", r)
	}
	c.settle()
	st = e.plState(t, role)
	st.CanReward["1"] = true
	e.st.SaveState(role.RID, plModule, st)
	c.send(23117, proto.Message{"dun_id": 1})
	if r := c.waitFor(23117); r.Int("flag") != 1 {
		t.Fatalf("first reward: %v", r)
	}
	c.send(23117, proto.Message{"dun_id": 1})
	if r := c.waitFor(23117); r.Int("flag") != 0 {
		t.Fatalf("reward twice: %v", r)
	}
	c.send(23100, proto.Message{})
	if b := c.waitFor(23100); len(b.Arr("reward_ids")) != 1 || b.Int("dun_id") != 1 {
		t.Fatalf("23100 after: %v", b)
	}
}

func TestPlanesRoundReset(t *testing.T) {
	e, c, role := plEnv(t)
	c.settle()
	st := e.plState(t, role)
	st.Taken["1"] = true
	st.Round = 1
	e.st.SaveState(role.RID, plModule, st)
	c.send(23100, proto.Message{})
	b := c.waitFor(23100)
	if b.Int("dun_id") != 0 || len(b.Arr("reward_ids")) != 1 { // the dungeon starts over, what was taken stays
		t.Fatalf("after the reset: %v", b)
	}
}
