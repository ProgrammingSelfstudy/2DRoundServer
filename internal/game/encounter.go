package game

import (
	_ "embed"
	"encoding/json"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// encounter.json: the hero stories of the client (tools/extract_encounter.py).
//
//go:embed data/encounter.json
var encounterJSON []byte

const encounterModule = "encounter"

type encounterConf struct {
	Level   int64      `json:"level"`
	Windows [][2]int64 `json:"windows"`
	Stories []struct {
		ID     int64              `json:"id"`
		BID    int64              `json:"bid"`
		Weight int64              `json:"weight"`
		Lev    int64              `json:"lev"`
		Power  int64              `json:"power"`
		Pages  int64              `json:"pages"`
		Choice int64              `json:"choices"`
		Reward map[string][]int64 `json:"reward"`
	} `json:"stories"`
}

var encounterCfg = func() *encounterConf {
	c := &encounterConf{}
	if err := json.Unmarshal(encounterJSON, c); err != nil || len(c.Stories) == 0 || len(c.Windows) == 0 {
		panic("game: bad encounter.json")
	}
	return c
}()

type encounterState struct {
	Window int64 // the refresh window already rolled, day*10+index
	Cur    int64
	Page   int64
	Choice int64
	Done   []int64
}

type encounterSvc struct{ st *store.Store }

// RegisterEncounter registers the hero stories (27100-27104): in the two daily windows of the client a story of a hero
// the role owns may turn up (by weight, with the level and power it asks for); the role reads it page by page, picks an
// answer where there is one and finishes it; the finished ones form the library. Our rules, not captured.
func RegisterEncounter(r *gateway.Router, db *store.Store) {
	e := &encounterSvc{st: db}
	r.Handle(27100, e.info)
	r.Handle(27101, e.finish)
	r.Handle(27102, e.library)
	r.Handle(27103, e.page)
	r.Handle(27104, e.choose)
}

// window is the refresh window now (day*10+index+1), 0 outside the windows.
func (st *encounterState) window(now time.Time) int64 {
	n := now.In(dayZone)
	sec := int64(n.Hour()*3600 + n.Minute()*60 + n.Second())
	for i, w := range encounterCfg.Windows {
		if sec >= w[0] && sec < w[1] {
			return dayKey(now)*10 + int64(i) + 1
		}
	}
	return 0
}

func (st *encounterState) done(id int64) bool {
	for _, d := range st.Done {
		if d == id {
			return true
		}
	}
	return false
}

func (e *encounterSvc) with(s *gateway.Session, fn func(role *store.Role, st *encounterState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &encounterState{}
	if _, err := e.st.LoadState(role.RID, encounterModule, st); err != nil {
		return err
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, encounterModule, st)
}

// roll puts a new story in the open slot once in each window: the stories of the heroes the role owns, that it has not
// read, whose level and power it reaches, drawn by weight.
func (e *encounterSvc) roll(s *gateway.Session, role *store.Role, st *encounterState) error {
	w := st.window(s.Now())
	if w == 0 || w == st.Window || st.Cur != 0 || int64(role.Lev) < encounterCfg.Level {
		return nil
	}
	st.Window = w
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	own := map[int64]bool{}
	for _, h := range heroes {
		own[h.BID] = true
	}
	power := rolePower(heroes)
	var pool []int
	var total int64
	for i, c := range encounterCfg.Stories {
		if own[c.BID] && !st.done(c.ID) && int64(role.Lev) >= c.Lev && power >= c.Power && c.Weight > 0 {
			pool, total = append(pool, i), total+c.Weight
		}
	}
	if len(pool) == 0 {
		return nil
	}
	pick := newRng().Int63n(total)
	for _, i := range pool {
		if pick -= encounterCfg.Stories[i].Weight; pick < 0 {
			st.Cur, st.Page, st.Choice = encounterCfg.Stories[i].ID, 1, 0
			return nil
		}
	}
	return nil
}

func (e *encounterSvc) info(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *encounterState) error {
		if err := e.roll(s, role, st); err != nil {
			return err
		}
		return s.Send(27100, proto.Message{"id": st.Cur, "page": st.Page})
	})
}

// page (27103) remembers how far the role has read; the client does not wait for a reply.
func (e *encounterSvc) page(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(_ *store.Role, st *encounterState) error {
		if p := req.Int("page"); st.Cur != 0 && req.Int("id") == st.Cur && p > st.Page && p <= storyPages(st.Cur) {
			st.Page = p
		}
		return nil
	})
}

func storyPages(id int64) int64 {
	for _, c := range encounterCfg.Stories {
		if c.ID == id {
			return c.Pages
		}
	}
	return 0
}

// choose (27104) takes the answer of the story being read.
func (e *encounterSvc) choose(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(_ *store.Role, st *encounterState) error {
		for _, c := range encounterCfg.Stories {
			if c.ID != st.Cur {
				continue
			}
			if n := req.Int("choice"); n >= 1 && n <= c.Choice {
				st.Choice = n
				return s.Send(27104, proto.Message{"msg": "选择成功"})
			}
		}
		return s.Send(27104, proto.Message{"msg": "没有可选择的物语"})
	})
}

// finish (27101) closes the story being read and puts it in the library. The reward ids of the client table are not
// items the client knows, so nothing is handed out for them.
func (e *encounterSvc) finish(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(_ *store.Role, st *encounterState) error {
		if st.Cur == 0 {
			return s.Send(27101, proto.Message{"msg": "没有可领取的物语"})
		}
		st.Done = append(st.Done, st.Cur)
		st.Cur, st.Page, st.Choice = 0, 0, 0
		return s.Send(27101, proto.Message{"msg": "领取成功"})
	})
}

// library (27102) is the finished stories.
func (e *encounterSvc) library(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(_ *store.Role, st *encounterState) error {
		list := []proto.Message{}
		for _, id := range st.Done {
			list = append(list, proto.Message{"id": id})
		}
		return s.Send(27102, proto.Message{"id_list": list})
	})
}
