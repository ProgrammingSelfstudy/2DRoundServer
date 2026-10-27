package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// daysrank.json: the seven-day ranking (开服冲榜, 22700~22704) of the client (tools/extract_daysrank.py).
//
//go:embed data/daysrank.json
var daysRankJSON []byte

const drModule = "daysrank"

var drCfg = func() (c struct {
	Days       int64
	LimitCount int64 `json:"limit_count"`
	Limits     map[string]int64
	Ranks      []struct {
		ID, View int64
		Name     string
	}
	Rewards map[string][]struct {
		Low, High int64
		Rewards   [][2]int64
	}
	Quests map[string]struct {
		Desc, Label string
		Param       []any
		Target      int64
		TargetVal   int64 `json:"target_val"`
		Rewards     [][2]int64
	}
	QuestsOf map[string][]int64 `json:"quests_of"`
}) {
	if err := json.Unmarshal(daysRankJSON, &c); err != nil || len(c.Ranks) != 7 || len(c.Quests) == 0 {
		panic("game: bad daysrank.json")
	}
	return
}()

// drTypes is the list each ranking of the window is made of.
var drTypes = map[int64]int64{1: rankDrama, 2: rankTower, 3: rankTreasure, 4: rankPartner, 5: rankArena, 6: rankEndless, 7: rankPower}

// daysRankValues is the value of every role of a server in the rankings that need more than a table: the strongest single
// hero, the draws of the lucky treasure and the highest round of the endless trial.
func daysRankValues(st *store.Store, srvID string, typ int64, roles []store.RankRole) (map[int64]int64, error) {
	out := map[int64]int64{}
	if typ == rankPartner {
		heroes, err := st.PartnersOfServer(srvID)
		if err != nil {
			return nil, err
		}
		for _, r := range roles {
			for _, h := range heroes[r.RID] {
				out[r.RID] = max(out[r.RID], partnerMessage(h).Int("power"))
			}
		}
		return out, nil
	}
	for _, r := range roles {
		v, err := daysRankLive(st, r.RID, typ)
		if err != nil {
			return nil, err
		}
		out[r.RID] = v
	}
	return out, nil
}

// daysRankLive is one role's value in one of those rankings now.
func daysRankLive(st *store.Store, rid, typ int64) (int64, error) {
	switch typ {
	case rankPartner:
		heroes, err := st.PartnersOf(rid)
		var best int64
		for _, h := range heroes {
			best = max(best, partnerMessage(h).Int("power"))
		}
		return best, err
	case rankTreasure:
		d := &trRole{}
		if _, err := st.LoadState(rid, trModule, d); err != nil {
			return 0, err
		}
		return d.Wheels[0].Count + d.Wheels[1].Count, nil
	case rankEndless:
		d := &endState{}
		if _, err := st.LoadState(rid, endlessModule, d); err != nil {
			return 0, err
		}
		var best int64
		for _, m := range d.Modes {
			best = max(best, m.Max)
		}
		return best, nil
	}
	return 0, nil
}

// drStart is the first day of the window: the day the server opened.
func drStart(st *store.Store, srv string) (time.Time, error) {
	sv, err := st.Server(srv)
	if err != nil {
		return time.Time{}, err
	}
	at := sv.OpenTime
	if at == 0 {
		at = sv.CreatedAt
	}
	return dayStart(time.Unix(at, 0)), nil
}

// drEnd is when the window ends: the last second of the open day the table names.
func drEnd(start time.Time) int64 { return start.Unix() + drCfg.Days*86400 - 1 }

// dungeonReach is the place in the chain of the first dungeon with an id from the given one on: the table names a few ids
// the chain does not have (10420), and reaching them means reaching what comes after.
func dungeonReach(id int64) int {
	for i, d := range dramaChain.Dungeons {
		if d.ID >= id {
			return i
		}
	}
	return len(dramaChain.Dungeons)
}

// drQualifies says whether the first place of a ranking reached the value the table asks of it.
func drQualifies(id, val int64) bool {
	lim := drCfg.Limits
	switch id {
	case 1:
		return dungeonIndex[val] >= dungeonReach(lim["drama"])
	case 2:
		return val >= lim["tower"]
	case 4:
		return val >= lim["partner"]
	case 5:
		return val >= lim["arena"]
	case 7:
		return val >= lim["team"]
	}
	return true
}

type drFrozen struct{ RID, Val int64 }

type drWorld struct {
	Settled bool
	Frozen  map[string][]drFrozen // ranking id -> the first places at the end
}

type drRole struct{ Claimed []int64 }

var drMu sync.Mutex

// settle freezes the rankings at the end and mails the rewards by place, once. The first place of a ranking whose value
// is under the table's limit gets nothing.
func (w *drWorld) settle(st *store.Store, srv string) error {
	w.Frozen = map[string][]drFrozen{}
	for _, rk := range drCfg.Ranks {
		list, err := ranking(st, srv, drTypes[rk.ID])
		if err != nil {
			return err
		}
		var frozen []drFrozen
		for i, e := range list {
			if int64(i) >= drCfg.LimitCount {
				break
			}
			frozen = append(frozen, drFrozen{e.role.RID, e.val})
			place := int64(i + 1)
			if place == 1 && !drQualifies(rk.ID, e.val) {
				continue
			}
			for _, tier := range drCfg.Rewards[strconv.FormatInt(rk.ID, 10)] {
				if place >= tier.Low && place <= tier.High {
					m := store.Mail{SrvID: srv, Subject: "开服冲榜·" + rk.Name + "奖励", Content: "开服冲榜“" + rk.Name + "”已结束，你排在第" + itoa64(place) + "名，这是排名奖励。",
						FromName: "开服冲榜", CreatedBy: "daysrank", Attachments: attachmentsOf(tier.Rewards)}
					if _, err := st.CreateMail(m, []int64{e.role.RID}); err != nil {
						return err
					}
					break
				}
			}
		}
		w.Frozen[strconv.FormatInt(rk.ID, 10)] = frozen
	}
	w.Settled = true
	return nil
}

// daysQuestValue is what a quest counts for a role now; the events this server cannot measure stay at 0.
func daysQuestValue(st *store.Store, rid int64, label string, param []any, target int64) (int64, error) {
	switch label {
	case "evt_dungeon_pass":
		d, err := st.Drama(rid)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return 0, nil
			}
			return 0, err
		}
		return int64(b2i(d.MaxDunID != 0 && dungeonIndex[d.MaxDunID] >= dungeonReach(target))), nil
	case "evt_partner": // heroes at a break step
		heroes, err := st.PartnersOf(rid)
		var n, step int64
		if len(param) == 1 {
			if pair, ok := param[0].([]any); ok && len(pair) == 2 {
				f, _ := pair[1].(float64)
				step = int64(f)
			}
		}
		for _, h := range heroes {
			if int64(h.BreakLev) >= step {
				n++
			}
		}
		return n, err
	case "evt_power":
		heroes, err := st.PartnersOf(rid)
		return int64(b2i(rolePower(heroes) >= target)), err
	case "evt_arena_fight":
		_, battles, _, err := st.ArenaScoreOf(rid)
		return battles, err
	case "evt_star_tower_floor_pass":
		t, err := st.StarTowerOf(rid)
		return t.MaxTower, err
	case "evt_dial":
		return daysRankLive(st, rid, rankTreasure)
	case "evt_endless_pass":
		v, err := daysRankLive(st, rid, rankEndless)
		return int64(b2i(v >= target)), err
	}
	return 0, nil
}

// RegisterDaysRank registers the seven-day ranking: from the day the server opens, for the days of the table, seven
// rankings (campaign, star tower, lucky treasure, strongest hero, arena, endless trial, team power); at the end the first
// places are frozen and get the rewards by mail, and the first place needs the value the table asks. Beside them the
// quests of each ranking (22703, 22704). The cross-server list is empty. Titles are not given: there is no title system.
func RegisterDaysRank(r *gateway.Router, st *store.Store) {
	type ctx struct {
		role       *store.Role
		w          *drWorld
		start, end int64
		over       bool
	}
	load := func(s *gateway.Session) (*ctx, error) {
		role := s.Role()
		if role == nil {
			return nil, nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		start, err := drStart(st, role.SrvID)
		if err != nil {
			return nil, err
		}
		c := &ctx{role: role, w: &drWorld{}, start: start.Unix(), end: drEnd(start)}
		c.over = s.Now().Unix() > c.end
		if _, err := st.LoadGlobal(role.SrvID, drModule, c.w); err != nil {
			return nil, err
		}
		if c.over && !c.w.Settled {
			if err := c.w.settle(st, role.SrvID); err != nil {
				return nil, err
			}
			if err := st.SaveGlobal(role.SrvID, drModule, c.w); err != nil {
				return nil, err
			}
		}
		return c, nil
	}
	r.Handle(22700, func(s *gateway.Session, req proto.Message) error {
		drMu.Lock()
		defer drMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		list := []proto.Message{}
		if req.Int("is_cluster") == 0 {
			for _, rk := range drCfg.Ranks {
				list = append(list, proto.Message{"id": rk.ID, "end_time": c.end})
			}
		}
		return s.Send(22700, proto.Message{"is_cluster": req.Int("is_cluster"), "rank_list": list})
	})
	r.Handle(22701, func(s *gateway.Session, req proto.Message) error {
		drMu.Lock()
		defer drMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		id := req.Int("id")
		typ, ok := drTypes[id]
		if !ok {
			return s.Send(22701, proto.Message{"id": id, "end_time": c.end, "rank": 0, "rank_list": []proto.Message{}})
		}
		roles, err := st.RankRoles(c.role.SrvID)
		if err != nil {
			return err
		}
		byRID := map[int64]store.RankRole{}
		for _, x := range roles {
			byRID[x.RID] = x
		}
		var ents []rankEntry
		myRank := int64(0)
		if c.w.Settled {
			for _, f := range c.w.Frozen[strconv.FormatInt(id, 10)] {
				if x, ok := byRID[f.RID]; ok {
					ents = append(ents, rankEntry{x, f.Val})
				}
			}
			for i, e := range ents {
				if e.role.RID == c.role.RID {
					myRank = int64(i + 1)
				}
			}
		} else {
			full, err := rankingAt(st, c.role.SrvID, typ, s.Now().Unix())
			if err != nil {
				return err
			}
			idx, _, err := rankSelf(st, c.role.RID, typ, full)
			if err != nil {
				return err
			}
			if idx >= 1 && idx <= drCfg.LimitCount {
				myRank = idx
			}
			ents = full[:min(int(drCfg.LimitCount), len(full))]
		}
		rows := []proto.Message{}
		for i, e := range ents {
			rows = append(rows, proto.Message{"idx": i + 1, "name": e.role.Name, "lev": e.role.Lev, "face_id": e.role.FaceID, "avatar_id": 0,
				"rid": e.role.RID, "srv_id": c.role.SrvID})
		}
		return s.Send(22701, proto.Message{"id": id, "end_time": c.end, "rank": myRank, "rank_list": rows})
	})

	// the quests
	questList := func(c *ctx, d *drRole) ([]proto.Message, error) {
		var ids []int64
		for _, rk := range drCfg.Ranks {
			ids = append(ids, drCfg.QuestsOf[strconv.FormatInt(rk.ID, 10)]...)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out := []proto.Message{}
		for _, id := range ids {
			q := drCfg.Quests[strconv.FormatInt(id, 10)]
			v, err := daysQuestValue(st, c.role.RID, q.Label, q.Param, q.Target)
			if err != nil {
				return nil, err
			}
			status := int64(questOpen)
			switch {
			case has(d.Claimed, id):
				status = questClaimed
			case v >= q.TargetVal:
				status = questDone
			}
			out = append(out, proto.Message{"id": id, "finish": status, "end_time": c.end, "finish_time": 0,
				"progress": []proto.Message{{"id": 1, "finish": b2i(v >= q.TargetVal), "target": q.Target, "target_val": q.TargetVal, "value": min(v, q.TargetVal)}}})
		}
		return out, nil
	}
	r.Handle(22703, func(s *gateway.Session, _ proto.Message) error {
		drMu.Lock()
		defer drMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		d := &drRole{}
		if _, err := st.LoadState(c.role.RID, drModule, d); err != nil {
			return err
		}
		list, err := questList(c, d)
		if err != nil {
			return err
		}
		return s.Send(22703, proto.Message{"feat_list": list})
	})
	r.Handle(22704, func(s *gateway.Session, req proto.Message) error {
		drMu.Lock()
		defer drMu.Unlock()
		c, err := load(s)
		if c == nil {
			return err
		}
		id := req.Int("quest_id")
		refuse := func(msg string) error { return s.Send(22704, proto.Message{"result": 0, "msg": msg, "quest_id": id}) }
		q, ok := drCfg.Quests[strconv.FormatInt(id, 10)]
		d := &drRole{}
		if _, err := st.LoadState(c.role.RID, drModule, d); err != nil {
			return err
		}
		switch {
		case !ok:
			return refuse("任务不存在")
		case c.over:
			return refuse("活动已结束")
		case has(d.Claimed, id):
			return refuse("已经领取过了")
		}
		v, err := daysQuestValue(st, c.role.RID, q.Label, q.Param, q.Target)
		if err != nil {
			return err
		}
		if v < q.TargetVal {
			return refuse("任务未完成")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, p := range q.Rewards {
			splitGains([]sellGain{{p[0], p[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(c.role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Claimed = append(d.Claimed, id)
		if err := st.SaveState(c.role.RID, drModule, d); err != nil {
			return err
		}
		if err := s.Send(22704, proto.Message{"result": 1, "msg": "领取成功", "quest_id": id}); err != nil {
			return err
		}
		if err := sendAssets(s, st, c.role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
