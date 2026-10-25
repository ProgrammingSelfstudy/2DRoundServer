package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
)

// planes_dun.json: the old planes ("秘境") rules; planes_maps.json: the walkable grids of its ten maps
// (tools/extract_planes_dun.py, tools/extract_planes_maps.py).
//
//go:embed data/planes_dun.json
var planesDunJSON []byte

//go:embed data/planes_maps.json
var planesMapsJSON []byte

// Event types of the old planes (PlanesConst.Evt_Type).
const (
	plEvtNormal  = 0
	plEvtMonster = 1
	plEvtGuard   = 2
	plEvtBox     = 3
	plEvtStart   = 4
	plEvtBoard   = 5
	plEvtGoods   = 6
	plEvtRecover = 7
	plEvtPortal  = 8
	plEvtLease   = 9
	plEvtDialog  = 10
	plEvtBuff    = 15
	plEvtRevive  = 16
	plDunCount   = 5
	plFloorCount = 2
	plResetDays  = 2
	plRosterSize = 7
)

type plEventCfg struct {
	Type   int64             `json:"type"`
	Group  int64             `json:"group"`
	Ext    []json.RawMessage `json:"ext"`
	Reward [][]int64         `json:"reward"`
	IsWalk int64             `json:"is_walk"`
	Name   string            `json:"name"`
}

type plCustom struct {
	ID          int64     `json:"id"`
	FirstReward [][]int64 `json:"first_reward"`
	IDLimit     []int64   `json:"id_limit"`
	LevLimit    int64     `json:"lev_limit"`
	PowerLimit  int64     `json:"power_limit"`
	AddRadio    int64     `json:"add_radio"`
}

type plMap struct {
	Cols  int       `json:"cols"`
	Rows  int       `json:"rows"`
	Block [][]int64 `json:"block"`
}

type plConf struct {
	Const   map[string]json.RawMessage `json:"const"`
	Customs []plCustom                 `json:"customs"`
	Events  map[string]plEventCfg      `json:"events"`
	Buffs   map[string]pfBuffCfg       `json:"buffs"`

	events map[int64]plEventCfg
	maps   map[int64]plMap
}

func (c *plConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var plCfg = func() *plConf {
	c := &plConf{}
	if err := json.Unmarshal(planesDunJSON, c); err != nil || len(c.Customs) != plDunCount || len(c.Events) == 0 {
		panic("game: bad planes_dun.json")
	}
	c.events = map[int64]plEventCfg{}
	for k, e := range c.Events {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.events[id] = e
	}
	var m struct {
		Maps map[string]plMap `json:"maps"`
	}
	if err := json.Unmarshal(planesMapsJSON, &m); err != nil || len(m.Maps) != plDunCount*plFloorCount {
		panic("game: bad planes_maps.json")
	}
	c.maps = map[int64]plMap{}
	for k, v := range m.Maps {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.maps[id] = v
	}
	return c
}()

func plMapID(dun, floor int64) int64 { return dun*10000 + floor*100 + 1 }

// plIndex is the index of the cell in column x, row y (both from 1): x * 1000 + y.
func plIndex(x, y int64) int64 { return x*1000 + y }

func plXY(index int64) (x, y int64) { return index / 1000, index % 1000 }

// plWalkable: cells with a value above 1 are the road (the client's A* treats the others as blocks).
func (m plMap) walkable(x, y int64) bool {
	if y < 1 || int(y) > len(m.Block) || x < 1 || int(x) > len(m.Block[y-1]) {
		return false
	}
	return m.Block[y-1][x-1] > 1
}

// plDistance is the client's PlanesTile.tileDistance: the grid is staggered, odd rows half a cell to the right.
func plDistance(x1, y1, x2, y2 int64) int64 {
	dx, dy := x2-x1, y2-y1
	var a, b int64
	if dy%2 == 0 {
		a = ceilDiv2(2*dx + dy)
		b = floorDiv2(dy - 2*dx)
	} else {
		a = ceilDiv2(2*(dx-y1%2) + dy)
		b = floorDiv2(dy - 2*dx)
	}
	return max(abs64(a), abs64(b))
}

func ceilDiv2(n int64) int64 {
	if n >= 0 {
		return (n + 1) / 2
	}
	return -((-n) / 2)
}

func floorDiv2(n int64) int64 {
	if n >= 0 {
		return n / 2
	}
	return -((-n + 1) / 2)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func (e plEventCfg) extInt(n int) int64 {
	if len(e.Ext) <= n {
		return 0
	}
	var v int64
	_ = json.Unmarshal(e.Ext[n], &v)
	return v
}

// extPair reads a row ["name", a, b] of the ext list.
func (e plEventCfg) extRow(name string, n int) int64 {
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

// dialogID is the dialogue an elder says: the ext of the event is [[dialogue id, weight]].
func (e plEventCfg) dialogID() int64 {
	if len(e.Ext) == 0 {
		return 0
	}
	var row []int64
	if json.Unmarshal(e.Ext[0], &row) != nil || len(row) == 0 {
		return 0
	}
	return row[0]
}
