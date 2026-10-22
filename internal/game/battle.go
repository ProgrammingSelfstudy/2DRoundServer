package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// combatDrama is the client's fight type of the campaign (Fight_Type.Darma).
const combatDrama = 3

// RegisterBattle registers the battle commands that have a defined meaning.
func RegisterBattle(r *gateway.Router, st *store.Store) {
	r.Handle(20060, func(s *gateway.Session, req proto.Message) error { return combatType(s, st, req) })
	r.Handle(11099, partnerDowerSkills)
	r.Handle(11067, partnerResetCount)
	r.Handle(11092, partnerHolyEquipment)
	// 10397: the client says whether it is watching a replay; there is no reply.
	r.Handle(10397, func(*gateway.Session, proto.Message) error { return nil })
	// 21322: is the double-reward period on (code 1)? Never, here.
	r.Handle(21322, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(21322, proto.Message{"code": 0})
	})
}

// partnerDowerSkills answers 11099, which the formation panel sends for the heroes
// on the field. The original server answers every id with an empty dower_skill
// list (capture original-2026-10-20); the feature itself is not implemented.
func partnerDowerSkills(s *gateway.Session, req proto.Message) error {
	out := []proto.Message{}
	for _, p := range req.Arr("partner_ids") {
		out = append(out, proto.Message{"partner_id": p.Int("partner_id"), "dower_skill": []proto.Message{}})
	}
	return s.Send(11099, proto.Message{"partner_ids": out})
}

// partnerResetCount answers 11067 (how often a hero may still be reset): the hero
// is echoed with no reset pending (end_time 0) and no use today (day_num 0). Hero
// reset itself is not implemented.
func partnerResetCount(s *gateway.Session, req proto.Message) error {
	return s.Send(11067, proto.Message{"partner_id": req.Int("partner_id"), "end_time": 0, "day_num": 0})
}

// partnerHolyEquipment answers 11092 (the holy equipment worn by heroes): every
// asked hero is echoed with an empty holy_eqm list; the feature is not implemented.
func partnerHolyEquipment(s *gateway.Session, req proto.Message) error {
	out := []proto.Message{}
	for _, p := range req.Arr("partner_ids") {
		out = append(out, proto.Message{"partner_id": p.Int("partner_id"), "holy_eqm": []proto.Message{}})
	}
	return s.Send(11092, proto.Message{"partner_ids": out})
}

// combatType answers 20060. The client sends it with the fight type it wants to
// open a window for (mainui_controller.lua requestOpenBattleRelevanceWindow) and,
// when it leaves a battle, with 0. The reply must repeat that combat_type, and
// type = 0 means "no unfinished fight of that kind", on which the client opens
// the window (openRelevanceWindowAtOnce compares the reply with what it asked).
//
// For the campaign the original server (capture original-2026-10-20) answers
// type 2 and pushes 20061 in the same tick: the data of the idle ("hook") battle
// that the client plays in the background of the campaign screen. Only once that
// scene exists does the client show the "挑战BOSS" button. Other fight types keep
// the plain answer: the server runs no real battles yet.
func combatType(s *gateway.Session, st *store.Store, req proto.Message) error {
	ct := req.Int("combat_type")
	role := s.Role()
	if ct != combatDrama || role == nil {
		return s.Send(20060, proto.Message{"combat_type": ct, "type": 0})
	}
	if err := s.Send(20060, proto.Message{"combat_type": ct, "type": 2}); err != nil {
		return err
	}
	hook, err := hookBattle(st, role.RID)
	if err != nil {
		return err
	}
	return s.Send(20061, hook)
}

// hookWave is the one wave of the idle battle: the monsters of the dungeon with 10 hp
// each and one weak skill, as in the capture (a hook fight is over at once).
func hookWave(wave dungeonUnit) []proto.Message {
	var out []proto.Message
	for i, m := range wave.Monsters {
		if i == len(monsterPos) {
			break
		}
		u, _ := monsterOf(m, i)
		sid := int64(defaultMonsterSkill)
		if len(u.skills) > 0 {
			sid = u.skills[0]
		}
		out = append(out, hookMonster(m.Bid, monsterPos[i]-9, u.speed, sid))
	}
	return out
}

func hookMonster(bid, pos, speed, sid int64) proto.Message {
	return proto.Message{
		"pos": pos, "bid": bid, "speed": speed, "hp": 10, "crit": 0,
		"skill_list": []proto.Message{skillEntry(sid, 1, 2)},
	}
}

// skillEntry is one skill of a battle unit. The effect id follows the pattern seen
// in the capture: skill 262901 -> effect 2629001, 200901 -> 2009001.
func skillEntry(sid, minHurt, maxHurt int64) proto.Message {
	return proto.Message{
		"sid": sid, "rand": 100,
		"effect_list": []proto.Message{{"eid": sid/100*1000 + sid%100, "num": 1, "min_hurt": minHurt, "max_hurt": maxHurt}},
	}
}

// hookBattle builds 20061 for a role: its campaign formation (kind 0) against the
// fixed wave.
func hookBattle(st *store.Store, rid int64) (proto.Message, error) {
	d, err := dramaState(st, rid)
	if err != nil {
		return nil, err
	}
	wave, ok := dungeonUnits[d.DunID]
	if !ok || len(wave.Monsters) == 0 {
		return nil, errNoWave
	}
	f, err := formationOf(st, rid, 0)
	if err != nil {
		return nil, err
	}
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	owned := map[int64]store.Partner{}
	for _, h := range heroes {
		owned[h.PartnerID] = h
	}
	slots := append([]store.FormationSlot(nil), f.Slots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Pos < slots[j].Pos })

	team := []proto.Message{}
	for _, sl := range slots {
		h, ok := owned[sl.PartnerID]
		if !ok || len(team) == formMaxHeroes {
			continue
		}
		if _, known := partnerTable[h.BID]; !known {
			continue
		}
		m := partnerMessage(h)
		atk := m.Int("atk")
		skills := []proto.Message{}
		if sk := m.Arr("skills"); len(sk) > 0 { // one skill, like the capture: the normal attack when we know which it is
			sid := sk[0].Int("skill_bid")
			for _, k := range sk {
				if r, ok := skillTable[k.Int("skill_bid")]; ok && r.Normal == 1 {
					sid = k.Int("skill_bid")
				}
			}
			skills = append(skills, skillEntry(sid, atk*9/10, atk))
		}
		team = append(team, proto.Message{
			"pos": battleCell(f.FormationType, sl.Pos), "speed": m.Int("speed"), "bid": h.BID, "lev": h.Lev, "star": h.Star,
			"quality": 0, "hp": m.Int("hp"), "crit": m.Int("crit_rate") + baseCritRate, "skill_list": skills, "use_skin": 0,
		})
	}
	return proto.Message{
		"combat_type": combatDrama, "bid": wave.UnitID, "dun_bid": d.DunID, "b_formation_type": f.FormationType,
		"partner_list": team,
		"wave_list":    []proto.Message{{"unit_list": hookWave(wave)}},
	}, nil
}
