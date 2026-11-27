package game

import (
	"errors"
	"maps"
	"slices"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

var drQuests = func() map[int64]drQuest {
	quests := make(map[int64]drQuest)
	for _, rank := range drCfg.Ranks {
		for _, id := range drCfg.QuestsOf[strconv.FormatInt(rank.ID, 10)] {
			quest, found := drCfg.Quests[strconv.FormatInt(id, 10)]
			if !found || quest.TargetVal <= 0 {
				panic("game: missing daysrank task")
			}
			quests[id] = quest
		}
	}
	if len(quests) != 14 {
		panic("game: expected 14 daysrank tasks")
	}
	return quests
}()

var drQuestIDs = slices.Sorted(maps.Keys(drQuests))

type drProgress struct {
	dungeon, gold, essence, explored int64
	broken                           []store.Partner
}

func loadDaysRankProgress(st *store.Store, rid int64) (drProgress, error) {
	var progress drProgress
	drama, err := st.Drama(rid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return progress, err
	}
	if err == nil {
		progress.dungeon = clearedMax(*drama)
	}
	progress.broken, err = st.PartnerBreaks(rid)
	if err != nil {
		return progress, err
	}
	var adventure struct{ Explored int64 }
	if _, err := st.LoadState(rid, advModule, &adventure); err != nil {
		return progress, err
	}
	progress.explored = adventure.Explored
	progress.gold, progress.essence, err = st.DaysRankSpending(rid)
	return progress, err
}

func (p drProgress) value(quest drQuest, h5 bool) int64 {
	switch quest.Label {
	case "evt_dungeon_pass":
		index, found := dungeonIndex[p.dungeon]
		return int64(b2i(found && index >= dungeonReach(quest.Target)))
	case "evt_partner":
		var step int
		if len(quest.Param) == 1 {
			if pair, ok := quest.Param[0].([]any); ok && len(pair) == 2 {
				value, _ := pair[1].(float64)
				step = int(value)
			}
		}
		var count int64
		for _, hero := range p.broken {
			if hero.BreakLev >= step && (!h5 || h5HeroEligible(hero.BID, hero.Star)) {
				count++
			}
		}
		return count
	case "evt_loss_hallows_essence":
		return p.essence
	case "evt_adventure_explore":
		return p.explored
	case "evt_loss_gold":
		return p.gold
	}
	return 0
}

type drTask struct{ ID, Finish, Value, End int64 }

func (task drTask) message() proto.Message {
	quest := drQuests[task.ID]
	return proto.Message{"id": task.ID, "finish": task.Finish, "end_time": task.End, "finish_time": 0,
		"progress": []proto.Message{{"id": 1, "finish": b2i(task.Finish != questOpen), "target": quest.Target, "target_val": quest.TargetVal, "value": task.Value}}}
}

func daysRankTasks(st *store.Store, rid, end int64, h5 bool) ([]drTask, error) {
	var state store.DaysRankRole
	if _, err := st.LoadState(rid, drModule, &state); err != nil {
		return nil, err
	}
	progress, err := loadDaysRankProgress(st, rid)
	if err != nil {
		return nil, err
	}
	tasks := make([]drTask, 0, len(drQuestIDs))
	for _, id := range drQuestIDs {
		quest := drQuests[id]
		value := min(progress.value(quest, h5), quest.TargetVal)
		finish := int64(questOpen)
		switch {
		case slices.Contains(state.Claimed, id):
			finish, value = questClaimed, quest.TargetVal
		case value == quest.TargetVal:
			finish = questDone
		}
		tasks = append(tasks, drTask{id, finish, value, end})
	}
	return tasks, nil
}

func daysRankTaskDelta(before, after []drTask) []proto.Message {
	var changed []proto.Message
	for idx, task := range after {
		previous := drTask{ID: task.ID, End: task.End, Finish: questOpen}
		if idx < len(before) {
			previous = before[idx]
		}
		if previous != task {
			changed = append(changed, task.message())
		}
	}
	return changed
}

var (
	errDaysRankClosed     = errors.New("daysrank closed")
	errDaysRankIncomplete = errors.New("daysrank task incomplete")
)

func registerDaysRankTasks(r *gateway.Router, st *store.Store) {
	r.Handle(22703, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		start, err := drStart(st, role.SrvID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		if drOpen(start, s.Now()) {
			tasks, err := daysRankTasks(st, role.RID, drEnd(start), isH5RoleSession(s))
			if err != nil {
				return err
			}
			for _, task := range tasks {
				list = append(list, task.message())
			}
		}
		return s.Send(22703, proto.Message{"feat_list": list})
	})
	r.Handle(22704, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("quest_id")
		refuse := func(msg string) error { return s.Send(22704, proto.Message{"result": 0, "msg": msg, "quest_id": id}) }
		quest, found := drQuests[id]
		if !found {
			return refuse("任务不存在")
		}
		start, err := drStart(st, role.SrvID)
		if err != nil {
			return err
		}
		now := s.Now()
		if now.Before(start) {
			return refuse("活动未开放")
		}
		if now.Unix() >= drEnd(start) {
			return refuse("活动已结束")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(quest.Rewards), 1, assets, items)
		h5 := isH5RoleSession(s)
		err = st.ClaimDaysRankTask(role.RID, id, assets, items, func(tx *store.Store) error {
			progress, err := loadDaysRankProgress(tx, role.RID)
			if err != nil {
				return err
			}
			open, err := daysRankIconOpen(tx, role.SrvID, s.Now())
			if err != nil {
				return err
			}
			if !open {
				return errDaysRankClosed
			}
			if progress.value(quest, h5) < quest.TargetVal {
				return errDaysRankIncomplete
			}
			return nil
		})
		switch {
		case errors.Is(err, store.ErrConflict):
			return refuse("已经领取过了")
		case errors.Is(err, errDaysRankIncomplete):
			return refuse("任务未完成")
		case errors.Is(err, errDaysRankClosed):
			return refuse("活动已结束")
		case err != nil:
			return err
		}
		if err := s.Send(22704, proto.Message{"result": 1, "msg": "领取成功", "quest_id": id}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
