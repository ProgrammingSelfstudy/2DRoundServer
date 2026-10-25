package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
)

// elfin.json: the sprite ("elfin") system of the client (tools/extract_elfin.py).
//
//go:embed data/elfin.json
var elfinJSON []byte

type elfinSprite struct {
	Skill int64            `json:"skill"`
	Step  int64            `json:"step"`
	Type  int64            `json:"type"`
	Power int64            `json:"power"`
	Altar map[string]int64 `json:"altar"`
	Help  map[string]int64 `json:"help"`
}

type elfinHatchCfg struct {
	Name      string    `json:"name"`
	Sort      int64     `json:"sort"`
	HatchCond []any     `json:"hatch_cond"`
	Expend    [][]int64 `json:"expend"`
}

type elfinHatchLev struct {
	Lev      int64     `json:"lev"`
	LimitVIP int64     `json:"limit_vip"`
	Expend   [][]int64 `json:"expend"`
	Award    [][]int64 `json:"award"`
}

type elfinEgg struct {
	ItemBid   int64     `json:"item_bid"`
	CanBuy    int64     `json:"can_buy"`
	LimitNum  int64     `json:"limit_num"`
	NeedPoint int64     `json:"need_piont"`
	Price     [][]int64 `json:"price"`
	Expend    [][]int64 `json:"expend"`
	Award     [][]int64 `json:"award"`
}

type elfinSmash struct {
	ItemBid  int64     `json:"item_bid"`
	CanBuy   int64     `json:"can_buy"`
	LimitNum int64     `json:"limit_num"`
	DelPoint int64     `json:"del_piont"`
	Price    [][]int64 `json:"price"`
}

type elfinTreeStep struct {
	AddAtk       int64               `json:"add_atk"`
	AddDef       int64               `json:"add_def"`
	AddHP        int64               `json:"add_hp"`
	AddSpeed     int64               `json:"add_speed"`
	Count        int64               `json:"count"`
	HelpfightNum int64               `json:"helpfight_num"`
	LevMax       int64               `json:"lev_max"`
	SkillNum     int64               `json:"skill_num"`
	AllAttr      [][]json.RawMessage `json:"all_attr"`
	Expend       [][]int64           `json:"expend"`
	StepCond     [][]json.RawMessage `json:"step_cond"`
}

type elfinCompose struct {
	Award  int64     `json:"award"`
	Expend [][]int64 `json:"expend"`
}

type elfinConf struct {
	Const    map[string]json.RawMessage `json:"const"`
	Hatch    []elfinHatchCfg            `json:"hatch"`
	HatchLev map[string]elfinHatchLev   `json:"hatch_lev"`
	Egg      map[string]elfinEgg        `json:"egg"`
	Smash    map[string]elfinSmash      `json:"smash"`
	Sprites  map[string]elfinSprite     `json:"sprites"`
	TreeAttr map[string]int64           `json:"tree_attr"`
	TreeUp   []struct {
		Lev    int64     `json:"lev"`
		Expend [][]int64 `json:"expend"`
	} `json:"tree_up"`
	TreeStep map[string]elfinTreeStep `json:"tree_step"`
	Compose  map[string]elfinCompose  `json:"compose"`
	Lottery  struct {
		Const  map[string]json.RawMessage `json:"const"`
		Summon struct {
			GainOnce     [][]int64 `json:"gain_once"`
			GainTen      [][]int64 `json:"gain_ten"`
			LossGoldOnce [][]int64 `json:"loss_gold_once"`
			LossGoldTen  [][]int64 `json:"loss_gold_ten"`
			LossItemOnce [][]int64 `json:"loss_item_once"`
			LossItemTen  [][]int64 `json:"loss_item_ten"`
		} `json:"summon"`
		Awards []struct {
			ID     int64     `json:"id"`
			Times  int64     `json:"times"`
			Reward [][]int64 `json:"reward"`
		} `json:"awards"`
	} `json:"lottery"`

	sprite map[int64]elfinSprite
}

func (c *elfinConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

func (c *elfinConf) lotteryNum(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Lottery.Const[name], &n)
	return n
}

var elfinCfg = func() *elfinConf {
	c := &elfinConf{}
	if err := json.Unmarshal(elfinJSON, c); err != nil || len(c.Sprites) == 0 || len(c.TreeUp) == 0 || len(c.TreeStep) == 0 {
		panic("game: bad elfin.json")
	}
	c.sprite = map[int64]elfinSprite{}
	for k, v := range c.Sprites {
		id, _ := strconv.ParseInt(k, 10, 64)
		c.sprite[id] = v
	}
	return c
}()

func (c *elfinConf) step(n int64) (elfinTreeStep, bool) {
	s, ok := c.TreeStep[strconv.FormatInt(n, 10)]
	return s, ok
}

// elfinTier is the tier a sprite id belongs to: 111 blue, 112 purple, 113 orange, 114 red.
func elfinTier(id int64) int64 { return id / 1000 }

// elfinQuality is the quality number a summon shows for a sprite (2 blue, 3 purple, 4 orange, 5 red).
func elfinQuality(id int64) int64 { return elfinTier(id) - 109 }

func jsonInts(raw json.RawMessage, out *[]int64) error { return json.Unmarshal(raw, out) }
