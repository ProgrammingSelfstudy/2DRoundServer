package game

import (
	_ "embed"
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// drama_rewards.json: per dungeon the enemy power, level limit and what one
// hook interval pays (tools/extract_drama_rewards.py).
//
//go:embed data/drama_rewards.json
var dramaRewardsJSON []byte

type dungeonNumbers struct {
	Power    int64      `json:"power"`
	LevLimit int        `json:"lev_limit"`
	PerHook  [][2]int64 `json:"per_hook"`
}

var dungeonTable = func() map[int64]dungeonNumbers {
	var f struct {
		Dungeons map[int64]dungeonNumbers `json:"dungeons"`
	}
	if err := json.Unmarshal(dramaRewardsJSON, &f); err != nil || len(f.Dungeons) == 0 {
		panic("game: bad drama_rewards.json")
	}
	return f.Dungeons
}()

// dungeonIndex maps a dungeon id to its place in the chain.
var dungeonByID = func() map[int64]dungeon {
	m := make(map[int64]dungeon, len(dramaChain.Dungeons))
	for _, d := range dramaChain.Dungeons {
		m[d.ID] = d
	}
	return m
}()

// dungeonIndex is a dungeon's position in the chain, from 0.
var dungeonIndex = func() map[int64]int {
	m := make(map[int64]int, len(dramaChain.Dungeons))
	for i, d := range dramaChain.Dungeons {
		m[d.ID] = i
	}
	return m
}()

const (
	fightTypeDrama = 3 // BattleConst.Fight_Type.Darma
	dungeonCleared = 3 // the status of a dungeon that was passed
)

// clearDungeon works out the campaign state after the role beat its current
// dungeon and the reward of that win. Beating the frontier dungeon opens the next
// one; beating an earlier one (a replay) changes nothing but pays again.
func clearDungeon(d store.Drama) (next store.Drama, reward []sellGain, msg string) {
	cur, ok := dungeonByID[d.DunID]
	if !ok {
		return d, nil, "关卡不存在"
	}
	nums := dungeonTable[d.DunID]
	reward = minutesOf(nums.PerHook, clearRewardMin)
	next = d
	if d.DunID == d.MaxDunID { // the frontier
		if nd, ok := dungeonByID[cur.Next]; ok {
			next.Mode, next.ChapterID, next.DunID, next.MaxDunID, next.Status = nd.Mode, nd.Chapter, nd.ID, nd.ID, dungeonNotCleared
		} else {
			next.Status = dungeonCleared // the last dungeon of the chain
		}
		next.IsFirst = false
	}
	return next, reward, ""
}

// RegisterDramaBattle registers 13003, the challenge of the current campaign
// dungeon, and the confirmations the client sends while it plays the fight
// (20001, 20019, 20005) and the skip button (20062). The fight is a script that
// ends in a win unless every hero is down or the monsters outlast the round limit
// (our policy, see docs/CAMPAIGN_PLAN.md); once the client has played it, or skipped
// it, a win sends the result (13016) and the new progress (13001), a loss only the
// result.
func RegisterDramaBattle(r *gateway.Router, st *store.Store) {
	fights := &FightService{Store: st}
	r.Handle(13003, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := dramaState(st, role.RID)
		if err != nil {
			return err
		}
		if _, _, msg := clearDungeon(d); msg != "" {
			return s.Send(13003, proto.Message{"code": 0, "msg": msg})
		}
		return fights.start(s, role, d, req.Int("is_auto"))
	})
	for _, cmd := range []uint16{20001, 20019, 20005} {
		cmd := cmd
		r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error {
			return fights.advance(s, cmd, func(won bool) error { return settleFight(s, st, won) })
		})
	}
	r.Handle(20062, func(s *gateway.Session, _ proto.Message) error {
		return fights.skip(s, func(won bool) error { return settleFight(s, st, won) })
	})
}

// settleFight ends a campaign fight in the way the script says.
func settleFight(s *gateway.Session, st *store.Store, won bool) error {
	if won {
		return settleDrama(s, st)
	}
	return defeatDrama(s, st)
}

// damageTable is the damage table of the result screen: the heroes of the campaign
// formation, their "damage" (our power, not real damage) and the MVP with the team
// total (partner_bid / partner_hurt / partner_total_hurt; all zero shows "0(nan%)").
func damageTable(st *store.Store, role *store.Role) (stats []proto.Message, mvpBID, mvpHurt, totalHurt int64, err error) {
	team, err := formationOf(st, role.RID, formDrama)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	heroes, err := st.PartnersOf(role.RID)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	byID := map[int64]store.Partner{}
	for _, h := range heroes {
		byID[h.PartnerID] = h
	}
	stats = []proto.Message{}
	for _, sl := range team.Slots {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		info := partnerTable[h.BID]
		dps := partnerMessage(h).Int("power")
		stats = append(stats, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star,
			"lev": h.Lev, "camp_type": info.Camp, "dps": dps, "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
		totalHurt += dps
		if dps > mvpHurt {
			mvpBID, mvpHurt = h.BID, dps
		}
	}
	return stats, mvpBID, mvpHurt, totalHurt, nil
}

// defeatDrama tells the client the fight was lost: the result screen with no
// reward and no new progress. The client opens its generic defeat window for every
// result but 1 (BattleController.openFinishView); that it is 2 is our guess, no
// loss was ever captured.
func defeatDrama(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	stats, bid, hurt, total, err := damageTable(st, role)
	if err != nil {
		return err
	}
	return s.Send(13016, proto.Message{"result": 2, "item_rewards": []proto.Message{}, "combat_type": fightTypeDrama,
		"partner_bid": bid, "partner_hurt": hurt, "partner_total_hurt": total, "lev": role.Lev, "exp": 0, "new_lev": role.Lev, "new_exp": 0, "auto_num": 0,
		"target_role_name": "", "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "use_skin": 0})
}

// settleDrama pays the win of the current campaign dungeon and tells the client:
// the result screen, the new progress, the assets and the backpack.
func settleDrama(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	d, err := dramaState(st, role.RID)
	if err != nil {
		return err
	}
	next, reward, msg := clearDungeon(d)
	if msg != "" {
		return s.Send(13003, proto.Message{"code": 0, "msg": msg})
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	splitGains(reward, 1, assets, items)
	if err := st.ClearDungeon(next, assets, items); err != nil {
		return err
	}
	rewards := []proto.Message{}
	for _, g := range reward {
		rewards = append(rewards, proto.Message{"bid": g.ID, "is_bind": 1, "num": g.Amount, "id": 0})
	}
	stats, mvpBID, mvpHurt, totalHurt, err := damageTable(st, role)
	if err != nil {
		return err
	}
	// The result window shows the MVP hero and its share of the team's damage
	// (partner_bid / partner_hurt / partner_total_hurt in the capture); all zero
	// gives "0(nan%)". The numbers are our power, not real damage.
	if err := s.Send(13016, proto.Message{"result": 1, "item_rewards": rewards, "combat_type": fightTypeDrama,
		"partner_bid": mvpBID, "partner_hurt": mvpHurt, "partner_total_hurt": totalHurt, "lev": 0, "exp": 0, "new_lev": 0, "new_exp": 0, "auto_num": 0,
		"target_role_name": "", "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}, "use_skin": 0}); err != nil {
		return err
	}
	if err := s.Send(13001, proto.Message{"mode": next.Mode, "chapter_id": next.ChapterID, "dun_id": next.DunID, "status": next.Status,
		"is_first": b2i(next.IsFirst), "cool_time": 0, "max_dun_id": clearedMax(next)}); err != nil {
		return err
	}
	if next.ChapterID != d.ChapterID {
		if err := s.Send(13010, proto.Message{"dun_id": next.DunID, "chapter_id": next.ChapterID}); err != nil {
			return err
		}
	}
	if err := sendAssets(s, st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, st)

}
