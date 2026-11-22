package game

import (
	"encoding/json"
	"log"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type towerReport struct {
	stats      []proto.Message
	durationMS int64
	replay     store.Replay
	power      int64
	ranked     bool
}

func (r *towerReport) apply(st *store.Store, result proto.Message) {
	result["hurt_statistics"] = r.stats
	result["timer"] = towerSeconds(r.durationMS)
	if r.replay.Data != "" {
		id, err := st.AddTowerReplay(r.replay, result.Int("tower"), r.durationMS, r.power, r.ranked)
		if err != nil {
			log.Printf("star tower: could not keep replay: %v", err)
			return
		}
		result["replay_id"] = id
	}
}

func towerSeconds(ms int64) int64 { return (ms + 999) / 1000 }

// towerStatistics pairs the actual simulated totals with the combat object snapshot, including foes and fallen units.
func towerStatistics(sim *fightSim, objects []proto.Message) []proto.Message {
	byPos := make(map[int64]fightUnit, len(sim.units))
	for _, u := range sim.units {
		byPos[u.pos] = u
	}
	groups := map[int64][]proto.Message{1: {}, 2: {}}
	for _, o := range objects {
		unit, ok := byPos[o.Int("pos")]
		if !ok {
			continue
		}
		group := o.Int("group")
		groups[group] = append(groups[group], proto.Message{
			"rid": o.Int("owner_id"), "srvid": o.Str("owner_srv_id"), "id": o.Int("object_id"),
			"bid": o.Int("object_bid"), "star": o.Int("star"), "lev": o.Int("lev"), "camp_type": o.Int("camp_type"),
			"dps": unit.damage, "be_hurt": unit.taken, "cure": unit.healing, "ext_data": []proto.Message{},
		})
	}
	return []proto.Message{{"type": 2, "partner_hurts": groups[2]}, {"type": 1, "partner_hurts": groups[1]}}
}

func towerReplayList(st *store.Store, role *store.Role, floor int64) (proto.Message, error) {
	entries := []proto.Message{}
	response := proto.Message{"m_replay_id": 0, "my_time": 0}
	records, err := st.TowerReplayRecords(role.SrvID, floor, role.RID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		seconds := towerSeconds(record.DurationMS)
		if record.Kind == store.TowerReplayOwn {
			response["m_replay_id"], response["my_time"] = record.ReplayID, seconds
			continue
		}
		var meta replayMeta
		if err := json.Unmarshal([]byte(record.Meta), &meta); err != nil {
			return nil, err
		}
		entries = append(entries, proto.Message{
			"type": record.Kind, "replay_id": record.ReplayID, "time": seconds, "power": record.Power,
			"rid": meta.A.RID, "sid": meta.A.SrvID, "name": meta.A.Name, "face_id": meta.A.Face,
			"face_update_time": 0, "face_file": "",
		})
	}
	response["tower_replay_data"] = entries
	return response, nil
}
