package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// hallows.json: the hallows ("神技") of the client (tools/extract_hallows.py).
//
//go:embed data/hallows.json
var hallowsJSON []byte

const (
	hallowsModule = "hallows"
	hallowsCount  = 5
)

// hwAttr is one {key, value} pair of an attribute list.
type hwAttr struct {
	Key string
	Val int64
}

func (a *hwAttr) UnmarshalJSON(b []byte) error {
	var p []json.RawMessage
	if err := json.Unmarshal(b, &p); err != nil || len(p) != 2 {
		return errors.New("game: bad attribute pair")
	}
	if err := json.Unmarshal(p[0], &a.Key); err != nil {
		return err
	}
	return json.Unmarshal(p[1], &a.Val)
}

type hwStep struct {
	Attr     []hwAttr  `json:"attr"`
	Loss     [][]int64 `json:"loss"`
	MaxLucky int64     `json:"max_lucky"`
}

type hwSkillUp struct {
	Lose     [][]int64 `json:"lose"`
	SkillBid int64     `json:"skill_bid"`
	LevLimit int64     `json:"lev_limit"`
}

type hwRefine struct {
	AddDps         int64     `json:"add_dps"`
	DecomposeAward [][]int64 `json:"decompose_award"`
	Expend         [][]int64 `json:"expend"`
	NeedLev        int64     `json:"need_lev"`
}

type hwProgress struct {
	Label     string `json:"cli_label"`
	Target    int64  `json:"target"`
	TargetVal int64  `json:"target_val"`
}

type hwTask struct {
	ID       int64             `json:"id"`
	HID      int64             `json:"hid"`
	Items    [][]int64         `json:"items"`
	Extra    []json.RawMessage `json:"extra"`
	Progress []hwProgress      `json:"progress"`
}

type hwLookDef struct {
	ID     int64     `json:"id"`
	IsItem int64     `json:"is_item"`
	Loss   [][]int64 `json:"loss"`
	Time   int64     `json:"time"`
}

type hwConf struct {
	Const         map[string]json.RawMessage `json:"const"`
	RefineOpenLev int64                      `json:"refine_open_lev"`
	AttrIDs       map[string]int64           `json:"attr_ids"`
	MaxLev        []int64                    `json:"max_lev"`
	SkillMaxLev   []int64                    `json:"skill_max_lev"`
	Info          map[string]hwStep          `json:"info"`
	SkillUp       map[string]hwSkillUp       `json:"skill_up"`
	Trace         map[string]int64           `json:"trace"`
	Tasks         map[string]hwTask          `json:"tasks"`
	Magic         []hwLookDef                `json:"magic"`
	MagicTasks    map[string]hwTask          `json:"magic_tasks"`
	Refine        [][]hwRefine               `json:"refine"`

	taskIDs, magicTaskIDs []int64
}

func (c *hwConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

func hwKey(id, n int64) string { return strconv.FormatInt(id, 10) + "_" + strconv.FormatInt(n, 10) }

func hwSortedIDs(m map[string]hwTask) []int64 {
	ids := make([]int64, 0, len(m))
	for _, t := range m {
		ids = append(ids, t.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

var hallowsCfg = func() *hwConf {
	c := &hwConf{}
	if err := json.Unmarshal(hallowsJSON, c); err != nil || len(c.Info) != hallowsCount*24 || len(c.Refine) != hallowsCount || len(c.Tasks) != 20 || len(c.Magic) == 0 {
		panic("game: bad hallows.json")
	}
	c.taskIDs, c.magicTaskIDs = hwSortedIDs(c.Tasks), hwSortedIDs(c.MagicTasks)
	return c
}()

func (c *hwConf) task(id int64) (hwTask, bool) {
	t, ok := c.Tasks[strconv.FormatInt(id, 10)]
	return t, ok
}

func (c *hwConf) magicTask(id int64) (hwTask, bool) {
	t, ok := c.MagicTasks[strconv.FormatInt(id, 10)]
	return t, ok
}

func (c *hwConf) look(id int64) (hwLookDef, bool) {
	for _, m := range c.Magic {
		if m.ID == id {
			return m, true
		}
	}
	return hwLookDef{}, false
}

// hwOne is a hallow a role owns.
type hwOne struct {
	ID, Step, Lucky, Seal, SkillLev, Refine, Look int64
}

// hwLook is a look ("幻化") a role owns, End = 0 for a permanent one, On = the hallow wearing it.
type hwLook struct{ ID, End, On int64 }

type hwState struct {
	Seen        bool
	Hallows     map[string]*hwOne
	Claimed     map[string]bool // unlock tasks whose reward was taken
	Looks       map[string]*hwLook
	LookClaimed map[string]bool // look tasks whose reward was taken
}

func (st *hwState) hallow(id int64) *hwOne { return st.Hallows[strconv.FormatInt(id, 10)] }
func (st *hwState) look(id int64) *hwLook  { return st.Looks[strconv.FormatInt(id, 10)] }

// next is the hallow whose unlock tasks are open (the highest owned + 1).
func (st *hwState) next() int64 {
	var m int64
	for _, h := range st.Hallows {
		if h.ID > m {
			m = h.ID
		}
	}
	return m + 1
}

func hwEntry(h *hwOne) proto.Message {
	c := hallowsCfg
	attrs := []proto.Message{}
	for _, a := range c.Info[hwKey(h.ID, h.Step)].Attr {
		attrs = append(attrs, proto.Message{"attr_id": c.AttrIDs[a.Key], "attr_val": a.Val})
	}
	return proto.Message{"id": h.ID, "step": h.Step, "lucky": h.Lucky, "lucky_endtime": 0, "seal": h.Seal, "add_attr": attrs,
		"skill": []proto.Message{{"lev": h.SkillLev, "skill_bid": c.SkillUp[hwKey(h.ID, h.SkillLev)].SkillBid}}, "look_id": h.Look, "refine_lev": h.Refine}
}

// hwFacts is what the unlock tasks measure.
type hwFacts struct {
	role   *store.Role
	heroes []store.Partner
	maxDun int64
	tower  int64
	count  map[string]int64
}

// lifeLabels are the events the tasks count over the whole life of the role (lifeCount feeds them).
var lifeLabels = []string{"evt_recruit", "evt_arena_fight", "evt_dungeon_fast_combat", "evt_guild_dun_fight"}

func lifeKey(label string) string { return "life:" + label }

// lifeCount counts one event over the whole life of the role (the hallows tasks read it).
func lifeCount(st *store.Store, rid int64, label string, n int64) {
	_, _ = st.AddCounter(rid, lifeKey(label), n)
}

func (t hwTask) extra() (string, int64) {
	if len(t.Extra) == 2 {
		var k string
		var v int64
		if json.Unmarshal(t.Extra[0], &k) == nil && json.Unmarshal(t.Extra[1], &v) == nil {
			return k, v
		}
	}
	return "", 0
}

func (f *hwFacts) value(t hwTask) int64 {
	p := hwProgress{}
	if len(t.Progress) > 0 {
		p = t.Progress[0]
	}
	var v int64
	switch p.Label {
	case "evt_dungeon_pass":
		v = int64(b2i(f.maxDun >= p.Target))
	case "evt_levup":
		v = int64(f.role.Lev)
	case "evt_power":
		v = rolePower(f.heroes)
	case "evt_star_tower_floor_pass":
		v = f.tower
	case "evt_partner":
		key, min := t.extra()
		for _, h := range f.heroes {
			if (key == "lev" && int64(h.Lev) >= min) || (key == "star" && int64(h.Star) >= min) {
				v++
			}
		}
	case "evt_partner_star_up":
		for _, h := range f.heroes {
			if int64(h.Star) >= p.Target {
				v++
			}
		}
	case "evt_shipping", "evt_mystery_buy":
		v = p.TargetVal // these systems do not exist here: counted as done
	default:
		v = f.count[p.Label]
	}
	if v > p.TargetVal {
		v = p.TargetVal
	}
	return v
}

type hwSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterHallows registers the hallows (24100-24135): five hallows, each unlocked by four tasks, raised through 24 steps
// with a lucky bar (advance), a skill level, a seal count and a refine level, reset for a refund; and the four looks
// ("幻化") worn over a hallow. 24102 (wear a hallow) has no field table in the client and no window that sends it.
func RegisterHallows(r *gateway.Router, db *store.Store) {
	h := &hwSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(24100, h.list)
	r.Handle(24101, h.advance)
	r.Handle(24103, h.skillUp)
	r.Handle(24104, h.trace)
	r.Handle(24120, h.tasks)
	r.Handle(24122, h.submit)
	r.Handle(24123, h.reset)
	r.Handle(24124, h.sign)
	r.Handle(24125, h.looks)
	r.Handle(24128, h.lookTasks)
	r.Handle(24130, h.submitLook)
	r.Handle(24131, h.unlockLook)
	r.Handle(24132, h.wear)
	r.Handle(24135, h.refine)
}

func (h *hwSvc) rng() *rand.Rand {
	if h.fights.Rand != nil {
		return h.fights.Rand()
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// with loads the state, drops the looks that ran out (24133), runs fn and saves.
func (h *hwSvc) with(s *gateway.Session, fn func(role *store.Role, st *hwState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := h.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &hwState{}
	if _, err := h.st.LoadState(role.RID, hallowsModule, st); err != nil {
		return err
	}
	if st.Hallows == nil {
		st.Hallows = map[string]*hwOne{}
	}
	if st.Claimed == nil {
		st.Claimed = map[string]bool{}
	}
	if st.Looks == nil {
		st.Looks = map[string]*hwLook{}
	}
	if st.LookClaimed == nil {
		st.LookClaimed = map[string]bool{}
	}
	var gone []int64
	for k, l := range st.Looks {
		if l.End > 0 && l.End <= s.Now().Unix() {
			if w := st.hallow(l.On); w != nil && w.Look == l.ID {
				w.Look = 0
			}
			delete(st.Looks, k)
			gone = append(gone, l.ID)
		}
	}
	for _, id := range gone {
		if err := s.Send(24133, proto.Message{"id": id}); err != nil {
			return err
		}
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return h.st.SaveState(role.RID, hallowsModule, st)
}

func (h *hwSvc) facts(role *store.Role) (*hwFacts, error) {
	f := &hwFacts{role: role, count: map[string]int64{}}
	var err error
	if f.heroes, err = h.st.PartnersOf(role.RID); err != nil {
		return nil, err
	}
	if d, err := h.st.Drama(role.RID); err == nil {
		f.maxDun = d.MaxDunID
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	t, err := h.st.StarTowerOf(role.RID)
	if err != nil {
		return nil, err
	}
	f.tower = t.MaxTower
	for _, l := range lifeLabels {
		if f.count[l], err = h.st.Counter(role.RID, lifeKey(l)); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// isOpen: the feature opens with the dungeon of the client's hallows_open constant.
func (h *hwSvc) isOpen(role *store.Role) (bool, error) {
	var need []json.RawMessage
	_ = json.Unmarshal(hallowsCfg.Const["hallows_open"], &need)
	var dun int64
	if len(need) == 2 {
		_ = json.Unmarshal(need[1], &dun)
	}
	d, err := h.st.Drama(role.RID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return d.MaxDunID >= dun, nil
}

func (h *hwSvc) gain(s *gateway.Session, role *store.Role, items [][]int64) error {
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 && p[1] > 0 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l), 1, ga, gi)
	if err := h.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, h.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, h.st)
}

// pay spends the items; false when the role cannot pay.
func (h *hwSvc) pay(s *gateway.Session, role *store.Role, items [][]int64) (bool, error) {
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 && p[1] > 0 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	sa, si := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l), 1, sa, si)
	err := h.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := sendAssets(s, h.st, role.RID); err != nil {
		return false, err
	}
	return true, sendBackpack(s, h.st)
}

// open runs fn when the feature is open for the role, otherwise answers cmd with fail.
func (h *hwSvc) open(s *gateway.Session, cmd uint16, fail proto.Message, fn func(role *store.Role, st *hwState) error) error {
	return h.with(s, func(role *store.Role, st *hwState) error {
		ok, err := h.isOpen(role)
		if err != nil {
			return err
		}
		if !ok {
			fail["msg"] = "通关卡达尔森林3开启"
			return s.Send(cmd, fail)
		}
		return fn(role, st)
	})
}

func (h *hwSvc) push(s *gateway.Session, o *hwOne) error { return s.Send(24107, hwEntry(o)) }

func (h *hwSvc) list(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, st *hwState) error {
		out := []proto.Message{}
		ids := make([]int64, 0, len(st.Hallows))
		for _, o := range st.Hallows {
			ids = append(ids, o.ID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			out = append(out, hwEntry(st.hallow(id)))
		}
		return s.Send(24100, proto.Message{"hallows": out})
	})
}

func (h *hwSvc) sign(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, st *hwState) error {
		st.Seen = true
		return s.Send(24124, proto.Message{"is_first": 1})
	})
}

func (h *hwSvc) taskMessage(f *hwFacts, claimed map[string]bool, t hwTask) proto.Message {
	tv := int64(1)
	if len(t.Progress) > 0 {
		tv = t.Progress[0].TargetVal
	}
	v := f.value(t)
	finish := int64(0)
	switch {
	case claimed[strconv.FormatInt(t.ID, 10)]:
		finish = 2
	case v >= tv:
		finish = 1
	}
	return proto.Message{"id": t.ID, "finish": finish, "target_val": tv, "value": v}
}

func (h *hwSvc) tasks(s *gateway.Session, _ proto.Message) error {
	return h.open(s, 24120, proto.Message{"list": []proto.Message{}}, func(role *store.Role, st *hwState) error {
		return h.sendTasks(s, role, st, 24120, 0)
	})
}

// sendTasks answers (or pushes) the unlock tasks of the hallows up to the open one; only = one task id, -n = the tasks of
// hallow n, 0 = all.
func (h *hwSvc) sendTasks(s *gateway.Session, role *store.Role, st *hwState, cmd uint16, only int64) error {
	f, err := h.facts(role)
	if err != nil {
		return err
	}
	cur := st.next()
	list := []proto.Message{}
	for _, id := range hallowsCfg.taskIDs {
		t, _ := hallowsCfg.task(id)
		if t.HID <= cur && (only == 0 || only == id || (only < 0 && t.HID == -only)) {
			list = append(list, h.taskMessage(f, st.Claimed, t))
		}
	}
	return s.Send(cmd, proto.Message{"list": list})
}

func (h *hwSvc) submit(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.open(s, 24122, proto.Message{"code": 0}, func(role *store.Role, st *hwState) error {
		refuse := func(msg string) error { return s.Send(24122, proto.Message{"code": 0, "msg": msg}) }
		t, ok := hallowsCfg.task(id)
		if !ok || t.HID != st.next() {
			return refuse("任务不存在")
		}
		if st.Claimed[strconv.FormatInt(id, 10)] {
			return refuse("已经领取")
		}
		f, err := h.facts(role)
		if err != nil {
			return err
		}
		if m := h.taskMessage(f, st.Claimed, t); m.Int("finish") != 1 {
			return refuse("任务未完成")
		}
		st.Claimed[strconv.FormatInt(id, 10)] = true
		if err := h.gain(s, role, t.Items); err != nil {
			return err
		}
		if err := s.Send(24122, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := h.sendTasks(s, role, st, 24121, id); err != nil {
			return err
		}
		for _, other := range hallowsCfg.taskIDs {
			if o, _ := hallowsCfg.task(other); o.HID == t.HID && !st.Claimed[strconv.FormatInt(other, 10)] {
				return nil
			}
		}
		// all four done: the hallow is yours
		o := &hwOne{ID: t.HID, Step: 1, SkillLev: 1}
		st.Hallows[strconv.FormatInt(o.ID, 10)] = o
		if err := s.Send(24108, hwEntry(o)); err != nil {
			return err
		}
		if st.next() <= hallowsCount {
			return h.sendTasks(s, role, st, 24121, -st.next())
		}
		return nil
	})
}

func (h *hwSvc) owned(st *hwState, id int64) *hwOne { return st.hallow(id) }

func (h *hwSvc) advance(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.open(s, 24101, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *hwState) error {
		refuse := func(msg string) error { return s.Send(24101, proto.Message{"result": 0, "msg": msg, "id": id}) }
		o := h.owned(st, id)
		if o == nil {
			return refuse("神技未解锁")
		}
		if o.Step >= hallowsCfg.MaxLev[id-1] {
			return refuse("神技已满级")
		}
		step := hallowsCfg.Info[hwKey(id, o.Step)]
		ok, err := h.pay(s, role, step.Loss)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("材料不足")
		}
		// the lucky bar fills by a tenth to a fifth of its cap per try (a guess: the cap is the only number the client has)
		o.Lucky += step.MaxLucky/10 + h.rng().Int63n(step.MaxLucky/10+1) + 1
		result := int64(2)
		if o.Lucky >= step.MaxLucky {
			o.Step++
			o.Lucky = 0
			result = 1
		}
		if err := h.push(s, o); err != nil {
			return err
		}
		return s.Send(24101, proto.Message{"result": result, "msg": "", "id": id})
	})
}

func (h *hwSvc) skillUp(s *gateway.Session, req proto.Message) error {
	id := req.Int("hallows_id")
	return h.open(s, 24103, proto.Message{"result": 0, "hallows_id": id}, func(role *store.Role, st *hwState) error {
		refuse := func(msg string) error { return s.Send(24103, proto.Message{"result": 0, "msg": msg, "hallows_id": id}) }
		o := h.owned(st, id)
		if o == nil {
			return refuse("神技未解锁")
		}
		up, ok := hallowsCfg.SkillUp[hwKey(id, o.SkillLev)]
		if !ok || len(up.Lose) == 0 || o.SkillLev >= hallowsCfg.SkillMaxLev[id-1] {
			return refuse("技能已满级")
		}
		if o.Step < up.LevLimit {
			return refuse("神技等级不足")
		}
		paid, err := h.pay(s, role, up.Lose)
		if err != nil {
			return err
		}
		if !paid {
			return refuse("材料不足")
		}
		o.SkillLev++
		if err := h.push(s, o); err != nil {
			return err
		}
		return s.Send(24103, proto.Message{"result": 1, "msg": "技能升级成功", "hallows_id": id})
	})
}

func (h *hwSvc) trace(s *gateway.Session, req proto.Message) error {
	id, num := req.Int("hallows_id"), req.Int("num")
	return h.open(s, 24104, proto.Message{"result": 0, "id": id}, func(role *store.Role, st *hwState) error {
		refuse := func(msg string) error { return s.Send(24104, proto.Message{"result": 0, "msg": msg, "id": id}) }
		o := h.owned(st, id)
		if o == nil {
			return refuse("神技未解锁")
		}
		if o.Step < hallowsCfg.num("imprint_lowest") {
			return refuse("神技等级不足，无法使用刻印石")
		}
		if num < 1 || o.Seal+num > hallowsCfg.Trace[hwKey(id, o.Step)] {
			return refuse("已达到可使用的刻印石上限")
		}
		paid, err := h.pay(s, role, [][]int64{{hallowsCfg.num("id_stone"), num}})
		if err != nil {
			return err
		}
		if !paid {
			return refuse("刻印石不足")
		}
		o.Seal += num
		if err := h.push(s, o); err != nil {
			return err
		}
		return s.Send(24104, proto.Message{"result": 1, "msg": "使用成功", "id": id})
	})
}

func (h *hwSvc) refine(s *gateway.Session, req proto.Message) error {
	id := req.Int("hallows_id")
	return h.open(s, 24135, proto.Message{"code": 0, "hallows_id": id, "refine_lev": 0}, func(role *store.Role, st *hwState) error {
		o := h.owned(st, id)
		refuse := func(msg string) error {
			var lev int64
			if o != nil {
				lev = o.Refine
			}
			return s.Send(24135, proto.Message{"code": 0, "msg": msg, "hallows_id": id, "refine_lev": lev})
		}
		if o == nil {
			return refuse("神技未解锁")
		}
		if int64(role.Lev) < hallowsCfg.RefineOpenLev {
			return refuse("船长等级不足")
		}
		if o.Refine >= int64(len(hallowsCfg.Refine[id-1])) {
			return refuse("精炼已满级")
		}
		next := hallowsCfg.Refine[id-1][o.Refine]
		if o.Step < next.NeedLev {
			return refuse("神技等级不足")
		}
		paid, err := h.pay(s, role, next.Expend)
		if err != nil {
			return err
		}
		if !paid {
			return refuse("材料不足")
		}
		o.Refine++
		return s.Send(24135, proto.Message{"code": 1, "msg": "精炼成功", "hallows_id": id, "refine_lev": o.Refine})
	})
}

// reset gives the hallow back to step 1 and returns what was spent on it, except the coins (the client's own warning text).
func (h *hwSvc) reset(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.open(s, 24123, proto.Message{"code": 0}, func(role *store.Role, st *hwState) error {
		refuse := func(msg string) error { return s.Send(24123, proto.Message{"code": 0, "msg": msg}) }
		o := h.owned(st, id)
		if o == nil {
			return refuse("神技未解锁")
		}
		if o.Step == 1 && o.Lucky == 0 && o.Seal == 0 && o.SkillLev == 1 && o.Refine == 0 {
			return refuse("神技还没有培养过")
		}
		back := map[int64]int64{}
		add := func(items [][]int64) {
			for _, p := range items {
				if len(p) > 1 && p[0] != 1 {
					back[p[0]] += p[1]
				}
			}
		}
		for n := int64(1); n < o.Step; n++ {
			add(hallowsCfg.Info[hwKey(id, n)].Loss)
		}
		for n := int64(1); n < o.SkillLev; n++ {
			add(hallowsCfg.SkillUp[hwKey(id, n)].Lose)
		}
		back[hallowsCfg.num("id_stone")] += o.Seal
		if o.Refine > 0 {
			add(hallowsCfg.Refine[id-1][o.Refine-1].DecomposeAward)
		}
		var items [][]int64
		for bid, n := range back {
			items = append(items, []int64{bid, n})
		}
		sort.Slice(items, func(i, j int) bool { return items[i][0] < items[j][0] })
		*o = hwOne{ID: id, Step: 1, SkillLev: 1, Look: o.Look}
		if err := h.gain(s, role, items); err != nil {
			return err
		}
		if err := h.push(s, o); err != nil {
			return err
		}
		return s.Send(24123, proto.Message{"code": 1, "msg": "洗练成功"})
	})
}

func (h *hwSvc) lookMessage(l *hwLook) proto.Message {
	return proto.Message{"id": l.ID, "endtime": l.End, "eqm_hallows": l.On}
}

func (h *hwSvc) looks(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, st *hwState) error {
		out := []proto.Message{}
		for _, m := range hallowsCfg.Magic {
			if l := st.look(m.ID); l != nil {
				out = append(out, h.lookMessage(l))
			}
		}
		return s.Send(24125, proto.Message{"hallows_looks": out})
	})
}

func (h *hwSvc) lookTasks(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, st *hwState) error {
		f, err := h.facts(role)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range hallowsCfg.magicTaskIDs {
			t, _ := hallowsCfg.magicTask(id)
			list = append(list, h.taskMessage(f, st.LookClaimed, t))
		}
		return s.Send(24128, proto.Message{"list": list})
	})
}

func (h *hwSvc) lookOpen(s *gateway.Session, cmd uint16, role *store.Role) (bool, error) {
	if need := hallowsCfg.num("illusion_open"); int64(role.Lev) < need {
		return false, s.Send(cmd, proto.Message{"code": 0, "msg": "船长等级到达" + strconv.FormatInt(need, 10) + "级开启"})
	}
	return true, nil
}

func (h *hwSvc) submitLook(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.with(s, func(role *store.Role, st *hwState) error {
		if ok, err := h.lookOpen(s, 24130, role); !ok {
			return err
		}
		refuse := func(msg string) error { return s.Send(24130, proto.Message{"code": 0, "msg": msg}) }
		t, ok := hallowsCfg.magicTask(id)
		if !ok {
			return refuse("任务不存在")
		}
		if st.LookClaimed[strconv.FormatInt(id, 10)] {
			return refuse("已经领取")
		}
		f, err := h.facts(role)
		if err != nil {
			return err
		}
		if m := h.taskMessage(f, st.LookClaimed, t); m.Int("finish") != 1 {
			return refuse("任务未完成")
		}
		st.LookClaimed[strconv.FormatInt(id, 10)] = true
		if err := h.gain(s, role, t.Items); err != nil {
			return err
		}
		if err := s.Send(24130, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		m := h.taskMessage(f, st.LookClaimed, t)
		return s.Send(24129, proto.Message{"list": []proto.Message{m}})
	})
}

func (h *hwSvc) unlockLook(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.with(s, func(role *store.Role, st *hwState) error {
		if ok, err := h.lookOpen(s, 24131, role); !ok {
			return err
		}
		refuse := func(msg string) error { return s.Send(24131, proto.Message{"code": 0, "msg": msg}) }
		def, ok := hallowsCfg.look(id)
		if !ok {
			return refuse("幻化不存在")
		}
		if st.look(id) != nil {
			return refuse("已经拥有")
		}
		switch def.IsItem {
		case 0: // by its tasks
			for _, tid := range hallowsCfg.magicTaskIDs {
				if t, _ := hallowsCfg.magicTask(tid); t.HID == id && !st.LookClaimed[strconv.FormatInt(tid, 10)] {
					return refuse("解锁任务未完成")
				}
			}
		case 1: // by an item
			paid, err := h.pay(s, role, def.Loss)
			if err != nil {
				return err
			}
			if !paid {
				return refuse("道具不足")
			}
		default:
			return refuse("该幻化只能通过活动获得")
		}
		l := &hwLook{ID: id}
		if def.Time > 0 {
			l.End = s.Now().Unix() + def.Time
		}
		st.Looks[strconv.FormatInt(id, 10)] = l
		if err := s.Send(24131, proto.Message{"code": 1, "msg": "解锁成功"}); err != nil {
			return err
		}
		if err := s.Send(24126, h.lookMessage(l)); err != nil {
			return err
		}
		return s.Send(24127, h.lookMessage(l))
	})
}

// wear puts a look over a hallow (flag 1) or takes it off (flag 2); a hallow wears one look, a look sits on one hallow.
func (h *hwSvc) wear(s *gateway.Session, req proto.Message) error {
	id, hid, flag := req.Int("id"), req.Int("hallows_id"), req.Int("flag")
	return h.with(s, func(role *store.Role, st *hwState) error {
		if ok, err := h.lookOpen(s, 24132, role); !ok {
			return err
		}
		refuse := func(msg string) error { return s.Send(24132, proto.Message{"code": 0, "msg": msg}) }
		l := st.look(id)
		if l == nil {
			return refuse("还没有这个幻化")
		}
		var touched []*hwOne
		var looks []*hwLook
		if l.On != 0 { // off whatever it sits on
			if o := st.hallow(l.On); o != nil && o.Look == id {
				o.Look = 0
				touched = append(touched, o)
			}
			l.On = 0
		}
		if flag == 1 {
			o := h.owned(st, hid)
			if o == nil {
				return refuse("神技未解锁")
			}
			if o.Look != 0 && o.Look != id { // the look it wore goes free
				if prev := st.look(o.Look); prev != nil {
					prev.On = 0
					looks = append(looks, prev)
				}
			}
			o.Look, l.On = id, hid
			touched = append(touched, o)
		}
		looks = append(looks, l)
		if err := s.Send(24132, proto.Message{"code": 1, "msg": "操作成功"}); err != nil {
			return err
		}
		for _, x := range looks {
			if err := s.Send(24126, h.lookMessage(x)); err != nil {
				return err
			}
		}
		for _, o := range touched {
			if err := h.push(s, o); err != nil {
				return err
			}
		}
		return nil
	})
}
