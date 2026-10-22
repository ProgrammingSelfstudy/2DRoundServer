package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The original server answers 20060 for the campaign with type 2 and pushes 20061
// (capture original-2026-10-20); other fight types keep type 0 and get no push.
func TestCombatTypeCampaignPushesHookBattle(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, r := e.enter("a", "甲")
	if _, err := e.st.GrantStarterPartners(r.RID, []store.PartnerSeed{{BID: 10401, Star: 4}, {BID: 10302, Star: 3}}); err != nil {
		t.Fatal(err)
	}

	c.send(20060, proto.Message{"combat_type": combatDrama})
	if a := c.recv(20060); a.Int("combat_type") != combatDrama || a.Int("type") != 2 {
		t.Fatalf("20060: %v", a)
	}
	hook := c.recv(20061)
	team, waves := hook.Arr("partner_list"), hook.Arr("wave_list")
	if hook.Int("combat_type") != combatDrama || len(team) == 0 || len(waves) != 1 || len(waves[0].Arr("unit_list")) != len(dungeonUnits[dramaChain.First].Monsters) {
		t.Fatalf("20061: %v", hook)
	}
	h := team[0]
	if h.Int("hp") <= 0 || h.Int("speed") <= 0 || len(h.Arr("skill_list")) != 1 || h.Int("pos") != battleCell(1, 1) {
		t.Fatalf("hero entry: %v", h)
	}
	sk := h.Arr("skill_list")[0]
	if ef := sk.Arr("effect_list"); len(ef) != 1 || ef[0].Int("eid") != sk.Int("sid")/100*1000+sk.Int("sid")%100 {
		t.Fatalf("effect id pattern: %v", sk)
	}

	// any other fight type: plain answer, no 20061
	c.send(20060, proto.Message{"combat_type": 0})
	if a := c.recv(20060); a.Int("type") != 0 {
		t.Fatalf("20060 type 0: %v", a)
	}
	c.expectNothing()
}

// 13000 lists the dungeons of the current chapter up to the current one.
func TestDramaModeList(t *testing.T) {
	d := dramaChain.Dungeons[2] // third dungeon of the first chapter
	m := dramaModeList(store.Drama{Mode: d.Mode, ChapterID: d.Chapter, DunID: d.ID, Status: dungeonNotCleared, MaxDunID: d.ID})
	chapters := m[0].Arr("chapter_list")
	duns := chapters[0].Arr("dun_list")
	if len(duns) != 3 || duns[0].Int("dun_id") != d.ID || duns[0].Int("status") != dungeonNotCleared || duns[1].Int("status") != dungeonCleared || duns[2].Int("status") != dungeonCleared {
		t.Fatalf("mode_list: %v", m)
	}
}
