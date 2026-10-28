package game

import (
	_ "embed"
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// primus.json: the primus play of the client (tools/extract_primus.py).
//
//go:embed data/primus.json
var primusJSON []byte

const (
	primusModule = "primus"
	combatPrimus = 38 // our number; the client picks the finish view by the result message, 20705
)

type primusUnit struct{ Pos, Min, Max, Unit, Look int64 }

var primusCfg = func() (c struct {
	Const map[string]int64
	Seats []struct {
		Pos, Rank int64
		Name      string
		Look      int64
	}
	Units []primusUnit
}) {
	if err := json.Unmarshal(primusJSON, &c); err != nil || len(c.Seats) != 6 || len(c.Units) == 0 {
		panic("game: bad primus.json")
	}
	return c
}()

// primusUnitOf is the boss of a seat that evolved n times: the last range when n is past them all.
func primusUnitOf(pos, n int64) primusUnit {
	var last primusUnit
	for _, u := range primusCfg.Units {
		if u.Pos != pos {
			continue
		}
		last = u
		if u.Min <= n && n <= u.Max {
			return u
		}
	}
	return last
}

type primusSeat struct {
	RID, Num int64 // the holder (0 = the boss) and how many times the seat evolved
	Name     string
	Lev      int64
	Face     int64
	Lineup   []arenaHero
	FormType int64
	Power    int64
	Log      []primusLog
}

type primusLog struct {
	RID, Lev, Face, Power, Time, Num, FormType int64
	Name                                       string
	Lineup                                     []arenaHero
}

type primusWorld struct{ Seats map[int64]*primusSeat }

type primusRole struct{ CoolUntil int64 }

func (w *primusWorld) seat(pos int64) *primusSeat {
	if w.Seats == nil {
		w.Seats = map[int64]*primusSeat{}
	}
	if w.Seats[pos] == nil {
		w.Seats[pos] = &primusSeat{}
	}
	return w.Seats[pos]
}

// held is the seat a role holds, 0 when none.
func (w *primusWorld) held(rid int64) int64 {
	for pos, s := range w.Seats {
		if s.RID == rid && rid != 0 {
			return pos
		}
	}
	return 0
}

func (g *primusSvc) seatMessage(w *primusWorld, role *store.Role, pos int64) proto.Message {
	s := w.seat(pos)
	u := primusUnitOf(pos, s.Num)
	m := proto.Message{"pos": pos, "num": s.Num, "status": 0, "look_id": u.Look, "rid": 0, "srv_id": "", "name": "", "lev": 0, "face_id": 0, "unit_id": u.Unit,
		"face_update_time": 0, "face_file": ""}
	if s.RID != 0 {
		m["status"], m["rid"], m["srv_id"], m["name"], m["lev"], m["face_id"] = 1, s.RID, role.SrvID, s.Name, s.Lev, s.Face
		m["look_id"] = primusCfg.Seats[pos-1].Look
	}
	return m
}

type primusSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterPrimus registers the overlord seats ("霸气修炼", 20700~20706): six seats, each held by a boss that grows stronger the more it
// was challenged, or by the role that beat it. A role takes a seat by beating its holder, if it stands high enough in the arena
// (the number of places each seat asks for is in the client's table) and holds one seat at most; a lost fight shuts every seat for
// the role for 20 minutes. Our rules, not captured: how strong the boss is (the challenger's own team, a little weaker, the more
// so the fewer evolutions), that a won fight adds the evolutions the challenger chose to the seat, and no titles (no honour system).
func RegisterPrimus(r *gateway.Router, st *store.Store) {
	g := &primusSvc{st: st, fights: fightServiceOf(st)}
	with := func(s *gateway.Session, fn func(role *store.Role, w *primusWorld, d *primusRole) (bool, error)) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		w, d := &primusWorld{}, &primusRole{}
		if _, err := st.LoadGlobal(role.SrvID, primusModule, w); err != nil {
			return err
		}
		if _, err := st.LoadState(role.RID, primusModule, d); err != nil {
			return err
		}
		save, err := fn(role, w, d)
		if err != nil || !save {
			return err
		}
		if err := st.SaveGlobal(role.SrvID, primusModule, w); err != nil {
			return err
		}
		return st.SaveState(role.RID, primusModule, d)
	}
	r.Handle(20700, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(role *store.Role, w *primusWorld, d *primusRole) (bool, error) {
			return false, s.Send(20700, proto.Message{"num": primusCfg.Const["daily_challenge_limit"], "time": d.CoolUntil})
		})
	})
	r.Handle(20706, func(s *gateway.Session, _ proto.Message) error { return s.Send(20706, proto.Message{"is_show": 0}) })
	r.Handle(20704, func(s *gateway.Session, _ proto.Message) error { return s.Send(20704, proto.Message{}) })
	r.Handle(20701, func(s *gateway.Session, _ proto.Message) error {
		return with(s, func(role *store.Role, w *primusWorld, d *primusRole) (bool, error) {
			list := []proto.Message{}
			for _, seat := range primusCfg.Seats {
				list = append(list, g.seatMessage(w, role, seat.Pos))
			}
			return false, s.Send(20701, proto.Message{"list": list})
		})
	})
	r.Handle(20703, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(role *store.Role, w *primusWorld, d *primusRole) (bool, error) {
			pos := req.Int("pos")
			list := []proto.Message{}
			if pos >= 1 && pos <= int64(len(primusCfg.Seats)) {
				for i := len(w.seat(pos).Log) - 1; i >= 0; i-- {
					l := w.seat(pos).Log[i]
					heroes := []proto.Message{}
					for _, h := range l.Lineup {
						heroes = append(heroes, proto.Message{"bid": h.BID, "lev": h.Lev, "star": h.Star})
					}
					list = append(list, proto.Message{"rid": l.RID, "srv_id": role.SrvID, "name": l.Name, "lev": l.Lev, "power": l.Power, "face_id": l.Face, "time": l.Time, "num": l.Num,
						"replay_id": 0, "formation_type": l.FormType, "formation_lev": 1, "face_update_time": 0, "face_file": "", "partner_list": heroes})
				}
			}
			return false, s.Send(20703, proto.Message{"pos": pos, "list": list})
		})
	})
	r.Handle(20702, func(s *gateway.Session, req proto.Message) error {
		return with(s, func(role *store.Role, w *primusWorld, d *primusRole) (bool, error) {
			refuse := func(msg string) (bool, error) { return false, s.Send(20702, proto.Message{"code": 0, "msg": msg}) }
			pos, evo := req.Int("pos"), req.Int("num")
			now := s.Now().Unix()
			switch {
			case int64(role.Lev) < primusCfg.Const["open_lev"]:
				return refuse("等级不足")
			case pos < 1 || pos > int64(len(primusCfg.Seats)):
				return refuse("霸主之位不存在")
			case evo != 1 && evo != 5 && evo != 10:
				return refuse("进化次数不对")
			case d.CoolUntil > now:
				return refuse("挑战失败后需要冷却")
			}
			seat := w.seat(pos)
			if seat.RID == role.RID {
				return refuse("你已经占领了这个霸主之位")
			}
			if _, _, in, err := st.ArenaScoreOf(role.RID); err != nil {
				return false, err
			} else if !in {
				return refuse("需要先进入竞技场排名")
			}
			rank, err := st.ArenaRank(role.RID)
			if err != nil {
				return false, err
			}
			if need := primusCfg.Seats[pos-1].Rank; rank > need {
				return refuse("竞技场排名需要进入前" + itoa64(need) + "名")
			}
			team, objects, form, err := g.fights.teamSide(role)
			if err != nil {
				return false, err
			}
			if len(team) == 0 {
				return refuse("没有可以出战的英雄")
			}
			// the holder: a role's line-up as it was when it took the seat, or the boss: a copy of our own team
			opp := arenaOpp{RID: seat.RID, SrvID: role.SrvID, Name: seat.Name, Lev: seat.Lev, Face: seat.Face, Power: seat.Power, Robot: true, FormType: seat.FormType, Lineup: seat.Lineup}
			factor := int64(1000)
			if seat.RID == 0 {
				mine, formType, _, err := lineupOf(st, role.RID)
				if err != nil {
					return false, err
				}
				opp.FormType, opp.Lineup, opp.Name = formType, nil, primusCfg.Seats[pos-1].Name
				for _, x := range mine {
					opp.Lineup = append(opp.Lineup, arenaHero{x.Pos, x.BID, x.Lev, x.Star, x.Break})
				}
				factor = min(600+3*(seat.Num+evo), 1400) * 1000 / 1000 // 60% at the start, 140% at the most
			} else {
				factor = 1000 + 5*min(evo, 10)
			}
			oslots, oform, obyID, err := opp.fighters(st)
			if err != nil {
				return false, err
			}
			foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
			if len(foes) == 0 {
				return refuse("霸主已无法应战")
			}
			scaleFoes(foes, foeObjects, factor)
			rounds, won, _ := playFightOpts(team, foes, newRng(), maxFightRounds, true)
			intro := proto.Message{
				"combat_type": combatPrimus, "formation": []proto.Message{{"group": 1, "formation_type": form.FormationType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
				"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
				"extra_args": []proto.Message{{"param": pos}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name,
				"target_role_name": opp.Name, "suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
			}
			if err := s.Send(20702, proto.Message{"code": 1, "msg": ""}); err != nil {
				return false, err
			}
			return false, g.fights.begin(s, role, combatPrimus, intro, rounds, won, func(s *gateway.Session, won bool) error {
				return g.settle(s, pos, evo, won, opp.Name)
			})
		})
	})
}

func (g *primusSvc) settle(s *gateway.Session, pos, evo int64, won bool, target string) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	w, d := &primusWorld{}, &primusRole{}
	if _, err := g.st.LoadGlobal(role.SrvID, primusModule, w); err != nil {
		return err
	}
	if _, err := g.st.LoadState(role.RID, primusModule, d); err != nil {
		return err
	}
	lineup, formType, power, err := lineupOf(g.st, role.RID)
	if err != nil {
		return err
	}
	seat := w.seat(pos)
	before := seat.Num
	entry := primusLog{RID: role.RID, Lev: int64(role.Lev), Face: int64(role.FaceID), Power: power, Time: s.Now().Unix(), Num: evo, FormType: formType, Name: role.Name, Lineup: lineup}
	seat.Log = append(seat.Log, entry)
	if len(seat.Log) > 30 {
		seat.Log = seat.Log[len(seat.Log)-30:]
	}
	if won {
		if old := w.held(role.RID); old != 0 { // one seat at most: the old one goes back to its boss
			o := w.seat(old)
			o.RID, o.Name, o.Lineup = 0, "", nil
		}
		seat.RID, seat.Name, seat.Lev, seat.Face, seat.Power, seat.FormType, seat.Lineup = role.RID, role.Name, int64(role.Lev), int64(role.FaceID), power, formType, lineup
		seat.Num += evo
		_ = RecordGrowth(g.st, role.RID, 124001, s.Now().Unix(), primusCfg.Seats[pos-1].Name) // "challenged the overlord seat"
	} else {
		d.CoolUntil = s.Now().Unix() + primusCfg.Const["lose_challange_cd"]
	}
	if err := g.st.SaveGlobal(role.SrvID, primusModule, w); err != nil {
		return err
	}
	if err := g.st.SaveState(role.RID, primusModule, d); err != nil {
		return err
	}
	if err := s.Send(20705, proto.Message{"pos": pos, "num1": before, "num2": seat.Num, "target_role_name": target, "hurt_statistics": []proto.Message{}}); err != nil {
		return err
	}
	return g.pushSeats(s, w, role)
}

// pushSeats tells the role the seats as they are now.
func (g *primusSvc) pushSeats(s *gateway.Session, w *primusWorld, role *store.Role) error {
	list := []proto.Message{}
	for _, seat := range primusCfg.Seats {
		list = append(list, g.seatMessage(w, role, seat.Pos))
	}
	return s.Send(20701, proto.Message{"list": list})
}
