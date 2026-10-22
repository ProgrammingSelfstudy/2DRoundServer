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

// feats.json is the client's achievement table (tools/extract_feats.py).
//
//go:embed data/feats.json
var featsJSON []byte

type featDef struct {
	Name      string     `json:"name"`
	Desc      string     `json:"desc"`
	Hide      int        `json:"hide"`
	Label     string     `json:"label"`
	Target    int64      `json:"target"`
	TargetVal int64      `json:"target_val"`
	Rewards   [][2]int64 `json:"rewards"`
}

var featTable = func() map[int64]featDef {
	var f struct {
		Feats map[int64]featDef `json:"feats"`
	}
	if err := json.Unmarshal(featsJSON, &f); err != nil || len(f.Feats) == 0 {
		panic("game: bad feats.json")
	}
	return f.Feats
}()

// The achievement events this server can measure. The others (battles, arena,
// guild, ...) stay at 0. levup, friend and power are read from the role's state
// when asked; friend_present is a cumulative counter fed by the gift code.
const (
	featLevup         = "evt_levup"
	featFriend        = "evt_friend"
	featPower         = "evt_power"
	featFriendPresent = "evt_friend_present"
	counterPresent    = "friend_present"
)

func sortedFeatIDs() []int64 {
	ids := make([]int64, 0, len(featTable))
	for id := range featTable {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// featValues reads what the measurable events are worth for a role right now.
func featValues(st *store.Store, rid int64) (map[string]int64, error) {
	role, err := st.RoleByID(rid)
	if err != nil {
		return nil, err
	}
	friends, err := st.Friends(rid)
	if err != nil {
		return nil, err
	}
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	present, err := st.Counter(rid, counterPresent)
	if err != nil {
		return nil, err
	}
	return map[string]int64{featLevup: int64(role.Lev), featFriend: int64(len(friends)), featPower: rolePower(heroes), featFriendPresent: present}, nil
}

func featMessage(id int64, f featDef, value int64, claimed bool) proto.Message {
	status := questOpen
	switch {
	case claimed:
		status = questClaimed
	case value >= f.TargetVal:
		status = questDone
	}
	return proto.Message{"id": id, "finish": status, "end_time": 0, "finish_time": 0,
		"progress": []proto.Message{{"id": 1, "finish": b2i(value >= f.TargetVal), "target": f.Target, "target_val": f.TargetVal, "value": min(value, f.TargetVal)}}}
}

// RegisterFeat registers the achievements: 16400 (the list) and 16402 (take a
// finished one's reward); 16401 pushes a changed one.
func RegisterFeat(r *gateway.Router, st *store.Store) {
	r.Handle(16400, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		vals, err := featValues(st, role.RID)
		if err != nil {
			return err
		}
		done, err := st.FeatsClaimed(role.RID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range sortedFeatIDs() {
			f := featTable[id]
			list = append(list, featMessage(id, f, vals[f.Label], done[id]))
		}
		return s.Send(16400, proto.Message{"feat_list": list})
	})
	r.Handle(16402, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("id")
		reply := func(code int, msg string) error {
			return s.Send(16402, proto.Message{"code": code, "msg": msg, "id": id})
		}
		f, ok := featTable[id]
		if !ok {
			return reply(0, "成就不存在")
		}
		vals, err := featValues(st, role.RID)
		if err != nil {
			return err
		}
		if vals[f.Label] < f.TargetVal {
			return reply(0, "成就尚未完成")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, g := range f.Rewards {
			splitGains([]sellGain{{g[0], g[1]}}, 1, assets, items)
		}
		if err := st.ClaimFeat(role.RID, id, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "奖励已领取")
			}
			return err
		}
		if err := reply(1, "领取成功"); err != nil {
			return err
		}
		if err := s.Send(16401, proto.Message{"feat_list": []proto.Message{featMessage(id, f, vals[f.Label], true)}}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}

// featCount adds to a cumulative achievement counter and pushes the achievements
// that count it (16401).
func featCount(s *gateway.Session, st *store.Store, rid int64, key, label string, n int64) error {
	v, err := st.AddCounter(rid, key, n)
	if err != nil {
		return err
	}
	done, err := st.FeatsClaimed(rid)
	if err != nil {
		return err
	}
	var list []proto.Message
	for _, id := range sortedFeatIDs() {
		if f := featTable[id]; f.Label == label && !done[id] && v-n < f.TargetVal {
			list = append(list, featMessage(id, f, v, false))
		}
	}
	if len(list) == 0 {
		return nil
	}
	return s.Send(16401, proto.Message{"feat_list": list})
}

// pushLevelFeats tells the client the level achievements ("船长等级达到N级") that a new
// role level moved (16401). The client reads the list once, at login, and only repaints
// from pushes afterwards, so without this the bars stay at the login level.
func pushLevelFeats(s *gateway.Session, st *store.Store, rid int64, lev int) error {
	done, err := st.FeatsClaimed(rid)
	if err != nil {
		return err
	}
	var list []proto.Message
	for _, id := range sortedFeatIDs() {
		if f := featTable[id]; f.Label == featLevup && !done[id] {
			list = append(list, featMessage(id, f, int64(lev), false))
		}
	}
	if len(list) == 0 {
		return nil
	}
	return s.Send(16401, proto.Message{"feat_list": list})
}
