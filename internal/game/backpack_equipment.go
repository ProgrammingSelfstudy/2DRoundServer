package game

import (
	"errors"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func equipmentAttrs(eq equipmentInfo) []proto.Message {
	keys := make([]string, 0, len(eq.Attr))
	for k := range eq.Attr {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]proto.Message, 0, len(keys))
	for _, k := range keys {
		out = append(out, proto.Message{"attr_id": attrIDs[k], "attr_val": eq.Attr[k]})
	}
	return out
}

func changeEquipment(st *store.Store, cmd uint16, sendBag func(*gateway.Session, int64) error) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(result int, msg string) error { return s.Send(cmd, proto.Message{"result": result, "msg": msg}) }
		pid := req.Int("partner_id")
		piece, pos := req.Int("item_id"), req.Int("pos_id")
		h5 := isH5RoleSession(s)
		var changes []store.Equip
		if cmd == 11010 && piece != 0 {
			base := piece / pieceIDFactor
			eq, ok := equipmentTable[base]
			if !ok || piece%pieceIDFactor == 0 || h5 && piece != equipmentStackID(base) {
				return reply(0, "装备不存在")
			}
			changes = []store.Equip{{Slot: eq.Slot, BaseID: base}}
		}
		hero, err := st.PartnerByID(role.RID, pid)
		if errors.Is(err, store.ErrNotFound) || err == nil && h5 && hero.Star < h5HeroStar {
			if cmd == 11011 && pos != 0 {
				return reply(0, "该位置没有装备")
			}
			return reply(0, "英雄不存在")
		}
		if err != nil {
			return err
		}

		success := "穿戴成功"
		if cmd == 11011 {
			success = "卸下成功"
			for _, e := range hero.Equips {
				if pos == 0 {
					changes = append(changes, store.Equip{Slot: e.Slot})
				} else if pos <= 4 && pos == e.Slot || pos > 4 && pos == e.BaseID {
					changes = []store.Equip{{Slot: e.Slot}}
				}
			}
			if len(changes) == 0 {
				if pos == 0 {
					return reply(0, "该英雄没有穿戴装备")
				}
				return reply(0, "该位置没有装备")
			}
		} else if piece == 0 {
			ledger, err := st.RoleItems(role.RID)
			if err != nil {
				return err
			}
			changes = bestEquipment(ledger, hero.Equips)
			if len(changes) == 0 {
				return reply(0, "没有可穿戴的更好装备")
			}
		}

		var previousPower int64
		if h5 {
			heroes, err := st.PartnersOf(role.RID)
			if err != nil {
				return err
			}
			previousPower = rolePower(heroes)
		}
		if err := applyEquipmentChanges(st, *hero, changes, h5); err != nil {
			switch {
			case h5 && errors.Is(err, store.ErrNotFound):
				return reply(0, "英雄或装备状态已变化")
			case h5 && errors.Is(err, store.ErrConflict):
				return reply(0, "装备状态已变化或背包数量不足")
			case cmd == 11010 && piece != 0 && errors.Is(err, store.ErrConflict):
				return reply(0, "背包中没有该装备")
			case cmd == 11010 && piece != 0 && errors.Is(err, store.ErrNotFound):
				return reply(0, "英雄不存在")
			case cmd == 11011 && pos != 0 && errors.Is(err, store.ErrNotFound):
				return reply(0, "该位置没有装备")
			default:
				return err
			}
		}
		fresh, err := st.PartnerByID(role.RID, pid)
		if err != nil {
			return err
		}
		if err := reply(1, success); err != nil {
			return err
		}
		if err := s.Send(11012, partnerMessage(*fresh)); err != nil {
			return err
		}
		if err := sendBag(s, role.RID); err != nil {
			return err
		}
		if h5 {
			return sendRolePower(s, st, role.RID, previousPower)
		}
		return nil
	}
}

func applyEquipmentChanges(st *store.Store, hero store.Partner, changes []store.Equip, h5 bool) error {
	if h5 {
		// An empty, non-nil snapshot means the hero was wearing nothing.
		expected := append([]store.Equip{}, hero.Equips...)
		return st.ChangeEquipment(hero.RID, hero.PartnerID, expected, changes)
	}
	// Native one-key actions apply each slot in order.
	for i := range changes {
		if err := st.ChangeEquipment(hero.RID, hero.PartnerID, nil, changes[i:i+1]); err != nil {
			return err
		}
	}
	return nil
}

// bestEquipment selects an upgrade from unassigned inventory for each slot.
func bestEquipment(ledger map[int64]int64, worn []store.Equip) []store.Equip {
	scores, best := [5]int64{}, [5]int64{}
	for _, e := range worn {
		if info, ok := equipmentTable[e.BaseID]; ok && e.Slot >= 1 && e.Slot <= 4 {
			scores[e.Slot] = pieceScore(info)
		}
	}
	for id, count := range ledger {
		info, ok := equipmentTable[id]
		if !ok || count < 1 {
			continue
		}
		score, slot := pieceScore(info), info.Slot
		if score > scores[slot] || (best[slot] != 0 && score == scores[slot] && id > best[slot]) {
			best[slot], scores[slot] = id, score
		}
	}
	var changes []store.Equip
	for slot, id := range best {
		if id != 0 {
			changes = append(changes, store.Equip{Slot: int64(slot), BaseID: id})
		}
	}
	return changes
}
