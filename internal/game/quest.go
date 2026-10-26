package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// quests.json is the client's daily quest table (tools/extract_quests.py).
//
//go:embed data/quests.json
var questsJSON []byte

type questDef struct {
	Name      string     `json:"name"`
	Lev       int        `json:"lev"`
	Label     string     `json:"label"`
	Target    int64      `json:"target"`
	TargetVal int64      `json:"target_val"`
	Rewards   [][2]int64 `json:"rewards"`
}

var questTable = func() map[int64]questDef {
	var f struct {
		Quests map[int64]questDef `json:"quests"`
	}
	if err := json.Unmarshal(questsJSON, &f); err != nil || len(f.Quests) == 0 {
		panic("game: bad quests.json")
	}
	return f.Quests
}()

// The labels of the game events a quest counts (the client's "evt_..." names).
// Only the events this server has are fed; the others (arena, guild, battles,
// ...) stay at 0 until their systems exist.
const (
	evtLoaded        = "evt_cli_loaded"     // the daily login
	evtRecruit       = "evt_recruit"        // one hero summon draw
	evtFriendPresent = "evt_friend_present" // one friend gift sent
	evtArena         = "evt_arena"          // one arena challenge
	evtGuildDonate   = "evt_guild_donate"   // one guild donation
	evtGuildBoss     = "evt_guild_boss"     // one guild boss challenge
	evtEndless       = "evt_endless"        // one endless trial run started
	evtVoyagePurple  = "evt_voyage_purple"  // one voyage order of purple quality or above taken
	evtVoyageOrange  = "evt_voyage_orange"  // one voyage order of orange quality or above taken
	evtStarTower     = "evt_star_tower"     // one challenge of the sea prison (star tower)
)

// Task status in the quest list (TaskConst.task_status).
const (
	questOpen    = 0
	questDone    = 1 // finished, reward not taken
	questClaimed = 2
)

func sortedQuestIDs() []int64 {
	ids := make([]int64, 0, len(questTable))
	for id := range questTable {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func questMessage(id int64, q questDef, row store.QuestRow, day int64) proto.Message {
	status := questOpen
	switch {
	case row.Claimed:
		status = questClaimed
	case row.Value >= q.TargetVal:
		status = questDone
	}
	return proto.Message{
		"id": id, "finish": status, "accept_time": dayStart(dayTime(day)).Unix(),
		"progress": []proto.Message{{
			"id": 1, "finish": b2i(row.Value >= q.TargetVal), "target": q.Target, "target_val": q.TargetVal,
			"value": row.Value, "ext_data": []proto.Message{},
		}},
	}
}

// questRecord counts n events for the role's daily quests with that label,
// without telling the client, and returns the ids of the quests it changed.
func questRecord(st *store.Store, rid, day int64, label string, n int64) ([]int64, error) {
	var changed []int64
	for _, id := range sortedQuestIDs() {
		q := questTable[id]
		if q.Label != label {
			continue
		}
		before, err := st.QuestRows(rid, day)
		if err != nil {
			return nil, err
		}
		if before[id].Claimed || before[id].Value >= q.TargetVal {
			continue
		}
		if _, err := st.AddQuestProgress(rid, id, day, n, q.TargetVal); err != nil {
			return nil, err
		}
		changed = append(changed, id)
	}
	return changed, nil
}

// questEvent counts an event and pushes the changed quests (10403) to the player.
func questEvent(s *gateway.Session, st *store.Store, rid int64, label string, n int64) error {
	lifeCount(st, rid, label, n)
	if err := activityEvent(s, st, rid, label, n); err != nil {
		return err
	}
	if err := sevenGoalEvent(s, st, s.Role(), label, n, true); err != nil {
		return err
	}
	day := dayKey(s.Now())
	changed, err := questRecord(st, rid, day, label, n)
	if err != nil || len(changed) == 0 {
		return err
	}
	rows, err := st.QuestRows(rid, day)
	if err != nil {
		return err
	}
	list := make([]proto.Message, 0, len(changed))
	for _, id := range changed {
		list = append(list, questMessage(id, questTable[id], rows[id], day))
	}
	return s.Send(10403, proto.Message{"quest_list": list})
}

// RegisterQuest registers the daily quests: 10400 (the list) and 10406 / 10402
// (take a finished quest's reward). Progress comes
// from questEvent / questRecord calls in the systems that have the events.
func RegisterQuest(r *gateway.Router, st *store.Store) {
	r.Handle(10400, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		day := dayKey(s.Now())
		rows, err := st.QuestRows(role.RID, day)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range sortedQuestIDs() {
			list = append(list, questMessage(id, questTable[id], rows[id], day))
		}
		return s.Send(10400, proto.Message{"quest_list": list})
	})
	claim := func(cmd uint16) gateway.Handler {
		return func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			id := req.Int("id")
			reply := func(flag int, msg string) error {
				return s.Send(cmd, proto.Message{"flag": flag, "msg": msg, "id": id})
			}
			q, ok := questTable[id]
			if !ok {
				return reply(0, "任务不存在")
			}
			day := dayKey(s.Now())
			assets, items := map[int64]int64{}, map[int64]int64{}
			for _, rw := range q.Rewards {
				splitGains([]sellGain{{rw[0], rw[1]}}, 1, assets, items)
			}
			if err := st.ClaimQuest(role.RID, id, day, q.TargetVal, assets, items); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return reply(0, "任务未完成或奖励已领取")
				}
				return err
			}
			if err := reply(1, "领取成功"); err != nil {
				return err
			}
			rows, err := st.QuestRows(role.RID, day)
			if err != nil {
				return err
			}
			if err := s.Send(10403, proto.Message{"quest_list": []proto.Message{questMessage(id, q, rows[id], day)}}); err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			return sendBackpack(s, st)
		}
	}
	// The client's task window submits with 10406 (TaskController.requestSubmitTask,
	// answered by on10406); 10402 has the same shape and is kept.
	r.Handle(10406, claim(10406))
	r.Handle(10402, claim(10402))
}
