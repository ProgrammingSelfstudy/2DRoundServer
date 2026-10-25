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

// guild_skill.json: the guild skill rules of the client (tools/extract_guild_skill.py).
//
//go:embed data/guild_skill.json
var guildSkillJSON []byte

type gskillRow struct {
	ID       int64
	Group    int64
	Seq      int
	GuildLev int64 `json:"guild_lev"`
	Lev      int
	Loss     [][2]int64
}

type gattrRow struct {
	ID      int
	Lev     int
	NeedLev int `json:"need_lev"`
	Loss    [][2]int64
	Back    [][2]int64
}

type gpvpSkillRow struct {
	SkillID int64 `json:"skill_id"`
	Career  int
	Lev     int
	NeedLev int `json:"need_lev"`
	Loss    [][2]int64
	Back    [][2]int64
}

type gskillConf struct {
	Const       map[string]int64
	ReturnRatio [][2]int64 `json:"return_ratio"`
	Skills      []gskillRow
	PvpAttrs    []gattrRow     `json:"pvp_attrs"`
	PvpAttrMax  map[string]int `json:"pvp_attr_max"`
	PvpSkills   []gpvpSkillRow `json:"pvp_skills"`

	byID    map[int64]gskillRow
	byGroup map[int64][]gskillRow
	attr    map[[2]int]gattrRow
	pvp     map[[2]int]gpvpSkillRow
}

var gskillCfg = func() *gskillConf {
	c := &gskillConf{}
	if err := json.Unmarshal(guildSkillJSON, &c); err != nil || len(c.Skills) == 0 || len(c.PvpAttrs) == 0 || len(c.PvpSkills) == 0 || len(c.Const) == 0 {
		panic("game: bad guild_skill.json")
	}
	c.byID, c.byGroup = map[int64]gskillRow{}, map[int64][]gskillRow{}
	for _, s := range c.Skills {
		c.byID[s.ID] = s
		c.byGroup[s.Group] = append(c.byGroup[s.Group], s)
	}
	for _, l := range c.byGroup {
		sort.Slice(l, func(i, j int) bool { return l[i].Seq < l[j].Seq })
	}
	c.attr, c.pvp = map[[2]int]gattrRow{}, map[[2]int]gpvpSkillRow{}
	for _, a := range c.PvpAttrs {
		c.attr[[2]int{a.ID, a.Lev}] = a
	}
	for _, p := range c.PvpSkills {
		c.pvp[[2]int{p.Career, p.Lev}] = p
	}
	return c
}()

// The careers that have guild skills (GuildskillConst.index): the heavens of each have their own id range
// and so have the four PvP attributes.
var gskillCareers = []int{2, 3, 4, 5}

const (
	gskillHeavens   = 40 // groups per career
	gskillPerHeaven = 6
)

func gskillBase(career int) int64 {
	switch career {
	case 3:
		return 1000
	case 2:
		return 2000
	case 4:
		return 3000
	case 5:
		return 4000
	}
	return 0
}

// gskillAttrIDs are the four PvP attributes of a career, in the order the client lists them.
func gskillAttrIDs(career int) []int {
	prefix := map[int]int{3: 1, 2: 2, 4: 3, 5: 4}[career]
	if prefix == 0 {
		return nil
	}
	return []int{prefix*10000 + 1, prefix*10000 + 2, prefix*10000 + 3, prefix*10000 + 4}
}

// gskillErr is a refusal with the text the client shows.
type gskillErr string

func (e gskillErr) Error() string { return string(e) }

func gskillCurrentGroup(career, done int) int64 { return gskillBase(career) + int64(done) + 1 }

func gskillPveLevel(st *store.GuildSkills, career int) int {
	return st.PveDone[career]*gskillPerHeaven + len(st.PveActive[career])
}

func gskillPveComplete(st *store.GuildSkills, career int) bool {
	return st.PveDone[career] >= gskillHeavens-1 && len(st.PveActive[career]) >= gskillPerHeaven
}

func addCost(into map[int64]int64, list [][2]int64) {
	for _, e := range list {
		into[guildLabel(e[0])] += e[1]
	}
}

// gskillResetCost is the diamonds a reset costs: a fixed sum the first time, then the larger of a base
// and a price per level.
func gskillResetCost(first bool, levels int, pvp bool) int64 {
	pre := ""
	if pvp {
		pre = "pvp_"
	}
	if first {
		return gskillCfg.Const[pre+"first_reset_consume"]
	}
	cost := gskillCfg.Const[pre+"reset_consume_var2"] * int64(levels)
	if base := gskillCfg.Const[pre+"reset_consume_var1"]; cost < base {
		cost = base
	}
	return cost
}

func (g *GuildService) cleanArr(m map[int64]int64) [][2]int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([][2]int64, 0, len(ids))
	for _, id := range ids {
		if m[id] > 0 {
			out = append(out, [2]int64{id, m[id]})
		}
	}
	return out
}

// pveRefund is what resetting a career gives back: the ratio of every cost paid so far, per currency.
func pveRefund(st *store.GuildSkills, career int) map[int64]int64 {
	paid := map[int64]int64{}
	base := gskillBase(career)
	for d := 0; d < st.PveDone[career]; d++ {
		for _, sk := range gskillCfg.byGroup[base+int64(d)+1] {
			addCost(paid, sk.Loss)
		}
	}
	for _, id := range st.PveActive[career] {
		addCost(paid, gskillCfg.byID[id].Loss)
	}
	out := map[int64]int64{}
	for _, r := range gskillCfg.ReturnRatio {
		if n := paid[guildLabel(r[0])] * r[1] / 1000; n > 0 {
			out[guildLabel(r[0])] = n
		}
	}
	return out
}

func pvpRefund(st *store.GuildSkills, career int) map[int64]int64 {
	back := map[int64]int64{}
	for id, lev := range st.PvpAttrs[career] {
		addCost(back, gskillCfg.attr[[2]int{id, lev}].Back)
	}
	addCost(back, gskillCfg.pvp[[2]int{career, st.PvpLev[career]}].Back)
	return back
}

func pvpLevels(st *store.GuildSkills, career int) int {
	n := 0
	for _, lev := range st.PvpAttrs[career] {
		n += lev
	}
	return n
}

func pveInfoMessage(st *store.GuildSkills, career int) proto.Message {
	active := []proto.Message{}
	for _, id := range st.PveActive[career] {
		active = append(active, proto.Message{"skill_id": id})
	}
	done := []proto.Message{}
	for d := 0; d < st.PveDone[career]; d++ {
		done = append(done, proto.Message{"group_id": gskillBase(career) + int64(d) + 1})
	}
	return proto.Message{"career": career, "group_id": gskillCurrentGroup(career, st.PveDone[career]), "skill_ids": active, "group_ids": done}
}

func pvpInfoMessage(st *store.GuildSkills, career int) proto.Message {
	attrs := []proto.Message{}
	if have, ok := st.PvpAttrs[career]; ok {
		for _, id := range gskillAttrIDs(career) {
			attrs = append(attrs, proto.Message{"id": id, "lev": have[id]})
		}
	}
	lev := st.PvpLev[career]
	return proto.Message{"attr_formation": attrs, "skill_id": gskillCfg.pvp[[2]int{career, lev}].SkillID, "skill_lev": lev, "career": career}
}

func (g *GuildService) registerSkills(r *gateway.Router) {
	valid := func(career int64) bool { return career >= 2 && career <= 5 }
	pay := func(s *gateway.Session) error { return sendAssets(s, g.Store, s.Role().RID) }
	// refuse sends the client's message box text for a failed action.
	refuse := func(s *gateway.Session, cmd uint16, career int, key, msg string) error {
		return s.Send(cmd, proto.Message{key: 0, "msg": msg, "career": career})
	}
	// fail turns what a change returned into the refusal the client shows.
	fail := func(s *gateway.Session, cmd uint16, career int, key string, err error) (bool, error) {
		var ge gskillErr
		switch {
		case err == nil:
			return false, nil
		case errors.As(err, &ge):
			return true, refuse(s, cmd, career, key, string(ge))
		case errors.Is(err, store.ErrConflict):
			return true, refuse(s, cmd, career, key, "材料不足")
		}
		return true, err
	}
	// sendBag updates the items the role holds, for the PvP materials.
	bag := func(s *gateway.Session) error { return sendBackpack(s, g.Store) }
	pvpPush := func(s *gateway.Session, st *store.GuildSkills, career int) error {
		m := pvpInfoMessage(st, career)
		m["is_first"] = b2i(!st.PvpReset)
		return s.Send(23710, m)
	}

	// 23700 the heavens of a career
	r.Handle(23700, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil || !valid(req.Int("career")) {
			return nil
		}
		st, err := g.Store.GuildSkillsOf(s.Role().RID)
		if err != nil {
			return err
		}
		return s.Send(23700, pveInfoMessage(st, int(req.Int("career"))))
	})
	// 23703 the next skill to activate in each career (the red dots)
	r.Handle(23703, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		st, err := g.Store.GuildSkillsOf(s.Role().RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, c := range gskillCareers {
			list := gskillCfg.byGroup[gskillCurrentGroup(c, st.PveDone[c])]
			if n := len(st.PveActive[c]); n < len(list) {
				out = append(out, proto.Message{"career": c, "skill_id": list[n].ID})
			}
		}
		return s.Send(23703, proto.Message{"outline": out})
	})
	// 23701 activate the next skill of the current heaven; the last one opens the next heaven (23702)
	r.Handle(23701, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		sk, ok := gskillCfg.byID[req.Int("skill_id")]
		career := 0
		for _, c := range gskillCareers {
			if sk.Group/1000*1000 == gskillBase(c) {
				career = c
			}
		}
		if !ok || career == 0 {
			return refuse(s, 23701, 0, "code", "参数错误")
		}
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return refuse(s, 23701, career, "code", "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		var next int64
		err = g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			done := st.PveDone[career]
			active := st.PveActive[career]
			switch {
			case sk.Group != gskillCurrentGroup(career, done):
				return nil, nil, nil, nil, gskillErr("请先激活当前重天的技能")
			case sk.Seq != len(active)+1:
				return nil, nil, nil, nil, gskillErr("请按顺序激活技能")
			case gd.Lev < sk.GuildLev:
				return nil, nil, nil, nil, gskillErr("公会等级不足")
			}
			active = append(append([]int64{}, active...), sk.ID)
			if len(active) == gskillPerHeaven && done < gskillHeavens-1 {
				done++
				active = nil
				next = gskillCurrentGroup(career, done)
			}
			st.PveDone[career], st.PveActive[career] = done, active
			a, i = g.payFor(sk.Loss)
			return a, i, nil, nil, nil
		})
		if failed, err := fail(s, 23701, career, "code", err); failed {
			return err
		}
		if err := pay(s); err != nil {
			return err
		}
		if err := bag(s); err != nil {
			return err
		}
		if err := s.Send(23701, proto.Message{"code": 1, "msg": "激活成功", "career": career, "skill_id": sk.ID}); err != nil {
			return err
		}
		if next != 0 {
			return s.Send(23702, proto.Message{"career": career, "group_id": next})
		}
		return nil
	})
	// 23704 what resetting a career costs and gives back
	r.Handle(23704, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil || !valid(req.Int("career")) {
			return nil
		}
		career := int(req.Int("career"))
		st, err := g.Store.GuildSkillsOf(s.Role().RID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, e := range g.cleanArr(pveRefund(st, career)) {
			list = append(list, proto.Message{"id": e[0], "num": e[1]})
		}
		return s.Send(23704, proto.Message{"is_first": b2i(!st.PveReset), "gold": gskillResetCost(!st.PveReset, gskillPveLevel(st, career), false), "list": list})
	})
	// 23705 reset the ordinary skills of a career
	r.Handle(23705, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !valid(req.Int("career")) {
			return nil
		}
		career := int(req.Int("career"))
		err := g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			levels := gskillPveLevel(st, career)
			if levels == 0 {
				return nil, nil, nil, nil, gskillErr("没有可重置的技能")
			}
			back := pveRefund(st, career)
			a, i = g.payFor([][2]int64{{3, gskillResetCost(!st.PveReset, levels, false)}})
			st.PveReset = true
			delete(st.PveDone, career)
			delete(st.PveActive, career)
			ga, gi = back, map[int64]int64{}
			return a, i, ga, gi, nil
		})
		if failed, err := fail(s, 23705, career, "code", err); failed {
			return err
		}
		if err := pay(s); err != nil {
			return err
		}
		return s.Send(23705, proto.Message{"code": 1, "msg": "重置成功", "career": career})
	})
	// 23706 break through: a career with every heaven done gets PvP attributes and a PvP skill
	r.Handle(23706, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !valid(req.Int("career")) {
			return nil
		}
		career := int(req.Int("career"))
		var st2 *store.GuildSkills
		err := g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			if _, open := st.PvpAttrs[career]; open {
				return nil, nil, nil, nil, gskillErr("已经突破")
			}
			if !gskillPveComplete(st, career) {
				return nil, nil, nil, nil, gskillErr("本系技能未达最高重天")
			}
			attrs := map[int]int{}
			for _, id := range gskillAttrIDs(career) {
				attrs[id] = 0
			}
			st.PvpAttrs[career], st.PvpLev[career] = attrs, 0
			st2 = st
			return nil, nil, nil, nil, nil
		})
		if failed, err := fail(s, 23706, career, "flag", err); failed {
			return err
		}
		if err := pvpPush(s, st2, career); err != nil {
			return err
		}
		return s.Send(23706, proto.Message{"flag": 1, "msg": "突破成功", "career": career})
	})
	// 23707 raise one PvP attribute by a level
	r.Handle(23707, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !valid(req.Int("career")) {
			return nil
		}
		career, id := int(req.Int("career")), int(req.Int("id"))
		var st2 *store.GuildSkills
		err := g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			attrs, open := st.PvpAttrs[career]
			if !open {
				return nil, nil, nil, nil, gskillErr("尚未突破")
			}
			lev, mine := attrs[id]
			row, ok := gskillCfg.attr[[2]int{id, lev}]
			if !mine || !ok || lev >= gskillCfg.PvpAttrMax[strconv.Itoa(id)] {
				return nil, nil, nil, nil, gskillErr("属性已达最大等级")
			}
			attrs[id] = lev + 1
			st2 = st
			a, i = g.payFor(row.Loss)
			return a, i, nil, nil, nil
		})
		if failed, err := fail(s, 23707, career, "flag", err); failed {
			return err
		}
		if err := pay(s); err != nil {
			return err
		}
		if err := bag(s); err != nil {
			return err
		}
		if err := pvpPush(s, st2, career); err != nil {
			return err
		}
		return s.Send(23707, proto.Message{"flag": 1, "msg": "升级成功", "career": career})
	})
	// 23708 raise the PvP skill of a career by a level; every attribute must have reached the level it asks
	r.Handle(23708, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !valid(req.Int("career")) {
			return nil
		}
		career := int(req.Int("career"))
		var st2 *store.GuildSkills
		err := g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			attrs, open := st.PvpAttrs[career]
			if !open {
				return nil, nil, nil, nil, gskillErr("尚未突破")
			}
			lev := st.PvpLev[career]
			row := gskillCfg.pvp[[2]int{career, lev}]
			if _, more := gskillCfg.pvp[[2]int{career, lev + 1}]; !more {
				return nil, nil, nil, nil, gskillErr("已经已达最大等级")
			}
			for _, v := range attrs {
				if v < row.NeedLev {
					return nil, nil, nil, nil, gskillErr("属性等级不足")
				}
			}
			st.PvpLev[career] = lev + 1
			st2 = st
			a, i = g.payFor(row.Loss)
			return a, i, nil, nil, nil
		})
		if failed, err := fail(s, 23708, career, "flag", err); failed {
			return err
		}
		if err := pay(s); err != nil {
			return err
		}
		if err := bag(s); err != nil {
			return err
		}
		if err := pvpPush(s, st2, career); err != nil {
			return err
		}
		return s.Send(23708, proto.Message{"flag": 1, "msg": "升级成功", "career": career})
	})
	// 23709 reset the PvP attributes and skill of a career; the materials come back in full
	r.Handle(23709, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !valid(req.Int("career")) {
			return nil
		}
		career := int(req.Int("career"))
		var st2 *store.GuildSkills
		err := g.Store.UpdateGuildSkills(role.RID, func(st *store.GuildSkills) (a, i, ga, gi map[int64]int64, err error) {
			levels := pvpLevels(st, career)
			if levels == 0 {
				return nil, nil, nil, nil, gskillErr("没有可重置的属性")
			}
			back := pvpRefund(st, career)
			a, i = g.payFor([][2]int64{{3, gskillResetCost(!st.PvpReset, levels, true)}})
			st.PvpReset = true
			attrs := map[int]int{}
			for _, id := range gskillAttrIDs(career) {
				attrs[id] = 0
			}
			st.PvpAttrs[career], st.PvpLev[career] = attrs, 0
			st2 = st
			ga, gi = map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(g.cleanArr(back)), 1, ga, gi)
			return a, i, ga, gi, nil
		})
		if failed, err := fail(s, 23709, career, "flag", err); failed {
			return err
		}
		if err := pay(s); err != nil {
			return err
		}
		if err := bag(s); err != nil {
			return err
		}
		if err := pvpPush(s, st2, career); err != nil {
			return err
		}
		return s.Send(23709, proto.Message{"flag": 1, "msg": "重置成功", "career": career})
	})
	// 23710 the PvP skills of one career
	r.Handle(23710, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil || !valid(req.Int("career")) {
			return nil
		}
		st, err := g.Store.GuildSkillsOf(s.Role().RID)
		if err != nil {
			return err
		}
		return pvpPush(s, st, int(req.Int("career")))
	})
	// 23711 the PvP skills of every career
	r.Handle(23711, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		st, err := g.Store.GuildSkillsOf(s.Role().RID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, c := range gskillCareers {
			list = append(list, pvpInfoMessage(st, c))
		}
		return s.Send(23711, proto.Message{"guild_skill_break_group": list, "is_first": b2i(!st.PvpReset)})
	})
}
