package game

import (
	"encoding/json"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func (e *expSvc) load(rid int64, now time.Time) (*expState, error) {
	previous, hasState, err := e.st.RoleModule(rid, expModule)
	if err != nil {
		return nil, err
	}
	st := &expState{}
	if hasState {
		if err := json.Unmarshal(previous, st); err != nil {
			return nil, err
		}
	}
	st.previous = previous
	st.roll(now)
	return st, nil
}

func expeditionChange(rid int64, st *expState, loot [][2]int64) store.ModuleStateChange {
	assets, items := dungeonGains(loot)
	return store.ModuleStateChange{RID: rid, Previous: st.previous, Next: st, GainAssets: assets, GainItems: items}
}

func (e *expSvc) save(rid int64, st *expState, loot [][2]int64) error {
	if err := e.st.CommitModuleState(expModule, expeditionChange(rid, st, loot)); err != nil {
		return err
	}
	st.saved = true
	return nil
}

func expeditionLoot(st *expState, times int64) [][2]int64 {
	loot := expShare(st.Diff)
	if st.Got == nil {
		st.Got = map[string]int64{}
	}
	for i := range loot {
		loot[i][1] *= times
		st.Got[strconv.FormatInt(loot[i][0], 10)] += loot[i][1]
	}
	return loot
}

func expeditionLootMessage(loot [][2]int64) []proto.Message {
	items := make([]proto.Message, 0, len(loot))
	for _, it := range loot {
		items = append(items, proto.Message{"bid": it[0], "num": it[1]})
	}
	return items
}

func (e *expSvc) sendBalances(s *gateway.Session, rid int64) error {
	if err := sendAssets(s, e.st, rid); err != nil {
		return err
	}
	return sendBackpack(s, e.st)
}

func sendExpeditionResult(s *gateway.Session, result int64, loot [][2]int64) error {
	items := expeditionLootMessage(loot)
	for _, it := range items {
		it["is_bind"], it["id"] = 1, 0
	}
	return s.Send(20006, proto.Message{"result": result, "item_rewards": items, "show_panel_type": 1, "current_time": 0, "best_time": 0,
		"combat_type": combatExpedit, "target_role_name": "远征守卫", "hurt_statistics": []proto.Message{},
		"adventure_end_hp": 0, "room_id": 0, "ext_list": []proto.Message{}})
}
