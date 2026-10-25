package game

import (
	"strconv"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func hallowsEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("uh", "神技客")
	if err := e.st.SetRoleLevel(role.RID, 80); err != nil {
		t.Fatal(err)
	}
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	return e, c, role
}

// hwSync round-trips a request so that the handler before it has saved its state.
func (c *client) hwSync() {
	c.send(24100, proto.Message{})
	c.waitFor(24100)
}

func (e *env) hwState(t *testing.T, role *store.Role) *hwState {
	st := &hwState{}
	if _, err := e.st.LoadState(role.RID, hallowsModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (e *env) hwOpen(t *testing.T, role *store.Role) {
	t.Helper()
	if err := e.st.SaveDrama(store.Drama{RID: role.RID, Mode: 1, ChapterID: 1, DunID: 10030, MaxDunID: 10030}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) hwGrant(t *testing.T, role *store.Role, assets, items map[int64]int64) {
	t.Helper()
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
		t.Fatal(err)
	}
}

// hwUnlockAll gives the role the five hallows without the tasks.
func (e *env) hwUnlockAll(t *testing.T, role *store.Role) {
	t.Helper()
	st := &hwState{Hallows: map[string]*hwOne{}}
	for id := int64(1); id <= hallowsCount; id++ {
		st.Hallows[strconv.FormatInt(id, 10)] = &hwOne{ID: id, Step: 1, SkillLev: 1}
	}
	if err := e.st.SaveState(role.RID, hallowsModule, st); err != nil {
		t.Fatal(err)
	}
}

func TestHallowsClosedUntilTheForest(t *testing.T) {
	_, c, _ := hallowsEnv(t)
	c.send(24100, proto.Message{})
	if r := c.waitFor(24100); len(r.Arr("hallows")) != 0 {
		t.Fatalf("24100: %v", r)
	}
	c.send(24120, proto.Message{})
	if r := c.waitFor(24120); len(r.Arr("list")) != 0 {
		t.Fatalf("24120 before the dungeon: %v", r)
	}
	c.send(24101, proto.Message{"id": 1, "is_auto": 0})
	if r := c.waitFor(24101); r.Int("result") != 0 || r.Str("msg") == "" {
		t.Fatalf("24101 before the dungeon: %v", r)
	}
}

func TestHallowsUnlockTasks(t *testing.T) {
	e, c, role := hallowsEnv(t)
	e.hwOpen(t, role)
	c.send(24120, proto.Message{})
	r := c.waitFor(24120)
	if len(r.Arr("list")) != 4 {
		t.Fatalf("24120: %v", r)
	}
	fin := map[int64]int64{}
	for _, m := range r.Arr("list") {
		fin[m.Int("id")] = m.Int("finish")
	}
	if fin[101] != 1 || fin[102] != 1 || fin[103] != 0 || fin[104] != 0 {
		t.Fatalf("task states: %v", fin)
	}
	c.send(24122, proto.Message{"id": 103})
	if r := c.waitFor(24122); r.Int("code") != 0 {
		t.Fatalf("claimed an open task: %v", r)
	}
	c.send(24122, proto.Message{"id": 101})
	if r := c.waitFor(24122); r.Int("code") != 1 {
		t.Fatalf("24122: %v", r)
	}
	if u := c.waitFor(24121); u.Arr("list")[0].Int("id") != 101 || u.Arr("list")[0].Int("finish") != 2 {
		t.Fatalf("24121: %v", u)
	}
	c.send(24122, proto.Message{"id": 101})
	if r := c.waitFor(24122); r.Int("code") != 0 {
		t.Fatalf("claimed twice: %v", r)
	}
	// a hero at level 20 and a summon finish the other two
	hero, err := e.st.PartnersOf(role.RID)
	if err != nil || len(hero) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	if err := e.st.UpgradePartner(role.RID, hero[0].PartnerID, hero[0].Lev, hero[0].BreakLev, 20, hero[0].BreakLev, map[int64]int64{}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	lifeCount(e.st, role.RID, "evt_recruit", 1)
	for _, id := range []int64{102, 103, 104} {
		c.send(24122, proto.Message{"id": id})
		if r := c.waitFor(24122); r.Int("code") != 1 {
			t.Fatalf("24122 %d: %v", id, r)
		}
		c.waitFor(24121)
	}
	got := c.waitFor(24108)
	if got.Int("id") != 1 || got.Int("step") != 1 || len(got.Arr("skill")) != 1 || got.Arr("skill")[0].Int("skill_bid") != 801001 {
		t.Fatalf("24108: %v", got)
	}
	next := c.waitFor(24121) // the tasks of the second hallow open
	if len(next.Arr("list")) != 4 || next.Arr("list")[0].Int("id") != 201 {
		t.Fatalf("second hallow's tasks: %v", next)
	}
	c.send(24100, proto.Message{})
	if l := c.waitFor(24100); len(l.Arr("hallows")) != 1 { // also: the handler above has saved by now
		t.Fatalf("24100: %v", l)
	}
	if st := e.hwState(t, role); st.hallow(1) == nil || st.next() != 2 {
		t.Fatalf("state: %+v", st)
	}
}

func TestHallowsAdvanceSkillSealRefineAndReset(t *testing.T) {
	e, c, role := hallowsEnv(t)
	e.hwOpen(t, role)
	e.hwUnlockAll(t, role)
	c.send(24101, proto.Message{"id": 1, "is_auto": 0})
	if r := c.waitFor(24101); r.Int("result") != 0 {
		t.Fatalf("advanced without stones: %v", r)
	}
	e.hwGrant(t, role, map[int64]int64{1: 100000000, 32: 100000000}, map[int64]int64{72001: 5000, 72002: 5000, 72003: 500})
	// advance until the step moves: result 2 while the bar fills, 1 on the step
	var upSeen bool
	for i := 0; i < 40 && !upSeen; i++ {
		c.send(24101, proto.Message{"id": 1, "is_auto": 0})
		push := c.waitFor(24107)
		r := c.waitFor(24101)
		switch r.Int("result") {
		case 2:
			if push.Int("lucky") == 0 || push.Int("step") != 1 {
				t.Fatalf("fill: %v", push)
			}
		case 1:
			upSeen = true
			if push.Int("step") != 2 || push.Int("lucky") != 0 || len(push.Arr("add_attr")) != 2 {
				t.Fatalf("step up: %v", push)
			}
		default:
			t.Fatalf("24101: %v", r)
		}
	}
	if !upSeen {
		t.Fatal("never stepped up")
	}
	// seals need step 3
	c.send(24104, proto.Message{"hallows_id": 1, "num": 1})
	if r := c.waitFor(24104); r.Int("result") != 0 {
		t.Fatalf("seal at step 2: %v", r)
	}
	c.hwSync()
	st := e.hwState(t, role)
	st.hallow(1).Step = 5 // cap 10 seals
	if err := e.st.SaveState(role.RID, hallowsModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(24104, proto.Message{"hallows_id": 1, "num": 11})
	if r := c.waitFor(24104); r.Int("result") != 0 {
		t.Fatalf("over the seal cap: %v", r)
	}
	c.send(24104, proto.Message{"hallows_id": 1, "num": 10})
	if push, r := c.waitFor(24107), c.waitFor(24104); r.Int("result") != 1 || push.Int("seal") != 10 {
		t.Fatalf("24104: %v %v", push, r)
	}
	// skill level 1 -> 2 needs step 4 (lev_limit of the first row)
	c.send(24103, proto.Message{"hallows_id": 1})
	if push, r := c.waitFor(24107), c.waitFor(24103); r.Int("result") != 1 || push.Arr("skill")[0].Int("lev") != 2 || push.Arr("skill")[0].Int("skill_bid") != 801002 {
		t.Fatalf("24103: %v %v", push, r)
	}
	// refine: step 5 satisfies need_lev 1
	c.send(24135, proto.Message{"hallows_id": 1})
	if r := c.waitFor(24135); r.Int("code") != 1 || r.Int("refine_lev") != 1 {
		t.Fatalf("24135: %v", r)
	}
	// reset refunds all but coins
	c.hwSync()
	bag, _ := e.st.RoleItems(role.RID)
	before := bag[72003]
	c.send(24123, proto.Message{"id": 1})
	if push, r := c.waitFor(24107), c.waitFor(24123); r.Int("code") != 1 || push.Int("step") != 1 || push.Int("seal") != 0 || push.Int("refine_lev") != 0 {
		t.Fatalf("24123: %v %v", push, r)
	}
	bag, _ = e.st.RoleItems(role.RID)
	if after := bag[72003]; after != before+10 {
		t.Fatalf("seals back: %d -> %d", before, after)
	}
	c.send(24123, proto.Message{"id": 1})
	if r := c.waitFor(24123); r.Int("code") != 0 {
		t.Fatalf("reset a fresh hallow: %v", r)
	}
}

func TestHallowsLooks(t *testing.T) {
	e, c, role := hallowsEnv(t)
	e.hwOpen(t, role)
	e.hwUnlockAll(t, role)
	c.send(24125, proto.Message{})
	if r := c.waitFor(24125); len(r.Arr("hallows_looks")) != 0 {
		t.Fatalf("24125: %v", r)
	}
	c.send(24128, proto.Message{})
	if r := c.waitFor(24128); len(r.Arr("list")) != 4 {
		t.Fatalf("24128: %v", r)
	}
	c.send(24131, proto.Message{"id": 2})
	if r := c.waitFor(24131); r.Int("code") != 0 {
		t.Fatalf("unlocked a look with open tasks: %v", r)
	}
	c.send(24131, proto.Message{"id": 1}) // from the weekly champion only
	if r := c.waitFor(24131); r.Int("code") != 0 {
		t.Fatalf("unlocked an event look: %v", r)
	}
	c.hwSync()
	st := e.hwState(t, role)
	for _, id := range []string{"101", "103", "104", "106"} {
		st.LookClaimed[id] = true
	}
	if err := e.st.SaveState(role.RID, hallowsModule, st); err != nil {
		t.Fatal(err)
	}
	c.send(24131, proto.Message{"id": 2})
	if r := c.waitFor(24131); r.Int("code") != 1 {
		t.Fatalf("24131: %v", r)
	}
	if m := c.waitFor(24126); m.Int("id") != 2 || m.Int("eqm_hallows") != 0 {
		t.Fatalf("24126: %v", m)
	}
	c.waitFor(24127)
	c.send(24132, proto.Message{"id": 2, "hallows_id": 3, "flag": 1})
	if r := c.waitFor(24132); r.Int("code") != 1 {
		t.Fatalf("24132: %v", r)
	}
	if m := c.waitFor(24126); m.Int("eqm_hallows") != 3 {
		t.Fatalf("worn: %v", m)
	}
	if h := c.waitFor(24107); h.Int("id") != 3 || h.Int("look_id") != 2 {
		t.Fatalf("hallow 3: %v", h)
	}
	c.send(24132, proto.Message{"id": 2, "hallows_id": 4, "flag": 1}) // moves to hallow 4
	c.waitFor(24132)
	c.waitFor(24126)
	if a, b := c.waitFor(24107), c.waitFor(24107); a.Int("id") != 3 || a.Int("look_id") != 0 || b.Int("id") != 4 || b.Int("look_id") != 2 {
		t.Fatalf("move: %v %v", a, b)
	}
	c.send(24132, proto.Message{"id": 2, "hallows_id": 4, "flag": 2})
	c.waitFor(24132)
	if m := c.waitFor(24126); m.Int("eqm_hallows") != 0 {
		t.Fatalf("off: %v", m)
	}
	if h := c.waitFor(24107); h.Int("id") != 4 || h.Int("look_id") != 0 {
		t.Fatalf("off hallow: %v", h)
	}
}
