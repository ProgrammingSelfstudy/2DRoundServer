package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestFormation(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}, {BID: 10201, Star: 2}, {BID: 10202, Star: 2}, {BID: 10301, Star: 3}, {BID: 10302, Star: 3}, {BID: 10401, Star: 4}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)

	// nothing saved yet: the first six heroes at positions 1-6
	c.send(11211, proto.Message{"type": 0})
	f := c.recv(11211)
	pi := f.Arr("pos_info")
	if f.Int("type") != 0 || f.Int("formation_type") != 1 || len(pi) != 6 || pi[0].Int("pos") != 1 || pi[0].Int("id") != heroes[0].PartnerID {
		t.Fatalf("default: %v", f)
	}
	save := func(ftype int64, slots ...[2]int64) proto.Message {
		var pos []proto.Message
		for _, s := range slots {
			pos = append(pos, proto.Message{"pos": s[0], "id": s[1]})
		}
		c.send(11212, proto.Message{"type": 0, "formation_type": ftype, "pos_info": pos, "hallows_id": 0})
		return c.recv(11212)
	}
	a, b := heroes[0].PartnerID, heroes[1].PartnerID
	for name, r := range map[string]proto.Message{
		"no heroes":      save(1),
		"unknown form":   save(9, [2]int64{1, a}),
		"bad position":   save(1, [2]int64{0, a}),
		"same position":  save(1, [2]int64{1, a}, [2]int64{1, b}),
		"same hero":      save(1, [2]int64{1, a}, [2]int64{2, a}),
		"not my hero":    save(1, [2]int64{1, 99999}),
		"position seven": save(1, [2]int64{7, a}),
	} {
		if r.Int("code") != 0 {
			t.Fatalf("%s should fail: %v", name, r)
		}
	}
	// the original server took six heroes on positions 1-6
	if r := save(1, [2]int64{1, a}, [2]int64{2, b}, [2]int64{3, heroes[2].PartnerID}, [2]int64{4, heroes[3].PartnerID}, [2]int64{5, heroes[4].PartnerID}, [2]int64{6, heroes[5].PartnerID}); r.Int("code") != 1 {
		t.Fatalf("six heroes: %v", r)
	}
	if r := save(2, [2]int64{3, b}, [2]int64{1, a}); r.Int("code") != 1 || r.Int("type") != 0 {
		t.Fatalf("save: %v", r)
	}
	// 11213 asks for several kinds at once: the saved campaign team and the arena one that mirrors it
	c.send(11213, proto.Message{"type_list": []proto.Message{{"type": 0}, {"type": 1}}})
	info := c.recv(11213).Arr("info")
	if len(info) != 2 || info[0].Int("formation_type") != 2 || len(info[0].Arr("pos_info")) != 2 || info[0].Arr("pos_info")[0].Int("pos") != 1 ||
		info[1].Int("type") != 1 || info[1].Int("formation_type") != 2 || len(info[1].Arr("pos_info")) != 2 { // a kind never saved mirrors the campaign team
		t.Fatalf("11213: %v", info)
	}
	// a hero that is gone drops out of the saved team
	if err := e.st.AscendPartner(role.RID, heroes[2].PartnerID, heroes[2].Star, []int64{b}, nil, nil); err != nil {
		t.Fatal(err)
	}
	saved, _ := e.st.Formation(role.RID, 0)
	if len(saved.Slots) != 1 || saved.Slots[0].PartnerID != a {
		t.Fatalf("after losing a hero: %+v", saved)
	}
}

func TestChallengeShowsTheFormation(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10101, Star: 1}, {BID: 10201, Star: 2}, {BID: 10202, Star: 2}, {BID: 10301, Star: 3}, {BID: 10302, Star: 3}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	if err := e.st.SaveFormation(role.RID, store.Formation{Type: 0, FormationType: 1, Slots: []store.FormationSlot{{Pos: 2, PartnerID: heroes[3].PartnerID}}}); err != nil {
		t.Fatal(err)
	}
	c.send(13003, proto.Message{})
	c.recv(13003)
	res := c.playFight()
	stats := res.Arr("hurt_statistics")[0].Arr("partner_hurts")
	if len(stats) != 1 || stats[0].Int("id") != heroes[3].PartnerID {
		t.Fatalf("damage table must list the formation: %v", stats)
	}
}
