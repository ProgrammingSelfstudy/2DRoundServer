package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	formDrama     = 0 // PartnerConst.Fun_Form.Drama
	formMaxHeroes = 6 // every formation of the client (data_form_data) has six positions
	formMaxPos    = 6 // positions in 11212 are 1-6 (the capture of the original server saved six heroes)
	formTypes     = 6 // data_form_data has six formations
)

// formCells is data_form_data of the client: for each formation type, the battle
// cell (1-9, our side) that each formation position 1-6 stands on. The capture of
// the original server agrees: type 1 puts positions 1 and 6 on cells 9 and 2.
var formCells = map[int64][formMaxPos]int64{
	1: {9, 7, 6, 4, 5, 2},
	2: {1, 3, 4, 6, 8, 5},
	3: {1, 2, 3, 5, 8, 9},
	4: {2, 4, 5, 6, 8, 7},
	5: {2, 5, 7, 8, 9, 4},
	6: {1, 3, 5, 7, 8, 9},
}

// battleCell is where a hero in formation position pos fights.
func battleCell(formationType, pos int64) int64 {
	cells, ok := formCells[formationType]
	if !ok {
		cells = formCells[1]
	}
	return cells[min(max(pos, 1), formMaxPos)-1]
}

// formationOf is the role's formation of a kind. A role that never saved one gets
// its first six heroes in positions 1-6 (our policy), so the client's formation
// panel is not empty; nothing is stored until it saves.
func formationOf(st *store.Store, rid, typ int64) (store.Formation, error) {
	f, err := st.Formation(rid, typ)
	if err == nil {
		return *f, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Formation{}, err
	}
	if typ != formDrama { // a kind never saved shows the campaign team (the capture of the original server does)
		if c, err := st.Formation(rid, formDrama); err == nil {
			cp := *c
			cp.Type = typ
			return cp, nil
		}
	}
	def := store.Formation{Type: typ, FormationType: 1}
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return def, err
	}
	for i, h := range heroes {
		if i == formMaxHeroes {
			break
		}
		def.Slots = append(def.Slots, store.FormationSlot{Pos: int64(i + 1), PartnerID: h.PartnerID})
	}
	return def, nil
}

func formationMessage(f store.Formation) proto.Message {
	slots := []proto.Message{}
	for _, sl := range f.Slots {
		slots = append(slots, proto.Message{"pos": sl.Pos, "id": sl.PartnerID})
	}
	return proto.Message{"type": f.Type, "formation_type": f.FormationType, "pos_info": slots, "hallows_id": f.HallowsID}
}

// RegisterFormation registers the formation commands: 11211 (one kind), 11213
// (several kinds) and 11212 (save). The campaign's challenge window opens on
// the formation of kind 0.
func RegisterFormation(r *gateway.Router, st *store.Store) {
	r.Handle(11211, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		f, err := formationOf(st, role.RID, req.Int("type"))
		if err != nil {
			return err
		}
		return s.Send(11211, formationMessage(f))
	})
	r.Handle(11213, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		info := []proto.Message{}
		for _, t := range req.Arr("type_list") {
			f, err := formationOf(st, role.RID, t.Int("type"))
			if err != nil {
				return err
			}
			info = append(info, formationMessage(f))
		}
		return s.Send(11213, proto.Message{"info": info})
	})
	r.Handle(11212, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ := req.Int("type")
		reply := func(code int, msg string) error {
			return s.Send(11212, proto.Message{"code": code, "msg": msg, "type": typ})
		}
		f := store.Formation{Type: typ, FormationType: req.Int("formation_type"), HallowsID: req.Int("hallows_id")}
		if f.FormationType < 1 || f.FormationType > formTypes {
			return reply(0, "阵法不存在")
		}
		seenPos, seenHero := map[int64]bool{}, map[int64]bool{}
		for _, a := range req.Arr("pos_info") {
			pos, id := a.Int("pos"), a.Int("id")
			if pos < 1 || pos > formMaxPos || seenPos[pos] || seenHero[id] {
				return reply(0, "阵容不合法")
			}
			seenPos[pos], seenHero[id] = true, true
			f.Slots = append(f.Slots, store.FormationSlot{Pos: pos, PartnerID: id})
		}
		if len(f.Slots) == 0 || len(f.Slots) > formMaxHeroes {
			return reply(0, "上阵英雄数量不正确")
		}
		if err := st.SaveFormation(role.RID, f); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "英雄不存在")
			}
			return err
		}
		return reply(1, "保存成功")
	})
}
