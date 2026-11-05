package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func emoEnv(t *testing.T) (*env, *client, *store.Role) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("ug", "果实人")
	if err := e.st.SetRoleLevel(role.RID, 80); err != nil {
		t.Fatal(err)
	}
	return e, c, role
}

func (e *env) emoState(t *testing.T, role *store.Role) *emoState {
	st := &emoState{}
	if _, err := e.st.LoadState(role.RID, emoModule, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEmoguoshiLevelStarAndAwaken(t *testing.T) {
	e, c, role := emoEnv(t)
	c.send(31101, proto.Message{"id": 101}) // no shards
	if r := c.waitFor(31101); r.Int("result") != 0 {
		t.Fatalf("level 1 without shards: %v", r)
	}
	e.addItems(t, role, map[int64]int64{19101: 200, 19502: 5000})
	e.grantAssets(c, role, map[int64]int64{1: 10000000})
	c.send(31101, proto.Message{"id": 101})
	if r := c.waitFor(31101); r.Int("result") != 1 {
		t.Fatalf("level 1: %v", r)
	}
	c.settle()
	f := e.emoState(t, role).Fruits["101"]
	if f.Level != 1 || f.Star != 1 {
		t.Fatalf("fruit: %+v", f)
	}
	c.send(31103, proto.Message{})
	if l := c.waitFor(31103).Arr("list"); len(l) != 1 || l[0].Int("id") != 101 || l[0].Int("level") != 1 {
		t.Fatalf("31103: %v", l)
	}
	c.send(31102, proto.Message{"id": 101})
	if r := c.waitFor(31102); r.Int("level") != 1 || r.Int("star") != 1 {
		t.Fatalf("31102: %v", r)
	}
	for lev := 2; lev <= 10; lev++ {
		c.send(31101, proto.Message{"id": 101})
		if r := c.waitFor(31101); r.Int("result") != 1 {
			t.Fatalf("level %d: %v", lev, r)
		}
	}
	c.send(31101, proto.Message{"id": 101}) // level 11 wants star 2
	if r := c.waitFor(31101); r.Int("result") != 0 || r.Str("msg") != "星级不足" {
		t.Fatalf("level 11: %v", r)
	}
	// star up: ten shards and one hero of four stars
	c.send(31104, proto.Message{"id": 101, "use_list": []proto.Message{}})
	if r := c.waitFor(31104); r.Int("result") != 0 {
		t.Fatalf("star up without a hero: %v", r)
	}
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 4}, {BID: 10101, Star: 1}}); err != nil || !ok {
		t.Fatalf("heroes: %v %v", ok, err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	var four, one int64
	for _, h := range heroes {
		if h.Star == 4 {
			four = h.PartnerID
		} else {
			one = h.PartnerID
		}
	}
	c.send(31104, proto.Message{"id": 101, "use_list": []proto.Message{{"use_id": one}}}) // a one-star hero does not do
	if r := c.waitFor(31104); r.Int("result") != 0 {
		t.Fatalf("star up with the wrong hero: %v", r)
	}
	c.send(31104, proto.Message{"id": 101, "use_list": []proto.Message{{"use_id": four}}})
	gone := c.waitFor(11006)
	if len(gone.Arr("expend2")) != 1 || gone.Arr("expend2")[0].Int("partner_id") != four {
		t.Fatalf("11006: %v", gone)
	}
	if r := c.waitFor(31104); r.Int("result") != 1 {
		t.Fatalf("star up: %v", r)
	}
	c.settle()
	if f := e.emoState(t, role).Fruits["101"]; f.Star != 2 {
		t.Fatalf("star: %+v", f)
	}
	if after, _ := e.st.PartnersOf(role.RID); len(after) != len(heroes)-1 {
		t.Fatalf("the hero was not consumed: %d -> %d", len(heroes), len(after))
	}
	// awaken: needs the top star
	c.send(31105, proto.Message{"id": 101, "use_list": []proto.Message{}})
	if r := c.waitFor(31105); r.Int("result") != 0 {
		t.Fatalf("awakened early: %v", r)
	}
	st := e.emoState(t, role)
	st.Fruits["101"] = emoFruit{Level: 70, Star: 5}
	st.Fruits["102"] = emoFruit{Level: 70, Star: 5}
	e.st.SaveState(role.RID, emoModule, st)
	e.addItems(t, role, map[int64]int64{19501: 0, 19102: 60})
	c.send(31105, proto.Message{"id": 101, "use_list": []proto.Message{{"use_id": 19102, "use_num": 60}}})
	if r := c.waitFor(31105); r.Int("result") != 1 {
		t.Fatalf("awaken: %v", r)
	}
	c.settle()
	if f := e.emoState(t, role).Fruits["101"]; f.Juexing != 1 {
		t.Fatalf("awakened: %+v", f)
	}
}

func TestEmoguoshiShardInstanceMatchesCapture(t *testing.T) {
	// Original 2026-11-04 round1 source541/472: the stack's instance is
	// 145, its base id is 19103; the request sends 145, never 19103.
	items := []proto.Message{
		{"id": 144, "base_id": 19502, "quantity": 1700},
		{"id": 145, "base_id": 19103, "quantity": 3},
	}
	if item := emoShardInstance(items, 145); item.Int("base_id") != 19103 || item.Int("quantity") != 3 {
		t.Fatalf("captured shard instance: %v", item)
	}
	for _, id := range []int64{19103, 144, 0} {
		if item := emoShardInstance(items, id); item != nil {
			t.Fatalf("accepted a base id, non-shard or unknown instance %d: %v", id, item)
		}
	}
}

func TestEmoguoshiShardsListedInBackpack(t *testing.T) {
	ledger := map[int64]int64{19101: 36, 19103: 3, 19502: 1700, 10601: 1, 100201: 1}
	items := backpackItems(ledger)
	if len(items) != 3 || items[0].Int("base_id") != 19101 || items[1].Int("base_id") != 19103 || items[2].Int("base_id") != 19502 {
		t.Fatalf("fruit shards and source must be in 10500: %v", items)
	}
	sprites := spriteBag(ledger)
	if len(sprites) != 2 || sprites[0].Int("base_id") != 10601 || sprites[1].Int("base_id") != 100201 {
		t.Fatalf("fruit shards must not be duplicated in 10503: %v", sprites)
	}
	if !ItemListedInBackpack(19103) || !summary(19103).InBackpack {
		t.Fatal("item metadata still reports fruit shards outside the backpack")
	}
}

// Original 2026-11-04: with 31103.list empty, three unactivated 19103 shards
// decomposed into 75 source. Requests use the id advertised by the backpack.
func TestEmoguoshiDecomposeUnactivatedShards(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseID     int64
		num        int64
		wantResult int64
		wantShards int64
		wantSource int64
	}{
		{"all three", 19103, 3, 1, 0, 1775},
		{"partial stack", 19103, 1, 1, 2, 1725},
		{"zero", 19103, 0, 0, 3, 1700},
		{"insufficient", 19103, 4, 0, 3, 1700},
		{"maximum wire quantity", 19103, 1<<32 - 1, 0, 3, 1700},
		{"not a shard", 19502, 1, 0, 3, 1700},
		{"missing instance", 145, 1, 0, 3, 1700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, c, role := emoEnv(t)
			e.addItems(t, role, map[int64]int64{19103: 3, 19502: 1700})
			c.send(31103, proto.Message{})
			if list := c.waitFor(31103).Arr("list"); len(list) != 0 {
				t.Fatalf("expected no activated fruits: %v", list)
			}
			c.send(10500, proto.Message{})
			bag := c.waitFor(10500).Arr("item_list")
			instance := tc.baseID
			found := false
			for _, item := range bag {
				if item.Int("base_id") == tc.baseID {
					instance = item.Int("id")
					found = true
				}
			}
			if tc.baseID != 145 && !found {
				t.Fatalf("owned item %d absent from 10500: %v", tc.baseID, bag)
			}
			c.settle()
			c.send(31130, proto.Message{"id": instance, "num": tc.num})
			if reply := c.waitFor(31130); reply.Int("result") != tc.wantResult || tc.wantResult == 1 && reply.Str("msg") != "分解成功" {
				t.Fatalf("decompose: %v", reply)
			}
			c.settle()
			items, err := e.st.RoleItems(role.RID)
			if err != nil {
				t.Fatal(err)
			}
			if items[19103] != tc.wantShards || items[19502] != tc.wantSource {
				t.Fatalf("shards=%d source=%d, want %d/%d", items[19103], items[19502], tc.wantShards, tc.wantSource)
			}
			if fruits := e.emoState(t, role).Fruits; len(fruits) != 0 {
				t.Fatalf("decomposition changed fruit activation: %v", fruits)
			}
			if tc.wantResult == 1 {
				// Reusing a fully consumed instance or asking for more than remains
				// must not credit the reward a second time.
				c.send(31130, proto.Message{"id": instance, "num": 3})
				if reply := c.waitFor(31130); reply.Int("result") != 0 {
					t.Fatalf("repeated decomposition: %v", reply)
				}
				after, err := e.st.RoleItems(role.RID)
				if err != nil || after[19103] != tc.wantShards || after[19502] != tc.wantSource {
					t.Fatalf("failed request changed inventory: %v, %v", after, err)
				}
			}
		})
	}
}

func TestEmoguoshiShopAndDraw(t *testing.T) {
	e, c, role := emoEnv(t)
	c.send(31109, proto.Message{})
	if r := c.waitFor(31109); r.Int("jifen") != 0 {
		t.Fatalf("31109: %v", r)
	}
	c.send(31106, proto.Message{"id": 8801, "num": 1}) // 1500 seeds
	if r := c.waitFor(31106); r.Int("result") != 0 {
		t.Fatalf("bought without seeds: %v", r)
	}
	// the draw: a ticket per pull; ten pulls give a hundred seeds
	e.addItems(t, role, map[int64]int64{10411: 10})
	c.send(31115, proto.Message{"id": 10001, "type": 10, "up_id": 0})
	if r := c.waitFor(31115); r.Int("result") != 1 {
		t.Fatalf("ten pulls: %v", r)
	}
	c.settle()
	assets, _ := e.st.RoleAssets(role.RID)
	items, _ := e.st.RoleItems(role.RID)
	if assets[emoSeed] != 100 || items[10411] != 0 {
		t.Fatalf("seeds=%d tickets=%d", assets[emoSeed], items[10411])
	}
	if e.emoState(t, role).Total["10001"] != 10 {
		t.Fatalf("pulls: %v", e.emoState(t, role).Total)
	}
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 0 {
		t.Fatalf("ticket pulls counted as diamond pulls: %v", r)
	}
	c.send(31115, proto.Message{"id": 10002, "type": 1}) // the second box wants 2100 points
	if r := c.waitFor(31115); r.Int("result") != 0 {
		t.Fatalf("a box that is not open: %v", r)
	}
	// diamonds: 300 a pull, ten pulls a day at most
	e.grantAssets(c, role, map[int64]int64{3: 100000, emoSeed: 5000})
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 0 {
		t.Fatalf("31116: %v", r)
	}
	c.send(31115, proto.Message{"id": 10001, "type": 10})
	if r := c.waitFor(31115); r.Int("result") != 1 {
		t.Fatalf("ten pulls with diamonds: %v", r)
	}
	if r := c.waitFor(31116); r.Int("times") != 10 {
		t.Fatalf("used diamond count push after ten pulls: %v", r)
	}
	c.send(31115, proto.Message{"id": 10001, "type": 1})
	if r := c.waitFor(31115); r.Int("result") != 0 {
		t.Fatalf("an eleventh diamond pull: %v", r)
	}
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 10 {
		t.Fatalf("failed pull changed used diamond count: %v", r)
	}
	e.addItems(t, role, map[int64]int64{10411: 1})
	c.send(31115, proto.Message{"id": 10001, "type": 1})
	if r := c.waitFor(31115); r.Int("result") != 1 {
		t.Fatalf("ticket pull at diamond limit: %v", r)
	}
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 10 {
		t.Fatalf("ticket pull changed used diamond count: %v", r)
	}
	// the shop
	c.send(31106, proto.Message{"id": 8801, "num": 1})
	if r := c.waitFor(31107); r.Int("id") != 8801 || r.Int("times") != 1 {
		t.Fatalf("31107: %v", r)
	}
	if r := c.waitFor(31106); r.Int("result") != 1 {
		t.Fatalf("buy: %v", r)
	}
	c.send(31106, proto.Message{"id": 8801, "num": 1}) // limit 1
	if r := c.waitFor(31106); r.Int("result") != 0 {
		t.Fatalf("bought past the limit: %v", r)
	}
	c.send(31106, proto.Message{"id": 8805, "num": 1}) // wants 2100 points
	if r := c.waitFor(31106); r.Int("result") != 0 {
		t.Fatalf("bought a good that is locked: %v", r)
	}
	c.send(31108, proto.Message{})
	if l := c.waitFor(31108).Arr("list"); len(l) != 1 {
		t.Fatalf("31108: %v", l)
	}
	c.send(31109, proto.Message{})
	if r := c.waitFor(31109); r.Int("jifen") != 1500 {
		t.Fatalf("points: %v", r)
	}
}

func TestEmoguoshiDrawTimesPersistAndReset(t *testing.T) {
	e, c, role := emoEnv(t)
	c.send(31115, proto.Message{"id": 10001, "type": 1})
	if r := c.waitFor(31115); r.Int("result") != 0 {
		t.Fatalf("draw without payment: %v", r)
	}
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 0 {
		t.Fatalf("failed payment consumed a diamond pull: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 1000})
	c.send(31115, proto.Message{"id": 10001, "type": 1})
	if r := c.waitFor(31115); r.Int("result") != 1 {
		t.Fatalf("single diamond pull: %v", r)
	}
	if r := c.waitFor(31116); r.Int("times") != 1 {
		t.Fatalf("single diamond pull count push: %v", r)
	}
	c.settle()
	assets, err := e.st.RoleAssets(role.RID)
	if err != nil || assets[3] != 700 {
		t.Fatalf("diamond balance after single pull: %v, %v", assets, err)
	}
	c, _ = e.dialSameRole("ug", role)
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 1 {
		t.Fatalf("used diamond count after reentry: %v", r)
	}
	c.settle()
	e.advance(24 * time.Hour)
	c.send(31116, proto.Message{})
	if r := c.waitFor(31116); r.Int("times") != 0 {
		t.Fatalf("used diamond count after daily reset: %v", r)
	}
	c.settle()
	if state := e.emoState(t, role); state.DrawToday != 0 || state.Total["10001"] != 1 {
		t.Fatalf("daily reset changed lifetime draws: %+v", state)
	}
}

func TestEmoguoshiFruitGift(t *testing.T) {
	e, c, role := emoEnv(t)
	c.send(31120, proto.Message{})
	g := c.waitFor(31120)
	if g.Int("open_tag") != 1 || g.Int("unlock_tag") != 0 || g.Int("open_id") != 1 || len(g.Arr("list")) != 16 || g.Int("end_time") <= time.Now().Unix() {
		t.Fatalf("31120: %v", g)
	}
	c.send(31122, proto.Message{"index": 1, "type": 1}) // locked
	if r := c.waitFor(31122); r.Int("result") != 0 {
		t.Fatalf("took before unlocking: %v", r)
	}
	c.send(31123, proto.Message{}) // 300 gems
	if r := c.waitFor(31123); r.Int("result") != 0 {
		t.Fatalf("unlocked without gems: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{3: 1000})
	c.send(31123, proto.Message{})
	if r := c.waitFor(31123); r.Int("result") != 1 {
		t.Fatalf("unlock: %v", r)
	}
	c.send(31122, proto.Message{"index": 1, "type": 1}) // process 0
	if r := c.waitFor(31122); r.Int("result") != 1 {
		t.Fatalf("first step: %v", r)
	}
	c.send(31122, proto.Message{"index": 1, "type": 1})
	if r := c.waitFor(31122); r.Int("result") != 0 {
		t.Fatalf("step twice: %v", r)
	}
	c.send(31122, proto.Message{"index": 2, "type": 1}) // 100 points needed
	if r := c.waitFor(31122); r.Int("result") != 0 {
		t.Fatalf("step without points: %v", r)
	}
	c.send(31122, proto.Message{"index": 1, "type": 2}) // the paid track
	if r := c.waitFor(31122); r.Int("result") != 0 {
		t.Fatalf("paid track: %v", r)
	}
	c.settle()
	st := e.emoState(t, role)
	st.GiftValue = 150
	e.st.SaveState(role.RID, emoModule, st)
	c.send(31122, proto.Message{"index": 2, "type": 1})
	if r := c.waitFor(31122); r.Int("result") != 1 {
		t.Fatalf("second step: %v", r)
	}
	c.send(31120, proto.Message{})
	if g := c.waitFor(31120); g.Int("unlock_tag") != 1 || g.Arr("list")[0].Int("tag1") != 1 || g.Arr("list")[1].Int("tag1") != 1 || g.Arr("list")[2].Int("tag1") != 0 {
		t.Fatalf("31120 after: %v", g)
	}
}
