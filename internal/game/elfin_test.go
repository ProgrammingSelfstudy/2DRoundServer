package game

import (
	"strconv"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func elfinEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ue", "养成者")
	if err := e.st.SetRoleLevel(role.RID, 80); err != nil {
		t.Fatal(err)
	}
	return e, c, role
}

func (e *env) addItems(t *testing.T, role *store.Role, items map[int64]int64) {
	t.Helper()
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, items); err != nil {
		t.Fatal(err)
	}
}

func (e *env) elfinState(t *testing.T, role *store.Role) *elfinState {
	st := &elfinState{}
	if _, err := e.st.LoadState(role.RID, elfinModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestElfinOpensAtLevel80(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ue", "新人")
	c.send(26500, proto.Message{})
	r := c.waitFor(26500)
	if len(r.Arr("sprite_hatchs")) != 6 || r.Arr("sprite_hatchs")[0].Int("is_open") != 0 {
		t.Fatalf("26500 below 80: %v", r)
	}
	c.send(26503, proto.Message{"id": 1, "item_bid": 10601})
	if r := c.waitFor(26503); r.Int("result") != 0 || r.Str("msg") != "精灵系统将在80级开放" {
		t.Fatalf("hatching below 80: %v", r)
	}
	e.st.SetRoleLevel(role.RID, 80)
	c.send(26500, proto.Message{})
	r = c.waitFor(26500)
	open := 0
	for _, h := range r.Arr("sprite_hatchs") {
		open += int(h.Int("is_open") % 2)
	}
	if open != 1 || r.Arr("sprite_hatchs")[0].Int("is_open") != 1 {
		t.Fatalf("grounds at 80: %v", r)
	}
}

func TestElfinHatchAndCollect(t *testing.T) {
	e, c, role := elfinEnv(t)
	c.send(26507, proto.Message{"type": 1, "item_id": 10601, "num": 1}) // 1,200,000 beli
	if r := c.waitFor(26507); r.Int("result") != 0 {
		t.Fatalf("bought an egg without beli: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{1: 2500000, 3: 1000})
	c.send(26507, proto.Message{"type": 1, "item_id": 10601, "num": 1})
	if r := c.waitFor(26507); r.Int("result") != 1 || r.Int("count") != 1 {
		t.Fatalf("buy egg: %v", r)
	}
	c.send(26503, proto.Message{"id": 1, "item_bid": 10601})
	h := c.waitFor(26501).Arr("sprite_hatch")[0]
	if r := c.waitFor(26503); r.Int("result") != 1 {
		t.Fatalf("hatch: %v", r)
	}
	if h.Int("state") != 1 || h.Int("do_id") != 10601 || h.Int("all_end_time") < time.Now().Unix()+2880*11-5 {
		t.Fatalf("hatching: %v", h)
	}
	c.send(26505, proto.Message{"id": 1}) // not done
	if r := c.waitFor(26505); r.Int("result") != 0 {
		t.Fatalf("collected early: %v", r)
	}
	c.send(26507, proto.Message{"type": 2, "item_id": 10703, "num": 1}) // 150 gems, takes 25920 points off
	if r := c.waitFor(26507); r.Int("result") != 1 {
		t.Fatalf("buy speed item: %v", r)
	}
	c.send(26504, proto.Message{"id": 1, "item_bid": 10703, "item_num": 1})
	if r := c.waitFor(26504); r.Int("result") != 1 {
		t.Fatalf("speed up: %v", r)
	}
	c.send(26505, proto.Message{"id": 1})
	r := c.waitFor(26505)
	if r.Int("result") != 1 || len(r.Arr("awards")) != 1 {
		t.Fatalf("collect: %v", r)
	}
	got := r.Arr("awards")[0].Int("item_bid")
	if _, ok := elfinCfg.sprite[got]; !ok || elfinCfg.sprite[got].Step != 1 {
		t.Fatalf("hatched %d", got)
	}
	c.settle()
	st := e.elfinState(t, role)
	if st.Hatches[0].State != elfinStateIdle || !st.Seen[strconv.FormatInt(got, 10)] {
		t.Fatalf("state after collecting: %+v", st.Hatches[0])
	}
	c.send(10503, proto.Message{})
	bag := c.waitFor(10503).Arr("item_list")
	if len(bag) != 1 || bag[0].Int("base_id") != got {
		t.Fatalf("sprite bag: %v", bag)
	}
	c.send(26509, proto.Message{})
	if b := c.waitFor(26509).Arr("awards"); len(b) != 1 || b[0].Int("item_bid") != got {
		t.Fatalf("book: %v", b)
	}
}

func TestElfinTreeSlotsAndCompose(t *testing.T) {
	e, c, role := elfinEnv(t)
	e.grantAssets(c, role, map[int64]int64{1: 1000000})
	e.addItems(t, role, map[int64]int64{111001: 2, 111002: 1, 10040: 100})
	c.send(26510, proto.Message{})
	tr := c.waitFor(26510)
	if tr.Int("lev") != 1 || tr.Int("break_lev") != 0 || len(tr.Arr("sprites")) != 4 || tr.Int("atk") < 5642 {
		t.Fatalf("tree: %v", tr)
	}
	c.send(26513, proto.Message{"pos": 2, "item_bid": 111001}) // the second place needs step 1
	if r := c.waitFor(26513); r.Int("result") != 0 {
		t.Fatalf("locked place: %v", r)
	}
	c.send(26513, proto.Message{"pos": 1, "item_bid": 111001})
	if r := c.waitFor(26513); r.Int("result") != 1 {
		t.Fatalf("place: %v", r)
	}
	after := c.waitFor(26510)
	if after.Arr("sprites")[0].Int("item_bid") != 111001 || after.Int("atk") != tr.Int("atk")+1318 || after.Int("power") <= tr.Int("power") {
		t.Fatalf("tree with a sprite: %v", after)
	}
	c.send(26513, proto.Message{"pos": 1, "item_bid": 112001}) // not owned
	if r := c.waitFor(26513); r.Int("result") != 0 {
		t.Fatalf("placed a sprite that is not owned: %v", r)
	}
	// compose the placed sprite: 111001 + 20 fruit -> 111101 in the same place
	c.send(26508, proto.Message{"item_bid": 111001, "num": 1, "pos": 1})
	if r := c.waitFor(26508); r.Int("result") != 1 {
		t.Fatalf("compose: %v", r)
	}
	c.settle()
	if st := e.elfinState(t, role); st.Slots[0] != 111101 {
		t.Fatalf("slot after compose: %v", st.Slots)
	}
	bag, _ := e.st.RoleItems(role.RID)
	if bag[111101] != 1 || bag[111001] != 1 || bag[10040] != 80 {
		t.Fatalf("bag after compose: %v", bag)
	}
	// level up
	c.send(26511, proto.Message{})
	if r := c.waitFor(26511); r.Int("result") != 1 {
		t.Fatalf("tree level up: %v", r)
	}
	c.settle()
	if st := e.elfinState(t, role); st.Lev != 2 {
		t.Fatalf("level: %d", st.Lev)
	}
	c.settle()
	st := e.elfinState(t, role)
	st.Lev = 10 // at the cap of step 0
	e.st.SaveState(role.RID, elfinModule, st)
	c.send(26511, proto.Message{})
	if r := c.waitFor(26511); r.Int("result") != 0 || r.Str("msg") != "需要先进阶" {
		t.Fatalf("level beyond the cap: %v", r)
	}
	c.send(26512, proto.Message{}) // 50 fruit and a step item, none of the latter
	if r := c.waitFor(26512); r.Int("result") != 0 {
		t.Fatalf("break without the step item: %v", r)
	}
	e.addItems(t, role, map[int64]int64{10041: 1, 10040: 50})
	c.send(26512, proto.Message{})
	if r := c.waitFor(26512); r.Int("result") != 1 {
		t.Fatalf("break: %v", r)
	}
	c.settle()
	if st := e.elfinState(t, role); st.Break != 1 {
		t.Fatalf("break level: %d", st.Break)
	}
}

func TestElfinSummonAndAwards(t *testing.T) {
	e, c, role := elfinEnv(t)
	c.send(26550, proto.Message{})
	if r := c.waitFor(26550); r.Int("times") != 0 || r.Int("day_count") != 0 {
		t.Fatalf("26550: %v", r)
	}
	c.send(26551, proto.Message{"times": 10, "recruit_type": 1}) // no tickets
	if r := c.waitFor(26551); r.Int("flag") != 0 {
		t.Fatalf("summoned without tickets: %v", r)
	}
	e.addItems(t, role, map[int64]int64{10415: 10})
	c.send(26551, proto.Message{"times": 10, "recruit_type": 1})
	res := c.waitFor(26553)
	if res.Int("group_id") != 110 || res.Int("times") != 10 || len(res.Arr("rewards")) < 10 {
		t.Fatalf("26553: %v", res)
	}
	purple := false
	for _, b := range res.Arr("sprite_bids") {
		purple = purple || b.Int("sprite_bid")/1000 >= 112
	}
	if !purple {
		t.Fatalf("ten pulls without a purple sprite: %v", res)
	}
	if r := c.waitFor(26551); r.Int("flag") != 1 {
		t.Fatalf("26551: %v", r)
	}
	c.send(26552, proto.Message{"id": 1}) // 30 pulls needed
	if r := c.waitFor(26552); r.Int("flag") != 0 {
		t.Fatalf("early award: %v", r)
	}
	c.settle()
	st := e.elfinState(t, role)
	st.Times = 30
	e.st.SaveState(role.RID, elfinModule, st)
	c.send(26552, proto.Message{"id": 1})
	if r := c.waitFor(26552); r.Int("flag") != 1 {
		t.Fatalf("award: %v", r)
	}
	c.send(26552, proto.Message{"id": 1})
	if r := c.waitFor(26552); r.Int("flag") != 0 {
		t.Fatalf("award twice: %v", r)
	}
	c.send(26554, proto.Message{"lucky_ids": []proto.Message{{"lucky_sprites_bid": 113001}}})
	if r := c.waitFor(26554); r.Int("flag") != 1 {
		t.Fatalf("lucky: %v", r)
	}
	c.send(26554, proto.Message{"lucky_ids": []proto.Message{{"lucky_sprites_bid": 113010}}}) // not a wish sprite
	if r := c.waitFor(26554); r.Int("flag") != 0 {
		t.Fatalf("bad wish: %v", r)
	}
	// the free pull
	c.send(26551, proto.Message{"times": 1, "recruit_type": 0})
	c.waitFor(26553)
	if r := c.waitFor(26551); r.Int("flag") != 1 {
		t.Fatalf("free pull: %v", r)
	}
	c.send(26551, proto.Message{"times": 1, "recruit_type": 0})
	if r := c.waitFor(26551); r.Int("flag") != 0 {
		t.Fatalf("a second free pull: %v", r)
	}
}

func TestElfinPlansAndTeams(t *testing.T) {
	e, c, role := elfinEnv(t)
	c.send(26556, proto.Message{})
	pl := c.waitFor(26556).Arr("plan_list")
	if len(pl) != 2 || pl[0].Str("name") != "方案1" {
		t.Fatalf("plans: %v", pl)
	}
	c.send(26558, proto.Message{"id": 1, "name": "我的方案"})
	if r := c.waitFor(26558); r.Int("flag") != 1 {
		t.Fatalf("rename: %v", r)
	}
	c.send(26557, proto.Message{"id": 1, "type": 0, "team": 1, "sprites": []proto.Message{{"pos": 1, "item_bid": 111001}}})
	if r := c.waitFor(26557); r.Int("flag") != 1 {
		t.Fatalf("save plan: %v", r)
	}
	c.send(26555, proto.Message{"type": 0})
	if tl := c.waitFor(26555).Arr("team_list"); len(tl) != 1 || tl[0].Int("plan_id") != 1 || len(tl[0].Arr("sprites")) != 1 {
		t.Fatalf("teams: %v", tl)
	}
	c.send(26561, proto.Message{"type": 4, "team": 2, "id": 1})
	if r := c.waitFor(26561); r.Int("flag") != 1 || r.Int("team") != 2 {
		t.Fatalf("apply: %v", r)
	}
	c.send(26564, proto.Message{"type": 4, "team_list": []proto.Message{{"team": 1, "old_team": 2}}})
	c.waitFor(26564)
	c.send(26555, proto.Message{"type": 4})
	if tl := c.waitFor(26555).Arr("team_list"); len(tl) != 1 || tl[0].Int("team") != 1 {
		t.Fatalf("reordered teams: %v", tl)
	}
	c.send(26562, proto.Message{"id": 3}) // 300 gems, none
	if r := c.waitFor(26562); r.Int("flag") != 0 {
		t.Fatalf("bought a plan without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 300})
	c.send(26562, proto.Message{"id": 3})
	if r := c.waitFor(26562); r.Int("flag") != 1 {
		t.Fatalf("buy plan: %v", r)
	}
	c.send(26562, proto.Message{"id": 5}) // one by one
	if r := c.waitFor(26562); r.Int("flag") != 0 {
		t.Fatalf("skipped a plan: %v", r)
	}
	e.addItems(t, role, map[int64]int64{111001: 1})
	c.send(26563, proto.Message{"base_id": 111001, "num": 1}) // the plan uses its only copy
	if r := c.waitFor(26563); r.Int("flag") != 1 || r.Str("name") != "我的方案" {
		t.Fatalf("tip: %v", r)
	}
}
