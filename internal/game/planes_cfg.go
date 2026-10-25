package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
)

// planes.json: the planes ("位面" / 长环岛冒险) rules of the client (tools/extract_planes.py).
//
//go:embed data/planes.json
var planesJSON []byte

// Event types (PlanesafkConst.Evt_Type).
const (
	pfEvtNormal      = 0
	pfEvtMonster     = 1
	pfEvtGuard       = 2
	pfEvtRecover     = 3
	pfEvtRevive      = 4
	pfEvtLease       = 5
	pfEvtBuff        = 6
	pfEvtBusinessman = 7
	pfEvtOccurrence  = 8
	combatPlanes     = 40 // BattleConst.Fight_Type.PlanesWar
)

type pfEventCfg struct {
	Type       int64             `json:"type"`
	Group      int64             `json:"group"`
	Ext        []json.RawMessage `json:"ext"`
	RewardList []int64           `json:"reward_list"`
	Reward     [][]int64         `json:"reward"`
	Exchange   int64             `json:"exchange"`
	Name       string            `json:"name"`
}

type pfGoodCfg struct {
	BaseID   int64  `json:"base_id"`
	Discount int64  `json:"discount"`
	ID       int64  `json:"id"`
	Num      int64  `json:"num"`
	Price    int64  `json:"price"`
	Type     string `json:"type"`
}

type pfRewardRow struct {
	Max    int64     `json:"max"`
	Min    int64     `json:"min"`
	Reward [][]int64 `json:"reward"`
}

type pfBuffCfg struct {
	Quality int64  `json:"quality"`
	Name    string `json:"name"`
}

type pfOrderLevel struct {
	Lev       int64     `json:"lev"`
	Period    int64     `json:"period"`
	Reward    [][]int64 `json:"reward"`
	RmbReward [][]int64 `json:"rmb_reward"`
	WinCount  int64     `json:"win_count"`
}

type pfConf struct {
	Const   map[string]json.RawMessage `json:"const"`
	Events  map[string]pfEventCfg      `json:"events"`
	Shop    []pfGoodCfg                `json:"shop"`
	Rewards map[string][]pfRewardRow   `json:"rewards"`
	Buffs   map[string]pfBuffCfg       `json:"buffs"`
	Order   struct {
		Constant map[string]json.RawMessage `json:"constant"`
		Levels   [][]pfOrderLevel           `json:"levels"`
		Days     []struct {
			Period    int64 `json:"period"`
			PeriodDay int64 `json:"period_day"`
		} `json:"days"`
	} `json:"order"`

	events map[int64]pfEventCfg
}

func (c *pfConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

func (c *pfConf) nums(name string) []int64 {
	var n []int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var pfCfg = func() *pfConf {
	c := &pfConf{}
	if err := json.Unmarshal(planesJSON, c); err != nil || len(c.Events) == 0 || len(c.Buffs) == 0 || len(c.Shop) == 0 {
		panic("game: bad planes.json")
	}
	c.events = map[int64]pfEventCfg{}
	for k, e := range c.Events {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.events[id] = e
	}
	return c
}()

// extNum reads the n-th number of an event's ext entry named name ("group", "radio", ...) or 0.
func (e pfEventCfg) extNum(name string, n int) int64 {
	for _, raw := range e.Ext {
		var row []json.RawMessage
		if json.Unmarshal(raw, &row) != nil || len(row) <= n {
			continue
		}
		var key string
		if json.Unmarshal(row[0], &key) == nil && key == name {
			var v int64
			if json.Unmarshal(row[n], &v) == nil {
				return v
			}
		}
	}
	return 0
}

// pfRewardFor picks the reward row of a table for a level.
func pfRewardFor(table int64, lev int64) [][2]int64 {
	for _, r := range pfCfg.Rewards[strconv.FormatInt(table, 10)] {
		if lev >= r.Min && lev <= r.Max {
			return pairsOf(r.Reward)
		}
	}
	return nil
}
