package game

import (
	"errors"
	"sort"
	"strconv"

	"2droundserver/internal/store"
)

func daysRankStandings(st *store.Store, srv string, typ int64, h5 bool) ([]rankEntry, error) {
	switch typ {
	case rankDrama, rankTreasure, rankEndless:
		return ranking(st, srv, typ)
	}
	candidates, err := st.DaysRankCandidates(srv)
	if err != nil {
		return nil, err
	}
	var out []rankEntry
	for _, candidate := range candidates {
		if drH5Pool(typ) != "" && (candidate.H5 != h5 || h5 && !candidate.AccountActive) {
			continue
		}
		var value int64
		switch typ {
		case rankTower:
			value = candidate.Tower
		case rankArena:
			if candidate.InArena {
				out = append(out, rankEntry{candidate.RankRole, candidate.Arena})
			}
			continue
		case rankPartner, rankPower:
			value, err = daysRankHeroPower(st, candidate.RID, typ, h5)
			if err != nil {
				return nil, err
			}
		}
		if value > 0 {
			out = append(out, rankEntry{candidate.RankRole, value})
		}
	}
	sort.Slice(out, func(left, right int) bool { return daysRankBefore(typ, out[left], out[right]) })
	return out[:min(len(out), rankMaxLen)], nil
}

func daysRankHeroPower(st *store.Store, rid, typ int64, h5 bool) (int64, error) {
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return 0, err
	}
	if typ == rankPower {
		return rolePower(playablePartners(heroes, h5)), nil
	}
	var strongest int64
	for _, hero := range heroes {
		if visiblePartner(hero, h5) {
			strongest = max(strongest, partnerMessage(hero).Int("power"))
		}
	}
	return strongest, nil
}

func daysRankBefore(typ int64, left, right rankEntry) bool {
	if left.val != right.val {
		return rankBefore(typ, left.val, right.val)
	}
	if typ == rankDrama && left.role.DramaAt != right.role.DramaAt {
		return left.role.DramaAt < right.role.DramaAt
	}
	return left.role.RID < right.role.RID
}

func daysRankPosition(st *store.Store, role *store.Role, typ int64, h5 bool, list []rankEntry) (int64, error) {
	rid := role.RID
	if h5 {
		if allowed, err := st.ArenaRoleInPool(role.SrvID, rid, true); err != nil || !allowed {
			return 0, err
		}
	}
	own := rankEntry{role: store.RankRole{RID: rid}}
	var err error
	switch typ {
	case rankDrama:
		own.val = dramaChain.First
		drama, err := st.Drama(rid)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return 0, err
		}
		if err == nil && drama.MaxDunID != 0 {
			own.val, own.role.DramaAt = drama.MaxDunID, drama.UpdatedAt
		}
	case rankPartner, rankPower:
		own.val, err = daysRankHeroPower(st, rid, typ, h5)
		if err != nil || own.val == 0 {
			return 0, err
		}
	default:
		var ranked bool
		own.val, ranked, err = rankLive(st, rid, typ)
		if err != nil || !ranked {
			return 0, err
		}
	}
	position := int64(1)
	for _, entry := range list {
		if entry.role.RID != rid && daysRankBefore(typ, entry, own) {
			position++
		}
	}
	return position, nil
}

func (w *drWorld) standings(st *store.Store, srv string, id, typ int64, h5 bool) ([]rankEntry, error) {
	rows := w.Frozen[strconv.FormatInt(id, 10)]
	h5Rows, split := w.Frozen[drH5Pool(typ)]
	if h5 && split {
		rows = h5Rows
	} else if h5 && typ != rankArena {
		return nil, nil
	}
	var legacy map[int64]store.RankRole
	var out []rankEntry
	for _, frozen := range rows {
		if frozen.Role != nil {
			out = append(out, rankEntry{*frozen.Role, frozen.Val})
			continue
		}
		if legacy == nil {
			roles, err := st.RankRoles(srv)
			if err != nil {
				return nil, err
			}
			legacy = make(map[int64]store.RankRole, len(roles))
			for _, role := range roles {
				legacy[role.RID] = role
			}
		}
		role, found := legacy[frozen.RID]
		if !found {
			continue
		}
		if drH5Pool(typ) != "" && !split {
			allowed, err := st.ArenaRoleInPool(srv, frozen.RID, h5)
			if err != nil {
				return nil, err
			}
			if !allowed {
				continue
			}
		}
		out = append(out, rankEntry{role, frozen.Val})
	}
	return out, nil
}
