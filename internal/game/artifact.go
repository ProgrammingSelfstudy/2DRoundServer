package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// artifact.json: the artifact (战技) tables of the client (tools/extract_artifact.py).
//
//go:embed data/artifact.json
var artifactJSON []byte

const (
	artFirst, artLast = 10451, 10457 // the seven tiers
	artSerial         = 10000        // piece id = base*artSerial + serial
	artTrain          = 10450        // 强化训练, what pieces are decomposed into and recast with
	artCoin           = 1
	artPositions      = 2 // artifact_pos 1, 2 on a hero
)

// the client's attribute ids (Config.AttrData.data_key_to_id) of the keys artifacts roll
var artAttrIDs = map[string]int64{"atk": 101, "hp_max": 104, "def": 107, "speed": 106, "atk_per": 118, "hp_max_per": 120, "def_per": 119,
	"crit_rate": 113, "crit_ratio": 114, "dam": 121, "res": 122, "tenacity": 117}
var artAttrKeys = func() map[int64]string {
	m := map[int64]string{}
	for k, id := range artAttrIDs {
		m[id] = k
	}
	return m
}()

var artFlatKeys = []string{"atk", "hp_max", "def", "speed"}
var artPerKeys = []string{"atk_per", "hp_max_per", "def_per", "crit_rate", "crit_ratio", "dam", "res", "tenacity"}

type artTier struct {
	AttrNum   int       `json:"attr_num"`
	SkillNum  int       `json:"skill_num"`
	RefExpend [][]int64 `json:"ref_expend"`
	LimitLv   int64     `json:"limit_lv"`
}

type artCompound struct {
	AddNum      int64     `json:"add_num"`
	Award       [][]int64 `json:"award"`
	CostNum     int       `json:"cost_num"`
	Materials   []int64   `json:"materials"`
	OtherExpend [][]int64 `json:"other_expend"`
	Rate        int64     `json:"rate"`
}

type artData struct {
	Const struct {
		ChangeCondition int64     `json:"change_condition"`
		ChangeGift      [][]int64 `json:"change_gift"`
		RecastTag       []int64   `json:"recast_tag"`
		LuckyItemID     int64     `json:"lucky_item_id"`
		LuckyIDs        [][]int64 `json:"lucky_artifact_ids"`
		LuckySkills     []int64   `json:"lucky_artifact_skillids"`
	} `json:"const"`
	Tiers    map[int64]artTier             `json:"tiers"`
	Attr     map[int64]map[string]int64    `json:"attr"`
	Compound map[int64]map[int]artCompound `json:"compound"`
	Resolve  map[int64][][]int64           `json:"resolve"`
	Skills   [][]int64                     `json:"skills"` // by level 1..4, the same skill type at the same index
}

var artTable = func() artData {
	var t artData
	if err := json.Unmarshal(artifactJSON, &t); err != nil || len(t.Tiers) != 7 {
		panic("game: bad artifact.json")
	}
	return t
}()

func isArtifact(base int64) bool { return base >= artFirst && base <= artLast }

// artSkillCombos are the skill levels (1 low .. 4 super) a tier rolls, one combination of them, from the tier's own text in the
// client (skill_desc). 10457 has three skills and its text names two: the third is a level 3 (ours).
var artSkillCombos = map[int64][][]int{
	10452: {{1}},
	10453: {{1, 1}, {1, 2}, {2}},
	10454: {{3, 2}, {3, 1}, {2, 2}, {3}},
	10455: {{3, 3}, {3, 2}},
	10456: {{4, 3}, {3, 2}},
	10457: {{4, 4, 3}, {4, 3, 3}},
}

// artSkillSlot is the extra_k of the n-th skill (0-based) and of its pending recast.
var artSkillSlot = [3][2]int64{{1, 3}, {2, 4}, {8, 9}}

// rollSkills draws skills for the levels; `keep` are skills to keep at their position (nil entries are rolled), and no skill
// type is drawn twice (the client: same-type effects do not stack).
func rollSkills(levels []int, keep []int64, rare bool) []int64 {
	out := make([]int64, len(levels))
	used := map[int]bool{}
	pool := func(lvl int) []int64 { return artTable.Skills[lvl-1] }
	for i, k := range keep {
		if i >= len(out) {
			break
		}
		out[i] = k
		for lvl := range artTable.Skills {
			for idx, sk := range pool(lvl + 1) {
				if sk == k {
					used[idx] = true
				}
			}
		}
	}
	for i, lvl := range levels {
		if out[i] != 0 {
			continue
		}
		list := pool(lvl)
		var sk int64
		if rare && i == firstFree(out) {
			sk = rareSkill(lvl)
		}
		for sk == 0 {
			idx := rand.Intn(len(list))
			if used[idx] {
				continue
			}
			used[idx], sk = true, list[idx]
		}
		out[i] = sk
	}
	return out
}

func firstFree(out []int64) int {
	for i, v := range out {
		if v == 0 {
			return i
		}
	}
	return -1
}

// rareSkill is a skill of the client's rare list at (or below) the level.
func rareSkill(lvl int) int64 {
	tags := artTable.Const.RecastTag
	return tags[rand.Intn(len(tags))]
}

// newArtPiece rolls a new piece of the tier: one flat attribute, then percent ones, valued 60%-100% of the tier's ceiling (the
// client has only the ceiling; the range is ours), and the skills.
func newArtPiece(d *store.ArtifactDoc, base int64) store.ArtPiece {
	d.Next[base]++
	a := store.ArtPiece{ID: base*artSerial + d.Next[base], Base: base, Attr: rollArtAttrs(base)}
	if combos := artSkillCombos[base]; len(combos) > 0 {
		a.Skills = rollSkills(combos[rand.Intn(len(combos))], nil, false)
	}
	return a
}

// rollArtAttrs rolls the attributes of a tier: the first is one of the four flat ones, the rest are percent ones.
func rollArtAttrs(base int64) []store.ArtAttr {
	var out []store.ArtAttr
	tier := artTable.Tiers[base]
	ceil := artTable.Attr[base]
	roll := func(key string) store.ArtAttr {
		// every attribute in the capture (original-2026-10-27, 14 of them over six tiers) is exactly the table's value
		return store.ArtAttr{ID: artAttrIDs[key], Val: max(ceil[key], 1)}
	}
	for i := 0; i < tier.AttrNum; i++ {
		if i == 0 {
			out = append(out, roll(artFlatKeys[rand.Intn(len(artFlatKeys))]))
			continue
		}
		have := map[int64]bool{}
		for _, x := range out {
			have[x.ID] = true
		}
		for {
			k := artPerKeys[rand.Intn(len(artPerKeys))]
			if !have[artAttrIDs[k]] {
				out = append(out, roll(k))
				break
			}
		}
	}
	return out
}

// syncArtifacts makes the pieces in the bag match the item ledger, which every other module (mail, shop, gift boxes, selling)
// counts in: more in the ledger than pieces gives new random pieces, fewer drops the newest. It reports whether it changed.
func syncArtifacts(d *store.ArtifactDoc, ledger map[int64]int64) bool {
	changed := false
	for base := int64(artFirst); base <= artLast; base++ {
		have := 0
		for _, a := range d.Pieces {
			if a.Base == base && a.Pos == 0 {
				have++
			}
		}
		for int64(have) < ledger[base] {
			d.Pieces = append(d.Pieces, newArtPiece(d, base))
			have, changed = have+1, true
		}
		for int64(have) > ledger[base] {
			last := -1
			for i, a := range d.Pieces {
				if a.Base == base && a.Pos == 0 && (last < 0 || a.ID > d.Pieces[last].ID) {
					last = i
				}
			}
			d.Pieces = append(d.Pieces[:last], d.Pieces[last+1:]...)
			have, changed = have-1, true
		}
	}
	return changed
}

func artFind(d *store.ArtifactDoc, id int64) int {
	for i, a := range d.Pieces {
		if a.ID == id {
			return i
		}
	}
	return -1
}

func artExtra(a store.ArtPiece) []proto.Message {
	out := []proto.Message{{"extra_k": 11, "extra_v": 0}} // every piece of the original has this first
	for i, sk := range a.Skills {
		out = append(out, proto.Message{"extra_k": artSkillSlot[i][0], "extra_v": sk})
	}
	for i, sk := range a.Pending {
		out = append(out, proto.Message{"extra_k": artSkillSlot[i][1], "extra_v": sk})
	}
	return out
}

func artAttrList(list []store.ArtAttr) []proto.Message {
	out := []proto.Message{}
	for _, x := range list {
		out = append(out, proto.Message{"attr_id": x.ID, "attr_val": x.Val * 1000}) // the wire has thousandths
	}
	return out
}

// artifactsOf is the `artifacts` list of a hero entry (11000, 11012, 11031).
func artifactsOf(p store.Partner) []proto.Message {
	out := []proto.Message{}
	for _, a := range p.Artifacts {
		out = append(out, proto.Message{"artifact_pos": a.Pos, "id": a.ID, "base_id": a.Base, "enchant": a.Star,
			"attr": artAttrList(a.Attr), "extra_attr": artAttrList(a.PendAttr), "extra": artExtra(a)})
	}
	return out
}

// artifactBag lists the pieces in the bag as 10500 entries.
func artifactBag(d *store.ArtifactDoc, firstPos int) []proto.Message {
	ps := []store.ArtPiece{}
	for _, a := range d.Pieces {
		if a.Pos == 0 {
			ps = append(ps, a)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	out := []proto.Message{}
	for i, a := range ps {
		out = append(out, proto.Message{"id": a.ID, "base_id": a.Base, "bind": 0, "quantity": 1, "pos": firstPos + i,
			"expire_type": 0, "expire_time": 0, "main_attr": []proto.Message{}, "enchant": a.Star, "attr": artAttrList(a.Attr),
			"extra_attr": artAttrList(a.PendAttr), "score": 0, "all_score": 0, "extra": artExtra(a), "gemstones": []proto.Message{},
			"holy_eqm_attr": []proto.Message{}})
	}
	return out
}

// artifactBonus adds the worn artifacts to the four attributes the hero has: flat values first, then the percent ones of the
// total. The other rolled attributes (crit, damage, resist...) are shown in the piece but not yet in the numbers.
func artifactBonus(arts []store.ArtPiece, hp, atk, def, speed int64) (int64, int64, int64, int64) {
	if len(arts) == 0 {
		return hp, atk, def, speed
	}
	var per = map[string]int64{}
	for _, a := range arts {
		for _, x := range a.Attr {
			switch artAttrKeys[x.ID] {
			case "atk":
				atk += x.Val
			case "hp_max":
				hp += x.Val
			case "def":
				def += x.Val
			case "speed":
				speed += x.Val
			case "atk_per", "hp_max_per", "def_per":
				per[artAttrKeys[x.ID]] += x.Val
			}
		}
	}
	// the percent values are in tenths of a percent (the window shows 50 as 5%)
	return hp * (1000 + per["hp_max_per"]) / 1000, atk * (1000 + per["atk_per"]) / 1000, def * (1000 + per["def_per"]) / 1000, speed
}

func artItems(pairs [][]int64, times int64) map[int64]int64 {
	m := map[int64]int64{}
	for _, p := range pairs {
		m[p[0]] += p[1] * times
	}
	return m
}

// artPity is the recasts of one tier before a rare skill is sure: the counters 11048 sends have the limit 25 for the first
// tier and 15 for the other three (capture original-2026-10-27, 14-gear-skill; what the counter does at the limit is still ours).
func artPity(base int64) int64 {
	if base == 10454 {
		return 25
	}
	return 15
}

// sendRecastCounters sends 11048: the recast counters of the four tiers that have one.
func sendRecastCounters(s *gateway.Session, d *store.ArtifactDoc) error {
	list := []proto.Message{}
	for typ, base := range []int64{10454, 10455, 10456, 10457} { // the client's counter types 1~4
		list = append(list, proto.Message{"type": typ + 1, "current": d.Recasts[base], "limit": artPity(base)})
	}
	return s.Send(11048, proto.Message{"artifact_ref_count": list})
}

// splitCost puts the currency part of a cost list (item id 1 and the other currencies) in assets and the rest in items.
func splitCost(pairs [][]int64, times int64) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	for _, p := range pairs {
		if ItemIsCurrency(p[0]) {
			assets[p[0]] += p[1] * times
		} else {
			items[p[0]] += p[1] * times
		}
	}
	return
}

func addMaps(a, b map[int64]int64) map[int64]int64 {
	out := map[int64]int64{}
	for k, v := range a {
		out[k] += v
	}
	for k, v := range b {
		out[k] += v
	}
	return out
}

// loadArtifacts reads a role's artifact state and makes the bag pieces match the item ledger.
func loadArtifacts(st *store.Store, rid int64) (*store.ArtifactDoc, error) {
	d, err := st.ArtifactsOf(rid)
	if err != nil {
		return nil, err
	}
	ledger, err := st.RoleItems(rid)
	if err != nil {
		return nil, err
	}
	if syncArtifacts(d, ledger) {
		if err := st.SaveArtifacts(rid, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// RegisterArtifact registers the artifacts: 11030 wear / take off, 11033 recast, 11034 save the recast, 11035 decompose,
// 11036 compose, 11037 the melting value, 11038 take its gift, 11048 the recast counters. 11032 (star up) is still a refusal:
// the client's table has no rule for it. The rules the client does not give are ours (OPEN_QUESTIONS.md).
func RegisterArtifact(r *gateway.Router, st *store.Store) {
	load := func(rid int64) (*store.ArtifactDoc, error) { return loadArtifacts(st, rid) }
	refresh := func(s *gateway.Session, rid int64) error {
		if err := sendAssets(s, st, rid); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}
	sendHero := func(s *gateway.Session, rid, partnerID int64) error {
		p, err := st.PartnerByID(rid, partnerID)
		if err != nil {
			return err
		}
		m := partnerMessage(*p)
		if err := s.Send(11031, m); err != nil {
			return err
		}
		return s.Send(11012, m)
	}
	reply := func(s *gateway.Session, cmd uint16, ok int, msg string, extra proto.Message) error {
		m := proto.Message{"result": ok, "msg": msg}
		for k, v := range extra {
			m[k] = v
		}
		return s.Send(cmd, m)
	}
	// after a change of one piece: its hero if worn, else the bag
	pushPiece := func(s *gateway.Session, rid int64, a store.ArtPiece) error {
		if a.Pos > 0 {
			return sendHero(s, rid, a.Partner)
		}
		return sendBackpack(s, st)
	}

	r.Handle(11030, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		pid, pos, id := req.Int("partner_id"), req.Int("pos_id"), req.Int("artifact_id")
		no := func(msg string) error { return reply(s, 11030, 0, msg, proto.Message{"partner_id": pid}) }
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		if _, err := st.PartnerByID(role.RID, pid); errors.Is(err, store.ErrNotFound) {
			return no("英雄不存在")
		} else if err != nil {
			return err
		}
		i := artFind(d, id)
		if i < 0 {
			return no("战技不存在")
		}
		a := d.Pieces[i]
		if req.Int("type") == 0 { // take off
			if a.Pos == 0 || a.Partner != pid {
				return no("该战技没有穿戴")
			}
			if err := st.Exchange(role.RID, nil, nil, nil, map[int64]int64{a.Base: 1}); err != nil {
				return err
			}
			d.Pieces[i].Partner, d.Pieces[i].Pos = 0, 0
		} else {
			if pos < 1 || pos > artPositions {
				return no("位置不对")
			}
			if a.Pos != 0 {
				return no("该战技已被穿戴")
			}
			// what sits there goes back to the bag
			back := map[int64]int64{}
			spend := map[int64]int64{a.Base: 1}
			for j, o := range d.Pieces {
				if o.Partner == pid && o.Pos == pos {
					d.Pieces[j].Partner, d.Pieces[j].Pos = 0, 0
					back[o.Base]++
				}
			}
			if err := st.Exchange(role.RID, nil, spend, nil, back); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return no("背包中没有该战技")
				}
				return err
			}
			d.Pieces[i].Partner, d.Pieces[i].Pos = pid, pos
		}
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		if req.Int("type") != 0 {
			if hp, err := st.PartnerByID(role.RID, pid); err == nil {
				growArtifact(st, role.RID, partnerTable[hp.BID].Name, a.Base, s.Now().Unix())
			}
		}
		// the original answers last and with an empty msg (capture original-2026-10-27, 14-gear-skill)
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := sendHero(s, role.RID, pid); err != nil {
			return err
		}
		return reply(s, 11030, 1, "", proto.Message{"partner_id": pid})
	})

	r.Handle(11033, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		luck := req.Int("luck_item")
		no := func(msg string) error { return reply(s, 11033, 0, msg, proto.Message{"luck_item": luck}) }
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		i := artFind(d, req.Int("artifact_id"))
		if i < 0 {
			return no("战技不存在")
		}
		a := d.Pieces[i]
		combos := artSkillCombos[a.Base]
		if len(a.Skills) == 0 || len(combos) == 0 {
			return no("该战技不能重铸")
		}
		cost := artTable.Tiers[a.Base].RefExpend
		spendA, spendI := splitCost(cost, 1)
		if luck != 0 {
			if luck != 1 {
				return no("该幸运宝珠暂不可用")
			}
			n := int64(0)
			for _, p := range artTable.Const.LuckyIDs {
				if p[0] == a.Base {
					n = p[1]
				}
			}
			if n == 0 {
				return no("该战技不能幸运重铸")
			}
			spendI[artTable.Const.LuckyItemID] += n
		}
		// locked skills stay where they are
		keep := make([]int64, len(a.Skills))
		for _, sk := range req.Arr("skills") {
			for j, cur := range a.Skills {
				if cur == sk.Int("skill_id") {
					keep[j] = cur
				}
			}
		}
		// the number of skills a tier has stays; levels follow the tier's own combinations
		levels := combos[rand.Intn(len(combos))]
		for len(levels) < len(a.Skills) {
			levels = append(levels, levels[len(levels)-1])
		}
		levels = levels[:len(a.Skills)]
		tierPity := a.Base >= 10454
		guaranteed := tierPity && d.Recasts[a.Base]+1 >= artPity(a.Base)
		rare := guaranteed || luck == 1
		if err := st.Exchange(role.RID, spendA, spendI, nil, nil); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("材料不足")
			}
			return err
		}
		d.Pieces[i].Pending = rollSkills(levels, keep, rare)
		d.Pieces[i].PendAttr = rollArtAttrs(a.Base) // the base attribute is rolled again too (the window says 随机属性)
		if tierPity {
			d.Recasts[a.Base]++
			if guaranteed {
				d.Recasts[a.Base] = 0
			}
		}
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		// the answer comes last, after the assets, the counters and the piece
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendRecastCounters(s, d); err != nil {
			return err
		}
		if err := pushPiece(s, role.RID, d.Pieces[i]); err != nil {
			return err
		}
		return reply(s, 11033, 1, "重铸成功", proto.Message{"luck_item": luck})
	})

	r.Handle(11034, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		i := artFind(d, req.Int("artifact_id"))
		if i < 0 || len(d.Pieces[i].Pending) == 0 {
			return reply(s, 11034, 0, "没有可保存的重铸", nil)
		}
		d.Pieces[i].Skills, d.Pieces[i].Pending = d.Pieces[i].Pending, nil
		if len(d.Pieces[i].PendAttr) > 0 {
			d.Pieces[i].Attr, d.Pieces[i].PendAttr = d.Pieces[i].PendAttr, nil
		}
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		if err := pushPiece(s, role.RID, d.Pieces[i]); err != nil {
			return err
		}
		return reply(s, 11034, 1, "", nil)
	})

	r.Handle(11035, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		gone := map[int64]bool{}
		spend, gain := map[int64]int64{}, map[int64]int64{}
		for _, e := range req.Arr("artifact_id") {
			i := artFind(d, e.Int("id"))
			if i < 0 || gone[e.Int("id")] {
				return reply(s, 11035, 0, "战技不存在", nil)
			}
			a := d.Pieces[i]
			if a.Pos != 0 {
				return reply(s, 11035, 0, "穿戴中的战技不能分解", nil)
			}
			gone[a.ID] = true
			spend[a.Base]++
			for k, v := range artItems(artTable.Resolve[a.Base], 1) {
				gain[k] += v
			}
		}
		if len(gone) == 0 {
			return reply(s, 11035, 0, "请选择战技", nil)
		}
		if err := st.Exchange(role.RID, nil, spend, nil, gain); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(s, 11035, 0, "背包中没有该战技", nil)
			}
			return err
		}
		kept := d.Pieces[:0]
		for _, a := range d.Pieces {
			if !gone[a.ID] {
				kept = append(kept, a)
			}
		}
		d.Pieces = kept
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		if err := refresh(s, role.RID); err != nil {
			return err
		}
		if err := sendAssetGain(s, gain); err != nil {
			return err
		}
		return reply(s, 11035, 1, "", nil)
	})

	r.Handle(11036, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		no := func(msg string) error { return reply(s, 11036, 0, msg, proto.Message{"flag": 0}) }
		target := req.Int("item_id")
		ids := req.Arr("expends")
		rule, ok := artTable.Compound[target][len(ids)]
		if !ok {
			return no("合成数量不对")
		}
		fresh, err := st.RoleByID(role.RID) // the level may have changed since the login
		if err != nil {
			return err
		}
		if fresh.Lev < int(artTable.Tiers[target].LimitLv) {
			return no("等级不足")
		}
		d, err := load(role.RID)
		if err != nil {
			return err
		}
		gone := map[int64]bool{}
		for _, e := range ids {
			i := artFind(d, e.Int("artifact_id"))
			if i < 0 || gone[e.Int("artifact_id")] || d.Pieces[i].Pos != 0 || d.Pieces[i].Base != rule.Materials[0] {
				return no("只能使用背包中同类的战技")
			}
			gone[d.Pieces[i].ID] = true
		}
		spendA, spendI := splitCost(rule.OtherExpend, 1)
		spendI[rule.Materials[0]] += int64(len(ids))
		win := rand.Int63n(1000) < rule.Rate
		gain := map[int64]int64{}
		if win {
			gain[target] = 1
		} else {
			gain = artItems(rule.Award, 1)
		}
		if err := st.Exchange(role.RID, spendA, spendI, nil, gain); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("材料不足")
			}
			return err
		}
		kept := d.Pieces[:0]
		for _, a := range d.Pieces {
			if !gone[a.ID] {
				kept = append(kept, a)
			}
		}
		d.Pieces = kept
		flag, msg := 1, "合成成功"
		if !win {
			d.Lucky += rule.AddNum
			flag, msg = 0, "合成失败，战技已分解"
		}
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		if err := refresh(s, role.RID); err != nil {
			return err
		}
		if err := sendAssetGain(s, gain); err != nil {
			return err
		}
		if !win { // a success sent no 11037 (capture original-2026-10-27); what a failure sent is not known
			if err := s.Send(11037, proto.Message{"lucky": d.Lucky}); err != nil {
				return err
			}
		}
		return reply(s, 11036, 1, msg, proto.Message{"flag": flag})
	})

	r.Handle(11037, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := st.ArtifactsOf(role.RID)
		if err != nil {
			return err
		}
		return s.Send(11037, proto.Message{"lucky": d.Lucky})
	})

	r.Handle(11038, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := st.ArtifactsOf(role.RID)
		if err != nil {
			return err
		}
		need := artTable.Const.ChangeCondition
		if d.Lucky < need {
			return reply(s, 11038, 0, "熔炼值不足", nil)
		}
		if err := st.Exchange(role.RID, nil, nil, nil, artItems(artTable.Const.ChangeGift, 1)); err != nil {
			return err
		}
		d.Lucky -= need
		if err := st.SaveArtifacts(role.RID, d); err != nil {
			return err
		}
		if err := reply(s, 11038, 1, "获得橙色战技", nil); err != nil {
			return err
		}
		if err := s.Send(11037, proto.Message{"lucky": d.Lucky}); err != nil {
			return err
		}
		return refresh(s, role.RID)
	})

	r.Handle(11048, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := st.ArtifactsOf(role.RID)
		if err != nil {
			return err
		}
		return sendRecastCounters(s, d)
	})

	r.Handle(11032, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return reply(s, 11032, 0, notOpen, nil)
	})
}
