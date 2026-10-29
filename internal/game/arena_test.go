package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestArenaChallenge(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "挑战者")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	c.send(20200, proto.Message{})
	me := c.waitFor(20200)
	if me.Int("score") != 1000 || me.Int("can_combat_num") != 3 || me.Int("rank") != 1 || me.Int("end_time") <= me.Int("start_time") {
		t.Fatalf("standing: %v", me)
	}
	c.send(20201, proto.Message{})
	list := c.waitFor(20201).Arr("f_list")
	if len(list) != 3 || len(list[0].Arr("p_list")) == 0 || list[0].Int("status") != 0 {
		t.Fatalf("list: %v", list)
	}
	rid := list[0].Int("rid")
	c.send(20202, proto.Message{"rid": rid, "srv_id": "symlf_1"})
	if d := c.waitFor(20202); d.Int("rid") != rid || len(d.Arr("p_list")) == 0 {
		t.Fatalf("detail: %v", d)
	}
	c.send(20203, proto.Message{"rid": 12345, "srv_id": "symlf_1", "is_auto": 0})
	if r := c.waitFor(20203); r.Int("code") != 0 {
		t.Fatalf("stranger: %v", r)
	}
	c.send(20203, proto.Message{"rid": rid, "srv_id": "symlf_1", "is_auto": 0})
	if r := c.waitFor(20203); r.Int("code") != 1 {
		t.Fatalf("challenge: %v", r)
	}
	if m := c.waitFor(20000); m.Int("combat_type") != combatArena {
		t.Fatalf("20000: %v", m)
	}
	intro := c.waitFor(20027)
	groups := map[int64]int{}
	for _, o := range intro.Arr("objects") {
		groups[o.Int("group")]++
	}
	if intro.Int("combat_type") != combatArena || groups[1] < 2 || groups[2] < 1 {
		t.Fatalf("intro: %v", groups)
	}
	c.send(20062, proto.Message{}) // skip: the result comes at once
	c.waitFor(20062)
	res := c.waitFor(20210)
	won := res.Int("result") == 1
	if res.Int("result") != 1 && res.Int("result") != 2 || res.Str("tar_name") == "" || len(res.Arr("items")) != 1 {
		t.Fatalf("result: %v", res)
	}
	if won && (res.Int("score") <= 1000 || res.Int("get_score") <= 0) || !won && (res.Int("score") != 1000 || res.Int("get_score") != 0) {
		t.Fatalf("score after the fight: %v", res)
	}
	if won { // a loss leaves the list alone (capture original-2026-10-28, 22-arena)
		upd := c.waitFor(20201)
		if upd.Int("type") != 1 || len(upd.Arr("f_list")) != 1 || upd.Arr("f_list")[0].Int("status") != 1 {
			t.Fatalf("slot update: %v", upd)
		}
	}
	c.send(20203, proto.Message{"rid": rid, "srv_id": "symlf_1", "is_auto": 0}) // fought already
	if r := c.waitFor(20203); r.Int("code") != 0 {
		t.Fatalf("same opponent twice: %v", r)
	}
	c.send(20200, proto.Message{})
	if me := c.waitFor(20200); me.Int("can_combat_num") != 2 {
		t.Fatalf("one used: %v", me)
	}
	c.send(20222, proto.Message{})
	if l := c.waitFor(20222).Arr("log_list"); len(l) != 1 || l[0].Int("type") != 1 {
		t.Fatalf("log: %v", l)
	}
	c.send(20206, proto.Message{}) // a new list
	if r := c.waitFor(20206); r.Int("code") != 1 {
		t.Fatalf("refresh: %v", r)
	}
	if l := c.waitFor(20201).Arr("f_list"); len(l) != 3 || l[0].Int("status") != 0 {
		t.Fatalf("refreshed list: %v", l)
	}
	c.send(20206, proto.Message{}) // too soon
	if r := c.waitFor(20206); r.Int("code") != 0 {
		t.Fatalf("refresh cool-down: %v", r)
	}
}

func TestArenaTicketsChestsAndRank(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "挑战者")
	b, rb := e.enter("ub", "对手乙")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	// the second role gets a standing so it can be matched with the first
	b.send(20200, proto.Message{})
	b.waitFor(20200)
	b.send(11000, proto.Message{})
	b.waitFor(11000)
	c.send(20201, proto.Message{})
	var real bool
	for _, o := range c.waitFor(20201).Arr("f_list") {
		real = real || o.Int("rid") == rb.RID
	}
	if !real {
		t.Fatal("the other role is not on the list")
	}
	c.send(20221, proto.Message{})
	if r := c.waitFor(20221); len(r.Arr("rank_list")) != 2 || r.Int("rank") != 1 && r.Int("rank") != 2 {
		t.Fatalf("ranking: %v", r)
	}
	c.send(20220, proto.Message{})
	if r := c.waitFor(20220); len(r.Arr("rank_list")) != 2 {
		t.Fatalf("statues: %v", r)
	}
	// tickets cost gems
	c.send(20207, proto.Message{"num": 2})
	if r := c.waitFor(20207); r.Int("code") != 0 {
		t.Fatalf("no gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(20207, proto.Message{"num": 2})
	if r := c.waitFor(20207); r.Int("code") != 1 {
		t.Fatalf("buy tickets: %v", r)
	}
	items, err := e.st.RoleItems(role.RID)
	if err != nil || items[arenaTicket] != 2 {
		t.Fatalf("tickets: %v %v", items, err)
	}
	// a chest needs the challenges
	c.send(20209, proto.Message{"num": 10})
	if r := c.waitFor(20209); r.Int("code") != 0 {
		t.Fatalf("chest too early: %v", r)
	}
	a, _ := e.st.ArenaPlayerOf(role.RID, 1000, 1)
	a.Battles = 10
	if err := e.st.SaveArenaPlayer(a); err != nil {
		t.Fatal(err)
	}
	c.send(20208, proto.Message{})
	if r := c.waitFor(20208); r.Int("had_combat_num") != 10 || len(r.Arr("num_list")) != 0 {
		t.Fatalf("chest state: %v", r)
	}
	c.send(20209, proto.Message{"num": 10})
	// the notice of the gain and the chest list come before the answer (capture original-2026-10-28, 22-arena)
	if n := c.waitFor(12742).Arr("asset_list"); len(n) != 1 || n[0].Int("bid") != 10002 {
		t.Fatalf("chest notice: %v", n)
	}
	if l := c.waitFor(20208); len(l.Arr("num_list")) != 1 {
		t.Fatalf("chest list after: %v", l)
	}
	if r := c.waitFor(20209); r.Int("code") != 1 {
		t.Fatalf("chest: %v", r)
	}
	c.send(20209, proto.Message{"num": 10})
	if r := c.waitFor(20209); r.Int("code") != 0 {
		t.Fatalf("chest twice: %v", r)
	}
}

func TestArenaRealOpponentLosesScore(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "挑战者")
	b, rb := e.enter("ub", "对手乙")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	b.send(20200, proto.Message{})
	b.waitFor(20200)
	b.send(11000, proto.Message{})
	b.waitFor(11000)
	c.send(20203, proto.Message{"rid": rb.RID, "srv_id": "symlf_1", "is_auto": 0})
	if r := c.waitFor(20203); r.Int("code") != 1 {
		t.Fatalf("challenge a real role: %v", r)
	}
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	res := c.waitFor(20210)
	won := res.Int("result") == 1
	theirs, err := e.st.ArenaPlayerOf(rb.RID, 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if won && theirs.Score != 1000+res.Int("lose_score") || !won && theirs.Score != 1000 {
		t.Fatalf("their score %d after %v", theirs.Score, res)
	}
	b.send(20222, proto.Message{})
	if l := b.waitFor(20222).Arr("log_list"); len(l) != 1 || l[0].Int("type") != 2 || l[0].Str("name") != "挑战者" {
		t.Fatalf("their log: %v", l)
	}
}

// A challenge answers in the original's order: the backpack when a ticket paid (the original sends the one changed item as 10512,
// we send the whole bag), the buff list, then the answer (capture original-2026-10-28, 22-arena).
func TestArenaChallengeFrameOrder(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "挑战者")
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	c.send(20201, proto.Message{})
	list := c.waitFor(20201).Arr("f_list")
	a, _ := e.st.ArenaPlayerOf(role.RID, 1000, 1)
	a.FreeUsed = arenaFreeCombats // the free ones are gone: a ticket pays
	if err := e.st.SaveArenaPlayer(a); err != nil {
		t.Fatal(err)
	}
	e.grantAssets(c, role, map[int64]int64{3: 100})
	c.send(20207, proto.Message{"num": 1})
	c.waitFor(20207)
	c.send(20203, proto.Message{"rid": list[0].Int("rid"), "srv_id": "symlf_1", "is_auto": 0})
	var order []uint16
	for i := 0; i < 40; i++ {
		cmd, _ := c.recvNext()
		if cmd == 10500 || cmd == 13011 || cmd == 20203 {
			order = append(order, cmd)
		}
		if cmd == 20203 {
			break
		}
	}
	want := []uint16{10500, 13011, 20203}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// The three points the captures gave: 1000 gave 20, 1107 gave 22, 985 gave 19 (and the loser paid 14).
func TestArenaGainPoints(t *testing.T) {
	for theirs, want := range map[int64]int64{1000: 20, 1107: 22, 985: 19} {
		if got := arenaGain(1100, theirs); got != want {
			t.Fatalf("against %d: %d, want %d", theirs, got, want)
		}
	}
	if arenaLoss(19) != 14 || arenaLoss(20) != 15 {
		t.Fatal("the loser's share")
	}
}
