package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestClearDungeonRules(t *testing.T) {
	first := dramaChain.Dungeons[0]
	d := store.Drama{RID: 1, Mode: first.Mode, ChapterID: first.Chapter, DunID: first.ID, Status: dungeonNotCleared, MaxDunID: first.ID, IsFirst: true}
	next, reward, msg := clearDungeon(d)
	if msg != "" || next.DunID != first.Next || next.MaxDunID != first.Next || next.IsFirst || next.Status != dungeonNotCleared {
		t.Fatalf("frontier: %+v %q", next, msg)
	}
	if len(reward) != 4 || reward[0] != (sellGain{5, 10 * clearRewardMin}) {
		t.Fatalf("reward of the first dungeon: %v", reward)
	}
	// a replay of an earlier dungeon pays again and moves nothing
	replay := store.Drama{RID: 1, Mode: first.Mode, ChapterID: first.Chapter, DunID: first.ID, Status: dungeonCleared, MaxDunID: first.Next}
	if n, r, _ := clearDungeon(replay); n != replay || len(r) == 0 {
		t.Fatalf("replay: %+v %v", n, r)
	}
	// the last dungeon of the chain stays and is marked cleared
	last := dramaChain.Dungeons[len(dramaChain.Dungeons)-1]
	end := store.Drama{RID: 1, Mode: last.Mode, ChapterID: last.Chapter, DunID: last.ID, MaxDunID: last.ID}
	if n, _, _ := clearDungeon(end); n.DunID != last.ID || n.Status != dungeonCleared {
		t.Fatalf("end of chain: %+v", n)
	}
	// every dungeon of the chain has numbers
	for _, dg := range dramaChain.Dungeons {
		if len(dungeonTable[dg.ID].PerHook) == 0 {
			t.Fatalf("dungeon %d has no reward", dg.ID)
		}
	}
}

func TestChallengeAdvancesTheCampaign(t *testing.T) {
	easyDungeons(t)
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	first := dramaChain.Dungeons[0]
	c.send(13003, proto.Message{"is_auto": 0})
	if r := c.recv(13003); r.Int("code") != 1 {
		t.Fatalf("13003: %v", r)
	}
	res := c.playFight()
	if res.Int("result") != 1 || res.Int("combat_type") != 3 || len(res.Arr("item_rewards")) != 4 {
		t.Fatalf("13016: %v", res)
	}
	for _, it := range res.Arr("item_rewards") { // every reward of both captures is bound
		if it.Int("is_bind") != 1 {
			t.Fatalf("item_rewards is_bind: %v", it)
		}
	}
	up := c.recv(13001)
	if up.Int("dun_id") != first.Next || up.Int("max_dun_id") != first.ID || up.Int("is_first") != 0 { // max_dun_id: the dungeon just cleared
		t.Fatalf("13001: %v", up)
	}
	c.recv(10302)
	c.recv(10500)
	as, _ := e.st.RoleAssets(role.RID)
	if as[1] != 200*clearRewardMin || as[22] != 25*clearRewardMin || as[5] != 10*clearRewardMin {
		t.Fatalf("reward: %v", as)
	}
	d, _ := e.st.Drama(role.RID)
	if d.DunID != first.Next || d.MaxDunID != first.Next {
		t.Fatalf("stored: %+v", d)
	}
	// fighting to the end of chapter 1 sends the chapter unlock once
	sawUnlock := false
	for i := 0; i < len(dramaChain.Dungeons) && !sawUnlock; i++ {
		c.send(13003, proto.Message{})
		c.recv(13003)
		c.playFight()
		u := c.recv(13001)
		if dungeonByID[u.Int("dun_id")].Chapter != 1 {
			if cp := c.recv(13010); cp.Int("chapter_id") != 2 {
				t.Fatalf("13010: %v", cp)
			}
			sawUnlock = true
		}
		c.recv(10302)
		c.recv(10500)
	}
	if !sawUnlock {
		t.Fatal("never left chapter 1")
	}
}
