package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// levgift.json: the level-up gifts (21200, 21201) and the time collect activity (16697, 16698) of the client
// (tools/extract_levgift.py).
//
//go:embed data/levgift.json
var levGiftJSON []byte

const (
	lgModule      = "levgift"
	lgPanelType   = 15 // ActionPanelTypeView: ActionAccLevelUpGiftPanel
	tcModule      = "timecollect"
	lgStatusReady = 1
	lgStatusTaken = 2
)

var lgCfg = func() (c struct {
	Gifts []struct {
		ID, Lev, Num int64
		Reward       [][2]int64
	}
	Collect struct {
		Award    [][2]int64
		From, To int64
		Camps    map[string][]struct{ ID, Pro int64 }
	}
}) {
	if err := json.Unmarshal(levGiftJSON, &c); err != nil || len(c.Gifts) == 0 || len(c.Collect.Camps) == 0 {
		panic("game: bad levgift.json")
	}
	return
}()

// openByPanel is the first open activity of a server that shows the client panel of a type.
func openByPanel(st *store.Store, srv string, panel int64, now time.Time) (actEntry, bool) {
	es, err := openActivities(st, srv, now)
	if err != nil {
		return actEntry{}, false
	}
	for _, e := range es {
		if e.Spec.PanelType == panel {
			return e, true
		}
	}
	return actEntry{}, false
}

type lgWorld struct {
	Start int64
	Taken map[int64]int64 // gift id -> how many of the server took it
}

type lgRole struct {
	Start int64
	Taken []int64
}

var lgMu sync.Mutex

// RegisterLevGift registers the level-up gifts (an operator opens an activity with panel_type 15): a gift for each
// level, a limited number of each for the whole server, one per role. And the time collect (16697, 16698): in the hours
// of the day the activity gives one item once a day; the client never asks for 16697, the server pushes it.
func RegisterLevGift(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *lgWorld, *lgRole, bool, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil, false, nil
		}
		if fresh, err := st.RoleByID(role.RID); err == nil {
			role = fresh
		}
		w, d := &lgWorld{}, &lgRole{}
		e, open := openByPanel(st, role.SrvID, lgPanelType, s.Now())
		if !open {
			return role, w, d, false, nil
		}
		if _, err := st.LoadGlobal(role.SrvID, lgModule, w); err != nil {
			return nil, nil, nil, false, err
		}
		if _, err := st.LoadState(role.RID, lgModule, d); err != nil {
			return nil, nil, nil, false, err
		}
		if w.Start != e.Row.StartAt || w.Taken == nil {
			*w = lgWorld{Start: e.Row.StartAt, Taken: map[int64]int64{}}
		}
		if d.Start != e.Row.StartAt {
			*d = lgRole{Start: e.Row.StartAt}
		}
		return role, w, d, true, nil
	}
	list := func(role *store.Role, w *lgWorld, d *lgRole) []proto.Message {
		out := []proto.Message{}
		for _, g := range lgCfg.Gifts {
			status := int64(0)
			if int64(role.Lev) >= g.Lev {
				status = lgStatusReady
			}
			if has(d.Taken, g.ID) {
				status = lgStatusTaken
			}
			out = append(out, proto.Message{"id": g.ID, "status": status, "num": w.Taken[g.ID]})
		}
		return out
	}
	r.Handle(21200, func(s *gateway.Session, _ proto.Message) error {
		lgMu.Lock()
		defer lgMu.Unlock()
		role, w, d, open, err := load(s)
		if role == nil {
			return err
		}
		if !open {
			return s.Send(21200, proto.Message{"gifts": []proto.Message{}})
		}
		return s.Send(21200, proto.Message{"gifts": list(role, w, d)})
	})
	r.Handle(21201, func(s *gateway.Session, req proto.Message) error {
		lgMu.Lock()
		defer lgMu.Unlock()
		role, w, d, open, err := load(s)
		if role == nil {
			return err
		}
		id := req.Int("id")
		refuse := func(msg string) error { return s.Send(21201, proto.Message{"code": 0, "msg": msg, "id": id}) }
		idx := -1
		for i, g := range lgCfg.Gifts {
			if g.ID == id {
				idx = i
			}
		}
		switch {
		case !open:
			return refuse("活动未开启")
		case idx < 0:
			return refuse("礼包不存在")
		case int64(role.Lev) < lgCfg.Gifts[idx].Lev:
			return refuse("等级不足")
		case has(d.Taken, id):
			return refuse("已经领取过了")
		case w.Taken[id] >= lgCfg.Gifts[idx].Num:
			return refuse("礼包已被领完")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range lgCfg.Gifts[idx].Reward {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		w.Taken[id]++
		d.Taken = append(d.Taken, id)
		if err := st.SaveGlobal(role.SrvID, lgModule, w); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, lgModule, d); err != nil {
			return err
		}
		if err := s.Send(21201, proto.Message{"code": 1, "msg": "", "id": id}); err != nil {
			return err
		}
		if err := s.Send(21200, proto.Message{"gifts": list(role, w, d)}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})

	// the time collect
	type tcRole struct{ Day int64 }
	collect := func(s *gateway.Session) (role *store.Role, camp, day int64, ok bool, err error) {
		if role = s.Role(); role == nil {
			return nil, 0, 0, false, nil
		}
		now := s.Now()
		hour := int64(now.In(dayZone).Hour())
		if hour < lgCfg.Collect.From || hour >= lgCfg.Collect.To {
			return role, 0, 0, false, nil
		}
		for k := range lgCfg.Collect.Camps {
			id, _ := strconv.ParseInt(k, 10, 64)
			if _, _, on := ActivityOpen(st, role.SrvID, id, now); on {
				return role, id, dayKey(now), true, nil
			}
		}
		return role, 0, 0, false, nil
	}
	taken := func(rid, day int64) (bool, error) {
		d := &tcRole{}
		if _, err := st.LoadState(rid, tcModule, d); err != nil {
			return false, err
		}
		return d.Day == day, nil
	}
	r.Handle(16697, func(s *gateway.Session, _ proto.Message) error {
		role, camp, day, ok, err := collect(s)
		if role == nil {
			return err
		}
		if ok {
			if done, err := taken(role.RID, day); err != nil {
				return err
			} else if done {
				ok = false
			}
		}
		if !ok {
			return s.Send(16697, proto.Message{"code": 0, "msg": "", "camp_id": 0, "id": 0})
		}
		// the figure that talks: by the weights of the table (all zero: the first)
		figs := lgCfg.Collect.Camps[strconv.FormatInt(camp, 10)]
		sort.Slice(figs, func(i, j int) bool { return figs[i].ID < figs[j].ID })
		var total int64
		for _, f := range figs {
			total += f.Pro
		}
		pick := figs[0].ID
		if total > 0 {
			n := rand.Int63n(total)
			for _, f := range figs {
				if n < f.Pro {
					pick = f.ID
					break
				}
				n -= f.Pro
			}
		}
		return s.Send(16697, proto.Message{"code": 1, "msg": "", "camp_id": camp, "id": pick})
	})
	r.Handle(16698, func(s *gateway.Session, _ proto.Message) error {
		role, _, day, ok, err := collect(s)
		if role == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(16698, proto.Message{"code": 0, "msg": msg}) }
		if !ok {
			return refuse("活动未开启")
		}
		if done, err := taken(role.RID, day); err != nil {
			return err
		} else if done {
			return refuse("今天已经领取过了")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range lgCfg.Collect.Award {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, tcModule, &tcRole{Day: day}); err != nil {
			return err
		}
		if err := s.Send(16698, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}

// openByBID is the open activity of a server with an activity number (bid) the client knows.
func openByBID(st *store.Store, srv string, bid int64, now time.Time) (actEntry, bool) {
	es, err := openActivities(st, srv, now)
	if err != nil {
		return actEntry{}, false
	}
	for _, e := range es {
		if e.Row.BID == bid {
			return e, true
		}
	}
	return actEntry{}, false
}
