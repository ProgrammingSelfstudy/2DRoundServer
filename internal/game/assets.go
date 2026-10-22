package game

import (
	_ "embed"
	"encoding/json"
	"strconv"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// asset_labels.json is the client's currency id table (tools/extract_asset_labels.py).
//
//go:embed data/asset_labels.json
var assetLabelsJSON []byte

var (
	assetLabelByName = map[string]int64{}
	assetNameByLabel = map[int64]string{}
)

func init() {
	var f struct {
		Labels map[string]int64 `json:"labels"`
	}
	if err := json.Unmarshal(assetLabelsJSON, &f); err != nil || len(f.Labels) == 0 {
		panic("game: bad asset_labels.json")
	}
	for name, id := range f.Labels {
		assetLabelByName[name] = id
		assetNameByLabel[id] = name
	}
}

// energyMax is the intelligence ("情报值", asset energy, shown as 远航情报) cap, the client's
// dungeon_const energy_max. 10302 carries it; the hook-income claim asks first when the
// claim would pass it, and with 0 there the dialog read "100/0".
const energyMax = 10000

// roleAssets builds 10302 from the role and its asset ledger. A ledger entry
// whose label name is a field of 10302 (gold, coin, hero_exp, ...) fills that
// field; the currencies are strings on the wire, the others integers. Labels
// the message has no field for are kept in the ledger but not shown.
func roleAssets(r *store.Role, ledger map[int64]int64) proto.Message {
	msg := proto.Message{"lev": r.Lev, "exp": 0, "gold": "0", "coin": "0", "hero_exp": "0", "energy_max": energyMax}
	schema, err := proto.LoadSchema()
	if err != nil {
		return msg
	}
	types := map[string]string{}
	for _, f := range schema.Recv[10302] {
		types[f.Name] = f.Type
	}
	for label, val := range ledger {
		name := assetNameByLabel[label]
		switch types[name] {
		case "":
		case "string":
			msg[name] = strconv.FormatInt(val, 10)
		default:
			msg[name] = val
		}
	}
	return msg
}
