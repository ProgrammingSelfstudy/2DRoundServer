package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The activity schedule: the holiday activities of the client's action tabs (16601 the list of what is open, 16602 the
// red dots, 16603 / 16620 one activity with its aims, 16604 take an aim's reward). The operator puts activities in the
// schedule (admin API /api/activities): a window, the texts and panel the client shows and a list of aims. An aim counts
// a game event (the same labels the daily quests count, see questEvent) and its reward is taken when the count reaches
// the target. The ticker tells the online roles (16607) when the set of open activities changes.
//
// This is the common part; an activity with a protocol family of its own (the dial, the petard, ...) is a module of its
// own that asks ActivityOpen for its window.

const (
	actModule  = "activity"
	actForever = 315360000 // the seconds left of an activity that never ends: ten years
	maxAims    = 60
)

// ActivityAim is one line of an activity: the aim's text, the event it counts (empty: reachable at once), the count to
// reach and the reward.
type ActivityAim struct {
	ID      int64      `json:"id"`
	Str     string     `json:"str"`
	Event   string     `json:"event"`
	Target  int64      `json:"target"`
	Rewards [][2]int64 `json:"rewards"`
}

// ActivitySpec is the JSON of an activity row: what the client's tab shows and the aims.
type ActivitySpec struct {
	Title       string        `json:"title"`
	Title2      string        `json:"title2"`
	Ico         string        `json:"ico"`
	TypeIco     int64         `json:"type_ico"`
	TopBanner   string        `json:"top_banner"`
	RuleStr     string        `json:"rule_str"`
	TimeStr     string        `json:"time_str"`
	BottomAlert string        `json:"bottom_alert"`
	AimTitle    string        `json:"aim_title"`
	RewardTitle string        `json:"reward_title"`
	ChannelBan  string        `json:"channel_ban"`
	PanelType   int64         `json:"panel_type"`    // ActionPanelTypeView of the client: 2 gift, 16 common, ...
	SortVal     int64         `json:"sort_val"`      // the order of the tabs
	CliType     int64         `json:"cli_type"`      // the main icon (MainuiConst.icon) the tab belongs to; 0 none
	CliTypeName string        `json:"cli_type_name"` // the name under that icon
	Daily       bool          `json:"daily"`         // counts and rewards start again every day
	Aims        []ActivityAim `json:"aims"`
}

// ParseActivitySpec reads and checks the spec JSON of an activity.
func ParseActivitySpec(raw string) (*ActivitySpec, error) {
	sp := &ActivitySpec{}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(sp); err != nil {
		return nil, fmt.Errorf("spec: %v", err)
	}
	switch {
	case strings.TrimSpace(sp.Title) == "" || utf8.RuneCountInString(sp.Title) > 40:
		return nil, errors.New("spec.title is required, at most 40 characters")
	case sp.PanelType < 1 || sp.PanelType > 145:
		return nil, errors.New("spec.panel_type must be 1 to 145")
	case len(sp.Aims) > maxAims:
		return nil, fmt.Errorf("at most %d aims", maxAims)
	}
	for _, f := range []string{sp.Title2, sp.Ico, sp.TopBanner, sp.TimeStr, sp.BottomAlert, sp.AimTitle, sp.RewardTitle, sp.ChannelBan, sp.CliTypeName} {
		if utf8.RuneCountInString(f) > 200 {
			return nil, errors.New("a text of the spec is longer than 200 characters")
		}
	}
	if utf8.RuneCountInString(sp.RuleStr) > 2000 {
		return nil, errors.New("spec.rule_str is longer than 2000 characters")
	}
	seen := map[int64]bool{}
	for _, a := range sp.Aims {
		switch {
		case a.ID <= 0 || seen[a.ID]:
			return nil, fmt.Errorf("aim id %d must be positive and unique", a.ID)
		case utf8.RuneCountInString(a.Str) > 200 || utf8.RuneCountInString(a.Event) > 40:
			return nil, fmt.Errorf("aim %d: text or event too long", a.ID)
		case a.Target < 0 || a.Target > 1000000000:
			return nil, fmt.Errorf("aim %d: target out of range", a.ID)
		case len(a.Rewards) == 0 || len(a.Rewards) > 10:
			return nil, fmt.Errorf("aim %d: 1 to 10 rewards", a.ID)
		}
		seen[a.ID] = true
		for _, r := range a.Rewards {
			if !ItemKnown(r[0]) || r[1] <= 0 || r[1] > 1000000000 {
				return nil, fmt.Errorf("aim %d: unknown item %d or bad amount", a.ID, r[0])
			}
		}
	}
	return sp, nil
}

// ValidateActivity is the reason a schedule row is not acceptable, empty when it is.
func ValidateActivity(a *store.Activity) string {
	switch {
	case a.CampID <= 0 || a.BID <= 0:
		return "camp_id and bid must be positive"
	case a.Status != 0 && a.Status != 1:
		return "status must be 0 (off) or 1 (on)"
	case a.StartAt < 0 || a.EndAt < 0:
		return "times cannot be negative"
	case a.EndAt != 0 && a.EndAt <= a.StartAt:
		return "end_at must be after start_at (or 0 for no end)"
	}
	if _, err := ParseActivitySpec(a.Spec); err != nil {
		return err.Error()
	}
	return ""
}

// actEntry is an activity that is open.
type actEntry struct {
	Row  store.Activity
	Spec *ActivitySpec
}

func (e actEntry) remain(now time.Time) int64 {
	if e.Row.EndAt == 0 {
		return actForever
	}
	return max(0, e.Row.EndAt-now.Unix())
}

// openActivities is what is open on a server now, by tab order; of two activities with one bid the older row wins.
func openActivities(st *store.Store, srv string, now time.Time) ([]actEntry, error) {
	rows, err := st.ActivitiesOf(srv)
	if err != nil {
		return nil, err
	}
	var out []actEntry
	seen := map[int64]bool{}
	for _, r := range rows {
		if (r.StartAt != 0 && now.Unix() < r.StartAt) || (r.EndAt != 0 && now.Unix() >= r.EndAt) || seen[r.BID] {
			continue
		}
		sp, err := ParseActivitySpec(r.Spec)
		if err != nil {
			continue // a row the admin API would not have taken: left out rather than breaking the list
		}
		seen[r.BID] = true
		out = append(out, actEntry{Row: r, Spec: sp})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Spec.SortVal < out[j].Spec.SortVal })
	return out, nil
}

// ActivityOpen is whether the activity with camp_id is open on a server now and its window; for the modules that run an
// activity of their own.
func ActivityOpen(st *store.Store, srv string, camp int64, now time.Time) (start, end int64, open bool) {
	es, err := openActivities(st, srv, now)
	if err != nil {
		return 0, 0, false
	}
	for _, e := range es {
		if e.Row.CampID == camp {
			return e.Row.StartAt, e.Row.EndAt, true
		}
	}
	return 0, 0, false
}

// ---- what a role has done

type actProg struct {
	Day      int64
	Progress map[int64]int64
	Claimed  []int64
}

type actState struct{ Camps map[int64]*actProg }

func loadAct(st *store.Store, rid int64) (*actState, error) {
	s := &actState{}
	if _, err := st.LoadState(rid, actModule, s); err != nil {
		return nil, err
	}
	if s.Camps == nil {
		s.Camps = map[int64]*actProg{}
	}
	return s, nil
}

// prog is the role's record of an activity; a daily one starts over on a new day.
func (s *actState) prog(e actEntry, now time.Time) *actProg {
	p := s.Camps[e.Row.CampID]
	day := dayKey(now)
	if p == nil || (e.Spec.Daily && p.Day != day) {
		p = &actProg{Day: day}
		s.Camps[e.Row.CampID] = p
	}
	if p.Progress == nil {
		p.Progress = map[int64]int64{}
	}
	return p
}

const (
	aimOpen    = 0
	aimReady   = 1
	aimClaimed = 2
)

func (p *actProg) status(a ActivityAim) int64 {
	switch {
	case has(p.Claimed, a.ID):
		return aimClaimed
	case a.Event == "" || p.Progress[a.ID] >= a.Target:
		return aimReady
	}
	return aimOpen
}

func (p *actProg) ready(e actEntry) int64 {
	var n int64
	for _, a := range e.Spec.Aims {
		if p.status(a) == aimReady {
			n++
		}
	}
	return n
}

func actRewardList(rs [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, r := range rs {
		out = append(out, proto.Message{"bid": r[0], "num": r[1]})
	}
	return out
}

// detail is the 16603 message of an activity for a role.
func actDetail(e actEntry, p *actProg, now time.Time) proto.Message {
	aims := []proto.Message{}
	finish := int64(1)
	for _, a := range e.Spec.Aims {
		cur := min(p.Progress[a.ID], a.Target)
		str := a.Str
		if a.Event != "" && a.Target > 1 {
			str = fmt.Sprintf("%s (%d/%d)", a.Str, cur, a.Target)
		}
		st := p.status(a)
		if st != aimClaimed {
			finish = 0
		}
		aims = append(aims, proto.Message{"aim": a.ID, "aim_str": str, "status": st, "item_list": actRewardList(a.Rewards),
			"aim_args": []proto.Message{{"aim_args_key": 4, "aim_args_val": a.Target, "aim_args_str": ""}, {"aim_args_key": 5, "aim_args_val": cur, "aim_args_str": ""}}})
	}
	if len(e.Spec.Aims) == 0 {
		finish = 0
	}
	return proto.Message{"bid": e.Row.BID, "cli_type": e.Spec.CliType, "remain_sec": e.remain(now), "finish": finish, "aim_list": aims,
		"item_effect_list": []proto.Message{}, "args": []proto.Message{}, "client_reward": "", "rank_list": []proto.Message{}}
}

// ---- the handlers

type actSvc struct{ st *store.Store }

// RegisterActivity registers the schedule's part of the action tabs (16601-16604, 16620, 16687).
func RegisterActivity(r *gateway.Router, st *store.Store) {
	a := &actSvc{st: st}
	r.Handle(16601, a.list)
	r.Handle(16602, a.red)
	r.Handle(16603, a.one)
	r.Handle(16604, a.claim)
	r.Handle(16620, a.batch)
	r.Handle(16687, a.redOne)
	// the original tells the role how many aims of each open activity can be taken as the city comes up
	r.OnEnter(func(s *gateway.Session, _ proto.Message) error {
		es, state, err := a.open(s)
		if err != nil || s.Role() == nil {
			return err
		}
		for _, e := range es {
			if err := s.Send(16606, proto.Message{"bid": e.Row.BID, "can_get_num": state.prog(e, s.Now()).ready(e)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *actSvc) open(s *gateway.Session) ([]actEntry, *actState, error) {
	role := s.Role()
	if role == nil {
		return nil, nil, nil
	}
	es, err := openActivities(a.st, role.SrvID, s.Now())
	if err != nil {
		return nil, nil, err
	}
	state, err := loadAct(a.st, role.RID)
	return es, state, err
}

func (a *actSvc) list(s *gateway.Session, _ proto.Message) error {
	es, _, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	out := []proto.Message{}
	for _, e := range es {
		sp := e.Spec
		out = append(out, proto.Message{"camp_id": e.Row.CampID, "bid": e.Row.BID, "title": sp.Title, "title2": sp.Title2, "ico": sp.Ico, "type_ico": sp.TypeIco,
			"top_banner": sp.TopBanner, "rule_str": sp.RuleStr, "time_str": sp.TimeStr, "bottom_alert": sp.BottomAlert, "aim_title": sp.AimTitle,
			"panel_type": sp.PanelType, "channel_ban": sp.ChannelBan, "sort_val": sp.SortVal, "reward_title": sp.RewardTitle, "remain_sec": e.remain(s.Now()),
			"cli_type": sp.CliType, "cli_type_name": sp.CliTypeName})
	}
	return s.Send(16601, proto.Message{"holiday_list": out})
}

func (a *actSvc) red(s *gateway.Session, _ proto.Message) error {
	es, state, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	out := []proto.Message{}
	for _, e := range es {
		out = append(out, proto.Message{"bid": e.Row.BID, "can_get_num": state.prog(e, s.Now()).ready(e)})
	}
	return s.Send(16602, proto.Message{"holiday_list": out})
}

func (a *actSvc) redOne(s *gateway.Session, req proto.Message) error {
	es, state, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	code := int64(0)
	for _, e := range es {
		if e.Row.BID == req.Int("bid") && state.prog(e, s.Now()).ready(e) > 0 {
			code = 1
		}
	}
	return s.Send(16687, proto.Message{"bid": req.Int("bid"), "code": code})
}

func actEntryOf(es []actEntry, bid int64) (actEntry, bool) {
	for _, e := range es {
		if e.Row.BID == bid {
			return e, true
		}
	}
	return actEntry{}, false
}

func (a *actSvc) one(s *gateway.Session, req proto.Message) error {
	es, state, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	e, ok := actEntryOf(es, req.Int("bid"))
	if !ok {
		return s.Send(16603, proto.Message{"bid": req.Int("bid"), "cli_type": 0, "remain_sec": 0, "finish": 0, "aim_list": []proto.Message{},
			"item_effect_list": []proto.Message{}, "args": []proto.Message{}, "client_reward": "", "rank_list": []proto.Message{}})
	}
	return s.Send(16603, actDetail(e, state.prog(e, s.Now()), s.Now()))
}

func (a *actSvc) batch(s *gateway.Session, req proto.Message) error {
	es, state, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	out := []proto.Message{}
	for _, id := range req.Arr("id_list") {
		if e, ok := actEntryOf(es, id.Int("id")); ok {
			out = append(out, actDetail(e, state.prog(e, s.Now()), s.Now()))
		}
	}
	return s.Send(16620, proto.Message{"holiday_list": out})
}

func (a *actSvc) claim(s *gateway.Session, req proto.Message) error {
	es, state, err := a.open(s)
	if err != nil || s.Role() == nil {
		return err
	}
	role := s.Role()
	refuse := func(msg string) error { return s.Send(16604, proto.Message{"code": 0, "msg": msg}) }
	e, ok := actEntryOf(es, req.Int("bid"))
	if !ok {
		return refuse("活动已经结束")
	}
	var aim *ActivityAim
	for i := range e.Spec.Aims {
		if e.Spec.Aims[i].ID == req.Int("aim") {
			aim = &e.Spec.Aims[i]
		}
	}
	if aim == nil {
		return refuse("没有这个奖励")
	}
	p := state.prog(e, s.Now())
	switch p.status(*aim) {
	case aimClaimed:
		return refuse("已经领取过了")
	case aimOpen:
		return refuse("还没有达成")
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(toGains(aim.Rewards), 1, ga, gi)
	if err := a.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	p.Claimed = append(p.Claimed, aim.ID)
	if err := a.st.SaveState(role.RID, actModule, state); err != nil {
		return err
	}
	if err := sendAssets(s, a.st, role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, a.st); err != nil {
		return err
	}
	if err := s.Send(16604, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	if err := s.Send(16603, actDetail(e, p, s.Now())); err != nil {
		return err
	}
	return s.Send(16606, proto.Message{"bid": e.Row.BID, "can_get_num": p.ready(e)})
}

// activityEvent counts a game event for the aims that wait for it and tells the role when an aim can be taken.
func activityEvent(s *gateway.Session, st *store.Store, rid int64, label string, n int64) error {
	role := s.Role()
	if role == nil || n <= 0 {
		return nil
	}
	es, err := openActivities(st, role.SrvID, s.Now())
	if err != nil || len(es) == 0 {
		return err
	}
	var state *actState
	for _, e := range es {
		wants := false
		for _, a := range e.Spec.Aims {
			wants = wants || a.Event == label
		}
		if !wants {
			continue
		}
		if state == nil {
			if state, err = loadAct(st, rid); err != nil {
				return err
			}
		}
		p := state.prog(e, s.Now())
		before := p.ready(e)
		for _, a := range e.Spec.Aims {
			if a.Event == label && !has(p.Claimed, a.ID) {
				p.Progress[a.ID] = min(p.Progress[a.ID]+n, max(a.Target, 1))
			}
		}
		if err := st.SaveState(rid, actModule, state); err != nil {
			return err
		}
		if now := p.ready(e); now > before {
			if err := s.Send(16606, proto.Message{"bid": e.Row.BID, "can_get_num": now}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- the ticker

// activitySignature names the set of open activities of a server and the state of their rows; it changes when one
// opens, closes or is edited.
func activitySignature(st *store.Store, srv string, now time.Time) string {
	es, err := openActivities(st, srv, now)
	if err != nil {
		return "error"
	}
	var b strings.Builder
	for _, e := range es {
		fmt.Fprintf(&b, "%d:%d;", e.Row.ID, e.Row.UpdatedAt)
	}
	return b.String()
}

// WatchActivities tells the online roles (16607 with type 0, which makes the client ask for the list again) when the
// set of open activities of their server changes. It checks every interval and returns when stop is closed.
func WatchActivities(st *store.Store, hub *gateway.Hub, every time.Duration, now func() time.Time, stop <-chan struct{}) {
	last := map[string]string{}
	tick := func() {
		bySrv := map[string][]*gateway.Session{}
		hub.Each(func(_ int64, s *gateway.Session) {
			if role := s.Role(); role != nil {
				bySrv[role.SrvID] = append(bySrv[role.SrvID], s)
			}
		})
		for srv, sessions := range bySrv {
			sig := activitySignature(st, srv, now())
			if prev, seen := last[srv]; seen && prev != sig {
				for _, s := range sessions {
					_ = s.Send(16607, proto.Message{"type": 0})
				}
			}
			last[srv] = sig
		}
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			tick()
		}
	}
}
