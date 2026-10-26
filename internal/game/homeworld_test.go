package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// homeOpen makes a role of level 70 and picks its first figure.
func (e *env) homeOpen(account, name string) (*client, *store.Role) {
	e.t.Helper()
	c, role := e.enter(account, name)
	if err := e.st.SetRoleLevel(role.RID, 70); err != nil {
		e.t.Fatal(err)
	}
	c.send(26004, proto.Message{})
	if r := c.waitFor(26004); r.Int("is_finish") != 1 || r.Int("use_id") != 0 {
		e.t.Fatalf("figures before the first: %v", r)
	}
	c.send(26007, proto.Message{"id": 1})
	if r := c.waitFor(26007); r.Int("code") != 1 {
		e.t.Fatalf("first figure: %v", r)
	}
	if r := c.waitFor(26004); r.Int("use_id") != 1 || len(r.Arr("list")) != 1 {
		e.t.Fatalf("figures after: %v", r)
	}
	return c, role
}

func TestHomeworldOpenAndFurnish(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("hw0", "家园客")
	c.send(26004, proto.Message{})
	if r := c.waitFor(26004); r.Int("is_finish") != 0 {
		t.Fatalf("open below the level: %v", r)
	}
	c.send(26007, proto.Message{"id": 1})
	if r := c.waitFor(26007); r.Int("code") != 0 {
		t.Fatalf("first figure below the level: %v", r)
	}
	if err := e.st.SetRoleLevel(role.RID, 70); err != nil {
		t.Fatal(err)
	}
	c.send(26007, proto.Message{"id": 3}) // not one of the two to start with
	if r := c.waitFor(26007); r.Int("code") != 0 {
		t.Fatalf("a figure that is not a first one: %v", r)
	}
	c.send(26017, proto.Message{})
	if r := c.waitFor(26017); r.Int("is_first") != 1 {
		t.Fatalf("first: %v", r)
	}
	c.send(26007, proto.Message{"id": 2})
	if r := c.waitFor(26007); r.Int("code") != 1 {
		t.Fatalf("first figure: %v", r)
	}
	c.waitFor(26004)
	c.send(26007, proto.Message{"id": 1})
	if r := c.waitFor(26007); r.Int("code") != 0 {
		t.Fatalf("a second first figure: %v", r)
	}
	if got := e.assetOf(t, role.RID, homeCoinAsset); got != 1700 {
		t.Fatalf("the first home coin: %d", got)
	}

	c.send(26001, proto.Message{})
	r := c.waitFor(26001)
	if r.Int("floor") != 1 || r.Int("wall_bid") != homeDefaultWall || r.Int("land_bid") != homeDefaultLand || r.Int("look_id") != 2 || r.Str("name") != "家园客的家园" {
		t.Fatalf("the new home: %v", r)
	}
	if r.Int("soft") != 200 { // the free wall and floor are worth 100 each
		t.Fatalf("soft of the bare room: %v", r)
	}
	if r.Int("rest_worship") != 6 {
		t.Fatalf("likes left: %v", r)
	}

	// the furniture comes from the home bag: two paintings, a vase and a table
	e.addItems(t, role, map[int64]int64{200101: 3, 200102: 1, 200104: 1})
	c.send(10502, proto.Message{})
	bag := c.waitFor(10502)
	if len(bag.Arr("item_list")) != 3 {
		t.Fatalf("home bag: %v", bag)
	}
	c.send(10500, proto.Message{})
	if b := c.waitFor(10500); len(b.Arr("item_list")) != 0 {
		t.Fatalf("furniture in the backpack: %v", b)
	}
	// more than it has
	c.send(26002, proto.Message{"floor": 1, "list": []proto.Message{{"bid": 200102, "index": 1, "dir": 1}, {"bid": 200102, "index": 2, "dir": 1}}})
	if r := c.waitFor(26002); r.Int("code") != 0 {
		t.Fatalf("furniture it does not have: %v", r)
	}
	// a wall as furniture
	c.send(26002, proto.Message{"floor": 1, "list": []proto.Message{{"bid": 200152, "index": 1, "dir": 1}}})
	if r := c.waitFor(26002); r.Int("code") != 0 {
		t.Fatalf("a wall in the list: %v", r)
	}
	c.send(26002, proto.Message{"floor": 1, "list": []proto.Message{
		{"bid": 200101, "index": 10, "dir": 1}, {"bid": 200101, "index": 11, "dir": 2}, {"bid": 200101, "index": 12, "dir": 1}, {"bid": 200102, "index": 20, "dir": 1}}})
	saved := c.waitFor(26002)
	// the paintings count twice (effect 2): 2*60 + 30 + the free wall and floor 200, and the set of four kinds adds 150
	if saved.Int("code") != 1 || saved.Int("soft") != 500 || len(saved.Arr("list")) != 4 {
		t.Fatalf("save: %v", saved)
	}
	bag = c.waitFor(10502)
	if items := bag.Arr("item_list"); len(items) != 1 || items[0].Int("base_id") != 200104 {
		t.Fatalf("the bag after placing: %v", bag)
	}
	c.send(26001, proto.Message{})
	if r := c.waitFor(26001); r.Int("soft") != 500 || len(r.Arr("list")) != 4 {
		t.Fatalf("reload: %v", r)
	}
	// taking things down gives them back
	c.send(26002, proto.Message{"floor": 1, "list": []proto.Message{}})
	if r := c.waitFor(26002); r.Int("code") != 1 || r.Int("soft") != 200 {
		t.Fatalf("clearing: %v", r)
	}
	if items := c.waitFor(10502).Arr("item_list"); len(items) != 3 {
		t.Fatalf("the bag after clearing: %v", items)
	}
	// the second floor stays shut until the comfort reaches 3000
	c.send(26002, proto.Message{"floor": 2, "list": []proto.Message{}})
	if r := c.waitFor(26002); r.Int("code") != 0 {
		t.Fatalf("second floor: %v", r)
	}
	c.send(26021, proto.Message{"main_floor": 2})
	if r := c.waitFor(26021); r.Int("code") != 0 {
		t.Fatalf("main floor 2: %v", r)
	}
	c.send(26011, proto.Message{"name": "我的小屋"})
	if r := c.waitFor(26011); r.Int("code") != 1 || r.Str("name") != "我的小屋" {
		t.Fatalf("rename: %v", r)
	}
	c.send(26011, proto.Message{"name": "一二三四五六七八九十一二三"})
	if r := c.waitFor(26011); r.Int("code") != 0 {
		t.Fatalf("a long name: %v", r)
	}
}

// homeSuit puts a full set on the two floors of a role through the state, which is how the 3000 comfort is reached.
func TestHomeworldSecondFloorSuitsAndFigures(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.homeOpen("hw1", "套装客")
	var furniture []int64
	for _, u := range homeCfg.Units {
		if u.Set == 2 && u.Type != homeUnitFloor && u.Type != homeUnitWall {
			furniture = append(furniture, u.Bid)
		}
	}
	items := map[int64]int64{}
	for _, b := range furniture {
		items[b] = 3
	}
	e.addItems(t, role, items)
	var list []proto.Message
	for i, b := range furniture {
		list = append(list, proto.Message{"bid": b, "index": i + 1, "dir": 1})
	}
	c.send(26002, proto.Message{"floor": 1, "wall_bid": 200252, "land_bid": 200251, "list": list})
	r := c.waitFor(26002)
	// the wall and the floor of a set are not bought in this test, so the save is refused
	if r.Int("code") != 0 {
		t.Fatalf("a wall it does not have: %v", r)
	}
	e.addItems(t, role, map[int64]int64{200251: 1, 200252: 1})
	c.send(26002, proto.Message{"floor": 1, "wall_bid": 200252, "land_bid": 200251, "list": list})
	r = c.waitFor(26002)
	if r.Int("code") != 1 || r.Int("soft") <= 2000 {
		t.Fatalf("a full set: %v", r)
	}
	if r.Int("max_all_soft") < r.Int("soft") {
		t.Fatalf("max all: %v", r)
	}
	c.waitFor(10502)
	if soft := r.Int("soft"); soft >= 3000 {
		c.send(26002, proto.Message{"floor": 2, "list": []proto.Message{}})
		if got := c.waitFor(26002); got.Int("code") != 1 {
			t.Fatalf("second floor at %d: %v", soft, got)
		}
		c.waitFor(10502)
	}

	// the set: sixteen pieces collected, the rewards of 3, 6, 10 and 16 pieces
	c.send(26013, proto.Message{})
	suits := c.waitFor(26013)
	var coll int
	for _, s := range suits.Arr("list") {
		if s.Int("set_id") == 2 {
			coll = len(s.Arr("collect"))
		}
	}
	if coll != 16 {
		t.Fatalf("collected %d of set 2: %v", coll, suits)
	}
	c.send(26014, proto.Message{"id": 204})
	if r := c.waitFor(26014); r.Int("code") != 1 {
		t.Fatalf("claim: %v", r)
	}
	c.send(26014, proto.Message{"id": 204})
	if r := c.waitFor(26014); r.Int("code") != 0 {
		t.Fatalf("claim twice: %v", r)
	}
	c.send(26014, proto.Message{"id": 104}) // nothing of set 1
	if r := c.waitFor(26014); r.Int("code") != 0 {
		t.Fatalf("claim of a set it lacks: %v", r)
	}

	// figures: the comfort opens them, diamonds pay
	if best := e.homeState(t, role).MaxSoft; best < 1000 {
		t.Fatalf("max soft %d", best)
	}
	c.send(26006, proto.Message{"id": 3}) // 1500 comfort, 500 diamonds; the role has the 400 of the set reward only
	if r := c.waitFor(26006); r.Int("code") != 0 {
		t.Fatalf("figure without the diamonds: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{homeDiamond: 1000})
	c.send(26006, proto.Message{"id": 3})
	if r := c.waitFor(26006); r.Int("code") != 1 {
		t.Fatalf("figure: %v", r)
	}
	c.send(26006, proto.Message{"id": 7}) // the new year figure
	if r := c.waitFor(26006); r.Int("code") != 0 {
		t.Fatalf("event figure: %v", r)
	}
	c.send(26005, proto.Message{"id": 3})
	if r := c.waitFor(26005); r.Int("code") != 1 || r.Int("id") != 3 {
		t.Fatalf("use: %v", r)
	}
	c.send(26005, proto.Message{"id": 4})
	if r := c.waitFor(26005); r.Int("code") != 0 {
		t.Fatalf("use of a figure it lacks: %v", r)
	}
}

func (e *env) homeState(t *testing.T, role *store.Role) *homeState {
	t.Helper()
	st := &homeState{}
	if _, err := e.st.LoadState(role.RID, homeModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (e *env) assetOf(t *testing.T, rid, id int64) int64 {
	t.Helper()
	a, err := e.st.RoleAssets(rid)
	if err != nil {
		t.Fatal(err)
	}
	return a[id]
}

func TestHomeworldCoinHook(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.homeOpen("hw2", "收币客")
	c.send(26016, proto.Message{})
	if r := c.waitFor(26016); r.Int("code") != 0 {
		t.Fatalf("collect at once: %v", r)
	}
	// the bare room (200 comfort) pays 16 an hour; five hours wait
	e.advance(5 * time.Hour)
	c.send(26018, proto.Message{})
	if r := c.waitFor(26018); r.Int("hook_code") != 1 {
		t.Fatalf("red: %v", r)
	}
	c.send(26001, proto.Message{})
	if r := c.waitFor(26001); r.Int("acc_hook_time") != 5*3600 {
		t.Fatalf("accumulated: %v", r)
	}
	before := e.assetOf(t, role.RID, homeCoinAsset)
	c.send(26016, proto.Message{})
	if r := c.waitFor(26016); r.Int("code") != 1 {
		t.Fatalf("collect: %v", r)
	}
	if r := c.waitFor(26015); r.Int("acc_hook_time") != 0 {
		t.Fatalf("push: %v", r)
	}
	if got := e.assetOf(t, role.RID, homeCoinAsset) - before; got != 5*16 {
		t.Fatalf("coin: %d", got)
	}
	// a long absence hoards no more than twelve hours
	e.advance(48 * time.Hour)
	before = e.assetOf(t, role.RID, homeCoinAsset)
	c.send(26016, proto.Message{})
	c.waitFor(26016)
	if got := e.assetOf(t, role.RID, homeCoinAsset) - before; got != 12*16 {
		t.Fatalf("hoard limit: %d", got)
	}
}

func TestHomeworldVisitLikeAndRank(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(ladderTime(21, 12)) // a Wednesday, so a day on stays in the week
	a, ra := e.homeOpen("hw3", "主人")
	b, rb := e.homeOpen("hw4", "访客")
	cc, rc := e.enter("hw5", "没家园")
	_ = cc

	b.send(26003, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	r := b.waitFor(26003)
	if r.Int("code") != 1 || r.Str("tar_name") != "主人" || r.Int("worship_status") != 0 || r.Int("floor") != 1 {
		t.Fatalf("visit: %v", r)
	}
	b.send(26003, proto.Message{"rid": rc.RID, "srv_id": "symlf_1"})
	if r := b.waitFor(26003); r.Int("code") != 0 {
		t.Fatalf("visit of a role without a home: %v", r)
	}
	b.send(26003, proto.Message{"rid": 999999, "srv_id": "symlf_1"})
	if r := b.waitFor(26003); r.Int("code") != 0 {
		t.Fatalf("visit of nobody: %v", r)
	}
	// the owner sees the guest, in the list and as a figure in the room
	a.send(26010, proto.Message{})
	rec := a.waitFor(26010).Arr("list")
	if len(rec) != 1 || rec[0].Int("rid") != rb.RID || rec[0].Int("type") != 1 || rec[0].Int("is_friend") != 0 {
		t.Fatalf("visitors: %v", rec)
	}
	a.send(26001, proto.Message{})
	if v := a.waitFor(26001).Arr("visitors"); len(v) != 1 || v[0].Int("rid") != rb.RID || v[0].Int("look_id") != 1 {
		t.Fatalf("figures of guests: %v", v)
	}
	// a second visit the same day is listed again but counts once for the popularity
	b.send(26003, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	b.waitFor(26003)

	// the like is the worship of kind 5
	b.send(10316, proto.Message{"type": 5, "rid": ra.RID, "srv_id": "symlf_1", "idx": 1})
	if r := b.waitFor(10316); r.Int("code") != 1 {
		t.Fatalf("like: %v", r)
	}
	if got := e.assetOf(t, rb.RID, homeCoinAsset); got != 1700+50 {
		t.Fatalf("coin for a like: %d", got)
	}
	b.send(10316, proto.Message{"type": 5, "rid": ra.RID, "srv_id": "symlf_1", "idx": 1})
	if r := b.waitFor(10316); r.Int("code") != 0 {
		t.Fatalf("like twice a day: %v", r)
	}
	b.send(10316, proto.Message{"type": 5, "rid": rc.RID, "srv_id": "symlf_1", "idx": 1})
	if r := b.waitFor(10316); r.Int("code") != 0 {
		t.Fatalf("like of a role without a home: %v", r)
	}
	b.send(26019, proto.Message{})
	if l := b.waitFor(26019).Arr("list"); len(l) != 1 || l[0].Int("rid") != ra.RID {
		t.Fatalf("liked today: %v", l)
	}
	b.send(26001, proto.Message{})
	if r := b.waitFor(26001); r.Int("rest_worship") != 5 {
		t.Fatalf("likes left: %v", r)
	}
	b.send(26003, proto.Message{"rid": ra.RID, "srv_id": "symlf_1"})
	if r := b.waitFor(26003); r.Int("worship") != 1 || r.Int("worship_status") != 1 {
		t.Fatalf("home after the like: %v", r)
	}

	// a friend gets the friendship points too
	if err := e.st.RequestFriend(rb.RID, ra.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(rb.RID, ra.RID, 50); err != nil {
		t.Fatal(err)
	}
	e.advance(24 * time.Hour)
	b.send(10316, proto.Message{"type": 5, "rid": ra.RID, "srv_id": "symlf_1", "idx": 1})
	if r := b.waitFor(10316); r.Int("code") != 1 {
		t.Fatalf("like of a friend: %v", r)
	}
	if got := e.assetOf(t, rb.RID, 14); got != 20 {
		t.Fatalf("friendship points: %d", got)
	}

	// popularity: two likes ×12 + one visit ×3
	b.send(26020, proto.Message{})
	rank := b.waitFor(26020)
	list := rank.Arr("rank_list")
	if len(list) != 1 || list[0].Int("rid") != ra.RID || list[0].Int("score") != 2*12+3 || rank.Int("my_rank") != 0 {
		t.Fatalf("rank: %v", rank)
	}
	a.send(26020, proto.Message{})
	if r := a.waitFor(26020); r.Int("my_rank") != 1 || r.Int("my_score") != 27 {
		t.Fatalf("my rank: %v", r)
	}
	// the week turns over and the rank is cleared
	e.advance(8 * 24 * time.Hour)
	a.send(26020, proto.Message{})
	if r := a.waitFor(26020); len(r.Arr("rank_list")) != 0 {
		t.Fatalf("rank after the week: %v", r)
	}

	// strangers' homes to visit
	b.send(26009, proto.Message{})
	if l := b.waitFor(26009).Arr("list"); len(l) != 1 || l[0].Int("rid") != ra.RID {
		t.Fatalf("random homes: %v", l)
	}
}

func TestHomeworldLikesPerDay(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	b, _ := e.homeOpen("hw6", "点赞客")
	var owners []*store.Role
	for i := 0; i < 7; i++ {
		_, r := e.homeOpen("hwo"+string(rune('a'+i)), "主"+string(rune('a'+i)))
		owners = append(owners, r)
	}
	for i, o := range owners {
		b.send(10316, proto.Message{"type": 5, "rid": o.RID, "srv_id": "symlf_1", "idx": 1})
		want := int64(1)
		if i == 6 {
			want = 0
		}
		if r := b.waitFor(10316); r.Int("code") != want {
			t.Fatalf("like %d: %v", i, r)
		}
	}
}
