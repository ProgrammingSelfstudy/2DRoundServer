package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestGuildSkillHeavens(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "会长")
	if _, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: role.RID}, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.send(23700, proto.Message{"career": 3})
	if r := c.waitFor(23700); r.Int("group_id") != 1001 || len(r.Arr("skill_ids")) != 0 || len(r.Arr("group_ids")) != 0 {
		t.Fatalf("fresh career: %v", r)
	}
	c.send(23703, proto.Message{})
	if r := c.waitFor(23703); len(r.Arr("outline")) != 4 {
		t.Fatalf("outline: %v", r)
	}
	c.send(23701, proto.Message{"skill_id": 10011})
	if r := c.waitFor(23701); r.Int("code") != 0 {
		t.Fatalf("cannot pay: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{10: 100000, 1: 10000000, 3: 1000})
	c.send(23701, proto.Message{"skill_id": 10013})
	if r := c.waitFor(23701); r.Int("code") != 0 {
		t.Fatalf("out of order: %v", r)
	}
	c.send(23701, proto.Message{"skill_id": 10021})
	if r := c.waitFor(23701); r.Int("code") != 0 {
		t.Fatalf("a later heaven: %v", r)
	}
	for _, id := range []int64{10011, 10012, 10013, 10014, 10015, 10016} {
		c.send(23701, proto.Message{"skill_id": id})
		if r := c.waitFor(23701); r.Int("code") != 1 || r.Int("career") != 3 || r.Int("skill_id") != id {
			t.Fatalf("activate %d: %v", id, r)
		}
	}
	if r := c.waitFor(23702); r.Int("career") != 3 || r.Int("group_id") != 1002 {
		t.Fatalf("next heaven: %v", r)
	}
	c.send(23700, proto.Message{"career": 3})
	if r := c.waitFor(23700); r.Int("group_id") != 1002 || len(r.Arr("skill_ids")) != 0 || len(r.Arr("group_ids")) != 1 || r.Arr("group_ids")[0].Int("group_id") != 1001 {
		t.Fatalf("after the heaven: %v", r)
	}
	// 10 contribution + 1 coin per skill: (12+14+16+18+20 + 10) * 1 contribution and the coin the table names
	c.send(23704, proto.Message{"career": 3})
	pre := c.waitFor(23704)
	if pre.Int("is_first") != 1 || pre.Int("gold") != 300 || len(pre.Arr("list")) != 2 {
		t.Fatalf("reset preview: %v", pre)
	}
	back := map[int64]int64{}
	for _, it := range pre.Arr("list") {
		back[it.Int("id")] = it.Int("num")
	}
	if back[10] != 10+12+14+16+18+20 || back[1] != (6000+8400+8400+9600+10800+12000)/2 {
		t.Fatalf("refund: %v", back)
	}
	c.send(23705, proto.Message{"career": 3})
	if r := c.waitFor(23705); r.Int("code") != 1 || r.Int("career") != 3 {
		t.Fatalf("reset: %v", r)
	}
	c.send(23700, proto.Message{"career": 3})
	if r := c.waitFor(23700); r.Int("group_id") != 1001 || len(r.Arr("group_ids")) != 0 {
		t.Fatalf("after reset: %v", r)
	}
	c.send(23705, proto.Message{"career": 3})
	if r := c.waitFor(23705); r.Int("code") != 0 {
		t.Fatalf("nothing left to reset: %v", r)
	}
}

func TestGuildSkillPvp(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ua", "会长")
	c.send(23706, proto.Message{"career": 3})
	if r := c.waitFor(23706); r.Int("flag") != 0 {
		t.Fatalf("heavens not done: %v", r)
	}
	err := e.st.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
		st.PveDone[3] = gskillHeavens - 1
		st.PveActive[3] = []int64{10401, 10402, 10403, 10404, 10405, 10406}
		return
	})
	if err != nil {
		t.Fatal(err)
	}
	c.send(23711, proto.Message{})
	all := c.waitFor(23711)
	if all.Int("is_first") != 1 || len(all.Arr("guild_skill_break_group")) != 4 || len(all.Arr("guild_skill_break_group")[0].Arr("attr_formation")) != 0 {
		t.Fatalf("before the break: %v", all)
	}
	c.send(23706, proto.Message{"career": 3})
	pushed := c.waitFor(23710)
	if len(pushed.Arr("attr_formation")) != 4 || pushed.Int("skill_lev") != 0 || pushed.Arr("attr_formation")[0].Int("id") != 10001 {
		t.Fatalf("pushed state: %v", pushed)
	}
	if r := c.waitFor(23706); r.Int("flag") != 1 || r.Int("career") != 3 {
		t.Fatalf("break: %v", r)
	}
	c.send(23707, proto.Message{"career": 3, "id": 10001})
	if r := c.waitFor(23707); r.Int("flag") != 0 {
		t.Fatalf("cannot pay: %v", r)
	}
	e.grantAssets(c, role, map[int64]int64{10: 1000000, 3: 1000})
	if err := e.st.UpdateGuildSkills(role.RID, func(*store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
		return nil, nil, nil, map[int64]int64{10006: 100}, nil
	}); err != nil {
		t.Fatal(err)
	}
	c.send(23707, proto.Message{"career": 3, "id": 10001})
	if r := c.waitFor(23710); r.Arr("attr_formation")[0].Int("lev") != 1 {
		t.Fatalf("attribute level: %v", r)
	}
	if r := c.waitFor(23707); r.Int("flag") != 1 {
		t.Fatalf("raise: %v", r)
	}
	c.send(23707, proto.Message{"career": 3, "id": 20001}) // another career's attribute
	if r := c.waitFor(23707); r.Int("flag") != 0 {
		t.Fatalf("foreign attribute: %v", r)
	}
	c.send(23708, proto.Message{"career": 3}) // the other three attributes are still 0
	if r := c.waitFor(23708); r.Int("flag") != 0 {
		t.Fatalf("skill needs every attribute at 5: %v", r)
	}
	err = e.st.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
		for id := range st.PvpAttrs[3] {
			st.PvpAttrs[3][id] = 5
		}
		return nil, nil, nil, map[int64]int64{10: 100000, 10006: 100}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c.send(23708, proto.Message{"career": 3})
	if r := c.waitFor(23710); r.Int("skill_lev") != 1 || r.Int("skill_id") != 1001101 {
		t.Fatalf("skill level: %v", r)
	}
	if r := c.waitFor(23708); r.Int("flag") != 1 {
		t.Fatalf("raise skill: %v", r)
	}
	c.send(23709, proto.Message{"career": 3})
	if r := c.waitFor(23710); r.Int("skill_lev") != 0 || r.Arr("attr_formation")[0].Int("lev") != 0 || r.Int("is_first") != 0 {
		t.Fatalf("after the reset: %v", r)
	}
	if r := c.waitFor(23709); r.Int("flag") != 1 {
		t.Fatalf("reset: %v", r)
	}
	st, err := e.st.GuildSkillsOf(role.RID)
	if err != nil || !st.PvpReset || st.PvpLev[3] != 0 {
		t.Fatalf("stored: %+v %v", st, err)
	}
}
