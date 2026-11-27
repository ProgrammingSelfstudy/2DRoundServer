package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"strconv"
	"time"
)

// adventure.json: the sky island adventure of the client (tools/extract_adventure.py).
//
//go:embed data/adventure.json
var adventureJSON []byte

// Event types (AdventureEvent.EventType of the client).
const (
	evtNull         = 0
	evtBuff         = 1
	evtBoss         = 2
	evtGuess        = 3
	evtBox          = 4
	evtMon          = 5
	evtNpc          = 6
	evtAnswer       = 7
	evtNext         = 9
	evtCollect      = 10
	evtMysterious   = 15
	evtNpcTalk      = 13
	evtShop         = 14
	evtMon1         = 17
	evtMon2         = 18
	evtMon3         = 19
	evtSkill        = 20
	advFloors       = 100
	advDoorRoom     = 13 // the room of the way to the next floor (the client sends 13 for it)
	advKeyItem      = 10152
	advOre          = 72001 // 炼神矿, the island's own currency
	advRoundDays    = 2
	advMaxHeroes    = 5 // data_explain: 可选择5个英雄进行冒险
	combatAdventure = 6 // BattleConst.Fight_Type.Adventrue
)

type advEventCfg struct {
	EvtID     int64           `json:"evt_id"`
	EvtType   int64           `json:"evt_type"`
	Lose      [][]int64       `json:"lose"`
	Effect    json.RawMessage `json:"effect"`
	UnitID    int64           `json:"unit_id"`
	BaseItems [][]int64       `json:"base_items"`
	Desc      string          `json:"desc"`
	BoxShow   [][]int64       `json:"box_show_item"`
	Power     int64           `json:"power"`
	Lock      int64           `json:"lock"`
	Face      int64           `json:"face"`
}

// pairs reads an effect that is a list of [id, weight] pairs.
func (e advEventCfg) pairs() [][]int64 {
	var out [][]int64
	_ = json.Unmarshal(e.Effect, &out)
	return out
}

// quiz reads the effect of an answer event: for 1, 2, 3 right answers the reward (the third column is unused).
func (e advEventCfg) quiz() map[int64][][2]int64 {
	var raw [][]json.RawMessage
	out := map[int64][][2]int64{}
	if json.Unmarshal(e.Effect, &raw) != nil {
		return out
	}
	for _, row := range raw {
		if len(row) < 2 {
			continue
		}
		var n int64
		var items [][]int64
		if json.Unmarshal(row[0], &n) != nil || json.Unmarshal(row[1], &items) != nil {
			continue
		}
		out[n] = pairsOf(items)
	}
	return out
}

type advNpcOption struct {
	EvtID int64     `json:"evt_id"`
	ID    int64     `json:"id"`
	Label string    `json:"lable_desc"`
	Msg   string    `json:"msg"`
	Num   int64     `json:"num"`
	Lose  [][]int64 `json:"lose"`
}

type advFloorCfg struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	FloorReward [][]int64 `json:"floor_reward"`
}

type advSkillCfg struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	MaxNum int64  `json:"max_num"`
}

type advBuffCfg struct {
	Num   int64     `json:"num"`
	Desc  string    `json:"desc"`
	Buffs [][]int64 `json:"buffs"`
}

type advMineRow struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Star        int64     `json:"star"`
	Pos         []int64   `json:"pos"`
	BaseItems   [][]int64 `json:"base_items"`
	HookItems   [][]int64 `json:"hook_items"`
	RobotPower  int64     `json:"robot_power"`
	RobotLev    [][]int64 `json:"robot_lev"`
	RobotName   string    `json:"robot_name"`
	MaxTime     int64     `json:"max_time"` // minutes
	PrimaryLoss [][]int64 `json:"primary_loss"`
	SeniorLoss  [][]int64 `json:"senior_loss"`
	ResID       string    `json:"res_id"`
}

type advConf struct {
	Events map[string]advEventCfg `json:"events"`
	Units  map[string]struct {
		Group    string       `json:"group"`
		Monsters []monsterDef `json:"monsters"`
	} `json:"units"`
	Map   []struct{ ID, ResID int64 } `json:"map"`
	Const map[string]struct {
		Val json.RawMessage `json:"val"`
	} `json:"const"`
	Floors       []advFloorCfg                    `json:"floors"`
	Skills       []advSkillCfg                    `json:"skills"`
	Answers      map[string]struct{ Desc string } `json:"answers"`
	AnswerOpts   map[string]map[string]string     `json:"answer_options"`
	Npc          map[string][][]advNpcOption      `json:"npc"`
	RoundRewards []struct {
		Count int64     `json:"count"`
		ID    int64     `json:"id"`
		Items [][]int64 `json:"items"`
	} `json:"round_rewards"`
	Buffs map[string]advBuffCfg `json:"buffs"`
	Mine  struct {
		Const map[string]struct {
			Val int64 `json:"val"`
		} `json:"data_const"`
		FloorData map[string]struct {
			Floor      int64     `json:"floor"`
			MinProduce [][]int64 `json:"min_produce"`
			Name       string    `json:"name"`
		} `json:"data_floor_data"`
		BoxReward []struct {
			EffectID int64     `json:"effect_id"`
			Items    [][]int64 `json:"items"`
			Num      int64     `json:"num"`
		} `json:"data_box_reward"`
		Attenuation map[string]struct {
			DecRate int64 `json:"dec_rate"`
		} `json:"data_attenuation_reward"`
	} `json:"mine"`

	Mines map[string]advMineRow `json:"mines"`

	events map[int64]advEventCfg
	consts map[string]int64
	mines  map[int64]advMineRow
}

var advCfg = func() *advConf {
	c := &advConf{}
	if err := json.Unmarshal(adventureJSON, c); err != nil || len(c.Events) == 0 || len(c.Floors) != advFloors {
		panic("game: bad adventure.json")
	}
	c.events = map[int64]advEventCfg{}
	for k, e := range c.Events {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.events[id] = e
	}
	c.mines = map[int64]advMineRow{}
	for k, m := range c.Mines {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.mines[id] = m
	}
	c.consts = map[string]int64{}
	for k, v := range c.Const {
		var n int64
		if json.Unmarshal(v.Val, &n) == nil {
			c.consts[k] = n
		}
	}
	sort.Slice(c.RoundRewards, func(i, j int) bool { return c.RoundRewards[i].ID < c.RoundRewards[j].ID })
	return c
}()

func (c *advConf) event(id int64) (advEventCfg, bool) { e, ok := c.events[id]; return e, ok }

func (c *advConf) skill(id int64) (advSkillCfg, bool) {
	for _, s := range c.Skills {
		if s.ID == id {
			return s, true
		}
	}
	return advSkillCfg{}, false
}

// The floor is the original's 5 × 5 grid (AdventureMainWindow: row 5, col 5), rooms 1-25 from the top left. Captured
// (testdata/captures original-2026-10-27/24-sky-adventure.jsonl, original-2026-11-04/d01-sky.jsonl, floors 1-6): the
// four guards stand in rooms 7, 17, 19 and 9 with the events xx01-xx04 and are the only rooms open at the start; room 13
// holds the mysterious man xx06, which turns into the boss xx00 and then the way on (99999). The other twenty rooms
// hold three or four empty rooms and the floor's events; which room holds what changes every floor.
var advGuardRooms = map[int64]int64{7: 1, 17: 2, 19: 3, 9: 4} // room -> event offset

const (
	advGridSide = 5
	advMystery  = 6 // event offset of the mysterious man in room 13
)

// advRoomBag is what the twenty other rooms of a floor hold: event offsets, 0 for an empty room. The counts are the
// spread of the captured floors (3-4 empty, 2-5 beli, 1-3 ore, up to one quiz, one npc, ...); the exact draw is ours.
func advRoomBag(rng *rand.Rand) []int64 {
	pick := func(lo, hi int) int { return lo + rng.Intn(hi-lo+1) }
	var bag []int64
	add := func(off int64, n int) {
		for i := 0; i < n; i++ {
			bag = append(bag, off)
		}
	}
	add(0, pick(3, 4))            // empty
	add(9, 1)                     // quiz
	add(14+int64(rng.Intn(2)), 1) // the npc (14) or the talking npc (15)
	add(18, pick(1, 2))           // buff
	add(24, pick(1, 2))           // merchant
	add(19+int64(rng.Intn(3)), 1) // a potion
	add(19+int64(rng.Intn(3)), pick(0, 1))
	add(22, pick(0, 1)) // rock paper scissors
	add(17, pick(0, 1)) // ancient treasure (needs the key)
	add(23, pick(0, 1)) // the key
	add(5, pick(1, 2))  // box
	add(8, pick(1, 3))  // ore
	for len(bag) < 20 {
		bag = append(bag, 7) // beli
	}
	bag = bag[:20]
	rng.Shuffle(len(bag), func(i, j int) { bag[i], bag[j] = bag[j], bag[i] })
	return bag
}

// advTerrain is the picture set of the room tiles (resource/adventure/img/<res_id>.png): floors 1-4 and 5 on use
// different sets in the captures. Which tile a room shows is drawn per floor.
func advTerrain(floor int64) []int64 {
	if floor <= 4 {
		return []int64{1, 1, 2, 2, 2, 2, 3, 4, 10, 11, 12, 13, 14}
	}
	return []int64{1, 3, 5, 6, 6, 7, 7, 8, 9, 9, 11, 12}
}

// advNeighbours are the up to eight rooms around a room of the grid.
func advNeighbours(id int64) []int64 {
	row, col := (id-1)/advGridSide, (id-1)%advGridSide
	var out []int64
	for dr := int64(-1); dr <= 1; dr++ {
		for dc := int64(-1); dc <= 1; dc++ {
			r, c := row+dr, col+dc
			if (dr != 0 || dc != 0) && r >= 0 && r < advGridSide && c >= 0 && c < advGridSide {
				out = append(out, r*advGridSide+c+1)
			}
		}
	}
	return out
}

// advRoundStart is the start of the round that holds now: rounds last two days and start at 00:00 Beijing time of the
// odd days since the epoch (captured: 10-29, 11-04 00:00, each 172800 s).
func advRoundStart(now time.Time) time.Time {
	day := (now.Unix() + 8*3600) / 86400
	if day%2 == 0 {
		day--
	}
	return time.Unix(day*86400-8*3600, 0)
}

// advEventID is the event a room of a floor holds.
func advEventID(floor, off int64) int64 {
	if off == 99999 {
		return 99999
	}
	return 100000 + floor*100 + off
}
