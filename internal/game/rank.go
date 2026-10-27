package game

import (
	"errors"
	"sort"
	"sync"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Rank types of the client's RankConstant.Rank_Type that this server can fill.
const (
	rankDrama = 2 // campaign progress: val1 is the furthest dungeon id
	rankTower = 3 // star tower: val1 is the highest floor
	rankArena = 5 // arena: val1 is the score
	rankPower = 6 // power: val1 is the power
	// the views of the seven-day ranking (days_rank_data rank_type)
	rankPartner  = 14 // the power of the strongest single hero
	rankTreasure = 28 // lucky treasure draws
	rankEndless  = 29 // endless trial: the highest round of any mode
	// rankSweet (49) is the personal score of the sweet activity, sweet.go
)

const (
	rankMaxLen   = 100
	rankSnapshot = 300 // seconds a list is served from its snapshot before it is built again
)

type rankKey struct {
	st  *store.Store
	srv string
	typ int64
}

type rankSnap struct {
	at   int64
	list []rankEntry
}

var (
	rankMu    sync.Mutex
	rankSnaps = map[rankKey]*rankSnap{}
)

// rankingAt is the list of a type from its snapshot: built again when it is older than rankSnapshot seconds (the first
// request after that pays for it; no timer), so a busy server does not scan every hero for each request. A settlement
// that pays by rank (a season, the end of an event) calls ranking directly and so pays by the final standing.
func rankingAt(st *store.Store, srvID string, typ int64, now int64) ([]rankEntry, error) {
	rankMu.Lock()
	defer rankMu.Unlock()
	k := rankKey{st, srvID, typ}
	if sn := rankSnaps[k]; sn != nil && now >= sn.at && now-sn.at < rankSnapshot {
		return sn.list, nil
	}
	list, err := ranking(st, srvID, typ)
	if err != nil {
		return nil, err
	}
	rankSnaps[k] = &rankSnap{at: now, list: list}
	return list, nil
}

// rankingTime is when the snapshot of a list was built, 0 for none.
func rankingTime(st *store.Store, srvID string, typ int64) int64 {
	rankMu.Lock()
	defer rankMu.Unlock()
	if sn := rankSnaps[rankKey{st, srvID, typ}]; sn != nil {
		return sn.at
	}
	return 0
}

type rankEntry struct {
	role store.RankRole
	val  int64
}

// rolePower is the power ranking's number for a role: the sum of the powers of
// its five strongest heroes (our policy; the client does not say).
func rolePower(heroes []store.Partner) int64 {
	powers := make([]int64, 0, len(heroes))
	for _, h := range heroes {
		if _, ok := partnerTable[h.BID]; ok {
			powers = append(powers, partnerMessage(h).Int("power"))
		}
	}
	sort.Slice(powers, func(i, j int) bool { return powers[i] > powers[j] })
	var sum int64
	for i := 0; i < len(powers) && i < 5; i++ {
		sum += powers[i]
	}
	return sum
}

// ranking builds one server's list for a type, best first, at most rankMaxLen
// long; nil for a type this server does not rank. Ties keep the older progress first.
func ranking(st *store.Store, srvID string, typ int64) ([]rankEntry, error) {
	roles, err := st.RankRoles(srvID)
	if err != nil {
		return nil, err
	}
	var out []rankEntry
	switch typ {
	case rankDrama:
		for _, r := range roles {
			v := r.MaxDunID
			if v == 0 {
				v = dramaChain.First
			}
			out = append(out, rankEntry{r, int64(dungeonIndex[v] + 1)})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].val != out[j].val {
				return out[i].val > out[j].val
			}
			if out[i].role.DramaAt != out[j].role.DramaAt {
				return out[i].role.DramaAt < out[j].role.DramaAt
			}
			return out[i].role.RID < out[j].role.RID
		})
		for i := range out { // val1 of the campaign rank is the dungeon id itself
			out[i].val = dramaChain.Dungeons[out[i].val-1].ID
		}
	case rankPower:
		heroes, err := st.PartnersOfServer(srvID)
		if err != nil {
			return nil, err
		}
		for _, r := range roles {
			out = append(out, rankEntry{r, rolePower(heroes[r.RID])})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].val != out[j].val {
				return out[i].val > out[j].val
			}
			return out[i].role.RID < out[j].role.RID
		})
	case rankTower, rankArena:
		var rows []struct{ rid, val int64 }
		if typ == rankTower {
			top, err := st.TowerTop(srvID, rankMaxLen)
			if err != nil {
				return nil, err
			}
			for _, t := range top {
				rows = append(rows, struct{ rid, val int64 }{t.RID, t.Tower})
			}
		} else {
			top, err := st.ArenaTop(srvID, rankMaxLen)
			if err != nil {
				return nil, err
			}
			for _, t := range top {
				rows = append(rows, struct{ rid, val int64 }{t.RID, t.Score})
			}
		}
		byRID := map[int64]store.RankRole{}
		for _, r := range roles {
			byRID[r.RID] = r
		}
		for _, row := range rows { // already in order
			if r, ok := byRID[row.rid]; ok {
				out = append(out, rankEntry{r, row.val})
			}
		}
	case rankPartner, rankTreasure, rankEndless:
		vals, err := daysRankValues(st, srvID, typ, roles)
		if err != nil {
			return nil, err
		}
		for _, r := range roles {
			if v := vals[r.RID]; v > 0 {
				out = append(out, rankEntry{r, v})
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].val != out[j].val {
				return out[i].val > out[j].val
			}
			return out[i].role.RID < out[j].role.RID
		})
	case rankSweet:
		return sweetRanking(st, srvID, roles)
	case rankSandybeach:
		return sandybeachRanking(st, srvID, roles)
	default:
		return nil, nil
	}
	if len(out) > rankMaxLen {
		out = out[:rankMaxLen]
	}
	return out, nil
}

func rankRow(e rankEntry, srvID string, idx int) proto.Message {
	return proto.Message{"rid": e.role.RID, "srv_id": srvID, "name": e.role.Name, "guild_name": "", "lev": e.role.Lev,
		"face_id": e.role.FaceID, "avatar_bid": 0, "val1": e.val, "val2": 0, "val3": 0, "idx": idx, "desc": "", "look_id": 0,
		"face_update_time": 0, "face_file": ""}
}

// guildRanking lists the guilds of a server by power, the sum of the powers of their members'
// five strongest heroes each (our policy; the client does not say), ties by level then id.
func guildRanking(st *store.Store, srvID string) ([]store.GuildRow, map[int64]int64, error) {
	guilds, _, err := st.GuildsOf(srvID, "", 1, rankMaxLen*10)
	if err != nil {
		return nil, nil, err
	}
	heroes, err := st.PartnersOfServer(srvID)
	if err != nil {
		return nil, nil, err
	}
	power := map[int64]int64{}
	for _, g := range guilds {
		members, err := st.MembersOf(g.GID)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range members {
			power[g.GID] += rolePower(heroes[m.RID])
		}
	}
	sort.SliceStable(guilds, func(i, j int) bool {
		a, b := guilds[i], guilds[j]
		if power[a.GID] != power[b.GID] {
			return power[a.GID] > power[b.GID]
		}
		return a.Lev > b.Lev
	})
	if len(guilds) > rankMaxLen {
		guilds = guilds[:rankMaxLen]
	}
	return guilds, power, nil
}

// RegisterRank registers the rankings: 12900 (a list), 12901 (when it was last
// refreshed) and 12902 (the first place of each ranking). Only the campaign, star tower, arena and
// power rankings of the role's own server are filled; any other type is an empty list.
func RegisterRank(r *gateway.Router, st *store.Store) {
	r.Handle(12900, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ, start, num := req.Int("type"), req.Int("start"), req.Int("num")
		list, err := rankingAt(st, role.SrvID, typ, s.Now().Unix())
		if err != nil {
			return err
		}
		myIdx, myVal, err := rankSelf(st, role.RID, typ, list)
		if err != nil {
			return err
		}
		if start < 1 {
			start = 1
		}
		rows := []proto.Message{}
		for i := int(start) - 1; i < len(list) && int64(len(rows)) < num; i++ {
			rows = append(rows, rankRow(list[i], role.SrvID, i+1))
		}
		return s.Send(12900, proto.Message{"is_cluster": 0, "type": typ, "start": start, "num": len(rows), "len": len(list),
			"time": rankingTime(st, role.SrvID, typ), "my_idx": myIdx, "lev": role.Lev, "face_id": role.FaceID, "avatar_bid": 0, "name": role.Name,
			"my_val1": myVal, "my_val2": 0, "my_val3": 0, "rank_list": rows})
	})
	r.Handle(12903, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list, power, err := guildRanking(st, role.SrvID)
		if err != nil {
			return err
		}
		var mine *store.Guild
		if gd, _, err := st.GuildOfRole(role.RID); err != nil {
			return err
		} else {
			mine = gd
		}
		start, num := req.Int("start"), req.Int("num")
		if start < 1 {
			start = 1
		}
		out := proto.Message{"start": start, "len": len(list), "time": s.Now().Unix(), "my_idx": 0,
			"gname": "", "leader_name": "", "glev": 0, "power": 0}
		rows := []proto.Message{}
		for i, x := range list {
			if mine != nil && x.GID == mine.GID {
				out["my_idx"], out["gname"], out["leader_name"], out["glev"], out["power"] = i+1, x.Name, x.LeaderName, x.Lev, power[x.GID]
			}
			if int64(i+1) < start || int64(len(rows)) >= num {
				continue
			}
			leader, err := st.RoleByID(x.LeaderRID)
			if err != nil {
				return err
			}
			row := proto.Message{"name": x.Name, "lev": x.Lev, "members_num": x.Members, "members_max": guildCapacity(x.Lev),
				"leader_name": x.LeaderName, "power": power[x.GID], "idx": i + 1, "leader_rid": x.LeaderRID, "leader_srvid": x.SrvID,
				"leader_face": 0, "leader_lev": 0, "leader_avatar_bid": 0, "leader_look_id": 0, "leader_face_update_time": 0, "leader_face_file": ""}
			if leader != nil {
				row["leader_face"], row["leader_lev"] = leader.FaceID, leader.Lev
			}
			rows = append(rows, row)
		}
		out["num"], out["rank_list"] = len(rows), rows
		return s.Send(12903, out)
	})
	r.Handle(12901, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if _, err := rankingAt(st, role.SrvID, req.Int("type"), s.Now().Unix()); err != nil {
			return err
		}
		return s.Send(12901, proto.Message{"is_cluster": 0, "type": req.Int("type"), "time": rankingTime(st, role.SrvID, req.Int("type"))})
	})
	r.Handle(12902, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		out := []proto.Message{}
		for _, typ := range []int64{rankDrama, rankTower, rankArena, rankPower} {
			list, err := rankingAt(st, role.SrvID, typ, s.Now().Unix())
			if err != nil {
				return err
			}
			if len(list) == 0 {
				continue
			}
			row := rankRow(list[0], role.SrvID, 1)
			out = append(out, proto.Message{"type": typ, "name": row["name"], "rid": row["rid"], "srv_id": role.SrvID, "guild_name": "",
				"face_id": row["face_id"], "avatar_bid": 0, "val1": row["val1"], "val2": 0, "val3": 0, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(12902, proto.Message{"is_cluster": 0, "rank_list": out})
	})
}

// rankLive is a role's value in a ranking right now (not from a snapshot) and whether it counts at all.
func rankLive(st *store.Store, rid, typ int64) (int64, bool, error) {
	switch typ {
	case rankDrama:
		d, err := st.Drama(rid)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return dramaChain.First, true, nil
			}
			return 0, false, err
		}
		if d.MaxDunID == 0 {
			return dramaChain.First, true, nil
		}
		return d.MaxDunID, true, nil
	case rankTower:
		t, err := st.StarTowerOf(rid)
		return t.MaxTower, t.MaxTower > 0, err
	case rankArena:
		score, _, ok, err := st.ArenaScoreOf(rid)
		return score, ok, err
	case rankPower:
		heroes, err := st.PartnersOf(rid)
		return rolePower(heroes), true, err
	case rankPartner, rankTreasure, rankEndless:
		v, err := daysRankLive(st, rid, typ)
		return v, v > 0, err
	}
	return 0, false, nil
}

// rankBefore says whether value a is ahead of value b in a ranking (the campaign ranks by its place in the chain).
func rankBefore(typ, a, b int64) bool {
	if typ == rankDrama {
		return dungeonIndex[a] > dungeonIndex[b]
	}
	return a > b
}

// rankSelf is the place and value of a role in a ranking: its value is read now and set against the snapshot's list,
// so a fight shows at once. A role that was in the snapshot with the same value keeps its place; one that changed goes
// behind those with an equal value (it arrived later). 0 when it is not within the list's length.
func rankSelf(st *store.Store, rid, typ int64, list []rankEntry) (idx, val int64, err error) {
	switch typ {
	case rankDrama, rankTower, rankArena, rankPower, rankPartner, rankTreasure, rankEndless:
	default: // a list that is not built from the roles' own state (the activities' scores): the list is the truth
		for i, e := range list {
			if e.role.RID == rid {
				return int64(i + 1), e.val, nil
			}
		}
		return 0, 0, nil
	}
	val, ok, err := rankLive(st, rid, typ)
	if err != nil || !ok {
		return 0, 0, err
	}
	ahead, same := int64(0), int64(0)
	for i, e := range list {
		if e.role.RID == rid {
			if e.val == val {
				return int64(i + 1), val, nil
			}
			continue
		}
		switch {
		case rankBefore(typ, e.val, val):
			ahead++
		case e.val == val:
			same++
		}
	}
	idx = ahead + same + 1
	if idx > rankMaxLen {
		idx = 0
	}
	return idx, val, nil
}
