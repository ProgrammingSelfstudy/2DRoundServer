package game

import (
	_ "embed"
	"encoding/json"
	"errors"
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

// A piece in the equipment bag has the id baseID*pieceIDFactor + k (k = 1, 2, ...
// for the role's copies of that piece); the base id is what the ledger counts.
const pieceIDFactor = 1000

func eqmsOf(p store.Partner) []proto.Message {
	out := []proto.Message{}
	for _, e := range p.Equips {
		out = append(out, proto.Message{"id": e.BaseID, "base_id": e.BaseID, "type": e.Slot})
	}
	return out
}

// equipmentBag lists the equipment in the item ledger, one entry per piece.
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
		main := []proto.Message{}
		keys := make([]string, 0, len(eq.Attr))
		for k := range eq.Attr {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			main = append(main, proto.Message{"attr_id": attrIDs[k], "attr_val": eq.Attr[k]})
		}
		for k := int64(1); k <= ledger[base]; k++ {
			out = append(out, proto.Message{"id": base*pieceIDFactor + k, "base_id": base, "bind": 0, "quantity": 1, "pos": len(out) + 1,
				"expire_type": 0, "expire_time": 0, "main_attr": main, "enchant": 0, "attr": []proto.Message{}, "extra_attr": []proto.Message{},
				"score": 0, "all_score": 0, "extra": []proto.Message{}, "gemstones": []proto.Message{}, "holy_eqm_attr": []proto.Message{}})
		}
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
		return s.Send(10501, bagReply(s, equipmentBag(ledger)))
	}
	r.Handle(10501, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		return sendBag(s, role.RID)
	})
	changed := func(s *gateway.Session, rid, partnerID int64) error {
		p, err := st.PartnerByID(rid, partnerID)
		if err != nil {
			return err
		}
		m := partnerMessage(*p)
		if err := s.Send(11012, m); err != nil {
			return err
		}
		return sendBag(s, rid)
	}
	r.Handle(11010, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(ok int, msg string) error { return s.Send(11010, proto.Message{"result": ok, "msg": msg}) }
		partnerID, piece := req.Int("partner_id"), req.Int("item_id")
		if piece == 0 { // the hero panel's one-key button: wear the best piece of the bag in every slot
			return wearBest(s, st, role.RID, partnerID, reply, changed)
		}
		base := piece / pieceIDFactor
		eq, ok := equipmentTable[base]
		if !ok || piece%pieceIDFactor == 0 {
			return reply(0, "装备不存在")
		}
		err := st.EquipPiece(role.RID, partnerID, eq.Slot, base)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return reply(0, "英雄不存在")
		case errors.Is(err, store.ErrConflict):
			return reply(0, "背包中没有该装备")
		case err != nil:
			return err
		}
		if err := reply(1, "穿戴成功"); err != nil {
			return err
		}
		return changed(s, role.RID, partnerID)
	})
	r.Handle(11011, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(ok int, msg string) error { return s.Send(11011, proto.Message{"result": ok, "msg": msg}) }
		partnerID := req.Int("partner_id")
		if req.Int("pos_id") == 0 { // the hero panel's one-key button: take every piece off
			return takeAllOff(s, st, role.RID, partnerID, reply, changed)
		}
		// The client's tips window sends the id of the worn piece (the `id` of its
		// eqms entry, which is the base id here); a slot number 1-4 is also taken.
		slot := req.Int("pos_id")
		if slot > 4 {
			p, err := st.PartnerByID(role.RID, partnerID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			slot = 0
			if err == nil {
				for _, e := range p.Equips {
					if e.BaseID == req.Int("pos_id") {
						slot = e.Slot
					}
				}
			}
		}
		err := st.UnequipPiece(role.RID, partnerID, slot)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return reply(0, "该位置没有装备")
		case err != nil:
			return err
		}
		if err := reply(1, "卸下成功"); err != nil {
			return err
		}
		return changed(s, role.RID, partnerID)
	})
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

// wearBest is 11010 with item_id 0 (the one-key wear button): for each slot the best
// piece of the equipment bag goes on, when it beats what the hero wears there. The
// hero panel then shows what changed; nothing to wear is a refusal with a message.
func wearBest(s *gateway.Session, st *store.Store, rid, partnerID int64, reply func(int, string) error,
	changed func(*gateway.Session, int64, int64) error) error {
	p, err := st.PartnerByID(rid, partnerID)
	if errors.Is(err, store.ErrNotFound) {
		return reply(0, "英雄不存在")
	} else if err != nil {
		return err
	}
	ledger, err := st.RoleItems(rid)
	if err != nil {
		return err
	}
	worn := map[int64]int64{} // slot -> score of the worn piece
	for _, e := range p.Equips {
		if info, ok := equipmentTable[e.BaseID]; ok {
			worn[e.Slot] = pieceScore(info)
		}
	}
	best := map[int64]int64{} // slot -> base id of the best piece in the bag
	for id, n := range ledger {
		info, ok := equipmentTable[id]
		if !ok || n < 1 {
			continue
		}
		if cur, has := best[info.Slot]; !has || pieceScore(info) > pieceScore(equipmentTable[cur]) || (pieceScore(info) == pieceScore(equipmentTable[cur]) && id > cur) {
			best[info.Slot] = id
		}
	}
	slots := make([]int64, 0, len(best))
	for slot := range best {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	did := 0
	for _, slot := range slots {
		id := best[slot]
		if cur, has := worn[slot]; has && pieceScore(equipmentTable[id]) <= cur {
			continue // not better than what the hero wears
		}
		if err := st.EquipPiece(rid, partnerID, slot, id); err != nil {
			return err
		}
		did++
	}
	if did == 0 {
		return reply(0, "没有可穿戴的更好装备")
	}
	if err := reply(1, "穿戴成功"); err != nil {
		return err
	}
	return changed(s, rid, partnerID)
}

// takeAllOff is 11011 with pos_id 0 (the one-key take-off button): every worn piece
// goes back into the bag.
func takeAllOff(s *gateway.Session, st *store.Store, rid, partnerID int64, reply func(int, string) error,
	changed func(*gateway.Session, int64, int64) error) error {
	p, err := st.PartnerByID(rid, partnerID)
	if errors.Is(err, store.ErrNotFound) {
		return reply(0, "英雄不存在")
	} else if err != nil {
		return err
	}
	if len(p.Equips) == 0 {
		return reply(0, "该英雄没有穿戴装备")
	}
	for _, e := range p.Equips {
		if err := st.UnequipPiece(rid, partnerID, e.Slot); err != nil {
			return err
		}
	}
	if err := reply(1, "卸下成功"); err != nil {
		return err
	}
	return changed(s, rid, partnerID)
}
