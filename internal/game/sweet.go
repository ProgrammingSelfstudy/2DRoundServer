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

// sweet.json: the sweet activity (甜蜜大作战: the flag donation of the national day, 28500~28502) of the client
// (tools/extract_sweet.py).
//
//go:embed data/sweet.json
var sweetJSON []byte

const (
	sweetModule = "sweet"
	sweetCamp   = 93049 // ActionRankCommonType.sweet: the camp_id the operator gives the activity in the schedule
	rankSweet   = 49    // RankConstant.RankType.sweet: the personal score
)

type sweetAward struct {
	ID, Count int64
	Award     [][2]int64
}

var sweetCfg = func() (c struct {
	Const  map[string]int64 `json:"const"`
	Awards []sweetAward     `json:"awards"`
	Ranks  []struct {
		Min, Max int64
		Award    [][2]int64
	} `json:"ranks"`
}) {
	if err := json.Unmarshal(sweetJSON, &c); err != nil || len(c.Awards) == 0 || len(c.Ranks) == 0 || c.Const["base_max_score"] == 0 {
		panic("game: bad sweet.json")
	}
	return
}()

// sweetItems are the three things that are put in and what each is worth.
var sweetItems = []struct{ Item, Score int64 }{
	{sweetCfg.Const["holiday_item1"], sweetCfg.Const["holiday_score1"]},
	{sweetCfg.Const["holiday_item2"], sweetCfg.Const["holiday_score2"]},
	{sweetCfg.Const["holiday_item3"], sweetCfg.Const["holiday_score3"]},
}

// sweetWorld is the server's side: what everybody put in together and what each put in.
type sweetWorld struct {
	Start   int64
	Acc     int64
	Score   map[int64]int64
	Settled bool
}

type sweetRole struct {
	Start   int64
	Claimed []int64
}

// sweetRanked lists the roles that put something in, the higher score first.
func (w *sweetWorld) ranked() []int64 {
	var ids []int64
	for rid, n := range w.Score {
		if n > 0 {
			ids = append(ids, rid)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if w.Score[ids[i]] != w.Score[ids[j]] {
			return w.Score[ids[i]] > w.Score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

// sweetRanking is the rank list 12900 gives for the activity.
func sweetRanking(st *store.Store, srvID string, roles []store.RankRole) ([]rankEntry, error) {
	w := &sweetWorld{}
	if _, err := st.LoadGlobal(srvID, sweetModule, w); err != nil {
		return nil, err
	}
	byRID := map[int64]store.RankRole{}
	for _, r := range roles {
		byRID[r.RID] = r
	}
	var out []rankEntry
	for _, rid := range w.ranked() {
		if r, ok := byRID[rid]; ok {
			out = append(out, rankEntry{r, w.Score[rid]})
		}
	}
	return out, nil
}

// settle mails the rank rewards of the activity that is over, once.
func (w *sweetWorld) settle(st *store.Store, srv string) error {
	if w.Settled || w.Start == 0 {
		return nil
	}
	for i, rid := range w.ranked() {
		rank := int64(i + 1)
		for _, rk := range sweetCfg.Ranks {
			if rank >= rk.Min && rank <= rk.Max {
				m := store.Mail{SrvID: srv, Subject: "甜蜜大作战排名奖励", Content: "甜蜜大作战已结束，你的积分排在第" + itoa64(rank) + "名，这是排名奖励。",
					FromName: "甜蜜大作战", CreatedBy: "sweet", Attachments: attachmentsOf(rk.Award)}
				if _, err := st.CreateMail(m, []int64{rid}); err != nil {
					return err
				}
				break
			}
		}
	}
	w.Settled = true
	return nil
}

// RegisterSweet registers the sweet activity (an operator opens it as activity 93049): the roles put the three
// items in, each worth some score, for their own score (the rank, mailed when the activity ends) and for the
// server's accumulated score, which opens the rewards every role can take once (28501). The stage ("level") is
// the number of rewards the server has reached (our reading); the maximum is base_max_score.
func RegisterSweet(r *gateway.Router, st *store.Store) {
	maxAcc := sweetCfg.Const["base_max_score"]
	// with loads the server's and the role's side for the activity running now (nothing running: the last one is settled).
	with := func(s *gateway.Session, fn func(role *store.Role, w *sweetWorld, d *sweetRole, end int64, open bool) (bool, error)) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		now := s.Now()
		start, end, open := ActivityOpen(st, role.SrvID, sweetCamp, now)
		w, d := &sweetWorld{}, &sweetRole{}
		if _, err := st.LoadGlobal(role.SrvID, sweetModule, w); err != nil {
			return err
		}
		if _, err := st.LoadState(role.RID, sweetModule, d); err != nil {
			return err
		}
		changed := false
		if !open {
			if !w.Settled && w.Start != 0 {
				if err := w.settle(st, role.SrvID); err != nil {
					return err
				}
				changed = true
			}
		} else if w.Start != start {
			if err := w.settle(st, role.SrvID); err != nil {
				return err
			}
			*w = sweetWorld{Start: start}
			changed = true
		}
		if w.Score == nil {
			w.Score = map[int64]int64{}
		}
		if open && d.Start != start {
			*d = sweetRole{Start: start}
		}
		saveRole, err := fn(role, w, d, end, open)
		if err != nil {
			return err
		}
		if changed || saveRole {
			if err := st.SaveGlobal(role.SrvID, sweetModule, w); err != nil {
				return err
			}
		}
		if saveRole {
			return st.SaveState(role.RID, sweetModule, d)
		}
		return nil
	}
	reached := func(w *sweetWorld) int64 {
		var n int64
		for _, a := range sweetCfg.Awards {
			if min(w.Acc, maxAcc)*1000/maxAcc >= a.Count {
				n++
			}
		}
		return n
	}
	r.Handle(28500, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(role *store.Role, w *sweetWorld, d *sweetRole, end int64, open bool) (bool, error) {
			claimed := []proto.Message{}
			for _, id := range d.Claimed {
				claimed = append(claimed, proto.Message{"id": id})
			}
			acc := min(w.Acc, maxAcc)
			return false, s.Send(28500, proto.Message{"end_time": end, "lev": 1 + reached(w), "score": acc, "max_score": maxAcc, "my_score": w.Score[role.RID],
				"acc_score": acc, "max_acc_score": maxAcc, "reward": claimed})
		})
	})
	r.Handle(28501, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(role *store.Role, w *sweetWorld, d *sweetRole, _ int64, open bool) (bool, error) {
			refuse := func(msg string) (bool, error) { return false, s.Send(28501, proto.Message{"flag": 0, "msg": msg}) }
			if !open {
				return refuse("活动已结束")
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			var take []int64
			for _, e := range req.Arr("reward_list") {
				id := e.Int("id")
				for _, a := range sweetCfg.Awards {
					if a.ID != id || has(d.Claimed, id) || has(take, id) {
						continue
					}
					if min(w.Acc, maxAcc)*1000/maxAcc < a.Count {
						continue // not reached yet: left for later
					}
					take = append(take, id)
					for _, it := range a.Award {
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
			if err := s.Send(28501, proto.Message{"flag": 1, "msg": ""}); err != nil {
				return true, err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return true, err
			}
			return true, sendBackpack(s, st)
		})
	})
	r.Handle(28502, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(role *store.Role, w *sweetWorld, d *sweetRole, _ int64, open bool) (bool, error) {
			id, num := req.Int("id"), req.Int("num")
			refuse := func(msg string) (bool, error) {
				return false, s.Send(28502, proto.Message{"code": 0, "msg": msg, "id": id})
			}
			if !open {
				return refuse("活动已结束")
			}
			var score int64
			for _, it := range sweetItems {
				if it.Item == id {
					score = it.Score
				}
			}
			if score == 0 || num < 1 || num > 10000 {
				return refuse("不能放入这个道具")
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{id: num}, map[int64]int64{}, map[int64]int64{}); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return refuse("道具不足")
				}
				return false, err
			}
			w.Acc += score * num
			w.Score[role.RID] += score * num
			if err := s.Send(28502, proto.Message{"code": 1, "msg": "", "id": id}); err != nil {
				return true, err
			}
			if err := sendBackpack(s, st); err != nil {
				return true, err
			}
			return true, nil
		})
	})
}
