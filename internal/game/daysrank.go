package game

import (
	_ "embed"
	"encoding/json"
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

const (
	drModule       = "daysrank"
	drIcon         = 108
	drH5PartnerKey = "partner_h5"
	drH5ArenaKey   = "arena_h5"
	drH5PowerKey   = "power_h5"
)

type drQuest struct {
	Desc, Label string
	Param       []any
	Target      int64
	TargetVal   int64 `json:"target_val"`
	Rewards     [][2]int64
}

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
	Quests   map[string]drQuest
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
	return drServerStart(*sv), nil
}

func drServerStart(sv store.Server) time.Time {
	at := sv.OpenTime
	if at == 0 {
		at = sv.CreatedAt
	}
	return dayStart(time.Unix(at, 0))
}

// drEnd is when the window ends: the last second of the open day the table names.
func drEnd(start time.Time) int64 { return start.Unix() + drCfg.Days*86400 - 1 }

func drOpen(start time.Time, now time.Time) bool {
	return !now.Before(start) && now.Unix() < drEnd(start)
}

func daysRankIconOpen(st *store.Store, srv string, now time.Time) (bool, error) {
	start, err := drStart(st, srv)
	return err == nil && drOpen(start, now), err
}

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

type drFrozen struct {
	RID, Val int64
	Role     *store.RankRole `json:",omitempty"`
}

type drWorld struct {
	Settled bool
	Frozen  map[string][]drFrozen
}

var drMu sync.Mutex

func advanceDaysRank(st *store.Store, srv string, now time.Time) (bool, error) {
	start, err := drStart(st, srv)
	if err != nil || now.Unix() < drEnd(start) {
		return false, err
	}
	return settleDaysRank(st, srv, now)
}

func settleDaysRank(st *store.Store, srv string, now time.Time) (bool, error) {
	var status struct{ Settled bool }
	if _, err := st.LoadGlobal(srv, drModule, &status); err != nil || status.Settled {
		return status.Settled, err
	}
	drMu.Lock()
	defer drMu.Unlock()
	var settled bool
	err := st.DaysRankSettlement(srv, func(tx *store.Store) error {
		start, err := drStart(tx, srv)
		if err != nil || now.Unix() < drEnd(start) {
			return err
		}
		world := &drWorld{}
		if _, err := tx.LoadGlobal(srv, drModule, world); err != nil {
			return err
		}
		if world.Settled {
			settled = true
			return nil
		}
		if err := world.settle(tx, srv); err != nil {
			return err
		}
		settled = true
		return tx.SaveGlobal(srv, drModule, world)
	})
	return settled && err == nil, err
}

func drH5Pool(typ int64) string {
	switch typ {
	case rankPartner:
		return drH5PartnerKey
	case rankArena:
		return drH5ArenaKey
	case rankPower:
		return drH5PowerKey
	}
	return ""
}

// settle freezes the rankings at the end and mails the rewards by place, once. The first place of a ranking whose value
// is under the table's limit gets nothing.
func (w *drWorld) settle(st *store.Store, srv string) error {
	w.Frozen = map[string][]drFrozen{}
	for _, rk := range drCfg.Ranks {
		typ := drTypes[rk.ID]
		key := strconv.FormatInt(rk.ID, 10)
		pools := []string{key}
		if h5Pool := drH5Pool(typ); h5Pool != "" {
			pools = append(pools, h5Pool)
		}
		for _, pool := range pools {
			list, err := daysRankStandings(st, srv, typ, pool != key)
			if err != nil {
				return err
			}
			frozen, err := freezeDaysRank(st, srv, rk.ID, rk.Name, list)
			if err != nil {
				return err
			}
			w.Frozen[pool] = frozen
		}
	}
	w.Settled = true
	return nil
}

func freezeDaysRank(st *store.Store, srv string, id int64, name string, list []rankEntry) ([]drFrozen, error) {
	var frozen []drFrozen
	for i, e := range list[:min(int(drCfg.LimitCount), len(list))] {
		frozen = append(frozen, drFrozen{RID: e.role.RID, Val: e.val, Role: &e.role})
		place := int64(i + 1)
		if place == 1 && !drQualifies(id, e.val) {
			continue
		}
		for _, tier := range drCfg.Rewards[strconv.FormatInt(id, 10)] {
			if place >= tier.Low && place <= tier.High {
				m := store.Mail{SrvID: srv, Subject: "开服冲榜·" + name + "奖励", Content: "开服冲榜“" + name + "”已结束，你排在第" + itoa64(place) + "名，这是排名奖励。",
					FromName: "开服冲榜", CreatedBy: "daysrank", Attachments: attachmentsOf(tier.Rewards)}
				if _, err := st.CreateMail(m, []int64{e.role.RID}); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return frozen, nil
}

func RegisterDaysRank(r *gateway.Router, st *store.Store) {
	bindAdminGlobal(st, drModule, &drMu, nil)
	var settledRealms sync.Map
	r.BeforeCommand(func(s *gateway.Session) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if _, seen := settledRealms.Load(role.SrvID); seen {
			return nil
		}
		settled, err := advanceDaysRank(st, role.SrvID, s.Now())
		if settled && err == nil {
			settledRealms.Store(role.SrvID, true)
		}
		return err
	})
	r.Handle(22700, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		start, err := drStart(st, role.SrvID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		if req.Int("is_cluster") == 0 && drOpen(start, s.Now()) {
			for _, rk := range drCfg.Ranks {
				list = append(list, proto.Message{"id": rk.ID, "end_time": drEnd(start)})
			}
		}
		return s.Send(22700, proto.Message{"is_cluster": req.Int("is_cluster"), "rank_list": list})
	})
	r.Handle(22701, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		start, err := drStart(st, role.SrvID)
		if err != nil {
			return err
		}
		id, now := req.Int("id"), s.Now()
		typ, ok := drTypes[id]
		if !ok || now.Before(start) {
			return s.Send(22701, proto.Message{"id": id, "end_time": drEnd(start), "rank": 0, "rank_list": []proto.Message{}})
		}
		var ents []rankEntry
		myRank := int64(0)
		h5 := drH5Pool(typ) != "" && isH5RoleSession(s)
		if now.Unix() >= drEnd(start) {
			if _, err := settleDaysRank(st, role.SrvID, now); err != nil {
				return err
			}
			world := &drWorld{}
			if _, err := st.LoadGlobal(role.SrvID, drModule, world); err != nil {
				return err
			}
			ents, err = world.standings(st, role.SrvID, id, typ, h5)
			if err != nil {
				return err
			}
			for i, e := range ents {
				if e.role.RID == role.RID {
					myRank = int64(i + 1)
				}
			}
		} else {
			key := rankKey{st: st, srv: role.SrvID, typ: typ, h5Pool: h5, daysRank: true}
			full, err := key.snapshot(now.Unix())
			if err != nil {
				return err
			}
			idx, err := daysRankPosition(st, role, typ, h5, full)
			if err != nil {
				return err
			}
			if idx <= drCfg.LimitCount {
				myRank = idx
			}
			ents = full[:min(int(drCfg.LimitCount), len(full))]
		}
		rows := []proto.Message{}
		for i, e := range ents {
			rows = append(rows, proto.Message{"idx": i + 1, "name": e.role.Name, "lev": e.role.Lev, "face_id": e.role.FaceID, "avatar_id": 0,
				"rid": e.role.RID, "srv_id": role.SrvID})
		}
		return s.Send(22701, proto.Message{"id": id, "end_time": drEnd(start), "rank": myRank, "rank_list": rows})
	})
	registerDaysRankTasks(r, st)
}

// Register the persisted state without loading or changing gameplay data.
func init() {
	registerAdminModule(AdminModule{Key: drModule, Title: "七天排行", Window: "22700–22704", Scope: "global", View: adminGlobalView, Rank: globalRankFor(drModule), New: adminState[drWorld]})
	registerAdminModule(AdminModule{Key: drModule, Title: "七天排行", Window: "22702–22704", Scope: "role", New: adminState[store.DaysRankRole]})
}
