package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// element.json: the element temple of the client (tools/extract_element.py).
//
//go:embed data/element.json
var elementJSON []byte

const (
	elementModule = "element"
	combatElement = 25 // BattleConst.Fight_Type.ElementWar
)

type elementCustoms struct {
	Group, ID, Power int64
	Limit            [][]int64
	First, Auto      [][]int64
	Monsters         []towerMonster
}

type elementBase struct {
	Type    int64
	Name    string
	OpenDay []int64 `json:"open_day"`
	Groups  []int64
}

type elementConf struct {
	Const   map[string]int64
	Bases   []elementBase
	Customs []elementCustoms
	Buy     []struct{ Cost, Num int64 }
	Boss    map[string]struct {
		Name string
		Head int64
	}
	byKey map[[2]int64]elementCustoms
}

var elementCfg = func() *elementConf {
	c := &elementConf{}
	if err := json.Unmarshal(elementJSON, c); err != nil || len(c.Customs) != 240 || len(c.Bases) != 5 || c.Const["refresh_time"] == 0 {
		panic("game: bad element.json")
	}
	c.byKey = map[[2]int64]elementCustoms{}
	for _, x := range c.Customs {
		c.byKey[[2]int64{x.Group, x.ID}] = x
	}
	return c
}()

const elementCustomsPerGroup = 12

type elementState struct {
	Count   int64           // challenges kept
	CountAt int64           // when the count was last brought up to date (unix)
	BuyDay  int64           // the day the buys below belong to
	Bought  int64           // challenges bought that day
	Cleared map[int64]int64 // group -> the highest customs cleared
}

// settle brings the challenges up to the time: one comes every refresh_time seconds up to the limit.
func (d *elementState) settle(now int64) {
	max := elementCfg.Const["refresh_number"]
	if d.CountAt == 0 {
		d.Count, d.CountAt = max, now
	}
	if d.Count >= max {
		d.CountAt = now
		return
	}
	step := elementCfg.Const["refresh_time"]
	if n := (now - d.CountAt) / step; n > 0 {
		d.Count = min(max, d.Count+n)
		d.CountAt += n * step
		if d.Count >= max {
			d.CountAt = now
		}
	}
	if d.Cleared == nil {
		d.Cleared = map[int64]int64{}
	}
}

func (d *elementState) day(today int64) {
	if d.BuyDay != today {
		d.BuyDay, d.Bought = today, 0
	}
}

// weekday is 1 for Monday to 7 for Sunday in China time.
func weekday(t time.Time) int64 {
	w := int64(t.In(dayZone).Weekday())
	if w == 0 {
		return 7
	}
	return w
}

// groupOf is the boss group a base shows in a week: they take turns (our rule, the original's is not known).
func (b elementBase) groupOf(now time.Time) int64 {
	return b.Groups[(dayNo(now)/7)%int64(len(b.Groups))]
}

func (b elementBase) openOn(now time.Time) bool { return has(b.OpenDay, weekday(now)) }

func elementBaseOf(typ int64) *elementBase {
	for i := range elementCfg.Bases {
		if elementCfg.Bases[i].Type == typ {
			return &elementCfg.Bases[i]
		}
	}
	return nil
}

// nextBoss is the customs a group offers next: the one after the highest cleared, the last one when all are.
func (d *elementState) nextBoss(group int64) int64 {
	return min(d.Cleared[group]+1, elementCustomsPerGroup)
}

// RegisterElement registers the element temple (25000~25005): five bases that open on some weekdays, each showing the
// 12 customs of one of its boss groups; a customs is fought with heroes of the camp it asks for, wins the first-time
// prize once and the sweep prize after that (a sweep needs a cleared customs); every fight or sweep takes one of the 10
// challenges, which come back one in 3 hours and can be bought. Level 90 opens it. Our rules, not captured: which group a
// base shows, the cost of a bought challenge (the client's buy table by the number bought), no VIP effects, no camp-skill
// effects of the customs and no rank rewards.
func RegisterElement(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	load := func(s *gateway.Session) (*store.Role, *elementState, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		d := &elementState{}
		if _, err := st.LoadState(role.RID, elementModule, d); err != nil {
			return nil, nil, err
		}
		d.settle(s.Now().Unix())
		d.day(dayNo(s.Now()))
		return role, d, nil
	}
	save := func(role *store.Role, d *elementState) error { return st.SaveState(role.RID, elementModule, d) }
	counts := func(d *elementState, now time.Time) proto.Message {
		next := int64(0)
		if d.Count < elementCfg.Const["refresh_number"] {
			next = d.CountAt + elementCfg.Const["refresh_time"]
		}
		return proto.Message{"num": d.Count, "buy_num": d.Bought, "pr_buy_num": 0, "refresh_time": next}
	}
	info := func(d *elementState, now time.Time) proto.Message {
		list := []proto.Message{}
		end := dayStart(now.In(dayZone)).AddDate(0, 0, 1).Unix()
		for _, b := range elementCfg.Bases {
			if b.openOn(now) {
				g := b.groupOf(now)
				list = append(list, proto.Message{"type": b.Type, "group": g, "boss_id": d.nextBoss(g), "endtime": end})
			}
		}
		m := counts(d, now)
		m["day"], m["list"] = weekday(now), list
		return m
	}
	r.Handle(25000, func(s *gateway.Session, _ proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(25000, info(d, s.Now()))
	})
	r.Handle(25006, func(s *gateway.Session, _ proto.Message) error { return s.Send(25006, proto.Message{}) })
	r.Handle(25003, func(s *gateway.Session, _ proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(25003, proto.Message{"code": 0, "msg": msg}) }
		buy := elementCfg.Buy
		cost := buy[min(int(d.Bought), len(buy)-1)].Cost
		if d.Count >= elementCfg.Const["refresh_number"] {
			return refuse("挑战次数已满")
		}
		if err := st.Exchange(role.RID, map[int64]int64{3: cost}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("钻石不足")
			}
			return err
		}
		d.Count++
		d.Bought++
		if err := save(role, d); err != nil {
			return err
		}
		if err := s.Send(25003, proto.Message{"code": 1, "msg": "购买成功"}); err != nil {
			return err
		}
		if err := s.Send(25004, counts(d, s.Now())); err != nil {
			return err
		}
		return sendAssets(s, st, role.RID)
	})
	// check says why a customs of a base cannot be taken now.
	check := func(role *store.Role, d *elementState, req proto.Message, now time.Time, sweep bool) (elementCustoms, string) {
		b := elementBaseOf(req.Int("type"))
		switch {
		case int64(role.Lev) < elementCfg.Const["join_lev"]:
			return elementCustoms{}, "等级不足"
		case b == nil || !b.openOn(now):
			return elementCustoms{}, "这个基地今天没有开放"
		}
		g := b.groupOf(now)
		c, ok := elementCfg.byKey[[2]int64{g, req.Int("boss_id")}]
		switch {
		case !ok:
			return c, "关卡不存在"
		case c.ID > d.Cleared[g]+1:
			return c, "请先通关前面的关卡"
		case sweep && c.ID > d.Cleared[g]:
			return c, "通关后才能扫荡"
		case d.Count <= 0:
			return c, "挑战次数不足"
		}
		return c, ""
	}
	reward := func(s *gateway.Session, role *store.Role, items [][]int64) error {
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(pairsOf(items)), 1, ga, gi)
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}
	finish := func(s *gateway.Session, role *store.Role, d *elementState, now time.Time) error {
		if err := s.Send(25004, counts(d, now)); err != nil {
			return err
		}
		return s.Send(25000, info(d, now))
	}
	r.Handle(25002, func(s *gateway.Session, req proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		c, msg := check(role, d, req, s.Now(), true)
		if msg != "" {
			return s.Send(25002, proto.Message{"code": 0, "msg": msg})
		}
		d.Count--
		if err := save(role, d); err != nil {
			return err
		}
		if err := reward(s, role, c.Auto); err != nil {
			return err
		}
		if err := s.Send(25002, proto.Message{"code": 1, "msg": "扫荡成功"}); err != nil {
			return err
		}
		return finish(s, role, d, s.Now())
	})
	r.Handle(25001, func(s *gateway.Session, req proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		refuse := func(m string) error { return s.Send(25001, proto.Message{"code": 0, "msg": m}) }
		c, msg := check(role, d, req, s.Now(), false)
		if msg != "" {
			return refuse(msg)
		}
		formType := req.Int("formation_type")
		var slots []store.FormationSlot
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			if id := p.Int("id"); id != 0 && !seen[id] && p.Int("pos") >= 1 {
				seen[id] = true
				slots = append(slots, store.FormationSlot{Pos: p.Int("pos"), PartnerID: id})
			}
		}
		var team []fightUnit
		var objects []proto.Message
		if len(slots) > 0 {
			formType = max(formType, 1)
			team, objects, err = fights.teamFor(role, formType, slots)
		} else {
			var form store.Formation
			team, objects, form, err = fights.teamSide(role)
			formType, slots = form.FormationType, form.Slots
		}
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		owned, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		campOf := map[int64]int{}
		for _, p := range owned {
			campOf[p.PartnerID] = partnerTable[p.BID].Camp
		}
		for _, lim := range c.Limit {
			if len(lim) < 2 {
				continue
			}
			n := int64(0)
			for _, sl := range slots {
				if int64(campOf[sl.PartnerID]) == lim[0] {
					n++
				}
			}
			if n < lim[1] {
				return refuse("阵容不满足关卡要求")
			}
		}
		var foes []fightUnit
		for i, m := range c.Monsters {
			unit, obj := monsterOf(m.def(), i)
			foes = append(foes, unit)
			objects = append(objects, obj)
		}
		d.Count--
		if err := save(role, d); err != nil {
			return err
		}
		rounds, won, _ := playFightOpts(team, foes, newRng(), maxFightRounds, true)
		name := elementCfg.Boss[itoa64(c.Group)].Name
		intro := proto.Message{
			"combat_type": combatElement, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": objects, "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1, "extra_args": []proto.Message{{"param": c.ID}}, "string_ext_args": []proto.Message{},
			"a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": name, "suppress": 0, "halo_list": []proto.Message{},
			"hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(25001, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return fights.begin(s, role, combatElement, intro, rounds, won, func(s *gateway.Session, won bool) error {
			role, d, err := load(s)
			if role == nil {
				return err
			}
			result := int64(2)
			var gained [][]int64
			if won {
				result = 1
				gained = c.Auto
				if c.ID > d.Cleared[c.Group] {
					d.Cleared[c.Group] = c.ID
					gained = c.First
				}
				if err := save(role, d); err != nil {
					return err
				}
				if err := reward(s, role, gained); err != nil {
					return err
				}
			}
			items := []proto.Message{}
			for _, g := range pairsOf(gained) {
				items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
			}
			if err := s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 0, "current_time": 0, "best_time": 0,
				"combat_type": combatElement, "target_role_name": name, "hurt_statistics": []proto.Message{}}); err != nil {
				return err
			}
			return finish(s, role, d, s.Now())
		})
	})
}
