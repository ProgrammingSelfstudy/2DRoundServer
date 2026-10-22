package game

import (
	"bufio"
	"encoding/json"
	"math/rand"
	"os"
	"testing"
)

// the monsters taken from the client's tables match what the original server sent
// for dungeon 10050 (capture original-2026-10-20): names, camps, careers, levels,
// skills, positions, and the hp of the two monsters that follow level x 134.
func TestDungeonUnitsMatchCapture(t *testing.T) {
	f, err := os.Open("../../testdata/captures/original-2026-10-20/battle-flow.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var intro struct {
		Cmd int `json:"cmd"`
		Msg struct {
			TargetRoleName string `json:"target_role_name"`
			Objects        []struct {
				Group  int    `json:"group"`
				Bid    int64  `json:"object_bid"`
				Name   string `json:"object_name"`
				Camp   int64  `json:"camp_type"`
				Career int64  `json:"career"`
				Lev    int64  `json:"lev"`
				Hp     int64  `json:"hp"`
				Pos    int64  `json:"pos"`
				Skills []struct {
					SkillBid int64 `json:"skill_bid"`
				} `json:"skills"`
			} `json:"objects"`
		} `json:"msg"`
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	found := false
	for sc.Scan() {
		var row struct {
			Cmd int `json:"cmd"`
		}
		json.Unmarshal(sc.Bytes(), &row)
		if row.Cmd == 20027 {
			if err := json.Unmarshal(sc.Bytes(), &intro); err != nil {
				t.Fatal(err)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no 20027 in the capture")
	}
	wave := dungeonUnits[10050]
	if wave.Group != intro.Msg.TargetRoleName || wave.UnitID != 20105 || len(wave.Monsters) != 3 {
		t.Fatalf("group %q unit %d with %d monsters, capture says %q", wave.Group, wave.UnitID, len(wave.Monsters), intro.Msg.TargetRoleName)
	}
	seen := 0
	for _, o := range intro.Msg.Objects {
		if o.Group != 2 {
			continue
		}
		seen++
		var idx = -1
		for i, m := range wave.Monsters {
			if m.Bid == o.Bid {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("monster %d (%s) is not in our dungeon", o.Bid, o.Name)
		}
		m := wave.Monsters[idx]
		var skills []int64
		for _, s := range o.Skills {
			skills = append(skills, s.SkillBid)
		}
		if m.Name != o.Name || m.Camp != o.Camp || m.Career != o.Career || m.Lev != o.Lev || len(m.Skills) != len(skills) {
			t.Fatalf("monster %d: ours %+v, capture %+v", o.Bid, m, o)
		}
		for i := range skills {
			if m.Skills[i] != skills[i] {
				t.Fatalf("monster %d skills: ours %v, capture %v", o.Bid, m.Skills, skills)
			}
		}
		u, obj := monsterOf(m, idx)
		if u.pos != o.Pos || obj.Int("pos") != o.Pos {
			t.Fatalf("monster %d stands on %d, capture %d", o.Bid, u.pos, o.Pos)
		}
		if o.Hp == m.Lev*monsterHPPerLevel && u.hp != o.Hp { // the calibration holds where the capture agrees with it
			t.Fatalf("monster %d hp %d, capture %d", o.Bid, u.hp, o.Hp)
		}
	}
	if seen != 3 {
		t.Fatalf("%d monsters in the capture", seen)
	}
}

// every dungeon of the chain has one to five monsters, all with an attack, and the
// first dungeon is the one-monster start of the campaign.
func TestDungeonUnitsCoverTheChain(t *testing.T) {
	for _, d := range dramaChain.Dungeons {
		w, ok := dungeonUnits[d.ID]
		if !ok || len(w.Monsters) < 1 || len(w.Monsters) > len(monsterPos) || w.UnitID == 0 || w.Group == "" {
			t.Fatalf("dungeon %d: %+v", d.ID, w)
		}
		for _, m := range w.Monsters {
			if m.Lev < 1 || len(m.Skills) == 0 || m.Name == "" {
				t.Fatalf("dungeon %d monster %+v", d.ID, m)
			}
		}
	}
	if w := dungeonUnits[dramaChain.First]; len(w.Monsters) != 1 || w.Monsters[0].Lev != 1 {
		t.Fatalf("the first dungeon: %+v", w)
	}
}

// a monster's hp follows its level, a monster without a skill gets the default one,
// and the positions follow the order of the client's monster1..monster5.
func TestMonsterOf(t *testing.T) {
	m := monsterDef{Bid: 101051, Name: "海贼10号", Career: 5, Camp: 2, Lev: 7, Head: "20202", Skills: nil}
	u, obj := monsterOf(m, 3)
	if u.hp != 7*monsterHPPerLevel || u.maxHP != u.hp || u.pos != monsterPos[3] || len(u.skills) != 1 || u.skills[0] != defaultMonsterSkill || u.speed <= 0 {
		t.Fatalf("unit %+v", u)
	}
	if obj.Int("hp") != u.hp || obj.Int("object_bid") != 101051 || obj.Int("group") != 2 || obj.Int("object_type") != 3 || len(obj.Arr("skills")) != 1 {
		t.Fatalf("object %v", obj)
	}
	// an unknown template still makes a usable monster
	if u2, _ := monsterOf(monsterDef{Bid: 1, Lev: 2, Head: "none", Skills: []int64{274001}}, 0); u2.hp != 2*monsterHPPerLevel || u2.speed <= 0 || u2.atk <= 0 {
		t.Fatalf("fallback unit %+v", u2)
	}
}

// the dungeons are made for five heroes: with fewer, the monsters hit lighter (in
// proportion), so a lone starter hero is not hopeless against five monsters.
func TestMonstersHitLighterAgainstSmallTeams(t *testing.T) {
	hit := func(teamSize int64) int64 {
		hero := fightUnit{pos: 9, speed: 10, hp: 4000, maxHP: 4000, atk: 10, skills: []int64{200001}, ours: true, lastCast: map[int64]int{}}
		mon := fightUnit{pos: 18, speed: 10, hp: 100, maxHP: 100, atk: 5, skills: []int64{267001}, lastCast: map[int64]int{}}
		f := &fightSim{units: []fightUnit{hero, mon}, rng: rand.New(rand.NewSource(1)), round: 1, teamSize: teamSize}
		return -f.act(&f.units[1], 2).Arr("effect_play")[0].Int("hp_changed")
	}
	one, three, five := hit(1), hit(3), hit(5)
	if five <= 0 || three*5 != five*3 && three != five*3/5 || one != five/5 {
		t.Fatalf("damage with 1 / 3 / 5 heroes: %d / %d / %d", one, three, five)
	}
}
