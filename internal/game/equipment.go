package game

import (
	_ "embed"
	"encoding/json"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// equipment.json: the hero equipment of the client (tools/extract_equipment.py):
// 92 pieces, the slot being the item type 1-4.
//
//go:embed data/equipment.json
var equipmentJSON []byte

type equipmentInfo struct {
	Name    string           `json:"name"`
	Slot    int64            `json:"slot"`
	Quality int              `json:"quality"`
	Attr    map[string]int64 `json:"attr"`
}

var equipmentTable = func() map[int64]equipmentInfo {
	var f struct {
		Equipment map[int64]equipmentInfo `json:"equipment"`
	}
	if err := json.Unmarshal(equipmentJSON, &f); err != nil || len(f.Equipment) == 0 {
		panic("game: bad equipment.json")
	}
	return f.Equipment
}()

// attrIDs is the client's attribute key -> id table (Config.AttrData.data_key_to_id)
// for the keys equipment uses.
var attrIDs = map[string]int64{"atk": 101, "def_p": 102, "def_s": 103, "hp_max": 104, "speed": 106, "def": 107}

// Ordinary equipment uses one stable stack id per base. Preserve the former
// first-piece id so existing local requests still resolve to the same base.
const pieceIDFactor = 1000

func equipmentStackID(baseID int64) int64 { return baseID*pieceIDFactor + 1 }

func eqmsOf(p store.Partner) []proto.Message {
	out := []proto.Message{}
	for _, e := range p.Equips {
		out = append(out, proto.Message{"id": e.BaseID, "base_id": e.BaseID, "type": e.Slot})
	}
	return out
}

// equipmentBag lists one stack per base, as 10501/10512 do in the original
// 2026-11-04 capture. Holy equipment retains its per-piece identity.
func equipmentBag(ledger map[int64]int64) []proto.Message {
	ids := make([]int64, 0)
	for id, n := range ledger {
		if _, ok := equipmentTable[id]; ok && n > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []proto.Message{}
	for _, base := range ids {
		eq := equipmentTable[base]
		out = append(out, proto.Message{"id": equipmentStackID(base), "base_id": base, "bind": 0, "quantity": ledger[base], "pos": len(out) + 1,
			"expire_type": 0, "expire_time": 0, "main_attr": equipmentAttrs(eq), "enchant": 0, "attr": []proto.Message{}, "extra_attr": []proto.Message{},
			"score": 0, "all_score": 0, "extra": []proto.Message{}, "gemstones": []proto.Message{}, "holy_eqm_attr": []proto.Message{}})
	}
	return out
}

// RegisterEquipment registers the equipment bag (10501) and wearing it: 11010
// (put a piece of the bag on a hero) and 11011 (take the piece of a slot off).
// Wearing changes the hero's attributes, which the client gets in 11012.
func RegisterEquipment(r *gateway.Router, st *store.Store) {
	sendBag := func(s *gateway.Session, rid int64) error {
		ledger, err := st.RoleItems(rid)
		if err != nil {
			return err
		}
		holy, err := loadHoly(st, rid)
		if err != nil {
			return err
		}
		eqm := equipmentBag(ledger)
		return s.Send(10501, bagReply(s, append(eqm, holyBag(holy, len(eqm)+1)...)))
	}
	r.Handle(10501, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		return sendBag(s, role.RID)
	})
	r.Handle(11010, changeEquipment(st, 11010, sendBag))
	r.Handle(11011, changeEquipment(st, 11011, sendBag))
}

// pieceScore ranks pieces of one slot: the sum of the main attributes, so a sword of
// 221 attack beats one of 88. Our rule; the client's own score field is 0.
func pieceScore(e equipmentInfo) int64 {
	var n int64
	for _, v := range e.Attr {
		n += v
	}
	return n
}
