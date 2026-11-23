package game

import (
	"encoding/json"
	"errors"
	"math/rand"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const formArenaDefense = 1 // PartnerConst.Fun_Form.Arena

var errArenaNoAttempts = errors.New("arena: no free attempts or tickets")

func refuseArenaChallenge(s *gateway.Session, msg string) error {
	return s.Send(20203, proto.Message{"code": 0, "msg": msg})
}

func arenaReplayRanks(st *store.Store, rid int64, opponent arenaOpp) (attacker, defender int64) {
	attacker, _ = st.ArenaRank(rid)
	if !opponent.Robot {
		defender, _ = st.ArenaRank(opponent.RID)
	}
	return
}

func arenaStanding(st *store.Store, rid int64, srvID string, now time.Time) (*store.ArenaPlayer, error) {
	a, err := st.ArenaPlayerOf(rid, arenaInitScore, now.Unix())
	if err != nil {
		return nil, err
	}
	season, _, _ := arenaSeason(now)
	day := dayKey(now)
	if a.Season >= season && a.Day >= day {
		return a, nil
	}
	if a.Season < season {
		if err := st.RollH5ArenaSeason(srvID, season, arenaInitScore, arenaKeepPermil); err != nil {
			return nil, err
		}
	}
	if err := st.ResetArenaDay(rid, day); err != nil {
		return nil, err
	}
	return st.ArenaPlayerOf(rid, arenaInitScore, now.Unix())
}

func newH5ArenaOpponents(st *store.Store, role *store.Role, score int64, rng *rand.Rand) ([]arenaOpp, error) {
	near, err := st.ArenaH5Near(role.SrvID, role.RID, score, h5MinHeroStar, 12)
	if err != nil {
		return nil, err
	}
	var out []arenaOpp
	for _, row := range near {
		o, ok, err := h5ArenaOpponentOf(st, row.RID, row.Score)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, o)
			if len(out) == arenaOpponents {
				return out, nil
			}
		}
	}
	team, err := h5FormationTeamOf(st, role.RID, formDrama)
	if err != nil {
		return nil, err
	}
	mine, _ := describeArenaLineup(team.form.Slots, team.byID)
	if len(mine) == 0 {
		return out, nil
	}
	return fillArenaRobots(out, role, score, rng, mine, team.form.FormationType), nil
}

func h5ArenaOpponentOf(st *store.Store, rid, score int64) (arenaOpp, bool, error) {
	role, err := st.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		return arenaOpp{}, false, nil
	}
	if err != nil || role.Status != 1 {
		return arenaOpp{}, false, err
	}
	team, err := h5FormationTeamOf(st, rid, formArenaDefense)
	if err != nil {
		return arenaOpp{}, false, err
	}
	lineup, power := describeArenaLineup(team.form.Slots, team.byID)
	if len(lineup) == 0 {
		return arenaOpp{}, false, nil
	}
	o := describeArenaOpponent(role, score, formArenaDefense, team.form.FormationType, power, lineup)
	o.defense = &team
	return o, true, nil
}

// Read current defenses even on lists saved before the arena used a defense formation.
// Robots keep their generated lineup, filtered in case the snapshot predates H5 matching.
func arenaOpponentOnSlot(st *store.Store, srvID string, slot store.ArenaSlot, h5 bool) (arenaOpp, bool, error) {
	var o arenaOpp
	if err := json.Unmarshal([]byte(slot.Info), &o); err != nil || o.RID != slot.OppRID || o.SrvID != srvID {
		return arenaOpp{}, false, nil
	}
	if !h5 {
		if o.Robot {
			return o, true, nil
		}
		ok, err := st.ArenaRoleInPool(srvID, o.RID, false)
		return o, ok, err
	}
	if o.Robot {
		lineup := o.Lineup[:0]
		var heroes []store.Partner
		for _, h := range o.Lineup {
			if h5HeroEligible(h.BID, int(h.Star)) {
				lineup = append(lineup, h)
				heroes = append(heroes, store.Partner{BID: h.BID, Lev: int(h.Lev), Star: int(h.Star), BreakLev: int(h.Break)})
			}
		}
		o.Lineup, o.Power = lineup, rolePower(heroes)
		return o, len(lineup) > 0, nil
	}
	score, ok, err := st.ArenaH5DefenderScore(srvID, o.RID)
	if err != nil || !ok {
		return arenaOpp{}, false, err
	}
	return h5ArenaOpponentOf(st, o.RID, score)
}

func arenaSlotMessage(slot store.ArenaSlot, o arenaOpp, mine int64) proto.Message {
	return proto.Message{"idx": slot.Idx, "rid": o.RID, "srv_id": o.SrvID, "name": o.Name, "lev": o.Lev, "sex": o.Sex, "face": o.Face, "power": o.Power,
		"score": o.Score, "get_score": arenaGain(mine, o.Score), "status": slot.Status, "p_list": o.heroMessages(), "face_update_time": 0, "face_file": "",
		"gid": 0, "gsrv_id": "", "gname": ""}
}

func arenaFightIntro(attacker, defender string, attackForm, defenseForm, auto int64, teamSize int, objects []proto.Message) proto.Message {
	return proto.Message{
		"combat_type": combatArena, "formation": []proto.Message{{"group": 1, "formation_type": attackForm, "formation_lev": 1}, {"group": 2, "formation_type": defenseForm, "formation_lev": 1}},
		"objects": objects, "is_auto": auto, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": teamSize, "actor_role_name": attacker, "target_role_name": defender,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
}

// The buff list and answer precede daily quest progress (original-2026-10-28, 22-arena).
func acceptArenaChallenge(s *gateway.Session, st *store.Store, rid int64) error {
	if err := s.Send(13011, proto.Message{"buff_list": []proto.Message{}}); err != nil {
		return err
	}
	if err := s.Send(20203, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	return questEvent(s, st, rid, evtArena, 1)
}

type arenaSlotLoader func(*gateway.Session, *store.ArenaPlayer, bool) ([]store.ArenaSlot, error)

func arenaChallenge(st *store.Store, fights *FightService, slotsFor arenaSlotLoader, rngOf func() *rand.Rand) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		role, err := st.RoleByID(s.Role().RID)
		if err != nil {
			return err
		}
		if role.Lev < arenaLimitLev {
			return refuseArenaChallenge(s, "等级不足，竞技场需要达到8级")
		}
		now := s.Now()
		a, err := arenaStanding(st, role.RID, role.SrvID, now)
		if err != nil {
			return err
		}
		slots, err := slotsFor(s, a, false)
		if err != nil {
			return err
		}
		for _, slot := range slots {
			if slot.OppRID != req.Int("rid") {
				continue
			}
			if slot.Status != 0 {
				return refuseArenaChallenge(s, "已经挑战过该对手")
			}
			opp, ok, err := arenaOpponentOnSlot(st, role.SrvID, slot, true)
			if err != nil {
				return err
			}
			if sid := req.Str("srv_id"); !ok || sid != "" && sid != opp.SrvID {
				break
			}
			return startArenaFight(s, st, fights, role, slot, opp, rngOf(), now, req.Int("is_auto"))
		}
		return refuseArenaChallenge(s, "对手不存在，请刷新列表")
	}
}

func startArenaFight(s *gateway.Session, st *store.Store, fights *FightService, role *store.Role, slot store.ArenaSlot,
	opp arenaOpp, rng *rand.Rand, now time.Time, auto int64) error {
	if _, err := starterGrant(st, role); err != nil {
		return err
	}
	attack, err := h5FormationTeamOf(st, role.RID, formDrama)
	if err != nil {
		return err
	}
	form := attack.form
	team, objects := ownFightSide(role, form.FormationType, form.Slots, attack.byID)
	if len(team) == 0 {
		return refuseArenaChallenge(s, "没有可以出战的英雄")
	}
	oslots, oform, obyID, err := opp.fighters(st)
	if err != nil {
		return err
	}
	foes, foeObjects := sideUnits(opp.RID, opp.SrvID, oslots, oform, obyID, false)
	if len(foes) == 0 {
		return refuseArenaChallenge(s, "对手已无法应战，请刷新列表")
	}
	intro := arenaFightIntro(role.Name, opp.Name, form.FormationType, oform, auto, len(team), append(objects, foeObjects...))
	rounds, won, sim := playFight(team, foes, rng, maxFightRounds)
	stats, bid, hurt, total := lineupDamageTable(role, form.Slots, attack.byID)
	_, power := describeArenaLineup(form.Slots, attack.byID)
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	attackerRank, defenderRank := arenaReplayRanks(st, role.RID, opp)
	attacker := replaySide{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Power: power, Form: form.FormationType, Rank: attackerRank}
	defender := replaySide{RID: opp.RID, SrvID: opp.SrvID, Name: opp.Name, Lev: opp.Lev, Face: opp.Face, Power: opp.Power, Form: oform, Rank: defenderRank}
	saveReplay := func(st *store.Store) int64 {
		return recordReplay(st, combatArena, attacker, defender, intro, rounds, won, now.Unix(), replayDetails{Map: fightMap, Sim: sim})
	}
	result, paid, err := commitArenaFight(st, role.RID, slot, opp, attacker, int64(role.Sex), now, won, saveReplay)
	switch {
	case errors.Is(err, errArenaNoAttempts):
		return refuseArenaChallenge(s, "挑战次数不足")
	case errors.Is(err, store.ErrConflict):
		return refuseArenaChallenge(s, "对手列表已变化，请重新查看")
	case err != nil:
		return err
	}
	lifeCount(st, role.RID, "evt_arena_fight", 1)
	result["hurt_statistics"] = []proto.Message{{"type": 1, "partner_hurts": stats}}
	result["partner_bid"], result["partner_hurt"], result["partner_total_hurt"] = bid, hurt, total
	if paid {
		if err := sendBackpack(s, st); err != nil {
			return err
		}
	}
	if err := acceptArenaChallenge(s, st, role.RID); err != nil {
		return err
	}
	return fights.begin(s, role, combatArena, intro, rounds, won, func(s *gateway.Session, _ bool) error {
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := s.Send(20210, result); err != nil || !won {
			return err
		}
		slots, err := st.ArenaSlots(role.RID)
		if err != nil {
			return err
		}
		for _, current := range slots {
			if current.Idx == slot.Idx && current.OppRID == slot.OppRID && current.Info == slot.Info && current.Status == 1 {
				row := arenaSlotMessage(current, opp, result.Int("score"))
				row["get_score"] = result.Int("get_score")
				return s.Send(20201, proto.Message{"f_list": []proto.Message{row}, "type": 1})
			}
		}
		return nil
	})
}

// Save the result before sending the fight. Leaving playback or losing the connection
// cannot consume an attempt without the associated reward and history.
func commitArenaFight(st *store.Store, rid int64, slot store.ArenaSlot, opp arenaOpp, attacker replaySide, sex int64, now time.Time, won bool,
	saveReplay func(*store.Store) int64) (result proto.Message, paid bool, err error) {
	err = st.CommitArenaMatch(rid, slot, func(m *store.ArenaMatchTx) error {
		a, err := arenaStanding(m.Store, rid, opp.SrvID, now)
		if err != nil {
			return err
		}
		if a.FreeUsed < arenaFreeCombats {
			a.FreeUsed++
		} else {
			if err := m.SpendTicket(rid, arenaTicket); errors.Is(err, store.ErrConflict) {
				return errArenaNoAttempts
			} else if err != nil {
				return err
			}
			paid = true
		}

		oppScore := opp.Score
		var other *store.ArenaPlayer
		if !opp.Robot {
			if _, eligible, err := m.ArenaH5DefenderScore(opp.SrvID, opp.RID); err != nil {
				return err
			} else if !eligible {
				return store.ErrConflict
			}
			other, err = arenaStanding(m.Store, opp.RID, opp.SrvID, now)
			if err != nil {
				return err
			}
			oppScore = other.Score
		}
		var gain, loss int64
		ret, defRet := int64(2), int64(1)
		drop := [2]int64{1, arenaWinCoin * arenaLoseRatio / 1000}
		if won {
			ret, defRet = 1, 2
			gain = arenaGain(a.Score, oppScore)
			loss = max(0, min(oppScore-arenaMinScore, arenaLoss(gain)))
			a.Score += gain
			a.ContWin++
			drop = [3][2]int64{{1, arenaWinCoin}, {8, 15}, {22, 8000}}[newRng().Intn(3)]
		} else {
			a.ContWin = 0
		}
		a.Battles++
		if err := m.SaveArenaPlayer(a); err != nil {
			return err
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains([][2]int64{drop}), 1, assets, items)
		if err := m.CreditReward(rid, assets, items); err != nil {
			return err
		}
		replayID := saveReplay(m.Store)
		if replayID == 0 {
			return errors.New("arena: could not save replay")
		}
		appendLog := func(rid int64, entry arenaLogEntry) error {
			entry.ReplayID = replayID
			raw, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			return m.AddArenaLog(rid, string(raw), now.Unix())
		}
		if err := appendLog(rid, arenaLogEntry{RID: opp.RID, SrvID: opp.SrvID, Name: opp.Name, Lev: opp.Lev, Sex: opp.Sex, Face: opp.Face,
			Power: opp.Power, Score: gain, Type: 1, Ret: ret}); err != nil {
			return err
		}
		if other != nil {
			other.Score -= loss
			if err := m.SaveArenaPlayer(other); err != nil {
				return err
			}
			if err := appendLog(other.RID, arenaLogEntry{RID: rid, SrvID: attacker.SrvID, Name: attacker.Name, Lev: attacker.Lev, Sex: sex, Face: attacker.Face,
				Power: attacker.Power, Score: -loss, Type: 2, Ret: defRet}); err != nil {
				return err
			}
		}
		result = proto.Message{"result": ret, "score": a.Score, "get_score": gain, "items": []proto.Message{{"bid": drop[0], "num": drop[1]}},
			"tar_name": opp.Name, "tar_lev": opp.Lev, "tar_face": opp.Face, "face_update_time": 0, "face_file": "", "tar_score": oppScore - loss, "lose_score": -loss,
			"target_role_name": opp.Name, "replay_id": replayID}
		return nil
	})
	return result, paid, err
}
