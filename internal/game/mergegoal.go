package game

import (
	_ "embed"
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// mergegoal.json: the merge-server goal of the client (tools/extract_mergegoal.py).
//
//go:embed data/mergegoal.json
var mergeGoalJSON []byte

const (
	mergeGoalCamp   = 9993039 // ActionRankCommonType.merge_aim: the camp_id the operator gives the activity in the schedule
	mergeGoalModule = "mergegoal"
)

var mergeGoalCfg = func() (c struct {
	Base   int64 `json:"base_max_score"`
	Awards []struct {
		ID, Count int64
		Items     [][2]int64
	}
	Days []struct{ Day, Interval, Pro, Hours int64 }
}) {
	if err := json.Unmarshal(mergeGoalJSON, &c); err != nil || c.Base == 0 || len(c.Awards) != 7 || len(c.Days) != 7 {
		panic("game: bad mergegoal.json")
	}
	return c
}()

// mergeGoalScore is the server's score after the activity ran for `elapsed` seconds. The client's table has the server's score
// grow by itself: on day n, every `interval` seconds, `pro` points, for `hours` hours of the day. (The score that the original
// got from the merge quests, ten a task, and from the gifts, is not here.)
func mergeGoalScore(elapsed int64) int64 {
	var score int64
	for _, d := range mergeGoalCfg.Days {
		if elapsed <= 0 {
			break
		}
		perDay := d.Hours * 3600 / d.Interval
		inDay := min(elapsed, 86400)
		score += min(perDay, inDay/d.Interval) * d.Pro
		elapsed -= 86400
	}
	return min(score, mergeGoalCfg.Base)
}

type mergeGoalRole struct {
	Start   int64
	Claimed []int64
}

// RegisterMergeGoal registers the merge-server goal (an operator opens it as activity 9993039): 27300 the score of the server
// and the boxes taken, 27301 take the boxes whose score the server has reached.
func RegisterMergeGoal(r *gateway.Router, st *store.Store) {
	reached := func(score int64) int64 {
		var n int64
		for _, a := range mergeGoalCfg.Awards {
			if score*1000/mergeGoalCfg.Base >= a.Count {
				n++
			}
		}
		return n
	}
	with := func(s *gateway.Session, fn func(role *store.Role, d *mergeGoalRole, score, end int64, open bool) (bool, error)) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		start, end, open := ActivityOpen(st, role.SrvID, mergeGoalCamp, s.Now())
		d := &mergeGoalRole{}
		if _, err := st.LoadState(role.RID, mergeGoalModule, d); err != nil {
			return err
		}
		if open && d.Start != start {
			*d = mergeGoalRole{Start: start}
		}
		score := int64(0)
		if open {
			score = mergeGoalScore(s.Now().Unix() - start)
		}
		save, err := fn(role, d, score, end, open)
		if err != nil || !save {
			return err
		}
		return st.SaveState(role.RID, mergeGoalModule, d)
	}
	r.Handle(27300, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(role *store.Role, d *mergeGoalRole, score, end int64, open bool) (bool, error) {
			claimed := []proto.Message{}
			for _, id := range d.Claimed {
				claimed = append(claimed, proto.Message{"id": id})
			}
			return false, s.Send(27300, proto.Message{"end_time": end, "lev": 1 + reached(score), "score": score, "max_score": mergeGoalCfg.Base, "reward": claimed})
		})
	})
	r.Handle(27301, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(role *store.Role, d *mergeGoalRole, score, end int64, open bool) (bool, error) {
			refuse := func(msg string) (bool, error) { return false, s.Send(27301, proto.Message{"flag": 0, "msg": msg}) }
			if !open {
				return refuse("活动已结束")
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			var take []int64
			for _, e := range req.Arr("reward_list") {
				id := e.Int("id")
				for _, a := range mergeGoalCfg.Awards {
					if a.ID != id || has(d.Claimed, id) || has(take, id) || score*1000/mergeGoalCfg.Base < a.Count {
						continue
					}
					take = append(take, id)
					for _, it := range a.Items {
						splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
					}
				}
			}
			if len(take) == 0 {
				return refuse("没有可以领取的奖励或积分还没有达到")
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return false, err
			}
			d.Claimed = append(d.Claimed, take...)
			if err := s.Send(27301, proto.Message{"flag": 1, "msg": ""}); err != nil {
				return true, err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return true, err
			}
			return true, sendBackpack(s, st)
		})
	})
}
