package game

import (
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// planesFightPlan is a fight of the planes made ready: the script, who wins, the 20027 intro, who fought and how much hp
// each of them has left (per cent, by battle cell).
type planesFightPlan struct {
	rounds   []fightRound
	won      bool
	intro    proto.Message
	fighters []pfSlot
	formType int64
	left     map[int64]int64
}

// planesFight makes the fight of a formation of the roster (hp in per cent, own and hired heroes) against a scaled copy of
// the role's own best heroes whose power is `power`. A non-empty message says why it cannot be fought.
func planesFight(db *store.Store, fights *FightService, role *store.Role, roster []pfHero, slots []pfSlot, formType, power int64, foeName string, extra []proto.Message, combat int64) (*planesFightPlan, string, error) {
	if formType == 0 {
		formType = 1
	}
	heroOf := func(sl pfSlot) *pfHero {
		for i := range roster {
			if roster[i].Flag == sl.Flag && roster[i].PID == sl.ID {
				return &roster[i]
			}
		}
		return nil
	}
	var ownSlots, hiredSlots []store.FormationSlot
	hired := map[int64]store.Partner{}
	var fighters []pfSlot
	for _, sl := range slots {
		h := heroOf(sl)
		if h == nil || h.HP <= 0 || len(fighters) == formMaxHeroes {
			continue
		}
		fighters = append(fighters, sl)
		fs := store.FormationSlot{Pos: sl.Pos, PartnerID: sl.ID}
		if sl.Flag == 1 {
			hiredSlots = append(hiredSlots, fs)
			hired[sl.ID] = store.Partner{PartnerID: sl.ID, RID: role.RID, BID: h.BID, Lev: int(h.Lev), Star: int(h.Star)}
		} else {
			ownSlots = append(ownSlots, fs)
		}
	}
	if len(fighters) == 0 {
		return nil, "没有可以出战的英雄", nil
	}
	team, objects, err := fights.teamFor(role, formType, ownSlots)
	if err != nil {
		return nil, "", err
	}
	if len(hiredSlots) > 0 {
		hu, ho := sideUnits(role.RID, role.SrvID, hiredSlots, formType, hired, true)
		team, objects = append(team, hu...), append(objects, ho...)
	}
	at := map[int64]*pfSlot{} // battle cell -> who stands there
	for i := range fighters {
		at[battleCell(formType, fighters[i].Pos)] = &fighters[i]
	}
	for i := range team {
		if sl := at[team[i].pos]; sl != nil {
			team[i].hp = max(1, team[i].maxHP*heroOf(*sl).HP/100)
		}
	}
	for _, o := range objects {
		if o.Int("object_type") == 2 {
			if sl := at[o.Int("pos")]; sl != nil {
				o["hp"] = max(1, o.Int("hp_max")*heroOf(*sl).HP/100)
			}
		}
	}
	all, err := db.PartnersOf(role.RID)
	if err != nil {
		return nil, "", err
	}
	best := bestHeroes(all, 5)
	factor := power * 1000 / max(1, rolePower(best))
	opp := arenaOpp{RID: arenaRobotBase + power, SrvID: role.SrvID, Name: foeName, Robot: true, FormType: 1, Power: power}
	for i, h := range best {
		opp.Lineup = append(opp.Lineup, arenaHero{Pos: int64(i + 1), BID: h.BID, Lev: max(1, int64(h.Lev)*factor/1000), Star: int64(h.Star), Break: int64(h.BreakLev)})
	}
	oslots, oform, obyID, err := opp.fighters(db)
	if err != nil {
		return nil, "", err
	}
	foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return nil, "守卫已无法应战", nil
	}
	rounds, won, sim := playFightOpts(team, foes, newRng(), maxFightRounds, false)
	left := map[int64]int64{}
	for _, u := range sim.units {
		if u.ours {
			left[u.pos] = max(0, u.hp) * 100 / max(1, u.maxHP)
		}
	}
	intro := proto.Message{
		"combat_type": combat, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": oform, "formation_lev": 1}},
		"objects": append(objects, foeObjects...), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": extra, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name,
		"target_role_name": foeName, "suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	return &planesFightPlan{rounds: rounds, won: won, intro: intro, fighters: fighters, formType: formType, left: left}, "", nil
}
