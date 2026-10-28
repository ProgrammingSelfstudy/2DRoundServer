package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// holyeqm.json: the holy equipment (神装) tables of the client (tools/extract_holyeqm.py).
//
//go:embed data/holyeqm.json
var holyJSON []byte

const (
	holyIDBase  = 1000000000 // piece id = holyIDBase + serial
	holyWashKey = 10         // extra_k of the number of washes
)

type holyBase struct {
	N     int                `json:"n"` // random attributes
	Group int64              `json:"group"`
	Price [][]int64          `json:"price"` // one wash
	Name  string             `json:"name"`
	Score int64              `json:"score"` // the score the tips window shows
	Range map[string][]int64 `json:"range"` // attribute key -> [min, max]
}

var holyTable = func() struct {
	Bases       map[int64]holyBase                  `json:"bases"`
	Suits       map[int64]map[int][]json.RawMessage `json:"suits"`
	ClearReturn [][]int64                           `json:"clear_return"`
	Plans       []struct {
		ID   int64     `json:"id"`
		Open int       `json:"open"`
		Cost [][]int64 `json:"cost"`
	} `json:"plans"`
} {
	var t struct {
		Bases       map[int64]holyBase                  `json:"bases"`
		Suits       map[int64]map[int][]json.RawMessage `json:"suits"`
		ClearReturn [][]int64                           `json:"clear_return"`
		Plans       []struct {
			ID   int64     `json:"id"`
			Open int       `json:"open"`
			Cost [][]int64 `json:"cost"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(holyJSON, &t); err != nil || len(t.Bases) != 1680 {
		panic("game: bad holyeqm.json: " + fmt.Sprint(err, " ", len(t.Bases)))
	}
	return t
}()

// the client's attribute ids of the keys holy equipment rolls (Config.AttrData.data_key_to_id)
var holyAttrIDs = map[string]int64{"atk": 101, "hp_max": 104, "speed": 106, "atk_per": 118, "def_per": 119, "hp_max_per": 120, "cure": 123,
	"crit_ratio": 114, "crit_rate": 113, "hit_rate": 111, "hit_magic": 115, "dam_p": 125, "dam_s": 126, "tenacity": 117, "dodge_rate": 112,
	"dodge_magic": 116, "res_p": 127, "res_s": 128}
var holyAttrKeys = func() map[int64]string {
	m := map[int64]string{}
	for k, id := range holyAttrIDs {
		m[id] = k
	}
	return m
}()

var holyMainKeys = []string{"atk", "hp_max", "speed"}

// holySlot is the slot (1~4) of an item: the client's item types 23~26.
func holySlot(base int64) int64 {
	if _, ok := holyTable.Bases[base]; !ok {
		return 0
	}
	if t := itemTable[base].Type; t >= 23 && t <= 26 {
		return int64(t - 22)
	}
	return 0
}

func holyValue(rg []int64) int64 {
	if rg[1] <= rg[0] {
		return rg[0]
	}
	return rg[0] + rand.Int63n(rg[1]-rg[0]+1)
}

// rollHolyAttr draws the random attribute at pos that is none of `taken`.
func rollHolyAttr(b holyBase, pos int64, taken map[int64]bool) store.HolyAttr {
	keys := []string{}
	for k := range b.Range {
		if !holyHas(holyMainKeys, k) && !taken[holyAttrIDs[k]] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	k := keys[rand.Intn(len(keys))]
	return store.HolyAttr{Pos: pos, ID: holyAttrIDs[k], Val: holyValue(b.Range[k])}
}

func holyHas(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// newHolyPiece rolls a piece: the three main attributes inside the ranges of the client's tips text, and the random ones the base
// has (attr_num) of the percent pool. The value inside a range is uniform (ours).
func newHolyPiece(d *store.HolyDoc, base int64) store.HolyPiece {
	d.Serial++
	b := holyTable.Bases[base]
	x := store.HolyPiece{ID: holyIDBase + d.Serial, Base: base}
	for _, k := range holyMainKeys {
		x.Main = append(x.Main, store.HolyAttr{ID: holyAttrIDs[k], Val: holyValue(b.Range[k])})
	}
	taken := map[int64]bool{}
	for i := 1; i <= b.N; i++ {
		a := rollHolyAttr(b, int64(i), taken)
		taken[a.ID] = true
		x.Rand = append(x.Rand, a)
	}
	return x
}

// syncHoly makes the bag pieces match the item ledger, like syncArtifacts.
func syncHoly(d *store.HolyDoc, ledger map[int64]int64) bool {
	changed := false
	bases := []int64{}
	for base := range ledger {
		if holySlot(base) > 0 {
			bases = append(bases, base)
		}
	}
	for _, x := range d.Pieces {
		if x.Slot == 0 {
			bases = append(bases, x.Base)
		}
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })
	for i, base := range bases {
		if i > 0 && bases[i-1] == base {
			continue
		}
		have := int64(0)
		for _, x := range d.Pieces {
			if x.Base == base && x.Slot == 0 {
				have++
			}
		}
		for have < ledger[base] {
			d.Pieces = append(d.Pieces, newHolyPiece(d, base))
			have, changed = have+1, true
		}
		for have > ledger[base] {
			last := -1
			for j, x := range d.Pieces {
				if x.Base == base && x.Slot == 0 && (last < 0 || x.ID > d.Pieces[last].ID) {
					last = j
				}
			}
			d.Pieces = append(d.Pieces[:last], d.Pieces[last+1:]...)
			have, changed = have-1, true
		}
	}
	return changed
}

func loadHoly(st *store.Store, rid int64) (*store.HolyDoc, error) {
	d, err := st.HolyOf(rid)
	if err != nil {
		return nil, err
	}
	ledger, err := st.RoleItems(rid)
	if err != nil {
		return nil, err
	}
	if syncHoly(d, ledger) {
		if err := st.SaveHoly(rid, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func holyFind(d *store.HolyDoc, id int64) int {
	for i, x := range d.Pieces {
		if x.ID == id {
			return i
		}
	}
	return -1
}

func holyAttrMsgs(l []store.HolyAttr, withPos bool) []proto.Message {
	out := []proto.Message{}
	for _, a := range l {
		m := proto.Message{"attr_id": a.ID, "attr_val": a.Val}
		if withPos {
			m["pos"] = a.Pos
		}
		out = append(out, m)
	}
	return out
}

// holyMessage is one piece as 10501 and 11092 show it.
func holyMessage(x store.HolyPiece) proto.Message {
	return proto.Message{"id": x.ID, "base_id": x.Base, "bind": 0, "quantity": 1, "expire_type": 0, "expire_time": 0,
		"main_attr": holyAttrMsgs(x.Main, false), "holy_eqm_attr": holyAttrMsgs(x.Rand, true), "enchant": 0, "attr": []proto.Message{},
		"extra_attr": []proto.Message{}, "score": holyTable.Bases[x.Base].Score, "all_score": 0, "gemstones": []proto.Message{},
		"extra": []proto.Message{{"extra_k": holyWashKey, "extra_v": x.Washes}}}
}

// holyBag lists the pieces in the bag for 10501; firstPos continues the numbering of the equipment before them.
func holyBag(d *store.HolyDoc, firstPos int) []proto.Message {
	ps := []store.HolyPiece{}
	for _, x := range d.Pieces {
		if x.Slot == 0 {
			ps = append(ps, x)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	out := []proto.Message{}
	for i, x := range ps {
		m := holyMessage(x)
		m["pos"] = firstPos + i
		out = append(out, m)
	}
	return out
}

// holyHeroMessages is the holy_eqm list of a hero entry.
func holyHeroMessages(p store.Partner) []proto.Message {
	out := []proto.Message{}
	for _, x := range p.Holy {
		out = append(out, holyMessage(x))
	}
	return out
}

// holyBonus adds the worn pieces to the hero: main attributes, percent attack / defense / hp, and the suit bonuses (2 and 4 pieces
// of one suit; whether the 4-piece one comes on top of the 2-piece one is not told, we add them).
func holyBonus(ps []store.HolyPiece, hp, atk, def, speed int64) (int64, int64, int64, int64) {
	if len(ps) == 0 {
		return hp, atk, def, speed
	}
	per := map[string]int64{}
	add := func(a store.HolyAttr) {
		switch holyAttrKeys[a.ID] {
		case "atk":
			atk += a.Val
		case "hp_max":
			hp += a.Val
		case "speed":
			speed += a.Val
		case "atk_per", "def_per", "hp_max_per":
			per[holyAttrKeys[a.ID]] += a.Val
		}
	}
	count := map[int64]int{}
	for _, x := range ps {
		for _, a := range x.Main {
			add(a)
		}
		for _, a := range x.Rand {
			add(a)
		}
		count[holyTable.Bases[x.Base].Group]++
	}
	for g, n := range count {
		for num, bonus := range holyTable.Suits[g] {
			if n < num {
				continue
			}
			for _, raw := range bonus {
				var pair []any // [key, value]; a bare number is a skill, which is not applied
				if json.Unmarshal(raw, &pair) != nil || len(pair) != 2 {
					continue
				}
				key, _ := pair[0].(string)
				v, _ := pair[1].(float64)
				add(store.HolyAttr{ID: holyAttrIDs[key], Val: int64(v)})
			}
		}
	}
	// percent values are tenths of a percent
	return hp * (1000 + per["hp_max_per"]) / 1000, atk * (1000 + per["atk_per"]) / 1000, def * (1000 + per["def_per"]) / 1000, speed
}

// RegisterHolyEqm registers the holy equipment: 11093 wear / take off, 11094 wash attributes, 11090 save, 11088 / 11089 sell,
// 11092 the pieces heroes wear, 11086 the hero's total, and the plans 25220~25224. Our rules are in OPEN_QUESTIONS.md.
func RegisterHolyEqm(r *gateway.Router, st *store.Store) {
	load := func(rid int64) (*store.HolyDoc, error) { return loadHoly(st, rid) }
	reply := func(s *gateway.Session, cmd uint16, field string, ok int, msg string, extra proto.Message) error {
		m := proto.Message{field: ok, "msg": msg}
		for k, v := range extra {
			m[k] = v
		}
		return s.Send(cmd, m)
	}
	push := func(s *gateway.Session, rid, partnerID int64) error {
		if partnerID > 0 {
			p, err := st.PartnerByID(rid, partnerID)
			if err != nil {
				return err
			}
			if err := s.Send(11091, partnerMessage(*p)); err != nil {
				return err
			}
		}
		return sendBackpack(s, st)
	}

	r.Handle(11093, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		pid, id := req.Int("partner_id"), req.Int("holy_eqm_id")
		no := func(msg string) error { return reply(s, 11093, "result", 0, msg, nil) }
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		if _, err := st.PartnerByID(role.RID, pid); errors.Is(err, store.ErrNotFound) {
			return no("英雄不存在")
		} else if err != nil {
			return err
		}
		back, spend := map[int64]int64{}, map[int64]int64{}
		switch {
		case req.Int("type") == 0 && id == 0: // all off
			for j, x := range d.Pieces {
				if x.Partner == pid && x.Slot > 0 {
					d.Pieces[j].Partner, d.Pieces[j].Slot = 0, 0
					back[x.Base]++
				}
			}
			if len(back) == 0 {
				return no("没有穿戴神装")
			}
		case req.Int("type") == 0:
			i := holyFind(d, id)
			if i < 0 || d.Pieces[i].Partner != pid || d.Pieces[i].Slot == 0 {
				return no("该神装没有穿戴")
			}
			back[d.Pieces[i].Base]++
			d.Pieces[i].Partner, d.Pieces[i].Slot = 0, 0
		default:
			i := holyFind(d, id)
			if i < 0 {
				return no("神装不存在")
			}
			if d.Pieces[i].Slot != 0 {
				return no("该神装已被穿戴")
			}
			slot := holySlot(d.Pieces[i].Base)
			spend[d.Pieces[i].Base]++
			for j, x := range d.Pieces {
				if x.Partner == pid && x.Slot == slot {
					d.Pieces[j].Partner, d.Pieces[j].Slot = 0, 0
					back[x.Base]++
				}
			}
			d.Pieces[i].Partner, d.Pieces[i].Slot = pid, slot
		}
		if err := st.Exchange(role.RID, nil, spend, nil, back); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("背包中没有该神装")
			}
			return err
		}
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 11093, "result", 1, "操作成功", nil); err != nil {
			return err
		}
		return push(s, role.RID, pid)
	})

	r.Handle(11094, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		no := func(msg string) error { return reply(s, 11094, "result", 0, msg, nil) }
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		i := holyFind(d, req.Int("holy_eqm_id"))
		if i < 0 {
			return no("神装不存在")
		}
		x := d.Pieces[i]
		b := holyTable.Bases[x.Base]
		pick := map[int64]bool{}
		for _, p := range req.Arr("pos") {
			pick[p.Int("pos")] = true
		}
		if len(pick) == 0 {
			return no("请选择要洗练的属性")
		}
		for p := range pick {
			if p < 1 || int(p) > len(x.Rand) {
				return no("属性不存在")
			}
		}
		spendA, spendI := splitCost(b.Price, 1)
		if err := st.Exchange(role.RID, spendA, spendI, nil, nil); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("材料不足")
			}
			return err
		}
		taken := map[int64]bool{}
		for _, a := range x.Rand {
			if !pick[a.Pos] {
				taken[a.ID] = true
			}
		}
		rolled := append([]store.HolyAttr(nil), x.Rand...)
		for j, a := range rolled {
			if pick[a.Pos] {
				rolled[j] = rollHolyAttr(b, a.Pos, taken)
				taken[rolled[j].ID] = true
			}
		}
		d.Pieces[i].Rand = rolled
		d.Pieces[i].Washes++
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 11094, "result", 1, "洗练成功", nil); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return push(s, role.RID, d.Pieces[i].Partner)
	})

	// 11090 saves the washed attributes. They are already in the piece (see OPEN_QUESTIONS), so there is nothing to keep.
	r.Handle(11090, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return reply(s, 11090, "result", 1, "保存成功", nil)
	})

	sellGains := func(d *store.HolyDoc, ids []proto.Message) (spend, gain map[int64]int64, gone map[int64]bool, msg string) {
		spend, gain, gone = map[int64]int64{}, map[int64]int64{}, map[int64]bool{}
		for _, e := range ids {
			i := holyFind(d, e.Int("item_id"))
			if i < 0 || gone[e.Int("item_id")] {
				return nil, nil, nil, "神装不存在"
			}
			x := d.Pieces[i]
			if x.Slot != 0 {
				return nil, nil, nil, "穿戴中的神装不能出售"
			}
			val := itemTable[x.Base].Value
			if len(val) == 0 {
				return nil, nil, nil, "该神装不能出售"
			}
			gone[x.ID] = true
			spend[x.Base]++
			for _, g := range val {
				gain[g.ID] += g.Amount
			}
		}
		if len(gone) == 0 {
			return nil, nil, nil, "请选择神装"
		}
		return spend, gain, gone, ""
	}
	r.Handle(11088, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		_, gain, _, msg := sellGains(d, req.Arr("item_ids"))
		if msg != "" {
			return s.Send(11088, proto.Message{"code": 0, "msg": msg, "list": []proto.Message{}})
		}
		ids := make([]int64, 0, len(gain))
		for id := range gain {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"id": id, "num": gain[id]})
		}
		return s.Send(11088, proto.Message{"code": 1, "msg": "", "list": list})
	})
	r.Handle(11089, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		spend, gain, gone, msg := sellGains(d, req.Arr("item_ids"))
		if msg != "" {
			return reply(s, 11089, "code", 0, msg, nil)
		}
		spendI := spend
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		for id, n := range gain {
			if ItemIsCurrency(id) {
				gainA[id] += n
			} else {
				gainI[id] += n
			}
		}
		if err := st.Exchange(role.RID, nil, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 11089, "code", 0, "背包中没有该神装", nil)
			}
			return err
		}
		kept := d.Pieces[:0]
		for _, x := range d.Pieces {
			if !gone[x.ID] {
				kept = append(kept, x)
			}
		}
		d.Pieces = kept
		for i := range d.Plans { // a sold piece leaves its plans
			items := d.Plans[i].Items[:0]
			for _, it := range d.Plans[i].Items {
				if !gone[it.Item] {
					items = append(items, it)
				}
			}
			d.Plans[i].Items = items
		}
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 11089, "code", 1, "出售成功", nil); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})

	r.Handle(11092, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		out := []proto.Message{}
		for _, e := range req.Arr("partner_ids") {
			list := []proto.Message{}
			if p, err := st.PartnerByID(role.RID, e.Int("partner_id")); err == nil {
				list = holyHeroMessages(*p)
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			out = append(out, proto.Message{"partner_id": e.Int("partner_id"), "holy_eqm": list})
		}
		return s.Send(11092, proto.Message{"partner_ids": out})
	})

	r.Handle(11086, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return s.Send(11086, proto.Message{"partner_id": req.Int("partner_id")})
		} else if err != nil {
			return err
		}
		return s.Send(11086, partnerMessage(*p))
	})

	// plans: a saved set of pieces per slot; 3 are free and more are bought with the cost of data_holy_suit_manage
	planCells := func(d *store.HolyDoc) int64 {
		n := int64(0)
		for _, pl := range holyTable.Plans {
			if pl.Open == 1 {
				n++
			}
		}
		return n + d.Opened
	}
	planMessages := func(d *store.HolyDoc) []proto.Message {
		out := []proto.Message{}
		for _, pl := range d.Plans {
			list := []proto.Message{}
			for _, it := range pl.Items {
				list = append(list, proto.Message{"partner_id": it.Partner, "item_id": it.Item})
			}
			out = append(out, proto.Message{"id": pl.ID, "partner_id": pl.Partner, "name": pl.Name, "list": list})
		}
		return out
	}
	r.Handle(25220, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		return s.Send(25220, proto.Message{"num": planCells(d), "holy_eqm_set_cell": planMessages(d)})
	})
	r.Handle(25221, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		id := req.Int("id")
		if id < 1 || id > planCells(d) {
			return reply(s, 25221, "code", 0, "方案未开启", nil)
		}
		pl := store.HolyPlan{ID: id, Partner: req.Int("partner_id"), Name: req.Str("name")}
		for _, e := range req.Arr("holy_eqm") {
			if holyFind(d, e.Int("item_id")) < 0 {
				return reply(s, 25221, "code", 0, "神装不存在", nil)
			}
			pl.Items = append(pl.Items, store.HolyPlanItem{Partner: e.Int("partner_id"), Item: e.Int("item_id")})
		}
		replaced := false
		for i := range d.Plans {
			if d.Plans[i].ID == id {
				d.Plans[i], replaced = pl, true
			}
		}
		if !replaced {
			d.Plans = append(d.Plans, pl)
		}
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 25221, "code", 1, "保存成功", nil); err != nil {
			return err
		}
		return s.Send(25220, proto.Message{"num": planCells(d), "holy_eqm_set_cell": planMessages(d)})
	})
	r.Handle(25223, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		next := planCells(d) + 1
		if next > int64(len(holyTable.Plans)) {
			return s.Send(25223, proto.Message{"code": 0, "msg": "方案已全部开启", "num": planCells(d)})
		}
		spendA, spendI := splitCost(holyTable.Plans[next-1].Cost, 1)
		if err := st.Exchange(role.RID, spendA, spendI, nil, nil); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return s.Send(25223, proto.Message{"code": 0, "msg": "材料不足", "num": planCells(d)})
			}
			return err
		}
		d.Opened++
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := s.Send(25223, proto.Message{"code": 1, "msg": "开启成功", "num": planCells(d)}); err != nil {
			return err
		}
		return sendAssets(s, st, role.RID)
	})
	// 25224: put the plan's pieces on the hero (the ones that sit on another hero come over)
	r.Handle(25224, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		no := func(msg string) error { return reply(s, 25224, "code", 0, msg, nil) }
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		pid := req.Int("partner_id")
		if _, err := st.PartnerByID(role.RID, pid); errors.Is(err, store.ErrNotFound) {
			return no("英雄不存在")
		} else if err != nil {
			return err
		}
		var plan *store.HolyPlan
		for i := range d.Plans {
			if d.Plans[i].ID == req.Int("id") {
				plan = &d.Plans[i]
			}
		}
		if plan == nil {
			return no("方案不存在")
		}
		touched := map[int64]bool{pid: true}
		spend, back := map[int64]int64{}, map[int64]int64{}
		for _, it := range plan.Items {
			i := holyFind(d, it.Item)
			if i < 0 {
				continue
			}
			x := d.Pieces[i]
			slot := holySlot(x.Base)
			if x.Slot != 0 { // it sits on a hero: it moves without passing the bag
				touched[x.Partner] = true
				d.Pieces[i].Partner, d.Pieces[i].Slot = 0, 0
			} else {
				spend[x.Base]++
			}
			for j, o := range d.Pieces {
				if o.Partner == pid && o.Slot == slot && j != i {
					d.Pieces[j].Partner, d.Pieces[j].Slot = 0, 0
					back[o.Base]++
				}
			}
			d.Pieces[i].Partner, d.Pieces[i].Slot = pid, slot
		}
		if err := st.Exchange(role.RID, nil, spend, nil, back); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("背包中没有该神装")
			}
			return err
		}
		if err := st.SaveHoly(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 25224, "code", 1, "装配成功", nil); err != nil {
			return err
		}
		for h := range touched {
			if err := push(s, role.RID, h); err != nil {
				return err
			}
		}
		return nil
	})
}
