package game

import (
	"errors"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const formGuildBoss = 4

var errGuildBossFormation = errors.New("invalid guild boss formation")

func h5GuildBossRoster(st *store.Store, role *store.Role) ([]store.Partner, error) {
	if _, err := starterGrant(st, role); err != nil {
		return nil, err
	}
	heroes, err := st.PartnersOf(role.RID)
	if err != nil {
		return nil, err
	}
	return playablePartners(heroes, true), nil
}

// h5GuildBossSlots keeps eligible heroes in distinct positions. It is also used
// to validate an explicit lineup submitted for a fight or saved by the client.
func h5GuildBossSlots(slots []store.FormationSlot, heroes []store.Partner) []store.FormationSlot {
	byID := make(map[int64]int64, len(heroes))
	for _, h := range heroes {
		byID[h.PartnerID] = h.BID
	}
	seenBID, seenPos := map[int64]bool{}, map[int64]bool{}
	out := make([]store.FormationSlot, 0, formMaxHeroes)
	for _, sl := range slots {
		bid := byID[sl.PartnerID]
		if bid == 0 || sl.Pos < 1 || sl.Pos > formMaxPos || seenBID[bid] || seenPos[sl.Pos] {
			continue
		}
		seenBID[bid], seenPos[sl.Pos] = true, true
		out = append(out, sl)
	}
	return out
}

func h5GuildBossFormation(st *store.Store, rid int64, heroes []store.Partner) (store.Formation, error) {
	for _, kind := range []int64{formGuildBoss, formDrama} {
		f, err := st.Formation(rid, kind)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Formation{}, err
		}
		f.Type, f.Slots = formGuildBoss, h5GuildBossSlots(f.Slots, heroes)
		if len(f.Slots) > 0 {
			return *f, nil
		}
	}
	f := store.Formation{Type: formGuildBoss, FormationType: 1}
	seen := map[int64]bool{}
	for _, h := range heroes {
		if seen[h.BID] {
			continue
		}
		seen[h.BID] = true
		f.Slots = append(f.Slots, store.FormationSlot{Pos: int64(len(f.Slots) + 1), PartnerID: h.PartnerID})
		if len(f.Slots) == formMaxHeroes {
			break
		}
	}
	return f, nil
}

func (f *FightService) guildBossTeam(role *store.Role, req proto.Message, h5 bool) ([]fightUnit, []proto.Message, int64, error) {
	formType := req.Int("formation_type")
	var slots []store.FormationSlot
	seen := map[int64]bool{}
	for _, p := range req.Arr("pos_info") {
		id, pos := p.Int("id"), p.Int("pos")
		if h5 || (id != 0 && !seen[id] && pos >= 1) {
			seen[id] = true
			slots = append(slots, store.FormationSlot{Pos: pos, PartnerID: id})
		}
	}
	if !h5 {
		if len(slots) == 0 {
			team, objects, form, err := f.teamSide(role, false)
			return team, objects, form.FormationType, err
		}
		formType = max(1, formType)
		team, objects, err := f.teamFor(role, formType, slots, false)
		return team, objects, formType, err
	}

	heroes, err := h5GuildBossRoster(f.Store, role)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(slots) == 0 {
		form, err := h5GuildBossFormation(f.Store, role.RID, heroes)
		if err != nil {
			return nil, nil, 0, err
		}
		formType, slots = form.FormationType, form.Slots
	} else if formType < 1 || formType > formTypes || len(h5GuildBossSlots(slots, heroes)) != len(slots) {
		return nil, nil, 0, errGuildBossFormation
	}
	team, objects := buildFightTeam(role, formType, slots, heroes)
	return team, objects, formType, nil
}
