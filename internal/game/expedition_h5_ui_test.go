package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestH5ExpeditionMedalShop(t *testing.T) {
	e := newDungeonEnv(t)
	e.setNow(time.Date(2026, time.November, 22, 12, 0, 0, 0, dayZone)) // Sunday
	c, role := enterDungeonRole(e, store.PlatformH5, "exp-shop", "远征商客")
	e.grantAssets(c, role, map[int64]int64{1: 50000})
	buy := func(id, count int64, wantCode int64) proto.Message {
		t.Helper()
		c.send(13402, proto.Message{"eid": id, "num": count})
		m := c.waitFor(13402)
		if m.Int("code") != wantCode || m.Int("eid") != id || m.Int("type") != 8 {
			t.Fatalf("buy %d x%d: %v", id, count, m)
		}
		return m
	}
	counts := func() map[int64]int64 {
		t.Helper()
		c.send(13401, proto.Message{"type": 8})
		m := c.waitFor(13401)
		if m.Int("code") != 1 || m.Int("type") != 8 {
			t.Fatalf("counts: %v", m)
		}
		out := make(map[int64]int64)
		for _, row := range m.Arr("item_list") {
			for _, ext := range row.Arr("ext") {
				if ext.Int("key") == 1 {
					out[row.Int("item_id")] = ext.Int("val")
				}
			}
		}
		return out
	}
	buy(8012, 1, 0) // 50,000 coins cannot pay the 3,750 medal price
	if got := counts(); len(got) != 0 {
		t.Fatalf("an unaffordable transaction took quota: %v", got)
	}
	e.grantAssets(c, role, map[int64]int64{25: 100000})
	buy(8004, 1, 0) // Four-star shards are excluded by the H5 shop policy.
	buy(8012, 2, 0)
	if m := buy(8012, 1, 1); len(m.Arr("ext")) != 1 || m.Arr("ext")[0].Int("val") != 1 {
		t.Fatalf("weekly count: %v", m)
	}
	if assets := c.waitFor(10302); assets.Int("expedition_medal") != 96250 || assets.Str("coin") != "50000" {
		t.Fatalf("wire balance after purchase: %v", assets)
	}
	c.waitFor(10500)
	buy(8012, 1, 0)
	buy(8007, 1, 1)
	c.settle()
	if got := counts(); got[8012] != 1 || got[8007] != 1 || len(got) != 2 {
		t.Fatalf("original exchange counts: %v", got)
	}

	e.advance(24 * time.Hour)
	if got := counts(); got[8012] != 0 || got[8007] != 1 {
		t.Fatalf("Monday resets only the weekly quota: %v", got)
	}
	buy(8012, 1, 1)
	buy(8007, 1, 0)
	e.setNow(time.Date(2026, time.December, 1, 12, 0, 0, 0, dayZone))
	buy(8007, 1, 1)
	c.settle()
	if assets, err := e.st.RoleAssets(role.RID); err != nil || assets[1] != 50000 || assets[25] != 2500 {
		t.Fatalf("persistent balances: %v (%v)", assets, err)
	}
	if items, err := e.st.RoleItems(role.RID); err != nil || items[10040] != 100 || items[26907] != 100 || items[29104] != 0 {
		t.Fatalf("persistent inventory: %v (%v)", items, err)
	}
}

func TestH5ExpeditionLoanIdentityAndFormationLimit(t *testing.T) {
	e, c, borrower, ownHero := expEnvFor(t, store.PlatformH5)
	type loan struct{ owner, hero, power int64 }
	var loans []loan
	for i, name := range []string{"甲助阵", "乙助阵"} {
		friend, owner := enterDungeonRole(e, store.PlatformH5, "exp-friend-"+itoa(int64(i)), name)
		friend.send(11000, nil)
		heroes := friend.waitFor(11000).Arr("partners")
		id := heroes[0].Int("partner_id")
		if err := e.st.RequestFriend(owner.RID, borrower.RID); err != nil {
			t.Fatal(err)
		}
		if err := e.st.AcceptFriendRequest(owner.RID, borrower.RID, 50); err != nil {
			t.Fatal(err)
		}
		friend.send(24407, proto.Message{"id": id})
		if reply := friend.waitFor(24407); reply.Int("code") != 1 {
			t.Fatalf("dispatch: %v", reply)
		}
		request := proto.Message{"rid": owner.RID, "srv_id": "symlf_other", "id": id}
		c.send(24408, request)
		if reply := c.waitFor(24408); reply.Int("code") != 0 {
			t.Fatalf("wrong server hire: %v", reply)
		}
		request["srv_id"] = owner.SrvID
		c.send(24408, request)
		if reply := c.waitFor(24408); reply.Int("code") != 1 {
			t.Fatalf("hire: %v", reply)
		}
		loans = append(loans, loan{owner.RID, id, heroes[0].Int("power")})
	}
	c.send(24409, nil)
	list := c.waitFor(24409).Arr("list")
	if len(list) != 2 {
		t.Fatalf("hired list: %v", list)
	}
	for i, row := range list {
		if row.Int("rid") != loans[i].owner || row.Int("id") != loans[i].hero || row.Str("srv_id") != borrower.SrvID ||
			row.Int("power") != loans[i].power || row.Int("is_used") != 0 || row.Int("hp_per") != 100 {
			t.Fatalf("hire %d: %v", i, row)
		}
	}
	c.send(24412, proto.Message{"difficulty": 1})
	if m := c.waitFor(24412); m.Int("code") != 1 {
		t.Fatalf("difficulty: %v", m)
	}
	c.waitFor(24400)
	team := expTeam(ownHero)
	team.Arr("pos_info")[0]["owner_id"], team.Arr("pos_info")[0]["owner_srv_id"] = borrower.RID, borrower.SrvID
	first := proto.Message{"pos": 2, "id": loans[0].hero, "owner_id": loans[0].owner, "owner_srv_id": "symlf_other"}
	second := proto.Message{"pos": 3, "id": loans[1].hero, "owner_id": loans[1].owner, "owner_srv_id": borrower.SrvID}
	team["pos_info"] = append(team.Arr("pos_info"), first)
	c.send(24403, team)
	if m := c.waitFor(24403); m.Int("code") != 0 {
		t.Fatalf("wrong owner server in formation: %v", m)
	}
	first["owner_srv_id"] = borrower.SrvID
	team["pos_info"] = append(team.Arr("pos_info"), second)
	c.send(24403, team)
	if m := c.waitFor(24403); m.Int("code") != 0 || m.Str("msg") != "每次只能上阵一个雇佣英雄" {
		t.Fatalf("two hired heroes in formation: %v", m)
	}
	team["pos_info"] = team.Arr("pos_info")[:2]
	c.send(24403, team)
	if m := c.waitFor(24403); m.Int("code") != 1 {
		t.Fatalf("valid borrowed hero in formation: %v", m)
	}
	intro := c.waitFor(20027)
	var helperFound bool
	for _, hero := range intro.Arr("objects") {
		helperFound = helperFound || hero.Int("group") == 1 && hero.Int("object_id") == loans[0].hero
	}
	if !helperFound || intro.Int("a_object_num") != 2 {
		t.Fatalf("borrowed hero missing from fight: %v", intro)
	}
}
