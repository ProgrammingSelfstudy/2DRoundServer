package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// dungeon_units.json holds the monsters of every main-line dungeon, taken from the
// restored client (tools/extract_dungeon_units.py): the unit group of the dungeon
// (its boss name), and per monster its name, career, camp, level and skills. The
// client does not carry the monsters' hp, attack and speed; see monsterOf.
//
//go:embed data/dungeon_units.json
var dungeonUnitsJSON []byte

type monsterDef struct {
	Bid    int64   `json:"bid"`
	Name   string  `json:"name"`
	Career int64   `json:"career"`
	Camp   int64   `json:"camp"`
	Lev    int64   `json:"lev"`
	Head   string  `json:"head"` // the hero template the monster is made of
	Skills []int64 `json:"skills"`
}

type dungeonUnit struct {
	Name     string       `json:"name"`
	UnitID   int64        `json:"unit_id"`
	Group    string       `json:"group"` // shown as the opponent's name
	Power    int64        `json:"power"`
	Monsters []monsterDef `json:"monsters"`
}

var dungeonUnits = func() map[int64]dungeonUnit {
	var f struct {
		Dungeons map[int64]dungeonUnit `json:"dungeons"`
	}
	if err := json.Unmarshal(dungeonUnitsJSON, &f); err != nil || len(f.Dungeons) == 0 {
		panic("game: bad dungeon_units.json")
	}
	return f.Dungeons
}()

// monsterPos are the battle positions of a dungeon's monsters in the order of the
// client's monster1..monster5: the capture has 18, 16, 15 for the first three, the
// same slots our heroes use (9, 7, 6) plus 9; the last two are our guess.
var monsterPos = []int64{18, 16, 15, 13, 14}

// monsterHPPerLevel gives a monster its hp: 134 per level makes the 3rd-level
// monsters of the capture's dungeon 402, as the capture shows for two of its three.
// Our rule; the real hp comes from the server and is only known for that sample.
const monsterHPPerLevel = 134

// defaultMonsterSkill is the attack of a monster whose template lists no active skill.
const defaultMonsterSkill = 274001

// lowestStar is the lowest star the hero template has data for (the client builds
// a monster from it).
func lowestStar(bid int64) int {
	info, ok := partnerTable[bid]
	if !ok {
		return 1
	}
	var stars []int
	for k := range info.Stars {
		if n, err := strconv.Atoi(k); err == nil {
			stars = append(stars, n)
		}
	}
	if len(stars) == 0 {
		return 1
	}
	sort.Ints(stars)
	return stars[0]
}

// monsterOf makes the fighter and the 20027 object of a dungeon's monster. Hp is
// monsterHPPerLevel per level; attack and speed come from the monster's hero
// template at its level (the speed times 1.31: the capture's level-3 monsters have
// 67 where the template gives 51). Attack only sizes heals and damage over time, as
// the monsters' hits are tiny (see buildFight).
func monsterOf(m monsterDef, idx int) (fightUnit, proto.Message) {
	pos := monsterPos[idx]
	sids := m.Skills
	if len(sids) == 0 {
		sids = []int64{defaultMonsterSkill}
	}
	hp := m.Lev * monsterHPPerLevel
	if hp < 1 {
		hp = 1
	}
	var atk, speed int64 = hp / 8, 60
	if head, err := strconv.ParseInt(m.Head, 10, 64); err == nil {
		if _, ok := partnerTable[head]; ok {
			_, a, _, sp := partnerStats(store.Partner{BID: head, Lev: int(m.Lev), Star: lowestStar(head)})
			atk, speed = a, sp*131/100
		}
	}
	skills := []proto.Message{}
	for _, sid := range sids {
		skills = append(skills, proto.Message{"skill_bid": sid, "end_round": 0})
	}
	obj := proto.Message{
		"camp_type": m.Camp, "career": m.Career, "extra_data": []proto.Message{}, "face_id": 0, "group": 2, "hp": hp, "hp_max": hp, "lev": m.Lev,
		"object_bid": m.Bid, "object_id": 0, "object_name": m.Name, "object_type": 3, "owner_id": 0, "owner_srv_id": "", "pos": pos,
		"sex": 0, "skills": skills, "sprites": []proto.Message{}, "star": 0,
	}
	return fightUnit{pos: pos, speed: speed, hp: hp, maxHP: hp, atk: atk, skills: sids}, obj
}
