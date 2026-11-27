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
	// 20605 answers and pushes before it saves the state; the heartbeat answer means the handler has finished
	c.settle()
	return heroes[0].PartnerID
}

func TestAdventureIslandAndRooms(t *testing.T) {
	_, c, _ := advEnv(t)
	c.send(20600, proto.Message{})
	b := c.waitFor(20600)
	start := b.Int("start_time")
	if b.Int("id") != 1 || b.Int("current_id") != 1 || b.Int("pass_id") != 0 || b.Int("end_time")-start != 2*86400 ||
		(start+8*3600)%86400 != 0 || ((start+8*3600)/86400)%2 != 1 || b.Int("room") != advDoorRoom {
		t.Fatalf("20600: %v", b)
	}
	c.send(20602, proto.Message{})
	rooms := c.waitFor(20602).Arr("room_list")
	if len(rooms) != 25 {
		t.Fatalf("rooms: %d", len(rooms))
	}
	for _, r := range rooms {
		id := r.Int("id")
		off, guard := advGuardRooms[id]
		switch {
		case guard:
			if r.Int("status") != roomOpen || r.Int("evt_id") != advEventID(1, off) {
				t.Fatalf("guard: %v", r)
			}
		case id == advDoorRoom:
			if r.Int("status") != roomLock || r.Int("evt_id") != advEventID(1, advMystery) {
				t.Fatalf("room 13: %v", r)
			}
		default:
			if r.Int("status") != roomLock || r.Int("evt_id") != 0 || r.Int("res_id") == 0 {
				t.Fatalf("a hidden room shows itself: %v", r)
			}
		}
	}
}

// fixedFloor gives floor 1 known contents: every other room holds beli unless set.
func fixedFloor(st *advState, set map[int64]int64) {
	st.buildFloor(1)
	for i := range st.Rooms {
		r := &st.Rooms[i]
		if _, guard := advGuardRooms[r.ID]; guard || r.ID == advDoorRoom {
			continue
		}
		r.Evt = advEventID(1, 7)
		if off, ok := set[r.ID]; ok {
			r.Evt = 0
			if off != 0 {
				r.Evt = advEventID(1, off)
			}
		}
	}
}

func TestAdventureRevealRules(t *testing.T) {
	st := &advState{}
	fixedFloor(st, map[int64]int64{2: 0, 16: 0, 21: 0})
	st.room(7).Status = roomOver
	batches := st.reveal(7)
	// rooms 1, 2 and 6 touch no other guard; 2 is empty and over at once; 3, 8, 11, 12 touch a standing guard
	if len(batches) == 0 || len(batches[0]) != 4 || batches[0][0].Int("id") != 7 || batches[0][1].Int("status") != roomCanOpen || batches[0][1].Int("evt_id") != 0 {
		t.Fatalf("first batch: %v", batches)
	}
	want := map[int64]int64{1: roomOpen, 2: roomOver, 6: roomOpen, 3: roomLock, 8: roomLock, 11: roomLock, 12: roomLock, advDoorRoom: roomLock}
	for id, status := range want {
		if st.room(id).Status != status {
			t.Fatalf("room %d: %d, want %d", id, st.room(id).Status, status)
		}
	}
	if st.PassNum != 3 || st.LastRoom != 6 {
		t.Fatalf("pass_num %d room %d", st.PassNum, st.LastRoom)
	}
	// 17 falls: 16 and 21 are empty and open 22 in turn
	st.room(17).Status = roomOver
	st.reveal(17)
	if st.room(16).Status != roomOver || st.room(21).Status != roomOver || st.room(22).Status != roomOpen || st.room(23).Status != roomLock {
		t.Fatalf("empty rooms: %+v", st.Rooms)
	}
	// the last guards: the mysterious man turns into the boss
	for _, g := range []int64{9, 19} {
		st.room(g).Status = roomOver
		st.reveal(g)
	}
	if door := st.room(advDoorRoom); door.Status != roomOpen || door.Evt != advEventID(1, 0) {
		t.Fatalf("boss: %+v", door)
	}
	if st.PassNum != 21 {
		t.Fatalf("every room revealed: %d", st.PassNum)
	}
	for _, r := range st.Rooms {
		if r.Status == roomLock || r.Status == roomCanOpen {
			t.Fatalf("room %d still hidden", r.ID)
		}
	}
}

func TestAdventureGuardFightAndReveal(t *testing.T) {
	e, c, role := advEnv(t)
	hero := advForm(t, e, c, role)
	c.send(20620, proto.Message{"room_id": 7, "action": 0})
	if r := c.waitFor(20624); r.Int("hp_per") != 100 {
		t.Fatalf("guard hp: %v", r)
	}
	if r := c.waitFor(20620); r.Int("action") != 0 || r.Int("code") != 1 {
		t.Fatalf("guard look: %v", r)
	}
	c.send(20620, proto.Message{"room_id": 7, "action": 1})
	if r := c.waitFor(20620); r.Int("code") != 2 {
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
	if res.Int("room_id") != 7 || res.Int("combat_type") != combatAdventure {
		t.Fatalf("result: %v", res)
	}
	h := st.heroOf(hero)
	if res.Int("result") == 1 {
		if st.room(7).Status != roomOver || st.Kills != 1 || st.room(1).Status < roomOpen || st.room(6).Status < roomOpen || st.room(12).Status != roomLock {
			t.Fatalf("a win did not open the way: %+v", st.Rooms)
		}
	} else if st.room(7).Status != roomOpen || st.room(7).HPPer >= 1000 {
		t.Fatalf("a loss: %+v", st.room(7))
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
	if r := c.waitFor(20611); len(r.Arr("room_list")) != 4 { // the four guards
		t.Fatalf("guards: %v", r)
	}
	c.send(20607, proto.Message{"skill_id": 2, "val": 7})
	if r := c.waitFor(20607); r.Int("code") != 1 {
		t.Fatalf("kill: %v", r)
	}
	c.settle()
	st = e.advState(t, role)
	if st.room(7).Status != roomOver || st.Kills != 1 || st.room(1).Status < roomOpen {
		t.Fatalf("kill result: %+v", st.Rooms)
	}
	c.send(20607, proto.Message{"skill_id": 2, "val": 9}) // no potion left
	if r := c.waitFor(20607); r.Int("code") != 0 {
		t.Fatalf("no potion: %v", r)
	}
	c.send(20636, proto.Message{"num": 1}) // no gems
	if r := c.waitFor(20636); r.Int("code") != 0 {
		t.Fatalf("buy without gems: %v", r)
	}
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
	// id 0 takes every box the kills have opened
	st = e.advState(t, role)
	st.Kills = 20
	e.st.SaveState(role.RID, advModule, st)
	c.send(20635, proto.Message{"id": 0})
	if r := c.waitFor(20635); r.Int("code") != 1 {
		t.Fatalf("take all: %v", r)
	}
	boxes := c.waitFor(20634).Arr("list")
	for i, b := range boxes {
		if want := map[bool]int64{true: 2, false: 0}[i < 4]; b.Int("status") != want {
			t.Fatalf("box %d after taking all: %v", i+1, boxes)
		}
	}
	c.send(20635, proto.Message{"id": 0})
	if r := c.waitFor(20635); r.Int("code") != 0 {
		t.Fatalf("nothing left to take: %v", r)
	}
}

func TestAdventureRoomsCollectQuizNpcBoxAndNextFloor(t *testing.T) {
	e, c, role := advEnv(t)
	advForm(t, e, c, role)
	c.send(20600, proto.Message{})
	c.waitFor(20600)
	st := e.advState(t, role)
	// guard 7 down: 1 collects beli, 2 is the quiz, 6 rock paper scissors; guard 17 down: 11 the npc, 12 the treasure
	fixedFloor(st, map[int64]int64{1: 7, 2: 9, 6: 22, 11: 14, 12: 17})
	for _, g := range []int64{7, 17} {
		st.room(g).Status = roomOver
		st.reveal(g)
	}
	st.Heroes = st.Heroes[:1]
	if err := e.st.SaveState(role.RID, advModule, st); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(20620, proto.Message{"room_id": 1, "action": 1, "ext_list": []proto.Message{{"type": 4, "val": 1}}})
	if tip := c.waitFor(12741); tip.Str("msg") == "" {
		t.Fatalf("tip: %v", tip)
	}
	if b := c.waitFor(20600); b.Int("pass_num") != st.PassNum {
		t.Fatalf("20600 after a room: %v", b)
	}
	if r := c.waitFor(20620); r.Int("code") != 1 {
		t.Fatalf("collect: %v", r)
	}
	c.settle()
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] <= before[1] || e.advState(t, role).room(1).Status != roomOver {
		t.Fatalf("collect: %v -> %v", before[1], after[1])
	}
	// the quiz: a known question answered wrong counts as wrong, one without an answer key counts as right
	st = e.advState(t, role)
	st.room(2).Qs = []int64{50408, 50387, 1}
	e.st.SaveState(role.RID, advModule, st)
	c.send(20620, proto.Message{"room_id": 2, "action": 0})
	if q := c.waitFor(20623); q.Int("ret") != 0 || q.Int("num") != 1 || q.Int("bid") != 50408 {
		t.Fatalf("first question: %v", q)
	}
	c.waitFor(20620)
	for i, step := range []struct{ sel, ret, right, next int64 }{{1, 1, 1, 50387}, {1, 2, 3, 1}, {2, 1, 2, 0}} {
		c.send(20620, proto.Message{"room_id": 2, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": step.sel}}})
		q := c.waitFor(20623)
		if q.Int("ret") != step.ret || q.Int("right") != step.right || q.Int("bid") != step.next {
			t.Fatalf("answer %d: %v", i, q)
		}
		if i == 2 && q.Str("ret_msg") != advQuizEnd[2] {
			t.Fatalf("end: %v", q)
		}
		c.waitFor(20620)
	}
	c.settle()
	if st := e.advState(t, role); st.room(2).Status != roomOver || st.room(2).Right != 2 {
		t.Fatalf("quiz: %+v", st.room(2))
	}
	// the npc asks one question
	c.send(20620, proto.Message{"room_id": 11, "action": 0})
	if q := c.waitFor(20627); q.Int("evt_id") != advEventID(1, 14) {
		t.Fatalf("npc: %v", q)
	}
	c.waitFor(20620)
	c.send(20620, proto.Message{"room_id": 11, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 1}}})
	if r := c.waitFor(20628); len(r.Arr("items")) != 1 {
		t.Fatalf("npc answer: %v", r)
	}
	c.waitFor(20620)
	c.settle()
	if e.advState(t, role).room(11).Status != roomOver {
		t.Fatal("the npc did not finish")
	}
	// the treasure wants a key
	c.send(20620, proto.Message{"room_id": 12, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 0}}})
	if r := c.waitFor(20620); r.Int("code") != 0 || r.Str("msg") != "秘宝钥匙就藏在本层的某个角落噢" {
		t.Fatalf("treasure without a key: %v", r)
	}
	if err := e.st.Exchange(role.RID, nil, nil, nil, map[int64]int64{advKeyItem: 1}); err != nil {
		t.Fatal(err)
	}
	c.send(20620, proto.Message{"room_id": 12, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 0}}})
	if r := c.waitFor(20630); len(r.Arr("items")) != 1 {
		t.Fatalf("treasure: %v", r)
	}
	c.waitFor(20620)
	c.settle()
	// the guessing game
	c.send(20620, proto.Message{"room_id": 6, "action": 1, "ext_list": []proto.Message{{"type": 1, "val": 1}, {"type": 2, "val": 1}}})
	if g := c.waitFor(20621); g.Int("sel_val") != 1 || g.Int("ret_val") < 1 || g.Int("ret_val") > 3 || len(g.Arr("items")) != 1 {
		t.Fatalf("guess: %v", g)
	}
	c.waitFor(20620)
	c.settle()
	// the way on is the boss's room once the boss is down
	c.send(20620, proto.Message{"room_id": advDoorRoom, "action": 1})
	if r := c.waitFor(20620); r.Int("code") != 0 {
		t.Fatalf("door opened early: %v", r)
	}
	st = e.advState(t, role)
	gains := len(st.Gains)
	st.Boss = true
	st.room(advDoorRoom).Evt, st.room(advDoorRoom).Status = 99999, roomOver
	e.st.SaveState(role.RID, advModule, st)
	c.send(20620, proto.Message{"room_id": advDoorRoom, "action": 1})
	if b := c.waitFor(20600); b.Int("id") != 2 || b.Int("pass_id") != 2 || b.Int("pass_num") != 0 || b.Int("room") != advDoorRoom {
		t.Fatalf("floor 2: %v", b)
	}
	if rooms := c.waitFor(20602).Arr("room_list"); len(rooms) != 25 || rooms[6].Int("evt_id") != advEventID(2, 1) {
		t.Fatalf("rooms of floor 2: %v", rooms)
	}
	if got := c.waitFor(20606).Arr("items_list"); gains == 0 || len(got) != gains {
		t.Fatalf("floor result: %v (%d kinds paid)", got, gains)
	}
	if r := c.waitFor(20620); r.Int("code") != 2 {
		t.Fatalf("next floor: %v", r)
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
	c.settle() // 20600 answers before it saves the new round
	st = e.advState(t, role)
	if st.LastNum != 1 || len(st.Buffs) != 2 || len(st.BoxTaken) != 0 || len(st.Rooms) != 25 {
		t.Fatalf("round state: %+v", st)
	}
	c.send(20601, proto.Message{})
	if r := c.waitFor(20601); len(r.Arr("buff_list")) != 2 {
		t.Fatalf("buffs: %v", r)
	}
	// a floor saved by the 13-room version is laid out again
	st.Rooms = st.Rooms[:13]
	e.st.SaveState(role.RID, advModule, st)
	c.send(20602, proto.Message{})
	if rooms := c.waitFor(20602).Arr("room_list"); len(rooms) != 25 {
		t.Fatalf("old floor: %d rooms", len(rooms))
	}
}

func TestAdventureRoundStart(t *testing.T) {
	for _, tc := range []struct{ now, start string }{
		{"2026-11-04T13:02:02+08:00", "2026-11-04T00:00:00+08:00"},
		{"2026-11-05T23:59:59+08:00", "2026-11-04T00:00:00+08:00"},
		{"2026-11-06T00:00:00+08:00", "2026-11-06T00:00:00+08:00"},
		{"2026-10-30T05:06:24+08:00", "2026-10-29T00:00:00+08:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.start)
		if got := advRoundStart(now); !got.Equal(want) {
			t.Fatalf("%s: %s, want %s", tc.now, got, want)
		}
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

// A crystal mine floor is a stop: the original opens the mine window there and 继续冒险 sends 20620 {room 13, action 1},
// which goes on to the next floor without the boss and without a floor result.
func TestAdventureMineFloorContinues(t *testing.T) {
	e, c, role := advEnv(t)
	advForm(t, e, c, role)
	c.send(20600, proto.Message{})
	c.waitFor(20600)
	st := e.advState(t, role)
	st.buildFloor(10)
	st.PassID = 10
	if err := e.st.SaveState(role.RID, advModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(20620, proto.Message{"room_id": 13, "action": 1})
	if b := c.waitFor(20600); b.Int("id") != 11 {
		t.Fatalf("20600 after the mine floor: %v", b)
	}
	c.waitFor(20602)
	if r := c.waitFor(20620); r.Int("code") != 2 {
		t.Fatalf("continue: %v", r)
	}
	if st := e.advState(t, role); st.Floor != 11 || st.PassID != 11 {
		t.Fatalf("floor %d pass %d", st.Floor, st.PassID)
	}
	// an ordinary floor still needs its boss
	c.send(20620, proto.Message{"room_id": 13, "action": 1})
	if r := c.waitFor(20620); r.Int("code") == 2 {
		t.Fatalf("left floor 11 without its boss: %v", r)
	}
}
