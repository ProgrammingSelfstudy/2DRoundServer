package game

import (
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	mineModule      = "adventure_mine"
	mineWorldModule = "adventure_mine_world"
	combatMine      = 30 // BattleConst.Fight_Type.AdventrueMine
)

// mineMu keeps one request at a time on the shared mines of the process.
var mineMu sync.Mutex

type mineDef struct{ Pos, PID int64 }

type mineSlot struct {
	Room, MineID, RID, Since, Prot, Plunder, Hallows, Pri, Sen int64
	SrvID                                                      string
	Def                                                        []mineDef
}

type mineWorld struct {
	Round  int64
	Floors map[string][]mineSlot
}

type mineLog struct {
	RID, Lev, Face, Power, Floor, Room, MineID, Ret, Time int64
	SrvID, Name                                           string
	Loss                                                  [][2]int64
}

type minePending struct{ Floor, Room, Until int64 }

type mineRole struct {
	Round, Used, Bought, Combats int64
	Taken                        map[int64]bool
	Pending                      minePending
	Logs                         []mineLog
	Unread                       bool
}

func mineConst(name string) int64 { return advCfg.Mine.Const[name].Val }

// mineFloors are the floors that have mines, in order.
func mineFloors() []int64 {
	var out []int64
	for _, f := range advCfg.Mine.FloorData {
		out = append(out, f.Floor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// mineRows are the mines of a floor, by the room (cell) they stand in.
func mineRows(floor int64) []advMineRow {
	var out []advMineRow
	for id, m := range advCfg.mines {
		if id/1000 == 100+floor/10 {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (w *mineWorld) floor(f int64) []mineSlot {
	key := strconv.FormatInt(f, 10)
	if s, ok := w.Floors[key]; ok {
		return s
	}
	var s []mineSlot
	for _, m := range mineRows(f) {
		if len(m.Pos) > 0 {
			s = append(s, mineSlot{Room: m.Pos[0], MineID: m.ID})
		}
	}
	w.Floors[key] = s
	return s
}

func (w *mineWorld) slot(f, room int64) *mineSlot {
	w.floor(f)
	list := w.Floors[strconv.FormatInt(f, 10)]
	for i := range list {
		if list[i].Room == room {
			return &list[i]
		}
	}
	return nil
}

// mineOutput is what a mine has made for its holder so far, and when it ends.
func mineOutput(sl mineSlot, now int64) (items [][2]int64, ends int64) {
	row := advCfg.mines[sl.MineID]
	minutes := row.MaxTime + sl.Pri*mineConst("primary_time") + sl.Sen*mineConst("senior_time")
	ends = sl.Since + minutes*60
	end := min(now, ends)
	per := max(1, mineConst("reward_time"))
	n := max(0, (end-sl.Since)/60/per)
	for _, h := range row.HookItems {
		if len(h) > 1 {
			items = append(items, [2]int64{h[0], h[1] * n})
		}
	}
	return
}

// mineRate is the plunder left after the decay for a mine far below the role's best floor.
func mineRate(passID, floor, amount int64) int64 {
	diff := max(0, (passID-floor)/10*10)
	if a, ok := advCfg.Mine.Attenuation[strconv.FormatInt(diff, 10)]; ok {
		return (amount*(1000-a.DecRate) + 500) / 1000
	}
	return amount
}

type mineCtx struct {
	a     *adv
	s     *gateway.Session
	role  *store.Role
	world *mineWorld
	me    *mineRole
	adv   *advState
}

// mineWith loads the world of the role's server and the role's own mine state, runs fn, and saves them.
func (a *adv) mineWith(s *gateway.Session, fn func(c *mineCtx) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	mineMu.Lock()
	defer mineMu.Unlock()
	c := &mineCtx{a: a, s: s, role: role, world: &mineWorld{}, me: &mineRole{}, adv: &advState{}}
	now := s.Now()
	round, _, _ := arenaSeason(now)
	if _, err := a.st.LoadGlobal(role.SrvID, mineWorldModule, c.world); err != nil {
		return err
	}
	if c.world.Floors == nil || c.world.Round != round {
		if err := a.mineReset(c.world, now); err != nil {
			return err
		}
		c.world = &mineWorld{Round: round, Floors: map[string][]mineSlot{}}
	}
	if _, err := a.st.LoadState(role.RID, mineModule, c.me); err != nil {
		return err
	}
	if c.me.Round != round {
		*c.me = mineRole{Round: round}
	}
	if c.me.Taken == nil {
		c.me.Taken = map[int64]bool{}
	}
	if _, err := a.st.LoadState(role.RID, advModule, c.adv); err != nil {
		return err
	}
	if err := a.mineExpire(c.world, now); err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	if err := a.st.SaveGlobal(role.SrvID, mineWorldModule, c.world); err != nil {
		return err
	}
	return a.st.SaveState(role.RID, mineModule, c.me)
}

// mineReset pays every holder what the old round made. (The original mails it; here it goes straight to the
// holder's ledger.)
func (a *adv) mineReset(w *mineWorld, now time.Time) error {
	for _, list := range w.Floors {
		for i := range list {
			if err := a.minePayout(list[i], now.Unix()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *adv) minePayout(sl mineSlot, now int64) error {
	if sl.RID == 0 {
		return nil
	}
	items, _ := mineOutput(sl, now)
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(items), 1, ga, gi)
	if len(ga)+len(gi) == 0 {
		return nil
	}
	return a.st.Exchange(sl.RID, map[int64]int64{}, map[int64]int64{}, ga, gi)
}

// mineExpire hands the mines whose time is up back to the guards, paying their holders.
func (a *adv) mineExpire(w *mineWorld, now time.Time) error {
	for _, list := range w.Floors {
		for i := range list {
			if list[i].RID == 0 {
				continue
			}
			if _, ends := mineOutput(list[i], now.Unix()); now.Unix() >= ends {
				if err := a.minePayout(list[i], now.Unix()); err != nil {
					return err
				}
				list[i] = mineSlot{Room: list[i].Room, MineID: list[i].MineID}
			}
		}
	}
	return nil
}

func (c *mineCtx) attacksLeft() int64 {
	return max(0, mineConst("free_attack")+c.me.Bought-c.me.Used)
}

func (c *mineCtx) held() int64 {
	var n int64
	for _, list := range c.world.Floors {
		for _, sl := range list {
			if sl.RID == c.role.RID {
				n++
			}
		}
	}
	return n
}

// holdLimit is how many mines the role may hold: one, a second from level 100 and a third from VIP 6
// (the client's mine_unlock_info).
func (c *mineCtx) holdLimit() int64 {
	n := int64(1)
	if c.role.Lev >= 100 {
		n++
	}
	if c.role.VIPLev >= 6 {
		n++
	}
	return n
}

func registerAdventureMine(r *gateway.Router, a *adv) {
	r.Handle(20640, a.mineList)
	r.Handle(20641, a.mineInfo)
	r.Handle(20642, a.mineDefense)
	r.Handle(20643, a.mineAttack)
	r.Handle(20644, a.mineLog)
	r.Handle(20645, func(s *gateway.Session, _ proto.Message) error { return s.Send(20645, proto.Message{}) })
	r.Handle(20646, a.mineOccupy)
	r.Handle(20647, a.mineBoxes)
	r.Handle(20648, a.mineTakeBox)
	r.Handle(20651, a.mineGiveUp)
	r.Handle(20652, a.mineMine)
	r.Handle(20653, a.mineLayers)
	r.Handle(20654, a.mineEmploy)
	r.Handle(20655, a.mineBuyAttack)
	r.Handle(20656, a.mineStrikeBack)
	r.Handle(20657, a.mineRecordRed)
	r.Handle(20658, a.mineHallows)
	r.Handle(20659, a.mineCountRed)
	r.Handle(20660, a.mineGiveUpOccupy)
}

func (c *mineCtx) holderInfo(sl mineSlot) (name string, lev, face int64) {
	if sl.RID == 0 {
		row := advCfg.mines[sl.MineID]
		lev = 1
		if len(row.RobotLev) > 0 {
			lev = row.RobotLev[0][0]
		}
		return row.RobotName, lev, arenaRobotFaces[0]
	}
	h, err := c.a.st.RoleByID(sl.RID)
	if err != nil {
		return "未知领主", 1, arenaRobotFaces[0]
	}
	face = int64(h.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	return h.Name, int64(h.Lev), face
}

func (a *adv) mineList(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f := req.Int("floor")
		if _, ok := advCfg.Mine.FloorData[strconv.FormatInt(f, 10)]; !ok {
			return s.Send(20640, proto.Message{"floor": f, "count": 0, "occupy_count": 0, "list": []proto.Message{}, "buy_count": 0})
		}
		list := []proto.Message{}
		now := s.Now().Unix()
		for _, sl := range c.world.floor(f) {
			name, lev, face := c.holderInfo(sl)
			srv := ""
			if sl.RID != 0 {
				srv = sl.SrvID
			}
			list = append(list, proto.Message{"room_id": sl.Room, "rid": sl.RID, "srv_id": srv, "lev": lev, "name": name, "mine_id": sl.MineID, "face": face,
				"status": 2 * b2i(sl.Prot > now)})
		}
		return s.Send(20640, proto.Message{"floor": f, "count": c.attacksLeft(), "occupy_count": c.held(), "list": list, "buy_count": c.me.Bought})
	})
}

// mineTeam is who defends a mine: the hero places its holder set, or a copy of the attacker's own team for the
// guards (a little weaker for a low star, as the arena robots).
func (c *mineCtx) mineFoes(sl mineSlot) (arenaOpp, error) {
	row := advCfg.mines[sl.MineID]
	name, lev, face := c.holderInfo(sl)
	if sl.RID != 0 && len(sl.Def) > 0 {
		heroes, err := c.a.st.PartnersOf(sl.RID)
		if err != nil {
			return arenaOpp{}, err
		}
		byID := map[int64]store.Partner{}
		for _, h := range heroes {
			byID[h.PartnerID] = h
		}
		o := arenaOpp{RID: sl.RID, SrvID: sl.SrvID, Name: name, Lev: lev, Face: face, FormType: 1}
		var all []store.Partner
		for _, d := range sl.Def {
			if h, ok := byID[d.PID]; ok {
				o.Lineup = append(o.Lineup, arenaHero{d.Pos, h.BID, int64(h.Lev), int64(h.Star), int64(h.BreakLev)})
				all = append(all, h)
			}
		}
		o.Power = rolePower(all)
		if len(o.Lineup) > 0 {
			return o, nil
		}
	}
	mine, formType, _, err := lineupOf(c.a.st, c.role.RID)
	if err != nil {
		return arenaOpp{}, err
	}
	o := arenaOpp{RID: arenaRobotBase + sl.MineID, SrvID: c.role.SrvID, Robot: true, FormType: formType, Name: row.RobotName, Lev: lev, Face: face, Power: row.RobotPower}
	factor := 70 + 8*row.Star
	for _, h := range mine {
		o.Lineup = append(o.Lineup, arenaHero{h.Pos, h.BID, max(1, h.Lev*factor/100), h.Star, h.Break})
	}
	return o, nil
}

func (c *mineCtx) defenseMessages(o arenaOpp) []proto.Message {
	out := []proto.Message{}
	for i, h := range o.Lineup {
		out = append(out, proto.Message{"pos": h.Pos, "id": i + 1, "bid": h.BID, "lev": h.Lev, "star": h.Star, "use_skin": 0})
	}
	return out
}

func (a *adv) mineInfo(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f, room := req.Int("floor"), req.Int("room_id")
		sl := c.world.slot(f, room)
		if sl == nil {
			return nil
		}
		o, err := c.mineFoes(*sl)
		if err != nil {
			return err
		}
		row := advCfg.mines[sl.MineID]
		left := max(0, mineConst("rob_number")-sl.Plunder)
		shown := left
		if sl.RID == c.role.RID {
			shown = sl.Plunder
		}
		loot := [][2]int64{}
		for _, b := range pairsOf(row.BaseItems) {
			loot = append(loot, [2]int64{b[0], mineRate(c.adv.PassID, f, b[1])})
		}
		return s.Send(20641, proto.Message{"room_id": room, "mine_id": sl.MineID, "rid": sl.RID, "srv_id": sl.SrvID, "name": o.Name, "lev": o.Lev, "face": o.Face,
			"power": o.Power, "plunder_count": shown, "occupy_time": sl.Since, "defense": c.defenseMessages(o), "items": itemPairs(loot), "id": 0, "hallows_id": sl.Hallows})
	})
}

func itemPairs(list [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, e := range list {
		out = append(out, proto.Message{"item_id": e[0], "num": e[1]})
	}
	return out
}

// mineDefense (20642) says which heroes stand in the mines of the role.
func (a *adv) mineDefense(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		out := []proto.Message{}
		for _, list := range c.world.Floors {
			for _, sl := range list {
				if sl.RID == c.role.RID {
					for _, d := range sl.Def {
						out = append(out, proto.Message{"id": d.PID})
					}
				}
			}
		}
		return s.Send(20642, proto.Message{"defense": out})
	})
}

// mineAttack (20643) fights for a mine; the fight ends with 20649, and a win may be followed by 20646 (occupy)
// within the countdown.
func (a *adv) mineAttack(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f, room := req.Int("floor"), req.Int("room_id")
		refuse := func(msg string) error { return s.Send(20643, proto.Message{"code": 0, "msg": msg}) }
		sl := c.world.slot(f, room)
		switch {
		case sl == nil:
			return refuse("灵矿不存在")
		case c.adv.PassID < f:
			return refuse("通关对应层数后才能进入")
		case sl.RID == c.role.RID:
			return refuse("不能进攻自己的灵矿")
		case sl.Prot > s.Now().Unix():
			return refuse("灵矿保护中")
		case c.attacksLeft() <= 0:
			return refuse("挑战次数不足")
		}
		team, objects, form, err := a.fights.teamSide(c.role)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		opp, err := c.mineFoes(*sl)
		if err != nil {
			return err
		}
		oslots, oform, obyID, err := opp.fighters(a.st)
		if err != nil {
			return err
		}
		foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
		if len(foes) == 0 {
			return refuse("守卫已无法应战")
		}
		c.me.Used++
		c.me.Combats++
		intro := proto.Message{
			"combat_type": combatMine, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
			"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{{"param": f}, {"param": room}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": c.role.Name,
			"target_role_name": opp.Name, "suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		rounds, won := buildFight(team, foes, newRng())
		if err := s.Send(20643, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return a.fights.begin(s, c.role, combatMine, intro, rounds, won, func(s *gateway.Session, won bool) error {
			return a.mineSettle(s, f, room, opp, won)
		})
	})
}

func (a *adv) mineSettle(s *gateway.Session, f, room int64, opp arenaOpp, won bool) error {
	return a.mineWith(s, func(c *mineCtx) error {
		sl := c.world.slot(f, room)
		result := int64(2)
		var loot [][2]int64
		now := s.Now().Unix()
		if won && sl != nil {
			result = 1
			row := advCfg.mines[sl.MineID]
			if sl.RID == 0 || sl.Plunder < mineConst("rob_number") {
				for _, b := range pairsOf(row.BaseItems) {
					loot = append(loot, [2]int64{b[0], mineRate(c.adv.PassID, f, b[1])})
				}
				if sl.RID != 0 {
					sl.Plunder++
				}
			}
			c.me.Pending = minePending{Floor: f, Room: room, Until: now + mineConst("count_down")}
			if sl.RID != 0 {
				a.mineNote(sl.RID, mineLog{RID: c.role.RID, SrvID: c.role.SrvID, Name: c.role.Name, Lev: int64(c.role.Lev), Floor: f, Room: room, MineID: sl.MineID, Ret: 2, Time: now, Loss: loot})
			}
		} else if sl != nil && sl.RID != 0 {
			a.mineNote(sl.RID, mineLog{RID: c.role.RID, SrvID: c.role.SrvID, Name: c.role.Name, Lev: int64(c.role.Lev), Floor: f, Room: room, MineID: sl.MineID, Ret: 1, Time: now})
		}
		if len(loot) > 0 {
			if err := a.gain(s, c.role, loot); err != nil {
				return err
			}
		}
		p, _ := a.partners(c.role)
		stats := []proto.Message{}
		heroes, _ := formationOf(a.st, c.role.RID, formDrama)
		for _, sl := range heroes.Slots {
			if pp, ok := p[sl.PartnerID]; ok {
				stats = append(stats, proto.Message{"rid": c.role.RID, "srvid": c.role.SrvID, "id": pp.PartnerID, "bid": pp.BID, "star": pp.Star, "lev": pp.Lev,
					"camp_type": partnerTable[pp.BID].Camp, "dps": partnerMessage(pp).Int("power"), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
			}
		}
		return s.Send(20649, proto.Message{"result": result, "floor": f, "room_id": room, "count": c.attacksLeft(), "award": itemPairs(loot), "target_role_name": opp.Name,
			"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "end_time": c.me.Pending.Until})
	})
}

// mineNote writes a line of the defence log of another role. The other role's state is written on its own, so a
// request of ours does not hold two documents of the same role.
func (a *adv) mineNote(rid int64, l mineLog) {
	me := &mineRole{}
	if ok, err := a.st.LoadState(rid, mineModule, me); err != nil || !ok {
		me = &mineRole{Round: l.Time}
	}
	me.Logs = append(me.Logs, l)
	if len(me.Logs) > 50 {
		me.Logs = me.Logs[len(me.Logs)-50:]
	}
	me.Unread = true
	_ = a.st.SaveState(rid, mineModule, me)
}

func (a *adv) mineLog(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		list := []proto.Message{}
		for i := len(c.me.Logs) - 1; i >= 0; i-- {
			l := c.me.Logs[i]
			loss := []proto.Message{}
			for _, e := range l.Loss {
				loss = append(loss, proto.Message{"item_id": e[0], "num": e[1]})
			}
			list = append(list, proto.Message{"rid": l.RID, "srv_id": l.SrvID, "name": l.Name, "lev": l.Lev, "face": store.DefaultFaceID, "power": l.Power, "floor": l.Floor,
				"room_id": l.Room, "mine_id": l.MineID, "ret": l.Ret, "replay_id": 0, "time": l.Time, "loss": loss})
		}
		c.me.Unread = false
		return s.Send(20644, proto.Message{"log_list": list})
	})
}

// mineOccupy (20646) sends the heroes into the mine just won (within the countdown) or changes the heroes of a
// mine the role holds.
func (a *adv) mineOccupy(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f, room := req.Int("floor"), req.Int("room_id")
		refuse := func(msg string) error { return s.Send(20646, proto.Message{"flag": 0, "msg": msg}) }
		sl := c.world.slot(f, room)
		if sl == nil {
			return refuse("灵矿不存在")
		}
		heroes, err := a.partners(c.role)
		if err != nil {
			return err
		}
		var def []mineDef
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			id := p.Int("id")
			if _, ok := heroes[id]; ok && !seen[id] && len(def) < formMaxHeroes {
				seen[id] = true
				def = append(def, mineDef{Pos: p.Int("pos"), PID: id})
			}
		}
		if len(def) == 0 {
			return refuse("请选择驻守英雄")
		}
		now := s.Now().Unix()
		switch {
		case sl.RID == c.role.RID: // change the heroes of a held mine
			sl.Def, sl.Hallows = def, req.Int("hallows_id")
		case c.me.Pending.Floor == f && c.me.Pending.Room == room && now <= c.me.Pending.Until:
			if c.held() >= c.holdLimit() {
				return refuse("占领数量已达上限")
			}
			if err := a.minePayout(*sl, now); err != nil {
				return err
			}
			*sl = mineSlot{Room: sl.Room, MineID: sl.MineID, RID: c.role.RID, SrvID: c.role.SrvID, Since: now, Prot: now + mineConst("protect_time")*60, Def: def, Hallows: req.Int("hallows_id")}
			c.me.Pending = minePending{}
		default:
			return refuse("占领时间已过")
		}
		return s.Send(20646, proto.Message{"flag": 1, "msg": "占领成功"})
	})
}

func (a *adv) mineGiveUpOccupy(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		c.me.Pending = minePending{}
		return s.Send(20660, proto.Message{"code": 1, "msg": ""})
	})
}

func (a *adv) mineBoxes(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		nums := []proto.Message{}
		for n := range c.me.Taken {
			nums = append(nums, proto.Message{"num": n})
		}
		return s.Send(20647, proto.Message{"had_combat_num": c.me.Combats, "num_list": nums, "count": c.attacksLeft()})
	})
}

func (a *adv) mineTakeBox(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		n := req.Int("num")
		refuse := func(msg string) error { return s.Send(20648, proto.Message{"code": 0, "msg": msg, "num": n}) }
		for _, b := range advCfg.Mine.BoxReward {
			if b.Num != n {
				continue
			}
			if c.me.Taken[n] || c.me.Combats < n {
				return refuse("无法领取")
			}
			c.me.Taken[n] = true
			if err := a.gain(s, c.role, pairsOf(b.Items)); err != nil {
				return err
			}
			return s.Send(20648, proto.Message{"code": 1, "msg": "领取成功", "num": n})
		}
		return refuse("无法领取")
	})
}

func (a *adv) mineGiveUp(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f, room := req.Int("floor"), req.Int("room_id")
		sl := c.world.slot(f, room)
		if sl == nil || sl.RID != c.role.RID {
			return s.Send(20651, proto.Message{"code": 0, "msg": "你没有占领该灵矿"})
		}
		if err := a.minePayout(*sl, s.Now().Unix()); err != nil {
			return err
		}
		*sl = mineSlot{Room: sl.Room, MineID: sl.MineID}
		if err := sendAssets(s, a.st, c.role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, a.st); err != nil {
			return err
		}
		return s.Send(20651, proto.Message{"code": 1, "msg": "已放弃占领"})
	})
}

func (a *adv) mineMine(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		list := []proto.Message{}
		now := s.Now().Unix()
		for _, f := range mineFloors() {
			for _, sl := range c.world.floor(f) {
				if sl.RID != c.role.RID {
					continue
				}
				items, _ := mineOutput(sl, now)
				list = append(list, proto.Message{"floor": f, "room_id": sl.Room, "mine_id": sl.MineID, "occupy_time": sl.Since, "primary_count": sl.Pri, "senior_count": sl.Sen,
					"hook_items": itemPairs(items)})
			}
		}
		return s.Send(20652, proto.Message{"mine_list": list})
	})
}

func (a *adv) mineLayers(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		list := []proto.Message{}
		for _, f := range mineFloors() {
			if f <= c.adv.PassID {
				list = append(list, proto.Message{"floor": f})
			}
		}
		return s.Send(20653, proto.Message{"floor_list": list})
	})
}

// mineEmploy (20654) hires miners (type 1 primary, 2 senior) to keep a held mine producing longer.
func (a *adv) mineEmploy(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		f, room, typ := req.Int("floor"), req.Int("room_id"), req.Int("type")
		reply := func(code int64, msg string) error {
			return s.Send(20654, proto.Message{"code": code, "msg": msg, "floor": f, "room_id": room, "type": typ})
		}
		sl := c.world.slot(f, room)
		if sl == nil || sl.RID != c.role.RID || (typ != 1 && typ != 2) {
			return reply(0, "无法雇佣")
		}
		row := advCfg.mines[sl.MineID]
		cost, have, limit := row.PrimaryLoss, &sl.Pri, mineConst("primary_count")
		if typ == 2 {
			cost, have, limit = row.SeniorLoss, &sl.Sen, mineConst("senior_count")
		}
		if *have >= limit {
			return reply(0, "雇佣次数已达上限")
		}
		ok, err := a.pay(s, c.role, pairsOf(cost))
		if err != nil {
			return err
		}
		if !ok {
			return reply(0, "资源不足")
		}
		*have++
		return reply(1, "雇佣成功")
	})
}

func (a *adv) mineBuyAttack(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		refuse := func(msg string) error {
			return s.Send(20655, proto.Message{"code": 0, "msg": msg, "count": c.attacksLeft(), "buy_count": c.me.Bought})
		}
		if c.me.Bought >= mineConst("diamond_attack") {
			return refuse("购买次数已达上限")
		}
		ok, err := a.pay(s, c.role, [][2]int64{{3, mineConst("diamond_cost")}})
		if err != nil {
			return err
		}
		if !ok {
			return refuse("钻石不足")
		}
		c.me.Bought++
		return s.Send(20655, proto.Message{"code": 1, "msg": "购买成功", "count": c.attacksLeft(), "buy_count": c.me.Bought})
	})
}

// mineStrikeBack (20656) finds a mine of the role that hit us, to hit back.
func (a *adv) mineStrikeBack(s *gateway.Session, req proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		rid := req.Int("rid")
		for _, f := range mineFloors() {
			for _, sl := range c.world.floor(f) {
				if sl.RID == rid && rid != 0 {
					return s.Send(20656, proto.Message{"floor": f, "room_id": sl.Room})
				}
			}
		}
		return s.Send(20656, proto.Message{"floor": 0, "room_id": 0})
	})
}

func (a *adv) mineRecordRed(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error { return s.Send(20657, proto.Message{"code": b2i(c.me.Unread)}) })
}

func (a *adv) mineCountRed(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error { return s.Send(20659, proto.Message{"code": b2i(c.attacksLeft() > 0)}) })
}

func (a *adv) mineHallows(s *gateway.Session, _ proto.Message) error {
	return a.mineWith(s, func(c *mineCtx) error {
		list := []proto.Message{}
		for _, f := range mineFloors() {
			for _, sl := range c.world.floor(f) {
				if sl.RID == c.role.RID && sl.Hallows != 0 {
					list = append(list, proto.Message{"floor": f, "room_id": sl.Room, "hallows_id": sl.Hallows})
				}
			}
		}
		return s.Send(20658, proto.Message{"list": list})
	})
}

var _ = errors.New
