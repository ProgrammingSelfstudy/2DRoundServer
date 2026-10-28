package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// exptasks.json is the client's advanced-trial task table (tools/extract_exptasks.py).
//
//go:embed data/exptasks.json
var expTasksJSON []byte

type expTaskDef struct {
	Name    string     `json:"name"`
	Desc    string     `json:"desc"`
	Hide    int        `json:"hide"`
	Lev     int        `json:"lev"`
	Type    int        `json:"type"`
	Rewards [][2]int64 `json:"rewards"`
}

var expTaskTable = func() map[int64]expTaskDef {
	var f struct {
		Tasks map[int64]expTaskDef `json:"tasks"`
	}
	if err := json.Unmarshal(expTasksJSON, &f); err != nil || len(f.Tasks) == 0 {
		panic("game: bad exptasks.json")
	}
	return f.Tasks
}()

// expTaskOpenLev is the client's experience_open_limit: the trials open at this level.
const expTaskOpenLev = 55

// expTaskGoal is how one trial is measured: the figure and the goal. The table has no goals, so
// these are read off the descriptions; trials about battles, ranks and seasons are not measured here
// and stay at 0 of 1.
type expTaskGoal struct {
	measure string
	target  int64
}

var expTaskGoals = map[int64]expTaskGoal{
	40000: {"tower", 300}, 40010: {"tower", 600},
	52000: {"power", 1000000}, 52010: {"power", 2000000}, 52020: {"power", 3500000},
	53000: {"star10", 5}, 53010: {"star11", 5}, 53710: {"star13", 1},
}

// expTaskValues reads the figures the trials are measured by.
func expTaskValues(st *store.Store, rid int64) (map[string]int64, error) {
	tower, err := st.StarTowerOf(rid)
	if err != nil {
		return nil, err
	}
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	v := map[string]int64{"tower": tower.MaxTower, "power": rolePower(heroes)}
	for _, h := range heroes {
		for star, key := range map[int]string{10: "star10", 11: "star11", 13: "star13"} {
			if h.Star >= star {
				v[key]++
			}
		}
	}
	return v, nil
}

func expTaskIDs(lev int) []int64 {
	ids := make([]int64, 0, len(expTaskTable))
	for id, t := range expTaskTable {
		if t.Lev <= lev {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func expTaskMessage(id int64, vals map[string]int64) proto.Message {
	g, ok := expTaskGoals[id]
	if !ok {
		g = expTaskGoal{target: 1}
	}
	value := vals[g.measure]
	return proto.Message{"id": id, "finish": b2i(value >= g.target) * questDone, "end_time": 0, "finish_time": 0,
		"progress": []proto.Message{{"id": 1, "finish": b2i(value >= g.target), "target": 0, "target_val": g.target, "value": min(value, g.target)}}}
}

func expTaskTime(st *store.Store, rid, id int64) (int64, error) {
	return st.Counter(rid, "exptask_"+strconv.FormatInt(id, 10))
}

// RegisterExpTask registers the advanced trials: 25810 (the list: open ones with their progress,
// taken ones with the time they were taken), 25811 pushes changes (nothing is pushed here) and
// 25812 takes a finished one's reward.
func RegisterExpTask(r *gateway.Router, st *store.Store) {
	// the original pushes the open trials with the city (the taken ones only come with 25810)
	r.OnEnter(func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil || role.Lev < expTaskOpenLev {
			return nil
		}
		vals, err := expTaskValues(st, role.RID)
		if err != nil {
			return err
		}
		done, err := st.FeatsClaimed(role.RID)
		if err != nil {
			return err
		}
		feats := []proto.Message{}
		for _, id := range expTaskIDs(int(role.Lev)) {
			if !done[id] {
				feats = append(feats, expTaskMessage(id, vals))
			}
		}
		return s.Send(25811, proto.Message{"feat_list": feats})
	})
	r.Handle(25810, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil { // the level may have changed since login
			role = fresh
		}
		out := proto.Message{"feat_list": []proto.Message{}, "finish_list": []proto.Message{}}
		if role.Lev >= expTaskOpenLev {
			vals, err := expTaskValues(st, role.RID)
			if err != nil {
				return err
			}
			done, err := st.FeatsClaimed(role.RID)
			if err != nil {
				return err
			}
			feats, finished := []proto.Message{}, []proto.Message{}
			for _, id := range expTaskIDs(int(role.Lev)) {
				if !done[id] {
					feats = append(feats, expTaskMessage(id, vals))
					continue
				}
				at, err := expTaskTime(st, role.RID, id)
				if err != nil {
					return err
				}
				finished = append(finished, proto.Message{"id": id, "finish_time": at})
			}
			out["feat_list"], out["finish_list"] = feats, finished
		}
		return s.Send(25810, out)
	})
	r.Handle(25812, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil { // the level may have changed since login
			role = fresh
		}
		id := req.Int("id")
		reply := func(code int, msg string, at int64) error {
			return s.Send(25812, proto.Message{"code": code, "msg": msg, "id": id, "finish_time": at})
		}
		t, ok := expTaskTable[id]
		if !ok || role.Lev < expTaskOpenLev || int(role.Lev) < t.Lev {
			return reply(0, "任务不存在", 0)
		}
		vals, err := expTaskValues(st, role.RID)
		if err != nil {
			return err
		}
		if expTaskMessage(id, vals).Int("finish") == 0 {
			return reply(0, "任务尚未完成", 0)
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, g := range t.Rewards {
			splitGains([]sellGain{{g[0], g[1]}}, 1, assets, items)
		}
		if err := st.ClaimFeat(role.RID, id, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "奖励已领取", 0)
			}
			return err
		}
		now := s.Now().Unix()
		if _, err := st.AddCounter(role.RID, "exptask_"+strconv.FormatInt(id, 10), now); err != nil {
			return err
		}
		if err := reply(1, "领取成功", now); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
