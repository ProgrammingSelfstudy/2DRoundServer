package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// shareBase turns what the client sends for an item (a base id, or an equipment piece id
// baseID*pieceIDFactor+k) into the base id; false for an id that is no item.
func shareBase(id int64) (int64, bool) {
	if _, ok := equipmentTable[id]; ok {
		return id, true
	}
	if _, ok := equipmentTable[id/pieceIDFactor]; ok && id%pieceIDFactor != 0 {
		return id / pieceIDFactor, true
	}
	_, ok := itemTable[id]
	return id, ok
}

// shareDetail is a shared item as 10536 shows it: equipment with its main attributes, any
// other item plain.
func shareDetail(base int64) proto.Message {
	main := []proto.Message{}
	if eq, ok := equipmentTable[base]; ok {
		keys := make([]string, 0, len(eq.Attr))
		for k := range eq.Attr {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			main = append(main, proto.Message{"attr_id": attrIDs[k], "attr_val": eq.Attr[k]})
		}
	}
	return proto.Message{"base_id": base, "quantity": 1, "pos": 0, "expire_type": itemTable[base].ExpireType, "expire_time": 0,
		"main_attr": main, "enchant": 0, "attr": []proto.Message{}, "score": 0, "all_score": 0,
		"extra": []proto.Message{}, "gemstones": []proto.Message{}}
}

// RegisterChatShare registers putting an item into a chat message, 10535 (the role must have
// the item in the bag, or a hero of theirs must wear it; the reply tells the client which
// item to write into the message) and looking at one that was shared, 10536 (the items are
// the same for everybody, so any role can look at any item).
func RegisterChatShare(r *gateway.Router, st *store.Store) {
	r.Handle(10535, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(flag int, msg string, id, base, count int64) error {
			return s.Send(10535, proto.Message{"flag": flag, "msg": msg, "share_id": id, "base_id": base, "count": count, "code": req.Int("code")})
		}
		id := req.Int("id")
		base, ok := shareBase(id)
		if !ok {
			return reply(0, "物品不存在", 0, 0, 0)
		}
		ledger, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		count := ledger[base]
		if count <= 0 && req.Int("partner_id") != 0 { // worn by a hero
			heroes, err := st.PartnersOf(role.RID)
			if err != nil {
				return err
			}
			for _, h := range heroes {
				if h.PartnerID != req.Int("partner_id") {
					continue
				}
				for _, e := range h.Equips {
					if e.BaseID == base {
						count = 1
					}
				}
			}
		}
		if count <= 0 {
			return reply(0, "你没有这件物品", 0, 0, 0)
		}
		return reply(1, "", id, base, count)
	})
	r.Handle(10536, func(s *gateway.Session, req proto.Message) error {
		base, ok := shareBase(req.Int("id"))
		if !ok {
			return nil
		}
		return s.Send(10536, shareDetail(base))
	})
}
