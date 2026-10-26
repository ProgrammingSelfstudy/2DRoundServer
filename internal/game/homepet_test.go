package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func (e *env) petState(t *testing.T, role *store.Role) *petState {
	t.Helper()
	p := &petState{}
	if _, err := e.st.LoadState(role.RID, petModule, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHomepetFirstTrip(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("pet0", "宠物客")
	e.addItems(t, role, map[int64]int64{100101: 1}) // one food: no second trip while the test waits

	c.send(26100, proto.Message{})
	if r := c.waitFor(26100); r.Int("state") != petHome || r.Int("vigor") != 20 || r.Int("vigor_time") == 0 {
		t.Fatalf("a new pet: %v", r)
	}
	c.send(26107, proto.Message{"set_item": []proto.Message{{"key": 2, "id": 100101}}}) // food does not go in an item slot
	if r := c.waitFor(26107); r.Int("code") != 0 {
		t.Fatalf("food in an item slot: %v", r)
	}
	c.send(26107, proto.Message{"set_item": []proto.Message{{"key": 1, "id": 100101}}})
	if r := c.waitFor(26107); r.Int("code") != 1 || len(r.Arr("set_item")) != 1 {
		t.Fatalf("set the bag: %v", r)
	}

	e.advance(16 * time.Minute) // one judgement; the first trip always goes
	c.send(26100, proto.Message{})
	if r := c.waitFor(26100); r.Int("state") != petOnWay || r.Int("vigor") != 20-petGoCost {
		t.Fatalf("on the way: %v", r)
	}
	if left, _ := e.st.RoleItems(role.RID); left[100101] != 0 {
		t.Fatalf("one food was eaten: %v", left)
	}
	c.send(26109, proto.Message{})
	if r := c.waitFor(26109); len(r.Arr("evt_list")) != 4 || r.Int("city_id") != petFirstCity {
		t.Fatalf("trip events: %v", r)
	}

	e.advance(time.Hour)
	c.send(26105, proto.Message{})
	r := c.waitFor(26105)
	l := r.Arr("evt_list")
	if len(l) != 4 {
		t.Fatalf("waiting events: %v", r)
	}
	var photo, back int64
	for _, ev := range l {
		switch ev.Int("evt_sid") {
		case 1060101:
			photo = ev.Int("evt_id")
		case petEvBack:
			back = ev.Int("evt_id")
		}
	}
	c.send(26106, proto.Message{"evt_id": photo})
	if r := c.waitFor(26106); r.Int("code") != 1 {
		t.Fatalf("take the photo: %v", r)
	}
	c.send(26111, proto.Message{"type": petPhotoType})
	if r := c.waitFor(26111); len(r.Arr("list")) != 1 || r.Arr("list")[0].Int("base_id") != 100401 {
		t.Fatalf("album: %v", r)
	}
	diamonds := e.assetOf(t, role.RID, homeDiamond)
	c.send(26106, proto.Message{"evt_id": back})
	if r := c.waitFor(26106); r.Int("code") != 1 || len(r.Arr("award")) != 5 {
		t.Fatalf("take the return: %v", r)
	}
	if got := e.assetOf(t, role.RID, homeDiamond); got != diamonds+20 {
		t.Fatalf("diamonds %d -> %d", diamonds, got)
	}
	if left, _ := e.st.RoleItems(role.RID); left[200113] != 1 || left[10450] != 30 {
		t.Fatalf("items of the return: %v", left)
	}
	c.send(26106, proto.Message{"evt_id": back})
	if r := c.waitFor(26106); r.Int("code") != 0 {
		t.Fatalf("take the return twice: %v", r)
	}
	c.send(26100, proto.Message{})
	if r := c.waitFor(26100); r.Int("state") != petHome {
		t.Fatalf("home again: %v", r)
	}
	c.send(26113, proto.Message{"type": petPhotoType, "id": 1})
	if r := c.waitFor(26113); r.Int("code") != 1 {
		t.Fatalf("throw the photo: %v", r)
	}
}

func TestHomepetNeedsFoodAndGetsVigor(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("pet1", "宠物乙")
	c.send(26100, proto.Message{})
	c.waitFor(26100)
	e.advance(3 * time.Hour) // no food, no trip, and the vigor comes back by a point every 20 minutes
	c.send(26101, proto.Message{})
	if r := c.waitFor(26101); r.Int("vigor") != 29 {
		t.Fatalf("vigor: %v", r)
	}
	if p := e.petState(t, role); p.Trip != nil {
		t.Fatalf("a trip without food: %+v", p.Trip)
	}
	e.advance(100 * time.Hour)
	c.send(26101, proto.Message{})
	if r := c.waitFor(26101); r.Int("vigor") != 100 || r.Int("vigor_time") != 0 {
		t.Fatalf("full vigor: %v", r)
	}
}

func TestHomepetTalkRenameAndPocketMoney(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("pet2", "宠物丙")
	for i := 0; i < 10; i++ {
		c.send(26103, proto.Message{"type": 1})
		if r := c.waitFor(26103); r.Int("code") != 1 || r.Int("flag") != 0 || r.Int("id") < 1 || r.Int("id") > 30 {
			t.Fatalf("talk %d: %v", i, r)
		}
	}
	c.send(26103, proto.Message{"type": 1})
	if r := c.waitFor(26103); r.Int("code") != 0 {
		t.Fatalf("talk past the limit: %v", r)
	}

	c.send(26102, proto.Message{"name": "旺财"})
	if r := c.waitFor(26102); r.Int("code") != 0 {
		t.Fatalf("rename without diamonds: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{homeDiamond: 1000, 1: 200000})
	c.send(26102, proto.Message{"name": "旺财"})
	if r := c.waitFor(26102); r.Int("code") != 1 || r.Str("name") != "旺财" || r.Int("rename_count") != 1 {
		t.Fatalf("rename: %v", r)
	}
	c.send(26102, proto.Message{"name": "名字太长了不行啊"})
	if r := c.waitFor(26102); r.Int("code") != 0 {
		t.Fatalf("a long name: %v", r)
	}

	c.send(26104, proto.Message{}) // nothing was asked for
	if r := c.waitFor(26104); r.Int("code") != 0 {
		t.Fatalf("pocket money without a question: %v", r)
	}
	c.send(26103, proto.Message{"type": 2})
	r := c.waitFor(26103)
	if r.Int("code") != 1 || r.Int("flag") != 1 || r.Int("id") < 31 {
		t.Fatalf("the pet asks: %v", r)
	}
	c.send(26104, proto.Message{})
	if r := c.waitFor(26104); r.Int("code") != 1 || r.Int("day_feed") != 1 {
		t.Fatalf("pocket money: %v", r)
	}
}
