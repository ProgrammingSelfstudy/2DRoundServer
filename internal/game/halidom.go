package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// halidom.json: the halidoms (元素信念, 22200~22205) of the client (tools/extract_halidom.py).
//
//go:embed data/halidom.json
var halidomJSON []byte

const halidomModule = "halidom"

type hdLvup struct {
	Lev      int64            `json:"lev"`
	Exp      int64            `json:"exp"`
	TotalExp int64            `json:"total_exp"`
	Loss     [][2]int64       `json:"loss"`
	ExpAttr  [][2]interface{} `json:"exp_attr"`
	Attr     [][2]interface{} `json:"attr"`
}

type hdStep struct {
	Step        int64            `json:"step"`
	Conds       [][2]interface{} `json:"conds"`
	LossFixed   [][3]int64       `json:"loss_fixed"`
	LossRand    [][3]int64       `json:"loss_rand"`
	LossItems   [][2]int64       `json:"loss_items"`
	DynamicAttr [][2]interface{} `json:"dynamic_attr"`
	FixedAttr   [][2]interface{} `json:"fixed_attr"`
}

var halidomCfg = func() (c struct {
	OpenLev int64            `json:"open_lev"`
	AttrIDs map[string]int64 `json:"attr_ids"`
	Base    []struct {
		ID, Camp int64
		Name     string
		Loss     [][3]int64
	} `json:"base"`
	MaxLev  []int64    `json:"max_lev"`
	MaxStep []int64    `json:"max_step"`
	Lvup    [][]hdLvup `json:"lvup"`
	Step    [][]hdStep `json:"step"`
}) {
	if err := json.Unmarshal(halidomJSON, &c); err != nil || len(c.Base) == 0 || len(c.Lvup) != len(c.Base) || len(c.Step) != len(c.Base) {
		panic("game: bad halidom.json")
	}
	return
}()

type hdOne struct{ Lev, Exp, Step int64 }

type hdState struct {
	Halidoms map[string]*hdOne
}

var hdMu sync.Mutex

func hdNum(v interface{}) int64 { f, _ := v.(float64); return int64(f) }

// hdAttrs is the total attribute list of a halidom (attribute id -> value): the level's base values and the exp clicks of
// the level, plus the fixed and the percent attributes of the step.
func hdAttrs(idx int, h *hdOne) []proto.Message {
	sum := map[int64]int64{}
	add := func(l [][2]interface{}, times int64) {
		for _, a := range l {
			k, _ := a[0].(string)
			sum[halidomCfg.AttrIDs[k]] += hdNum(a[1]) * times
		}
	}
	if h.Lev >= 1 && h.Lev <= int64(len(halidomCfg.Lvup[idx])) {
		row := halidomCfg.Lvup[idx][h.Lev-1]
		add(row.Attr, 1)
		if row.Exp > 0 {
			add(row.ExpAttr, h.Exp/row.Exp)
		}
	}
	if h.Step >= 1 && h.Step < int64(len(halidomCfg.Step[idx])) {
		row := halidomCfg.Step[idx][h.Step]
		add(row.DynamicAttr, 1)
		add(row.FixedAttr, 1)
	}
	var ids []int64
	for id := range sum {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []proto.Message{}
	for _, id := range ids {
		out = append(out, proto.Message{"name": id, "val": sum[id]})
	}
	return out
}

func hdMessage(idx int, h *hdOne) proto.Message {
	return proto.Message{"id": halidomCfg.Base[idx].ID, "lev": h.Lev, "exp": h.Exp, "step": h.Step, "all_attr": hdAttrs(idx, h)}
}

// hdFree are the heroes of a role that may be paid: not locked, not in a resonance slot, not in the campaign team.
func hdFree(st *store.Store, rid int64) (map[int64]store.Partner, error) {
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return nil, err
	}
	inForm := map[int64]bool{}
	if f, err := st.Formation(rid, formDrama); err == nil {
		for _, sl := range f.Slots {
			inForm[sl.PartnerID] = true
		}
	}
	free := map[int64]store.Partner{}
	for _, h := range heroes {
		if !h.Locked && h.ResSlot == 0 && !inForm[h.PartnerID] {
			free[h.PartnerID] = h
		}
	}
	return free, nil
}

// hdPick checks that the chosen heroes pay the needs [key, star, count] exactly, where key is read by match (a hero bid or a
// camp). It returns the heroes to consume, or false.
func hdPick(free map[int64]store.Partner, ids []int64, needs [][3]int64, match func(h store.Partner, key int64) bool) ([]int64, bool) {
	used := map[int64]bool{}
	var out []int64
	for _, need := range needs {
		got := int64(0)
		for _, id := range ids {
			h, ok := free[id]
			if !ok || used[id] || got == need[2] || int64(h.Star) != need[1] || !match(h, need[0]) {
				continue
			}
			used[id] = true
			out = append(out, id)
			got++
		}
		if got < need[2] {
			return nil, false
		}
	}
	if len(out) != len(ids) { // a hero that pays nothing is not taken
		return nil, false
	}
	return out, true
}

func hdIDs(l []proto.Message) []int64 {
	var out []int64
	for _, m := range l {
		out = append(out, m.Int("id"))
	}
	return out
}

func hdCamp(h store.Partner, camp int64) bool { return int64(partnerTable[h.BID].Camp) == camp }

// RegisterHalidom registers the halidoms (22200 the list, 22202 unlock with heroes, 22203 one level-up click, 22204 step up with
// heroes and items; 22201 and 22205 are pushed). A halidom is open from the role level of the client's constant (90) and
// unlocked by free heroes of its camp; every click of 22203 pays the level's cost, gives the level's exp and a level at the
// full exp; a step needs the level of its conditions and pays items, the named heroes (list1) and heroes of a camp (list2).
// Not done: the attributes are only shown, they are not added to the heroes' attributes; the red dots (the client computes
// them).
func RegisterHalidom(r *gateway.Router, db *store.Store) {
	with := func(s *gateway.Session, cmd uint16, fn func(role *store.Role, st *hdState, fail func(msg string, id int64) error) error) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		hdMu.Lock()
		defer hdMu.Unlock()
		if fresh, err := db.RoleByID(role.RID); err == nil {
			role = fresh
		}
		st := &hdState{}
		if _, err := db.LoadState(role.RID, halidomModule, st); err != nil {
			return err
		}
		if st.Halidoms == nil {
			st.Halidoms = map[string]*hdOne{}
		}
		fail := func(msg string, id int64) error { return s.Send(cmd, proto.Message{"code": 0, "msg": msg, "id": id}) }
		return fn(role, st, fail)
	}
	save := func(role *store.Role, st *hdState) error { return db.SaveState(role.RID, halidomModule, st) }
	find := func(id int64) int {
		for i, b := range halidomCfg.Base {
			if b.ID == id {
				return i
			}
		}
		return -1
	}
	key := func(id int64) string { return strconv.FormatInt(id, 10) }
	pay := func(s *gateway.Session, role *store.Role, consumed []int64, items [][2]int64) (bool, error) {
		sa, si := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(items), 1, sa, si)
		err := db.ConsumePartners(role.RID, consumed, sa, si, map[int64]int64{}, map[int64]int64{})
		if errors.Is(err, store.ErrConflict) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if err := sendAssets(s, db, role.RID); err != nil {
			return false, err
		}
		return true, sendBackpack(s, db)
	}

	r.Handle(22200, func(s *gateway.Session, _ proto.Message) error {
		return with(s, 22200, func(role *store.Role, st *hdState, _ func(string, int64) error) error {
			list := []proto.Message{}
			for i, b := range halidomCfg.Base {
				if h := st.Halidoms[key(b.ID)]; h != nil {
					list = append(list, hdMessage(i, h))
				}
			}
			return s.Send(22200, proto.Message{"list": list})
		})
	})
	r.Handle(22202, func(s *gateway.Session, req proto.Message) error {
		return with(s, 22202, func(role *store.Role, st *hdState, fail func(string, int64) error) error {
			id := req.Int("id")
			idx := find(id)
			switch {
			case idx < 0:
				return fail("信念不存在", id)
			case int64(role.Lev) < halidomCfg.OpenLev:
				return fail(strconv.FormatInt(halidomCfg.OpenLev, 10)+"级开启元素信念", id)
			case st.Halidoms[key(id)] != nil:
				return fail("信念已经开启", id)
			}
			free, err := hdFree(db, role.RID)
			if err != nil {
				return err
			}
			ids := hdIDs(req.Arr("list"))
			consumed, ok := hdPick(free, ids, halidomCfg.Base[idx].Loss, hdCamp)
			if !ok {
				return fail("所需英雄不足", id)
			}
			if paid, err := pay(s, role, consumed, nil); err != nil {
				return err
			} else if !paid {
				return fail("所需英雄不足", id)
			}
			h := &hdOne{Lev: 1}
			st.Halidoms[key(id)] = h
			if err := save(role, st); err != nil {
				return err
			}
			if err := s.Send(22202, proto.Message{"code": 1, "msg": "", "id": id}); err != nil {
				return err
			}
			return s.Send(22201, hdMessage(idx, h))
		})
	})
	r.Handle(22203, func(s *gateway.Session, req proto.Message) error {
		return with(s, 22203, func(role *store.Role, st *hdState, fail func(string, int64) error) error {
			id := req.Int("id")
			idx := find(id)
			if idx < 0 {
				return fail("信念不存在", id)
			}
			h := st.Halidoms[key(id)]
			if h == nil {
				return fail("信念还没有开启", id)
			}
			if h.Lev >= halidomCfg.MaxLev[idx] {
				return fail("已经满级", id)
			}
			row := halidomCfg.Lvup[idx][h.Lev-1]
			if paid, err := pay(s, role, nil, row.Loss); err != nil {
				return err
			} else if !paid {
				return fail("材料不足", id)
			}
			h.Exp += row.Exp
			up := h.Exp >= row.TotalExp
			if up {
				h.Lev++
				h.Exp = 0
			}
			if err := save(role, st); err != nil {
				return err
			}
			if err := s.Send(22203, proto.Message{"code": 1, "msg": "", "id": id}); err != nil {
				return err
			}
			if err := s.Send(22201, hdMessage(idx, h)); err != nil {
				return err
			}
			if up {
				return s.Send(22205, proto.Message{"id": id})
			}
			return nil
		})
	})
	r.Handle(22204, func(s *gateway.Session, req proto.Message) error {
		return with(s, 22204, func(role *store.Role, st *hdState, fail func(string, int64) error) error {
			id := req.Int("id")
			idx := find(id)
			if idx < 0 {
				return fail("信念不存在", id)
			}
			h := st.Halidoms[key(id)]
			if h == nil {
				return fail("信念还没有开启", id)
			}
			if h.Step >= halidomCfg.MaxStep[idx] {
				return fail("已经满阶", id)
			}
			row := halidomCfg.Step[idx][h.Step+1]
			for _, c := range row.Conds {
				if k, _ := c[0].(string); k == "lev" && h.Lev < hdNum(c[1]) {
					return fail("信念等级不足", id)
				}
			}
			free, err := hdFree(db, role.RID)
			if err != nil {
				return err
			}
			fixed, ok1 := hdPick(free, hdIDs(req.Arr("list1")), row.LossFixed, func(p store.Partner, bid int64) bool { return p.BID == bid })
			if !ok1 {
				return fail("所需英雄不足", id)
			}
			for _, f := range fixed {
				delete(free, f)
			}
			rnd, ok2 := hdPick(free, hdIDs(req.Arr("list2")), row.LossRand, hdCamp)
			if !ok2 {
				return fail("所需英雄不足", id)
			}
			if paid, err := pay(s, role, append(fixed, rnd...), row.LossItems); err != nil {
				return err
			} else if !paid {
				return fail("材料不足", id)
			}
			h.Step++
			if err := save(role, st); err != nil {
				return err
			}
			if err := s.Send(22204, proto.Message{"code": 1, "msg": "", "id": id}); err != nil {
				return err
			}
			return s.Send(22201, hdMessage(idx, h))
		})
	})
}
