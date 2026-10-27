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

// sandybeach.json: the sandybeach boss fight (沙滩Boss, 25400~25405) of the client (tools/extract_sandybeach.py).
//
//go:embed data/sandybeach.json
var sandybeachJSON []byte

const (
	sbModule       = "sandybeach"
	sbCamp         = 25400 // the camp_id the operator gives the activity in the schedule (the client has no id for it: ours)
	sbForm         = 18    // PartnerConst.Fun_Form.Sandybeach_boss
	combatSB       = 12    // BattleConst.Fight_Type.SandybeachBossFight
	rankSandybeach = 43    // RankConstant.RankType.sandybeach_boss_fight: the score
	sbHPPerScore   = 500   // a boss has this much hp for each point of its damage score (ours: the client has no hp)
	sbHPScale      = 100000
)

var sbCfg = func() (c struct {
	Const  map[string]int64 `json:"const"`
	Bosses []struct {
		Order     int64  `json:"order"`
		Unit      int64  `json:"unit"`
		Name      string `json:"name"`
		DPSScore  int64  `json:"dps_score"`
		KillScore int64  `json:"kill_score"`
	} `json:"bosses"`
	Units map[string]monsterDef `json:"units"`
	Buy   []struct {
		Min, Max int64
		Expend   [][2]int64
	} `json:"buy"`
	Rewards []struct {
		ID, Num int64
		Items   [][2]int64
	} `json:"rewards"`
	Ranks []struct {
		Min, Max int64
		Items    [][2]int64
	} `json:"ranks"`
}) {
	if err := json.Unmarshal(sandybeachJSON, &c); err != nil || len(c.Bosses) != 50 || len(c.Units) == 0 {
		panic("game: bad sandybeach.json")
	}
	return
}()

func sbMaxHP(order int64) int64 { return sbCfg.Bosses[order-1].DPSScore * sbHPPerScore }

type sbLayer struct {
	Killed bool
	Best   int64 // the best damage in one fight, per mille of the boss's hp
}

type sbRole struct {
	Start, Day  int64
	Count, Buys int64
	Layers      map[int64]*sbLayer
	Left        int64 // hp the boss of the current layer has left, 0 for all of it
	Taken       []int64
	Fights, Dmg int64
}

type sbWorld struct {
	Start   int64
	Score   map[int64]int64
	Settled bool
}

func (r *sbRole) order() int64 {
	for o := int64(1); o <= int64(len(sbCfg.Bosses)); o++ {
		if l := r.Layers[o]; l == nil || !l.Killed {
			return o
		}
	}
	return int64(len(sbCfg.Bosses)) + 1
}

func (r *sbRole) score() int64 {
	var n int64
	for o, l := range r.Layers {
		b := sbCfg.Bosses[o-1]
		n += b.DPSScore * l.Best / 1000
		if l.Killed {
			n += b.KillScore
		}
	}
	return n
}

func (w *sbWorld) ranked() []int64 {
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

func (w *sbWorld) rankOf(rid int64) int64 {
	for i, id := range w.ranked() {
		if id == rid {
			return int64(i + 1)
		}
	}
	return 0
}

// sandybeachRanking is the rank list 12900 gives for the activity.
func sandybeachRanking(st *store.Store, srvID string, roles []store.RankRole) ([]rankEntry, error) {
	w := &sbWorld{}
	if _, err := st.LoadGlobal(srvID, sbModule, w); err != nil {
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

func (w *sbWorld) settle(st *store.Store, srv string) error {
	if w.Settled || w.Start == 0 {
		return nil
	}
	for i, rid := range w.ranked() {
		rank := int64(i + 1)
		for _, rk := range sbCfg.Ranks {
			if rank >= rk.Min && rank <= rk.Max {
				m := store.Mail{SrvID: srv, Subject: "沙滩Boss排名奖励", Content: "沙滩Boss活动已结束，你的积分排在第" + itoa64(rank) + "名，这是排名奖励。",
					FromName: "沙滩Boss", CreatedBy: "sandybeach", Attachments: attachmentsOf(rk.Items)}
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

type sbCtx struct {
	role  *store.Role
	w     *sbWorld
	r     *sbRole
	open  bool
	end   int64
	score int64
}

// RegisterSandybeach registers the sandybeach boss fight (an operator opens it with camp_id 25400): fifty bosses one
// after the other, each with the hp left from fight to fight; a fight uses one of the five daily tries (more can be bought for
// diamonds, five times); the score is the kill score of every boss killed and, for each boss, its damage score by the best
// damage of a single fight; the score opens rewards and makes the rank, which is mailed when the activity ends. The
// monsters' hp (500 per point of damage score) and the effect texts of the layers are not in the client's tables: the effects
// are not applied.
func RegisterSandybeach(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	with := func(s *gateway.Session, fn func(c *sbCtx) (bool, error)) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		now := s.Now()
		start, end, open := ActivityOpen(st, role.SrvID, sbCamp, now)
		c := &sbCtx{role: role, w: &sbWorld{}, r: &sbRole{}, open: open, end: end}
		if _, err := st.LoadGlobal(role.SrvID, sbModule, c.w); err != nil {
			return err
		}
		if _, err := st.LoadState(role.RID, sbModule, c.r); err != nil {
			return err
		}
		changed := false
		if !open {
			if !c.w.Settled && c.w.Start != 0 {
				if err := c.w.settle(st, role.SrvID); err != nil {
					return err
				}
				changed = true
			}
		} else if c.w.Start != start {
			if err := c.w.settle(st, role.SrvID); err != nil {
				return err
			}
			*c.w = sbWorld{Start: start}
			changed = true
		}
		if c.w.Score == nil {
			c.w.Score = map[int64]int64{}
		}
		if open && c.r.Start != start {
			*c.r = sbRole{Start: start}
		}
		if c.r.Layers == nil {
			c.r.Layers = map[int64]*sbLayer{}
		}
		if open {
			if day := dayKey(now); c.r.Day != day {
				c.r.Day, c.r.Count, c.r.Buys = day, sbCfg.Const["start_fight_count"], 0
			}
		}
		save, err := fn(c)
		if err != nil {
			return err
		}
		if save && open {
			c.w.Score[role.RID] = c.r.score()
			changed = true
		}
		if changed {
			if err := st.SaveGlobal(role.SrvID, sbModule, c.w); err != nil {
				return err
			}
		}
		if save {
			return st.SaveState(role.RID, sbModule, c.r)
		}
		return nil
	}
	taken := func(c *sbCtx) []proto.Message {
		out := []proto.Message{}
		for _, id := range c.r.Taken {
			out = append(out, proto.Message{"award_id": id})
		}
		return out
	}
	hpPer := func(c *sbCtx) int64 {
		order := c.r.order()
		if order > int64(len(sbCfg.Bosses)) || c.r.Left <= 0 {
			return sbHPScale
		}
		return c.r.Left * sbHPScale / sbMaxHP(order)
	}
	r.Handle(25400, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *sbCtx) (bool, error) {
			return false, s.Send(25400, proto.Message{"order": min(c.r.order(), int64(len(sbCfg.Bosses))), "score": c.r.score(), "rank": c.w.rankOf(c.role.RID), "count": c.r.Count})
		})
	})
	r.Handle(25401, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *sbCtx) (bool, error) {
			return false, s.Send(25401, proto.Message{"order": min(c.r.order(), int64(len(sbCfg.Bosses))), "score": c.r.score(), "rank": c.w.rankOf(c.role.RID), "count": c.r.Count,
				"buy_count": c.r.Buys, "hp_per": hpPer(c), "award_info": taken(c)})
		})
	})
	r.Handle(25402, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *sbCtx) (bool, error) {
			refuse := func(msg string) (bool, error) {
				return false, s.Send(25402, proto.Message{"code": 0, "msg": msg, "count": c.r.Count, "buy_count": c.r.Buys})
			}
			if !c.open {
				return refuse("活动未开启")
			}
			var price [][2]int64
			for _, b := range sbCfg.Buy {
				if c.r.Buys >= b.Min && c.r.Buys <= b.Max {
					price = b.Expend
				}
			}
			last := sbCfg.Buy[len(sbCfg.Buy)-1]
			if price == nil || c.r.Buys >= last.Max {
				return refuse("今天的购买次数已用完")
			}
			spendA, spendI := map[int64]int64{}, map[int64]int64{}
			for _, p := range price {
				splitGains([]sellGain{{p[0], p[1]}}, 1, spendA, spendI)
			}
			if err := st.Exchange(c.role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return refuse("钻石不足")
				}
				return false, err
			}
			c.r.Count++
			c.r.Buys++
			if err := s.Send(25402, proto.Message{"code": 1, "msg": "", "count": c.r.Count, "buy_count": c.r.Buys}); err != nil {
				return true, err
			}
			return true, sendAssets(s, st, c.role.RID)
		})
	})
	r.Handle(25403, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *sbCtx) (bool, error) {
			refuse := func(msg string) (bool, error) {
				return false, s.Send(25403, proto.Message{"code": 0, "msg": msg, "award_info": taken(c)})
			}
			if !c.open {
				return refuse("活动未开启")
			}
			// the request names no reward: everything the score has reached and was not taken yet
			score := c.r.score()
			ga, gi := map[int64]int64{}, map[int64]int64{}
			var take []int64
			for _, rw := range sbCfg.Rewards {
				if has(c.r.Taken, rw.ID) || score < rw.Num {
					continue
				}
				take = append(take, rw.ID)
				for _, it := range rw.Items {
					splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
				}
			}
			if len(take) == 0 {
				return refuse("没有可以领取的奖励")
			}
			if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return false, err
			}
			c.r.Taken = append(c.r.Taken, take...)
			if err := s.Send(25403, proto.Message{"code": 1, "msg": "", "award_info": taken(c)}); err != nil {
				return true, err
			}
			if err := sendAssets(s, st, c.role.RID); err != nil {
				return true, err
			}
			return true, sendBackpack(s, st)
		})
	})
	r.Handle(25404, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(c *sbCtx) (bool, error) {
			refuse := func(msg string) (bool, error) { return false, s.Send(25404, proto.Message{"code": 0, "msg": msg}) }
			order := c.r.order()
			switch {
			case !c.open:
				return refuse("活动未开启")
			case order > int64(len(sbCfg.Bosses)):
				return refuse("已通关全部关卡")
			case c.r.Count <= 0:
				return refuse("挑战次数不足")
			}
			boss := sbCfg.Bosses[order-1]
			def, ok := sbCfg.Units[strconv.FormatInt(boss.Unit, 10)]
			if !ok {
				return refuse("关卡数据缺失")
			}
			if _, err := st.GrantStarterPartners(c.role.RID, starterSeeds(c.role.Sex)); err != nil {
				return false, err
			}
			form, err := formationOf(st, c.role.RID, sbForm)
			if err != nil {
				return false, err
			}
			formType := max(1, form.FormationType)
			team, objects, err := fights.teamFor(c.role, formType, form.Slots)
			if err != nil {
				return false, err
			}
			if len(team) == 0 {
				return refuse("没有可以出战的英雄")
			}
			maxHP := sbMaxHP(order)
			hp := maxHP
			if c.r.Left > 0 {
				hp = c.r.Left
			}
			unit, obj := monsterOf(def, 0)
			unit.hp, unit.maxHP = hp, maxHP
			obj["hp"], obj["hp_max"], obj["object_name"] = hp, maxHP, boss.Name
			rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
			damage := max(0, min(hp, hp-sim.units[len(team)].hp))
			killed := damage >= hp
			before := c.r.score()
			layer := c.r.Layers[order]
			if layer == nil {
				layer = &sbLayer{}
				c.r.Layers[order] = layer
			}
			layer.Best = max(layer.Best, damage*1000/maxHP)
			if killed {
				layer.Killed, c.r.Left = true, 0
			} else {
				c.r.Left = hp - damage
			}
			c.r.Count--
			c.r.Fights++
			c.r.Dmg += damage
			gained := c.r.score() - before
			killScore := int64(0)
			if killed {
				killScore = boss.KillScore
			}
			dpsScore := gained - killScore
			hurts, best, err := (&tbSvc{st: st}).hurts(c.role, form.Slots, damage)
			if err != nil {
				return false, err
			}
			if err := s.Send(25404, proto.Message{"code": 1, "msg": ""}); err != nil {
				return true, err
			}
			result := int64(2)
			if killed {
				result = 1
			}
			end := proto.Message{"result": result, "dps_score": dpsScore, "kill_score": killScore, "all_dps": damage, "best_partner": best,
				"target_role_name": boss.Name, "hurt_statistics": hurts}
			return true, fights.begin(s, c.role, combatSB, tbIntro(c.role, combatSB, formType, objects, obj, len(team), boss.Name), rounds, killed, func(s *gateway.Session, _ bool) error {
				return s.Send(25405, end)
			})
		})
	})
}
