package game

import (
	_ "embed"
	"encoding/json"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// trainingcamp.json: the training camp of the client (tools/extract_trainingcamp.py).
//
//go:embed data/trainingcamp.json
var trainingcampJSON []byte

const (
	campModule = "trainingcamp"
	combatCamp = 36 // BattleConst.Fight_Type.Training_Camp
	campBasic  = 1  // the type of the basic lessons; the advanced ones need all of them
)

type campMonster struct {
	Bid    int64  `json:"bid"`
	Name   string `json:"name"`
	Career int64  `json:"career"`
	Camp   int64  `json:"camp"`
	Lev    int64  `json:"lev"`
	Head   string `json:"head"`
	Star   int64  `json:"star"`
}

type campStage struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Type      int64         `json:"type"`
	Formation int64         `json:"formation"`
	Unlock    []int64       `json:"unlock"`
	Reward    [][]int64     `json:"reward"`
	Enemy     []campMonster `json:"enemy"`
	EnemyName string        `json:"enemy_name"`
	Partners  []struct {
		Unit int64       `json:"unit"`
		Hero campMonster `json:"hero"`
	} `json:"partners"`
	Required []struct {
		Unit int64       `json:"unit"`
		Pos  int64       `json:"pos"`
		Hero campMonster `json:"hero"`
	} `json:"required"`
}

type campConf struct {
	Const  map[string]int64 `json:"const"`
	Stages []campStage      `json:"stages"`
}

var campCfg = func() *campConf {
	c := &campConf{}
	if err := json.Unmarshal(trainingcampJSON, c); err != nil || len(c.Stages) != 8 || c.Const["training_camp_lev_limit"] == 0 {
		panic("game: bad trainingcamp.json")
	}
	return c
}()

func (c *campConf) stage(id int64) *campStage {
	for i := range c.Stages {
		if c.Stages[i].ID == id {
			return &c.Stages[i]
		}
	}
	return nil
}

// hero is the lent hero of a unit group of the stage (a lent one or a required one).
func (s *campStage) hero(unit int64) (campMonster, bool) {
	for _, p := range s.Partners {
		if p.Unit == unit {
			return p.Hero, true
		}
	}
	for _, p := range s.Required {
		if p.Unit == unit {
			return p.Hero, true
		}
	}
	return campMonster{}, false
}

// partner makes the hero of a monster row: the template and star it has, at the level it has.
func (m campMonster) partner(id int64) (store.Partner, bool) {
	bid, err := strconv.ParseInt(m.Head, 10, 64)
	if err != nil {
		return store.Partner{}, false
	}
	if _, ok := partnerTable[bid]; !ok {
		return store.Partner{}, false
	}
	return store.Partner{PartnerID: id, BID: bid, Lev: int(m.Lev), Star: int(m.Star)}, true
}

type campState struct {
	Done []int64 // lessons finished
	Seen []int64 // lessons opened once (the first time shows the tips)
	Form bool    // the formation lesson's hint has been used
}

func inList(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// basicDone: every basic lesson is finished (the flag of 27600).
func (st *campState) basicDone() bool {
	for _, s := range campCfg.Stages {
		if s.Type == campBasic && !inList(st.Done, s.ID) {
			return false
		}
	}
	return true
}

// open: the lessons that must be finished first are, and for an advanced one all the basic lessons.
func (st *campState) open(s *campStage) bool {
	for _, id := range s.Unlock {
		if !inList(st.Done, id) {
			return false
		}
	}
	return s.Type == campBasic || st.basicDone()
}

type campSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterTrainingcamp registers the training camp (27600-27603): eight lessons (seven basic, one advanced) of a fight
// with heroes lent by the camp against a fixed enemy, each to be won once for a prize, the later ones opening after the
// earlier; the advanced lesson opens once all the basic ones are won. The camp is open from level 20. Our rules, not
// captured (the lent heroes and the enemies are the client's unit tables, their stats ours).
func RegisterTrainingcamp(r *gateway.Router, db *store.Store) {
	c := &campSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(27600, c.info)
	r.Handle(27601, c.fight)
	r.Handle(27602, c.enter)
	r.Handle(27603, c.formation)
}

func (c *campSvc) with(s *gateway.Session, fn func(role *store.Role, st *campState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := c.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &campState{}
	if _, err := c.st.LoadState(role.RID, campModule, st); err != nil {
		return err
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return c.st.SaveState(role.RID, campModule, st)
}

func (c *campSvc) sendInfo(s *gateway.Session, st *campState) error {
	ids := []proto.Message{}
	for _, id := range st.Done {
		ids = append(ids, proto.Message{"id": id})
	}
	flag := int64(0)
	if st.basicDone() {
		flag = 1
	}
	return s.Send(27600, proto.Message{"ids": ids, "flag": flag})
}

func (c *campSvc) info(s *gateway.Session, _ proto.Message) error {
	return c.with(s, func(_ *store.Role, st *campState) error { return c.sendInfo(s, st) })
}

// enter (27602): a lesson's window was opened; is_first the first time, is_formation whether the formation hint has
// been used (the lesson of the formation shows a hint until then).
func (c *campSvc) enter(s *gateway.Session, req proto.Message) error {
	return c.with(s, func(role *store.Role, st *campState) error {
		stage := campCfg.stage(req.Int("id"))
		if stage == nil || !st.open(stage) || int64(role.Lev) < campCfg.Const["training_camp_lev_limit"] {
			return s.Send(27602, proto.Message{"flag": 0, "msg": "该训练尚未开启", "is_first": 0, "is_formation": 1})
		}
		first := int64(0)
		if !inList(st.Seen, stage.ID) {
			st.Seen = append(st.Seen, stage.ID)
			first = 1
		}
		form := int64(0)
		if st.Form {
			form = 1
		}
		return s.Send(27602, proto.Message{"flag": 1, "msg": "", "is_first": first, "is_formation": form})
	})
}

func (c *campSvc) formation(s *gateway.Session, _ proto.Message) error {
	return c.with(s, func(_ *store.Role, st *campState) error {
		st.Form = true
		return nil
	})
}

// fight (27601): the lent heroes sent play the lesson's enemy.
func (c *campSvc) fight(s *gateway.Session, req proto.Message) error {
	return c.with(s, func(role *store.Role, st *campState) error {
		refuse := func(msg string) error { return s.Send(27601, proto.Message{"flag": 0, "msg": msg}) }
		stage := campCfg.stage(req.Int("id"))
		switch {
		case int64(role.Lev) < campCfg.Const["training_camp_lev_limit"]:
			return refuse("等级不足")
		case stage == nil:
			return refuse("训练不存在")
		case !st.open(stage):
			return refuse("请先完成前面的训练")
		}
		byID := map[int64]store.Partner{}
		var slots []store.FormationSlot
		for _, q := range req.Arr("pos_info") {
			unit := q.Int("id")
			hero, ok := stage.hero(unit)
			if !ok || byID[unit].PartnerID != 0 || q.Int("pos") < 1 || len(slots) >= formMaxHeroes {
				continue
			}
			if p, ok := hero.partner(unit); ok {
				byID[unit] = p
				slots = append(slots, store.FormationSlot{Pos: q.Int("pos"), PartnerID: unit})
			}
		}
		if len(slots) == 0 {
			return refuse("请选择关卡英雄")
		}
		for _, r := range stage.Required {
			if _, ok := byID[r.Unit]; !ok {
				return refuse("必须上阵指定的英雄")
			}
		}
		formType := max(1, stage.Formation)
		if stage.Formation == 0 {
			formType = max(1, req.Int("formation_type"))
		}
		team, objects := sideUnits(role.RID, role.SrvID, slots, formType, byID, true)
		foeByID := map[int64]store.Partner{}
		var foeSlots []store.FormationSlot
		for i, m := range stage.Enemy {
			if p, ok := m.partner(m.Bid); ok {
				foeByID[m.Bid] = p
				foeSlots = append(foeSlots, store.FormationSlot{Pos: int64(i + 1), PartnerID: m.Bid})
			}
		}
		foes, foeObjects := sideUnits(0, "", foeSlots, 1, foeByID, false)
		if len(team) == 0 || len(foes) == 0 {
			return refuse("没有可以出战的英雄")
		}
		rounds, won, _ := playFightOpts(team, foes, newRng(), maxFightRounds, true)
		intro := proto.Message{
			"combat_type": combatCamp, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{{"param": stage.ID}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": stage.EnemyName,
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(27601, proto.Message{"flag": 1, "msg": ""}); err != nil {
			return err
		}
		return c.fights.begin(s, role, combatCamp, intro, rounds, won, func(s *gateway.Session, won bool) error {
			return c.settle(s, stage.ID, won)
		})
	})
}

func (c *campSvc) settle(s *gateway.Session, id int64, won bool) error {
	return c.with(s, func(role *store.Role, st *campState) error {
		stage := campCfg.stage(id)
		prize := [][2]int64{}
		if won && !inList(st.Done, id) {
			st.Done = append(st.Done, id)
			prize = ptPairs(stage.Reward)
		}
		if len(prize) > 0 {
			ga, gi := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(prize), 1, ga, gi)
			if err := c.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return err
			}
			if err := sendAssets(s, c.st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, c.st); err != nil {
				return err
			}
		}
		result := int64(2)
		if won {
			result = 1
		}
		items := []proto.Message{}
		for _, g := range prize {
			items = append(items, proto.Message{"bid": g[0], "is_bind": 1, "num": g[1], "id": 0})
		}
		if err := s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0, "combat_type": combatCamp,
			"target_role_name": stage.EnemyName, "hurt_statistics": []proto.Message{}, "adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}}); err != nil {
			return err
		}
		if won {
			return c.sendInfo(s, st)
		}
		return nil
	})
}
