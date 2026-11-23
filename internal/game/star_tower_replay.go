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
