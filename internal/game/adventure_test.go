package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func advEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ua", "冒险家")
	c.send(11000, proto.Message{}) // the starter hero is granted on the first look at the hero list
	c.waitFor(11000)
	return e, c, role
}

func (e *env) advState(t *testing.T, role *store.Role) *advState {
	st := &advState{}
	if ok, err := e.st.LoadState(role.RID, advModule, st); err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	return st
}

func advForm(t *testing.T, e *env, c *client, role *store.Role) int64 {
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	c.send(20605, proto.Message{"plist": []proto.Message{{"id": heroes[0].PartnerID}}})
	if r := c.waitFor(20605); r.Int("code") != 1 {
		t.Fatalf("20605: %v", r)
	}
	c.waitFor(20604)
	return heroes[0].PartnerID
}

func TestAdventureIslandAndRooms(t *testing.T) {
	_, c, _ := advEnv(t)
	c.send(20600, proto.Message{})
	b := c.waitFor(20600)
	if b.Int("id") != 1 || b.Int("current_id") != 1 || b.Int("pass_id") != 0 || b.Int("end_time")-b.Int("start_time") != 7*86400 {
		t.Fatalf("20600: %v", b)
	}
	c.send(20602, proto.Message{})
	rooms := c.waitFor(20602).Arr("room_list")
	if len(rooms) != 13 {
		t.Fatalf("rooms: %d", len(rooms))
	}
	open := 0
	for _, r := range rooms {
		if r.Int("status") == roomCanOpen {
			open++
		}
		if r.Int("evt_id") == 0 {
			t.Fatalf("room without an event: %v", r)
		}
	}
	if open != 3 {
		t.Fatalf("%d rooms open at the start", open)
	}
	c.send(20608, proto.Message{"room_id": 4}) // locked until room 1 is over
	if r := c.waitFor(20608); r.Int("code") != 0 {
		t.Fatalf("a locked room opened: %v", r)
	}
}

func TestAdventureGuardFightAndUnlock(t *testing.T) {
	e, c, role := advEnv(t)
	hero := advForm(t, e, c, role)
	c.send(20608, proto.Message{"room_id": 1})
	if r := c.waitFor(20608); r.Int("code") != 1 {
		t.Fatalf("open: %v", r)
	}
	c.send(20620, proto.Message{"room_id": 1, "action": 0})
	if r := c.waitFor(20624); r.Int("hp_per") != 100 {
		t.Fatalf("guard hp: %v", r)
	}
	c.send(20620, proto.Message{"room_id": 1, "action": 1})
	if r := c.waitFor(20620); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatAdventure {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	c.settle()
	st := e.advState(t, role)
	if res.Int("room_id") != 1 || res.Int("combat_type") != combatAdventure {
		t.Fatalf("result: %v", res)
	}
	h := st.heroOf(hero)
	if res.Int("result") == 1 {
		if st.room(1).Status != roomOver || st.Kills != 1 || st.room(4).Status != roomCanOpen || st.room(5).Status != roomCanOpen {
			t.Fatalf("a win did not open the way: %+v", st.Rooms)
		}
	} else if st.room(1).Status != roomOpen || st.room(1).HPPer >= 1000 {
		t.Fatalf("a loss: %+v", st.room(1))
	}
	if h == nil || h.Per > 1000 || res.Int("adventure_end_hp") != h.Per {
		t.Fatalf("hero hp: %+v %v", h, res)
	}
}

func TestAdventureSkillsPotionsAndBoxes(t *testing.T) {
	e, c, role := advEnv(t)
	hero := advForm(t, e, c, role)
	st := e.advState(t, role)
	st.Skills = map[int64]advSkillHold{1: {Num: 2}, 2: {Num: 1}}
	st.Heroes[0].Per = 300
	if err := e.st.SaveState(role.RID, advModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(20607, proto.Message{"skill_id": 1, "val": hero})
	if r := c.waitFor(20607); r.Int("code") != 1 {
		t.Fatalf("heal: %v", r)
	}
	c.settle()
	if st := e.advState(t, role); st.Heroes[0].Per != 800 || st.Skills[1].Num != 1 {
		t.Fatalf("heal result: %+v", st)
	}
	c.send(20611, proto.Message{})
	if r := c.waitFor(20611); len(r.Arr("room_list")) != 3 { // rooms 1-3 are guards that can be fought
		t.Fatalf("guards: %v", r)
	}
	c.send(20607, proto.Message{"skill_id": 2, "val": 2})
	if r := c.waitFor(20607); r.Int("code") != 1 {
		t.Fatalf("kill: %v", r)
	}
	c.settle()
	st = e.advState(t, role)
	if st.room(2).Status != roomOver || st.Kills != 1 || st.room(6).Status != roomCanOpen {
		t.Fatalf("kill result: %+v", st.Rooms)
	}
	c.send(20607, proto.Message{"skill_id": 2, "val": 3}) // no potion left
	if r := c.waitFor(20607); r.Int("code") != 0 {
		t.Fatalf("no potion: %v", r)
	}
	c.send(20636, proto.Message{"num": 1}) // no gems
	if r := c.waitFor(20636); r.Int("code") != 0 {
		t.Fatalf("buy without gems: %v", r)
	}
	// boxes
	c.send(20634, proto.Message{})
	if r := c.waitFor(20634); r.Int("kill_mon") != 1 || len(r.Arr("list")) != 5 || r.Arr("list")[0].Int("status") != 0 {
		t.Fatalf("boxes: %v", r)
	}
	st = e.advState(t, role)
	st.Kills = 5
	e.st.SaveState(role.RID, advModule, st)
	c.send(20635, proto.Message{"id": 1})
	if r := c.waitFor(20635); r.Int("code") != 1 {
		t.Fatalf("take box: %v", r)
	}
	c.send(20635, proto.Message{"id": 1})
	if r := c.waitFor(20635); r.Int("code") != 0 {
		t.Fatalf("box taken twice: %v", r)
	}
}

func TestAdventureRoomsCollectQuizGuessAndNextFloor(t *testing.T) {
	e, c, role := advEnv(t)
	advForm(t, e, c, role)
	c.send(20600, proto.Message{})
	c.waitFor(20600)
	st := e.advState(t, role)
	for _, id := range []int64{1, 2, 3} {
		st.room(id).Status = roomOver
	}
	st.refresh()
	if err := e.st.SaveState(role.RID, advModule, st); err != nil {
		t.Fatal(err)
	}
	// room 4 collects beli
	c.send(20608, proto.Message{"room_id": 4})
	c.waitFor(20608)
	before, _ := e.st.RoleAssets(role.RID)
	c.send(20620, proto.Message{"room_id": 4, "action": 1})
	c.waitFor(20628)
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] <= before[1] || e.advState(t, role).room(4).Status != roomOver {
		t.Fatalf("collect: %v -> %v", before[1], after[1])
	}
	// room 7 is the quiz: three questions, every answer is right
	c.send(20608, proto.Message{"room_id": 7})
	c.waitFor(20608)
	c.send(20620, proto.Message{"room_id": 7, "action": 0})
	if q := c.waitFor(20623); q.Int("ret") != 0 || q.Int("num") != 1 || q.Int("max") != 3 || q.Int("bid") == 0 {
		t.Fatalf("first question: %v", q)
	}
	for i := 0; i < 3; i++ {
		c.send(20620, proto.Message{"room_id": 7, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 1}}})
		q := c.waitFor(20623)
		if (i < 2) != (q.Int("bid") != 0) || q.Int("ret") != 1 {
			t.Fatalf("answer %d: %v", i, q)
		}
		c.waitFor(20620)
	}
	c.settle()
	if e.advState(t, role).room(7).Status != roomOver {
		t.Fatal("the quiz did not finish")
	}
	// room 8 is the guessing game: a bet and a finger
	c.send(20608, proto.Message{"room_id": 8})
	c.waitFor(20608)
	c.send(20620, proto.Message{"room_id": 8, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 1}, {"type": 2, "val": 1}}})
	if g := c.waitFor(20621); g.Int("sel_val") != 1 || g.Int("ret_val") < 1 || g.Int("ret_val") > 3 || len(g.Arr("items")) != 1 {
		t.Fatalf("guess: %v", g)
	}
	c.waitFor(20620)
	// the way on opens only after the guard of room 11
	st = e.advState(t, role)
	if st.room(advDoorRoom).Status != roomLock {
		t.Fatalf("door: %+v", st.room(advDoorRoom))
	}
	c.send(20620, proto.Message{"room_id": advDoorRoom, "action": 1})
	if r := c.waitFor(20620); r.Int("code") != 0 {
		t.Fatalf("door opened early: %v", r)
	}
	st.Boss = true
	st.room(advDoorRoom).Status = roomOver
	e.st.SaveState(role.RID, advModule, st)
	c.send(20620, proto.Message{"room_id": advDoorRoom, "action": 1})
	if r := c.waitFor(20620); r.Int("code") != 1 {
		t.Fatalf("next floor: %v", r)
	}
	if b := c.waitFor(20600); b.Int("id") != 2 || b.Int("pass_id") != 1 {
		t.Fatalf("floor 2: %v", b)
	}
	if rooms := c.waitFor(20602).Arr("room_list"); len(rooms) != 13 || rooms[0].Int("evt_id") != advEventID(2, 1) {
		t.Fatalf("rooms of floor 2: %v", rooms)
	}
}

func TestAdventureShopAndRoundReset(t *testing.T) {
	e, c, role := advEnv(t)
	c.send(20632, proto.Message{})
	shop := c.waitFor(20632).Arr("list")
	if len(shop) != len(advCatalog) {
		t.Fatalf("shop: %v", shop)
	}
	c.send(20633, proto.Message{"id": 1}) // no beli
	if r := c.waitFor(20633); r.Int("code") != 0 {
		t.Fatalf("poor buy: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{1: 100000})
	c.send(20633, proto.Message{"id": 1})
	if r := c.waitFor(20633); r.Int("code") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.send(20633, proto.Message{"id": 1})
	if r := c.waitFor(20633); r.Int("code") != 0 {
		t.Fatalf("bought twice: %v", r)
	}
	// a round over: one box taken of five leaves the buff for taking one box
	st := e.advState(t, role)
	st.PassID, st.BoxTaken = 12, map[int64]bool{1: true}
	st.End = time.Now().Unix() - 1
	e.st.SaveState(role.RID, advModule, st)
	c.send(20600, proto.Message{})
	if b := c.waitFor(20600); b.Int("id") != 1 || b.Int("pass_id") != 12 {
		t.Fatalf("new round: %v", b)
	}
	st = e.advState(t, role)
	if st.LastNum != 1 || len(st.Buffs) != 2 || len(st.BoxTaken) != 0 {
		t.Fatalf("round state: %+v", st)
	}
	c.send(20601, proto.Message{})
	if r := c.waitFor(20601); len(r.Arr("buff_list")) != 2 {
		t.Fatalf("buffs: %v", r)
	}
}

func TestAdventureMineList(t *testing.T) {
	e, c, role := advEnv(t)
	c.send(20600, proto.Message{})
	c.waitFor(20600)
	c.send(20653, proto.Message{})
	if r := c.waitFor(20653); len(r.Arr("floor_list")) != 0 {
		t.Fatalf("floors before clearing 10: %v", r)
	}
	st := e.advState(t, role)
	st.PassID = 20
	e.st.SaveState(role.RID, advModule, st)
	c.send(20653, proto.Message{})
	if r := c.waitFor(20653); len(r.Arr("floor_list")) != 2 {
		t.Fatalf("floors after clearing 20: %v", r)
	}
	c.send(20640, proto.Message{"floor": 10})
	r := c.waitFor(20640)
	if r.Int("floor") != 10 || r.Int("count") != 5 || r.Int("occupy_count") != 0 || len(r.Arr("list")) != 9 {
		t.Fatalf("20640: %v", r)
	}
	for _, m := range r.Arr("list") {
		if m.Int("rid") != 0 || m.Int("mine_id")/1000 != 101 || m.Str("name") == "" {
			t.Fatalf("a mine: %v", m)
		}
	}
	c.send(20641, proto.Message{"floor": 10, "room_id": 1})
	if m := c.waitFor(20641); m.Int("mine_id") != 101011 || m.Int("plunder_count") != 3 || len(m.Arr("items")) != 1 || m.Int("power") != 68945 {
		t.Fatalf("20641: %v", m)
	}
	c.send(20640, proto.Message{"floor": 15}) // there is no such floor
	if r := c.waitFor(20640); len(r.Arr("list")) != 0 {
		t.Fatalf("floor 15: %v", r)
	}
}

func TestAdventureMineFightOccupyAndLeave(t *testing.T) {
	e, c, role := advEnv(t)
	hero := advForm(t, e, c, role)
	c.send(20643, proto.Message{"floor": 10, "room_id": 1}) // floor 10 is not cleared yet
	if r := c.waitFor(20643); r.Int("code") != 0 {
		t.Fatalf("fought an unopened floor: %v", r)
	}
	st := e.advState(t, role)
	st.PassID = 10
	e.st.SaveState(role.RID, advModule, st)
	c.send(20643, proto.Message{"floor": 10, "room_id": 1})
	if r := c.waitFor(20643); r.Int("code") != 1 {
		t.Fatalf("attack: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatMine {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20649)
	if res.Int("floor") != 10 || res.Int("room_id") != 1 || res.Int("count") != 4 {
		t.Fatalf("20649: %v", res)
	}
	if res.Int("result") != 1 {
		t.Skip("the fight was lost; occupation not tested")
	}
	if len(res.Arr("award")) != 1 || res.Int("end_time") <= time.Now().Unix() {
		t.Fatalf("a win pays and offers the mine: %v", res)
	}
	c.send(20646, proto.Message{"floor": 10, "room_id": 1, "pos_info": []proto.Message{{"pos": 1, "id": hero}}})
	if r := c.waitFor(20646); r.Int("flag") != 1 {
		t.Fatalf("occupy: %v", r)
	}
	c.send(20640, proto.Message{"floor": 10})
	l := c.waitFor(20640)
	if l.Int("occupy_count") != 1 || l.Arr("list")[0].Int("rid") != role.RID || l.Arr("list")[0].Int("status") != 2 {
		t.Fatalf("held mine: %v", l)
	}
	c.send(20652, proto.Message{})
	if m := c.waitFor(20652).Arr("mine_list"); len(m) != 1 || m[0].Int("mine_id") != 101011 {
		t.Fatalf("my mines: %v", m)
	}
	c.send(20642, proto.Message{})
	if d := c.waitFor(20642).Arr("defense"); len(d) != 1 || d[0].Int("id") != hero {
		t.Fatalf("defense: %v", d)
	}
	c.send(20646, proto.Message{"floor": 10, "room_id": 3, "pos_info": []proto.Message{{"pos": 1, "id": hero}}}) // no win there
	if r := c.waitFor(20646); r.Int("flag") != 0 {
		t.Fatalf("occupied without winning: %v", r)
	}
	c.send(20643, proto.Message{"floor": 10, "room_id": 1}) // the own mine
	if r := c.waitFor(20643); r.Int("code") != 0 {
		t.Fatalf("attacked the own mine: %v", r)
	}
	c.send(20654, proto.Message{"floor": 10, "room_id": 1, "type": 1}) // 90000 beli, none here
	if r := c.waitFor(20654); r.Int("code") != 0 {
		t.Fatalf("hired without beli: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{1: 200000})
	c.send(20654, proto.Message{"floor": 10, "room_id": 1, "type": 1})
	if r := c.waitFor(20654); r.Int("code") != 1 {
		t.Fatalf("hire: %v", r)
	}
	c.send(20651, proto.Message{"floor": 10, "room_id": 1})
	if r := c.waitFor(20651); r.Int("code") != 1 {
		t.Fatalf("give up: %v", r)
	}
	c.send(20640, proto.Message{"floor": 10})
	if l := c.waitFor(20640); l.Int("occupy_count") != 0 || l.Arr("list")[0].Int("rid") != 0 {
		t.Fatalf("after giving up: %v", l)
	}
}

func TestAdventureMineBoxesAttacksAndHallows(t *testing.T) {
	e, c, role := advEnv(t)
	c.send(20647, proto.Message{})
	if r := c.waitFor(20647); r.Int("had_combat_num") != 0 || r.Int("count") != 5 {
		t.Fatalf("20647: %v", r)
	}
	c.send(20648, proto.Message{"num": 3}) // nothing fought yet
	if r := c.waitFor(20648); r.Int("code") != 0 {
		t.Fatalf("early box: %v", r)
	}
	c.send(20655, proto.Message{}) // no gems
	if r := c.waitFor(20655); r.Int("code") != 0 {
		t.Fatalf("buy without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 500})
	c.send(20655, proto.Message{})
	if r := c.waitFor(20655); r.Int("code") != 1 || r.Int("count") != 6 || r.Int("buy_count") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.send(20659, proto.Message{})
	if r := c.waitFor(20659); r.Int("code") != 1 {
		t.Fatalf("red point: %v", r)
	}
	c.send(20657, proto.Message{})
	if r := c.waitFor(20657); r.Int("code") != 0 {
		t.Fatalf("log red point: %v", r)
	}
	c.send(20656, proto.Message{"rid": 12345, "srvid": "x"})
	if r := c.waitFor(20656); r.Int("floor") != 0 || r.Int("room_id") != 0 {
		t.Fatalf("strike back: %v", r)
	}
	c.send(20658, proto.Message{})
	if r := c.waitFor(20658); len(r.Arr("list")) != 0 {
		t.Fatalf("hallows: %v", r)
	}
	c.send(20660, proto.Message{"floor": 10, "room_id": 1})
	if r := c.waitFor(20660); r.Int("code") != 1 {
		t.Fatalf("20660: %v", r)
	}
}
