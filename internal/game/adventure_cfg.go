package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
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
	advRoundDays    = 7
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

// advRoomPlan is one room of a floor: the event kind it holds (the last two digits of the event id) and the
// rooms that must be over before it can be opened.
type advRoomPlan struct {
	ID  int64
	Off int64
	Pre []int64
}

// advLayout is the rooms of a floor. The client has no layout (the original server makes it), so this one is
// ours: twelve rooms and the way on, spread over the twenty events each floor has.
func advLayout(floor int64) []advRoomPlan {
	skill := 19 + floor%3
	return []advRoomPlan{
		{1, 1, nil}, {2, 2, nil}, {3, 3, nil},
		{4, 7, []int64{1}}, {5, 14, []int64{1}},
		{6, 18, []int64{2}}, {7, 9, []int64{2}},
		{8, 22, []int64{3}}, {9, 24, []int64{3}},
		{10, skill, []int64{4, 5}},
		{11, 6, []int64{1, 2, 3}},
		{12, 4, []int64{6, 7}},
		{advDoorRoom, 99999, []int64{11}},
	}
}

// advEventID is the event a room of a floor holds.
func advEventID(floor, off int64) int64 {
	if off == 99999 {
		return 99999
	}
	return 100000 + floor*100 + off
}
