package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func expEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ux", "远征者")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	return e, c, role, heroes[0].PartnerID
}

func (e *env) expState(t *testing.T, role *store.Role) *expState {
	st := &expState{}
	if _, err := e.st.LoadState(role.RID, expModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (e *env) expSave(t *testing.T, role *store.Role, st *expState) {
	if err := e.st.SaveState(role.RID, expModule, st); err != nil {
		t.Fatal(err)
	}
}

func expTeam(hero int64) proto.Message {
	return proto.Message{"formation_type": 1, "hallows_id": 0, "pos_info": []proto.Message{{"pos": 1, "owner_id": 0, "owner_srv_id": "", "id": hero}}}
}

func TestExpeditionDifficultyGates(t *testing.T) {
	_, c, _, hero := expEnv(t)
	c.send(24400, proto.Message{})
	b := c.waitFor(24400)
	if b.Int("guard_id") != 1 || b.Int("difficulty") != 0 || b.Int("max_difficulty") != 0 || len(b.Arr("reward")) != 0 {
		t.Fatalf("24400: %v", b)
	}
	c.send(24403, expTeam(hero)) // no difficulty yet
	if r := c.waitFor(24403); r.Int("code") != 0 {
		t.Fatalf("fight before choosing: %v", r)
	}
	c.send(24412, proto.Message{"difficulty": 2}) // the previous one is not cleared
	if r := c.waitFor(24412); r.Int("code") != 0 {
		t.Fatalf("hard too early: %v", r)
	}
	c.send(24412, proto.Message{"difficulty": 1})
	if r := c.waitFor(24412); r.Int("code") != 1 {
		t.Fatalf("choose: %v", r)
	}
	if b := c.waitFor(24400); b.Int("difficulty") != 1 || b.Int("guard_id") != 1 || len(b.Arr("rewards")) != 2 {
		t.Fatalf("24400 after: %v", b)
	}
	c.send(24412, proto.Message{"difficulty": 1}) // once a day
	if r := c.waitFor(24412); r.Int("code") != 0 {
		t.Fatalf("choose twice: %v", r)
	}
}

func TestExpeditionFloorsFightsAndBoxes(t *testing.T) {
	e, c, role, hero := expEnv(t)
	c.send(24412, proto.Message{"difficulty": 1})
	c.waitFor(24412)
	c.waitFor(24400)
	c.send(24401, proto.Message{"id": 1})
	f := c.waitFor(24401)
	if f.Int("id") != 1 || len(f.Arr("guards")) == 0 || len(f.Arr("rewards")) != 2 || f.Int("status") != 0 || f.Int("power") <= 0 {
		t.Fatalf("24401: %v", f)
	}
	c.send(24402, proto.Message{"id": 4}) // the box before the floors are cleared
	if r := c.waitFor(24402); r.Int("code") != 0 {
		t.Fatalf("early box: %v", r)
	}
	before, _ := e.st.RoleAssets(role.RID)
	c.send(24403, expTeam(hero))
	if r := c.waitFor(24403); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatExpedit {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	if res.Int("result") != 1 {
		c.settle()
		if st := e.expState(t, role); st.Guard > 1 {
			t.Fatalf("a lost fight must not advance: %+v", st)
		}
		t.Skip("the first floor was lost")
	}
	if clr := c.waitFor(24414); clr.Int("guard_id") != 1 || clr.Int("floor_id") != 1 || len(clr.Arr("rewards")) != 2 {
		t.Fatalf("24414: %v", clr)
	}
	c.waitFor(24400)
	c.settle()
	st := e.expState(t, role)
	after, _ := e.st.RoleAssets(role.RID)
	if after[1] <= before[1] || st.Guard != 2 || st.Got["1"] == 0 {
		t.Fatalf("a floor pays: %v -> %v, %+v", before[1], after[1], st)
	}
	c.send(24401, proto.Message{"id": 1})
	if f := c.waitFor(24401); f.Int("status") != 2 {
		t.Fatalf("a cleared floor: %v", f)
	}
	// the box after the third floor
	st.Guard = 5
	e.expSave(t, role, st)
	c.send(24402, proto.Message{"id": 4})
	if r := c.waitFor(24402); r.Int("code") != 1 {
		t.Fatalf("box: %v", r)
	}
	c.send(24402, proto.Message{"id": 4})
	if r := c.waitFor(24402); r.Int("code") != 0 {
		t.Fatalf("box twice: %v", r)
	}
	c.send(24402, proto.Message{"id": 8})
	if r := c.waitFor(24402); r.Int("code") != 0 {
		t.Fatalf("a box not reached: %v", r)
	}
	c.send(24402, proto.Message{"id": 1}) // not a box
	if r := c.waitFor(24402); r.Int("code") != 0 {
		t.Fatalf("a floor as a box: %v", r)
	}
	c.send(24400, proto.Message{})
	if b := c.waitFor(24400); len(b.Arr("reward")) != 1 || b.Arr("reward")[0].Int("reward_id") != 4 {
		t.Fatalf("24400: %v", b)
	}
}

func TestExpeditionPointsAndNext(t *testing.T) {
	for floor, want := range map[int64]int64{1: 1, 3: 3, 4: 5, 6: 7, 7: 9, 15: 19} {
		if got := expPointID(floor); got != want {
			t.Fatalf("point of floor %d: %d, want %d", floor, got, want)
		}
	}
	if expNext(3) != 5 || expNext(1) != 2 || expNext(19) != 21 {
		t.Fatalf("next: %d %d %d", expNext(3), expNext(1), expNext(19))
	}
}

func TestExpeditionSweepsYesterdaysFloors(t *testing.T) {
	e, c, role, _ := expEnv(t)
	yesterday := time.Unix(0, e.clk.Load()).AddDate(0, 0, -1)
	// yesterday: the easy difficulty, 14 floors cleared
	e.expSave(t, role, &expState{Day: dayKey(yesterday), Diff: 1, Guard: expPointID(14) + 1})
	before, _ := e.st.RoleAssets(role.RID)
	c.send(24412, proto.Message{"difficulty": 2}) // another difficulty: no sweep
	if r := c.waitFor(24412); r.Int("code") != 0 {
		t.Fatalf("hard: %v", r)
	}
	c.send(24412, proto.Message{"difficulty": 1})
	if r := c.waitFor(24412); r.Int("code") != 1 {
		t.Fatalf("choose: %v", r)
	}
	sw := c.waitFor(24414)
	if sw.Int("floor_id") != 4 || len(sw.Arr("rewards")) != 2 {
		t.Fatalf("sweep: %v", sw)
	}
	b := c.waitFor(24400)
	c.settle()
	st := e.expState(t, role)
	after, _ := e.st.RoleAssets(role.RID)
	if b.Int("guard_id") != expPointID(4)+1 || st.Guard != expPointID(4)+1 || after[1] <= before[1] || len(st.Taken) != 1 || st.Taken[0] != 4 {
		t.Fatalf("after the sweep: %v %+v", b, st)
	}
}

func TestExpeditionLentAndHiredHeroes(t *testing.T) {
	e, c, role, hero := expEnv(t)
	cb, other := e.enter("uy", "好友")
	if err := e.st.RequestFriend(other.RID, role.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(other.RID, role.RID, 50); err != nil {
		t.Fatal(err)
	}
	cb.send(11000, proto.Message{})
	cb.waitFor(11000)
	theirs, _ := e.st.PartnersOf(other.RID)
	c.send(24408, proto.Message{"rid": other.RID, "srv_id": other.SrvID, "id": theirs[0].PartnerID}) // not lent yet
	if r := c.waitFor(24408); r.Int("code") != 0 {
		t.Fatalf("hire a hero that is not lent: %v", r)
	}
	cb.send(24407, proto.Message{"id": 999999})
	if r := cb.waitFor(24407); r.Int("code") != 0 {
		t.Fatalf("lend a hero that does not exist: %v", r)
	}
	cb.send(24407, proto.Message{"id": theirs[0].PartnerID})
	if r := cb.waitFor(24407); r.Int("code") != 1 {
		t.Fatalf("lend: %v", r)
	}
	cb.send(24405, proto.Message{})
	if l := cb.waitFor(24405).Arr("list"); len(l) != 1 || l[0].Int("id") != theirs[0].PartnerID {
		t.Fatalf("24405: %v", l)
	}
	c.send(24406, proto.Message{})
	fl := c.waitFor(24406).Arr("list")
	if len(fl) != 1 || fl[0].Int("rid") != other.RID || fl[0].Int("is_employ") != 0 {
		t.Fatalf("24406: %v", fl)
	}
	c.send(24408, proto.Message{"rid": other.RID, "srv_id": other.SrvID, "id": theirs[0].PartnerID})
	if r := c.waitFor(24408); r.Int("code") != 1 {
		t.Fatalf("hire: %v", r)
	}
	if assets, _ := e.st.RoleAssets(other.RID); assets[14] != 10 {
		t.Fatalf("the owner is paid: %v", assets[14])
	}
	c.send(24408, proto.Message{"rid": other.RID, "srv_id": other.SrvID, "id": theirs[0].PartnerID}) // the same hero twice
	if r := c.waitFor(24408); r.Int("code") != 0 {
		t.Fatalf("hire twice: %v", r)
	}
	c.send(24406, proto.Message{})
	if fl := c.waitFor(24406).Arr("list"); fl[0].Int("is_employ") != 1 {
		t.Fatalf("24406 after: %v", fl)
	}
	c.send(24409, proto.Message{})
	hp := c.waitFor(24409)
	if l := hp.Arr("list"); len(l) != 1 || l[0].Int("is_used") != 0 || l[0].Int("hp_per") != 100 || len(hp.Arr("p_list")) != 0 {
		t.Fatalf("24409: %v", hp)
	}
	// the hired hero goes along
	c.send(24412, proto.Message{"difficulty": 1})
	c.waitFor(24412)
	c.waitFor(24400)
	team := expTeam(hero)
	team["pos_info"] = append(team["pos_info"].([]proto.Message), proto.Message{"pos": 2, "owner_id": other.RID, "owner_srv_id": other.SrvID, "id": theirs[0].PartnerID})
	c.send(24403, team)
	if r := c.waitFor(24403); r.Int("code") != 1 {
		t.Fatalf("fight with a friend: %v", r)
	}
	if m := c.waitFor(20027); len(m.Arr("objects")) < 2 {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(20006)
	if res.Int("result") == 1 {
		c.waitFor(24400)
	}
	c.settle()
	st := e.expState(t, role)
	if res.Int("result") == 1 && !st.Hired[0].Used {
		t.Fatalf("a hired hero that won cannot fight again: %+v", st.Hired)
	}
}
