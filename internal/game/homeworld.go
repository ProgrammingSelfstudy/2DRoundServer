package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// homeworld.json: the home world of the client (tools/extract_homeworld.py).
//
//go:embed data/homeworld.json
var homeworldJSON []byte

const (
	homeModule      = "homeworld"
	homeBoardModule = "homeworld_board"
	homeCoinAsset   = 31 // 家园币
	homeDiamond     = 3
	homeFurniture   = 30 // item type of a piece of furniture; the home bag (10502) lists them
	homeNameMax     = 12
	homeVisitKeep   = 50
	homeRandomMax   = 6
	homeDefaultWall = 200152 // the wall and the floor every new home starts with, free and without limit
	homeDefaultLand = 200151
	homeUnitFloor   = 3
	homeUnitWall    = 4
)

type homeUnitConf struct {
	Bid, Soft, Set, Area, Type, Effect int64
	Name                               string
}

type homeConf struct {
	Const     map[string]json.RawMessage    `json:"const"`
	Units     []homeUnitConf                `json:"units"`
	SuitSoft  [][]struct{ Num, Soft int64 } `json:"suit_soft"`
	SuitAward [][]struct {
		ID, Num int64
		Reward  [][2]int64
	} `json:"suit_award"`
	Figures []struct {
		ID, First, OpenSoft int64
		Loss                [][2]int64
		Tips                string
	} `json:"figures"`
	Coin []struct{ Soft, Coin int64 } `json:"coin"`
	unit map[int64]homeUnitConf
}

var homeCfg = func() *homeConf {
	c := &homeConf{}
	if err := json.Unmarshal(homeworldJSON, c); err != nil || len(c.Units) != 80 || len(c.Figures) == 0 || len(c.Coin) == 0 {
		panic("game: bad homeworld.json")
	}
	c.unit = map[int64]homeUnitConf{}
	for _, u := range c.Units {
		c.unit[u.Bid] = u
	}
	return c
}()

func (c *homeConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

func (c *homeConf) pairs(key string) [][2]int64 {
	var l [][2]int64
	_ = json.Unmarshal(c.Const[key], &l)
	return l
}

// homeUnit is a piece of furniture standing in a room: Index is the cell the client put it on.
type homeUnit struct{ Bid, Index, Dir int64 }

type homeFloor struct {
	Wall, Land int64
	List       []homeUnit
}

// homeVisit is one visit to a home.
type homeVisit struct {
	RID, Time int64
	SrvID     string
}

// homeState is what a role keeps of its home.
type homeState struct {
	Name       string
	Opened     bool
	Used       int64   // the figure in use
	Figures    []int64 // the figures it has
	MaxSoft    int64   // the best comfort a floor ever had; opens the figures
	MaxAll     int64   // the best total over the floors; opens the second floor
	Floors     []homeFloor
	Main       int64 // the floor visitors and the owner enter first
	HookAt     int64 // when the home coin was last collected
	Claimed    []int64
	Collected  map[int64][]int64 // suit -> pieces of it the role ever had
	Visits     []homeVisit       // newest first
	Likes      int64             // all likes the home got
	LikeDay    int64
	LikedToday []int64 // rids of the homes liked today
	SeenDay    int64
	Seen       []int64 // rids that visited today (a visit counts once a day)
}

type homeBoard struct {
	Week   int64
	Open   []int64            // roles with a home
	Weekly map[int64][2]int64 // rid -> likes, visits of the week
}

type homeSvc struct {
	st *store.Store
	mu *sync.Mutex
}

// homeLock serialises everything that touches a home, the handlers here and the like of 10316.
var homeLock sync.Mutex

// RegisterHomeworld registers the home world (26001-26021): a room of two floors that the role furnishes with furniture
// from the home bag (bought in the furniture shop, type 41), comfort that opens figures and pays home coin by the hour,
// visits and likes of other homes, the sets of furniture with their rewards and the weekly popularity rank. Our rules
// where the client does not say, not captured. 10502 (the home bag) lists the furniture.
func RegisterHomeworld(r *gateway.Router, db *store.Store) {
	h := &homeSvc{st: db, mu: &homeLock}
	r.Handle(26001, h.myHome)
	r.Handle(26002, h.save)
	r.Handle(26003, h.visit)
	r.Handle(26004, h.figures)
	r.Handle(26005, h.useFigure)
	r.Handle(26006, h.buyFigure)
	r.Handle(26007, h.firstFigure)
	r.Handle(26009, h.randomHomes)
	r.Handle(26010, h.visitors)
	r.Handle(26011, h.rename)
	r.Handle(26013, h.suits)
	r.Handle(26014, h.claimSuit)
	r.Handle(26016, h.collectCoin)
	r.Handle(26017, h.first)
	r.Handle(26018, h.red)
	r.Handle(26019, h.worshipped)
	r.Handle(26020, h.rank)
	r.Handle(26021, h.mainFloor)
}

func homeWeek(now time.Time) int64 {
	t := dayStart(now.In(dayZone))
	return dayKey(t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)))
}

func contains(l []int64, v int64) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func (st *homeState) fix(role *store.Role, now time.Time) {
	for len(st.Floors) < 2 {
		st.Floors = append(st.Floors, homeFloor{Wall: homeDefaultWall, Land: homeDefaultLand})
	}
	if st.Main < 1 || st.Main > 2 {
		st.Main = 1
	}
	if day := dayKey(now); st.LikeDay != day {
		st.LikeDay, st.LikedToday = day, nil
	}
	if day := dayKey(now); st.SeenDay != day {
		st.SeenDay, st.Seen = day, nil
	}
}

func (h *homeSvc) load(rid int64, now time.Time) (*homeState, error) {
	st := &homeState{}
	if _, err := h.st.LoadState(rid, homeModule, st); err != nil {
		return nil, err
	}
	role, err := h.st.RoleByID(rid)
	if err != nil {
		return nil, err
	}
	st.fix(role, now)
	return st, nil
}

func (h *homeSvc) board(srv string, now time.Time) (*homeBoard, error) {
	b := &homeBoard{}
	if _, err := h.st.LoadGlobal(srv, homeBoardModule, b); err != nil {
		return nil, err
	}
	if wk := homeWeek(now); b.Week != wk {
		b.Week, b.Weekly = wk, nil
	}
	if b.Weekly == nil {
		b.Weekly = map[int64][2]int64{}
	}
	return b, nil
}

func (h *homeSvc) saveBoard(srv string, b *homeBoard) error {
	return h.st.SaveGlobal(srv, homeBoardModule, b)
}

func (b *homeBoard) score(rid int64) int64 {
	var f []int64 // likes, visits
	_ = json.Unmarshal(homeCfg.Const["popularity_formula"], &f)
	w := b.Weekly[rid]
	return w[0]*f[0] + w[1]*f[1]
}

// ---- the room

// floorUnits lists what stands in a floor as units, the wall and the floor first.
func (f homeFloor) bids() []int64 {
	out := []int64{f.Wall, f.Land}
	for _, u := range f.List {
		out = append(out, u.Bid)
	}
	return out
}

// soft is the comfort of a floor: every kind of furniture counts as many pieces as its effect count allows, a set of
// three or more kinds adds the bonus of its size.
func (f homeFloor) soft() int64 {
	count := map[int64]int64{}
	for _, b := range f.bids() {
		count[b]++
	}
	var total int64
	kinds := map[int64]int64{} // set -> kinds of it in the room
	for b, n := range count {
		u, ok := homeCfg.unit[b]
		if !ok {
			continue
		}
		total += min(n, max(1, u.Effect)) * u.Soft
		kinds[u.Set]++
	}
	for set, n := range kinds {
		if set < 1 || int(set) > len(homeCfg.SuitSoft) {
			continue
		}
		var bonus int64
		for _, e := range homeCfg.SuitSoft[set-1] {
			if n >= e.Num {
				bonus = e.Soft
			}
		}
		total += bonus
	}
	return min(total, homeCfg.num("comfort_limit"))
}

func (st *homeState) softs() (cur [2]int64, best, all int64) {
	bestFloor := int64(1)
	for i, f := range st.Floors {
		if i == 1 && !st.floorOpen(2) { // a floor not yet open does not count toward opening itself
			break
		}
		cur[i] = f.soft()
		all += cur[i]
		if cur[i] > cur[bestFloor-1] {
			bestFloor = int64(i + 1)
		}
	}
	return cur, bestFloor, all
}

// refresh keeps the records of the best comfort that open figures and the second floor.
func (st *homeState) refresh() (cur [2]int64, bestFloor int64) {
	var all int64
	cur, bestFloor, all = st.softs()
	st.MaxSoft = max(st.MaxSoft, cur[bestFloor-1])
	st.MaxAll = max(st.MaxAll, all)
	return
}

func (st *homeState) floorOpen(floor int64) bool {
	switch floor {
	case 1:
		return true
	case 2:
		return st.MaxAll >= 3000
	}
	return false
}

// otherBids counts what the other floors hold, which the client must subtract from what it may still place.
func (st *homeState) otherBids(floor int64) []proto.Message {
	count := map[int64]int64{}
	for i, f := range st.Floors {
		if int64(i+1) == floor {
			continue
		}
		for _, b := range f.bids() {
			if b == homeDefaultWall || b == homeDefaultLand {
				continue
			}
			count[b]++
		}
	}
	ids := make([]int64, 0, len(count))
	for b := range count {
		ids = append(ids, b)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []proto.Message{}
	for _, b := range ids {
		out = append(out, proto.Message{"bid": b, "num": count[b]})
	}
	return out
}

func unitMessages(l []homeUnit) []proto.Message {
	out := []proto.Message{}
	for _, u := range l {
		out = append(out, proto.Message{"bid": u.Bid, "index": u.Index, "dir": u.Dir})
	}
	return out
}

// coinRate is the home coin an hour for a comfort: the highest step of the client's table it reaches.
func coinRate(soft int64) int64 {
	var rate int64
	for _, c := range homeCfg.Coin {
		if soft >= c.Soft {
			rate = c.Coin
		}
	}
	return rate
}

// accumulated is the seconds of coin waiting, at most the hours the home may hoard.
func (st *homeState) accumulated(now time.Time) int64 {
	if !st.Opened || st.HookAt == 0 {
		return 0
	}
	return min(max(0, now.Unix()-st.HookAt), homeCfg.num("homecoin_hoarding_limit")*3600)
}

func (h *homeSvc) homeName(st *homeState, role *store.Role) string {
	if st.Name != "" {
		return st.Name
	}
	return role.Name + "的家园"
}

// visitors are the guests whose figures stand in the home: the last few that came within the show time.
func (h *homeSvc) visitorList(st *homeState, now time.Time) []proto.Message {
	out := []proto.Message{}
	seen := map[int64]bool{}
	limit := now.Unix() - homeCfg.num("visitor_show_time")*3600
	for _, v := range st.Visits {
		if v.Time < limit || seen[v.RID] || int64(len(out)) >= homeCfg.num("visitors_num_limit") {
			continue
		}
		seen[v.RID] = true
		g, err := h.st.RoleByID(v.RID)
		if err != nil {
			continue
		}
		gs, err := h.load(g.RID, now)
		if err != nil {
			continue
		}
		out = append(out, proto.Message{"rid": g.RID, "srv_id": g.SrvID, "name": g.Name, "look_id": gs.Used})
	}
	return out
}

func (h *homeSvc) with(s *gateway.Session, needHome bool, fn func(role *store.Role, st *homeState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fresh, err := h.st.RoleByID(role.RID)
	if err != nil {
		return err
	}
	st, err := h.load(fresh.RID, s.Now())
	if err != nil {
		return err
	}
	if err := fn(fresh, st); err != nil {
		return err
	}
	return h.st.SaveState(fresh.RID, homeModule, st)
}

func homeReply(s *gateway.Session, cmd uint16, ok bool, msg string, extra proto.Message) error {
	code := int64(0)
	if ok {
		code = 1
	}
	m := proto.Message{"code": code, "msg": msg}
	for k, v := range extra {
		m[k] = v
	}
	return s.Send(cmd, m)
}

// 26001: the role's own room. floor 0 is the main floor.
func (h *homeSvc) myHome(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		if !st.Opened {
			return nil
		}
		floor := req.Int("floor")
		if floor == 0 || !st.floorOpen(floor) {
			floor = st.Main
		}
		cur, best := st.refresh()
		f := st.Floors[floor-1]
		board, err := h.board(role.SrvID, s.Now())
		if err != nil {
			return err
		}
		_ = board
		left := homeCfg.num("day_praise_limit") - int64(len(st.LikedToday))
		_, _, all := st.softs()
		return s.Send(26001, proto.Message{
			"name": h.homeName(st, role), "look_id": st.Used, "worship": st.Likes, "rest_worship": max(0, left), "soft": cur[floor-1],
			"wall_bid": f.Wall, "land_bid": f.Land, "list": unitMessages(f.List), "visitors": h.visitorList(st, s.Now()),
			"acc_hook_time": st.accumulated(s.Now()), "floor": floor, "main_floor": st.Main, "max_soft_floor": best, "max_all_soft": max(st.MaxAll, all),
			"other_bid": st.otherBids(floor), "max_floor_soft": cur[best-1]})
	})
}

// 26002: save a floor. What the floor held goes back to the home bag, what it holds now comes out of it.
func (h *homeSvc) save(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		fail := func(msg string) error { return homeReply(s, 26002, false, msg, nil) }
		if !st.Opened {
			return fail("家园尚未开启")
		}
		floor := req.Int("floor")
		if floor == 0 {
			floor = st.Main
		}
		if !st.floorOpen(floor) {
			return fail("该楼层尚未解锁")
		}
		wall, land := req.Int("wall_bid"), req.Int("land_bid")
		if wall == 0 {
			wall = st.Floors[floor-1].Wall
		}
		if land == 0 {
			land = st.Floors[floor-1].Land
		}
		if u, ok := homeCfg.unit[wall]; !ok || u.Type != homeUnitWall {
			return fail("墙壁不存在")
		}
		if u, ok := homeCfg.unit[land]; !ok || u.Type != homeUnitFloor {
			return fail("地板不存在")
		}
		next := homeFloor{Wall: wall, Land: land}
		for _, m := range req.Arr("list") {
			u, ok := homeCfg.unit[m.Int("bid")]
			if !ok || u.Type == homeUnitFloor || u.Type == homeUnitWall {
				return fail("家具不存在")
			}
			if dir := m.Int("dir"); dir != 1 && dir != 2 {
				return fail("家具方向不对")
			}
			next.List = append(next.List, homeUnit{m.Int("bid"), m.Int("index"), m.Int("dir")})
		}
		delta := map[int64]int64{}
		for _, b := range next.bids() {
			if b != homeDefaultWall && b != homeDefaultLand {
				delta[b]++
			}
		}
		for _, b := range st.Floors[floor-1].bids() {
			if b != homeDefaultWall && b != homeDefaultLand {
				delta[b]--
			}
		}
		spend, gain := map[int64]int64{}, map[int64]int64{}
		for b, d := range delta {
			switch {
			case d > 0:
				spend[b] = d
			case d < 0:
				gain[b] = -d
			}
		}
		if err := h.st.Exchange(role.RID, map[int64]int64{}, spend, map[int64]int64{}, gain); err != nil {
			return fail("家具数量不足")
		}
		st.Floors[floor-1] = next
		cur, best := st.refresh()
		_, _, all := st.softs()
		if err := homeReply(s, 26002, true, "", proto.Message{"soft": cur[floor-1], "wall_bid": wall, "land_bid": land, "list": unitMessages(next.List),
			"floor": floor, "max_soft_floor": best, "max_all_soft": max(st.MaxAll, all), "other_bid": st.otherBids(floor), "max_floor_soft": cur[best-1]}); err != nil {
			return err
		}
		return sendHomeBag(s, h.st)
	})
}

// visits record: the owner sees who came.
func (h *homeSvc) noteVisit(owner *homeState, ownerRID int64, srv string, visitor *store.Role, now time.Time) error {
	owner.Visits = append([]homeVisit{{RID: visitor.RID, SrvID: visitor.SrvID, Time: now.Unix()}}, owner.Visits...)
	if len(owner.Visits) > homeVisitKeep {
		owner.Visits = owner.Visits[:homeVisitKeep]
	}
	if contains(owner.Seen, visitor.RID) {
		return nil
	}
	owner.Seen = append(owner.Seen, visitor.RID)
	b, err := h.board(srv, now)
	if err != nil {
		return err
	}
	w := b.Weekly[ownerRID]
	w[1]++
	b.Weekly[ownerRID] = w
	return h.saveBoard(srv, b)
}

// 26003: go into somebody's home (floor 0 = the owner's main floor).
func (h *homeSvc) visit(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fail := func(msg string) error {
		return homeReply(s, 26003, false, msg, proto.Message{"rid": req.Int("rid"), "srv_id": req.Str("srv_id")})
	}
	target, err := h.st.RoleByID(req.Int("rid"))
	if err != nil || target.SrvID != req.Str("srv_id") || target.Status != 1 {
		return fail("玩家不存在")
	}
	tst, err := h.load(target.RID, s.Now())
	if err != nil {
		return err
	}
	if !tst.Opened {
		return fail("对方还没有开启家园")
	}
	if target.RID != me.RID {
		if err := h.noteVisit(tst, target.RID, target.SrvID, me, s.Now()); err != nil {
			return err
		}
		if err := h.st.SaveState(target.RID, homeModule, tst); err != nil {
			return err
		}
	}
	floor := req.Int("floor")
	if floor == 0 || !tst.floorOpen(floor) {
		floor = tst.Main
	}
	if target.RID != me.RID {
		if err := questEvent(s, h.st, me.RID, evtVisitHome, 1); err != nil {
			return err
		}
	}
	cur, _ := tst.refresh()
	f := tst.Floors[floor-1]
	status := int64(0)
	if mine, err := h.load(me.RID, s.Now()); err == nil && contains(mine.LikedToday, target.RID) {
		status = 1
	}
	return s.Send(26003, proto.Message{"code": 1, "msg": "", "rid": target.RID, "srv_id": target.SrvID, "name": h.homeName(tst, target), "look_id": tst.Used,
		"worship": tst.Likes, "worship_status": status, "soft": cur[floor-1], "wall_bid": f.Wall, "land_bid": f.Land, "list": unitMessages(f.List),
		"visitors": h.visitorList(tst, s.Now()), "tar_name": target.Name, "floor": floor})
}

// ---- figures

func (st *homeState) figureConf(id int64) (int, bool) {
	for i, f := range homeCfg.Figures {
		if f.ID == id {
			return i, true
		}
	}
	return 0, false
}

func (h *homeSvc) figuresMessage(st *homeState) proto.Message {
	list := []proto.Message{}
	for _, id := range st.Figures {
		list = append(list, proto.Message{"id": id})
	}
	return proto.Message{"use_id": st.Used, "max_soft": st.MaxSoft, "list": list}
}

// 26004: the figures of the home; is_finish says whether the home may be used at all (the client's own level check).
func (h *homeSvc) figures(s *gateway.Session, _ proto.Message) error {
	return h.with(s, false, func(role *store.Role, st *homeState) error {
		m := h.figuresMessage(st)
		m["is_finish"] = 0
		if int64(role.Lev) >= homeCfg.num("open_lev") {
			m["is_finish"] = 1
		}
		return s.Send(26004, m)
	})
}

// 26005: put a figure it has on.
func (h *homeSvc) useFigure(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		id := req.Int("id")
		if !contains(st.Figures, id) {
			return homeReply(s, 26005, false, "还没有解锁这个形象", proto.Message{"id": id})
		}
		st.Used = id
		return homeReply(s, 26005, true, "", proto.Message{"id": id})
	})
}

// 26006: unlock a figure for diamonds, once the home has been comfortable enough.
func (h *homeSvc) buyFigure(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		id := req.Int("id")
		fail := func(msg string) error { return homeReply(s, 26006, false, msg, proto.Message{"id": id}) }
		i, ok := st.figureConf(id)
		switch {
		case !st.Opened:
			return fail("家园尚未开启")
		case !ok:
			return fail("形象不存在")
		case contains(st.Figures, id):
			return fail("已经解锁了这个形象")
		}
		f := homeCfg.Figures[i]
		if f.Tips != "" {
			return fail(f.Tips)
		}
		if st.MaxSoft < f.OpenSoft {
			return fail("舒适度不足")
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		splitGains(pairsToGains(f.Loss), 1, spendA, spendI)
		if err := h.st.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); err != nil {
			return fail("钻石不足")
		}
		st.Figures = append(st.Figures, id)
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		return homeReply(s, 26006, true, "", proto.Message{"id": id})
	})
}

func pairsToGains(l [][2]int64) []sellGain {
	out := make([]sellGain, 0, len(l))
	for _, p := range l {
		out = append(out, sellGain{p[0], p[1]})
	}
	return out
}

// 26007: the first figure, one of the two the client offers, which opens the home and gives the first home coin.
func (h *homeSvc) firstFigure(s *gateway.Session, req proto.Message) error {
	return h.with(s, false, func(role *store.Role, st *homeState) error {
		id := req.Int("id")
		fail := func(msg string) error { return homeReply(s, 26007, false, msg, proto.Message{"id": id}) }
		i, ok := st.figureConf(id)
		switch {
		case int64(role.Lev) < homeCfg.num("open_lev"):
			return fail("等级不足")
		case st.Opened:
			return fail("已经选过形象了")
		case !ok || homeCfg.Figures[i].First != 1:
			return fail("形象不存在")
		}
		if err := h.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{homeCoinAsset: homeCfg.num("initial_homecoin_reward")}, map[int64]int64{}); err != nil {
			return err
		}
		st.Opened, st.Used, st.Figures, st.HookAt = true, id, []int64{id}, s.Now().Unix()
		st.refresh()
		b, err := h.board(role.SrvID, s.Now())
		if err != nil {
			return err
		}
		if !contains(b.Open, role.RID) {
			b.Open = append(b.Open, role.RID)
			if err := h.saveBoard(role.SrvID, b); err != nil {
				return err
			}
		}
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		if err := homeReply(s, 26007, true, "", proto.Message{"id": id}); err != nil {
			return err
		}
		m := h.figuresMessage(st)
		m["is_finish"] = 1
		return s.Send(26004, m)
	})
}

// ---- other homes

func (h *homeSvc) faceOf(r *store.Role) int64 {
	if r.FaceID == 0 {
		return store.DefaultFaceID
	}
	return int64(r.FaceID)
}

// 26009: homes of strangers to visit.
func (h *homeSvc) randomHomes(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := h.board(me.SrvID, s.Now())
	if err != nil {
		return err
	}
	pool := []int64{}
	for _, rid := range b.Open {
		if rid != me.RID {
			pool = append(pool, rid)
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	list := []proto.Message{}
	for _, rid := range pool {
		if len(list) >= homeRandomMax {
			break
		}
		r, err := h.st.RoleByID(rid)
		if err != nil || r.Status != 1 {
			continue
		}
		rs, err := h.load(rid, s.Now())
		if err != nil {
			return err
		}
		cur, best := rs.refresh()
		_, _, power, _ := lineupOf(h.st, rid)
		list = append(list, proto.Message{"rid": rid, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "sex": r.Sex, "face": h.faceOf(r), "power": power,
			"last_login": r.LastLoginAt, "soft": cur[best-1], "face_update_time": 0, "face_file": ""})
	}
	return s.Send(26009, proto.Message{"list": list})
}

// 26010: who visited the role's home.
func (h *homeSvc) visitors(s *gateway.Session, _ proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		list := []proto.Message{}
		for _, v := range st.Visits {
			g, err := h.st.RoleByID(v.RID)
			if err != nil {
				continue
			}
			friend, _ := h.st.AreFriends(role.RID, g.RID)
			isFriend := int64(0)
			if friend {
				isFriend = 1
			}
			list = append(list, proto.Message{"rid": g.RID, "srv_id": g.SrvID, "face": h.faceOf(g), "lev": g.Lev, "name": g.Name, "is_friend": isFriend,
				"type": 1, "time": v.Time, "int_args": []proto.Message{}, "str_args": []proto.Message{}, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(26010, proto.Message{"list": list})
	})
}

// 26011: name the home.
func (h *homeSvc) rename(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		name := req.Str("name")
		if n := utf8.RuneCountInString(name); n == 0 || n > homeNameMax {
			return homeReply(s, 26011, false, "家园名称长度不对", proto.Message{"name": st.Name})
		}
		st.Name = name
		return homeReply(s, 26011, true, "", proto.Message{"name": name})
	})
}

// ---- like (the worship of kind 5 in 10316)

// homeLike is the part of 10316 that belongs to the home world: the role likes the home of target. It pays the
// rewards of the visit and counts the like for the popularity of the target. msg is not empty when the like is refused.
func homeLike(st *store.Store, s *gateway.Session, role, target *store.Role) (msg string, err error) {
	h := &homeSvc{st: st, mu: &homeLock}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := s.Now()
	tst, err := h.load(target.RID, now)
	if err != nil {
		return "", err
	}
	if !tst.Opened {
		return "对方还没有开启家园", nil
	}
	mine, err := h.load(role.RID, now)
	if err != nil {
		return "", err
	}
	if int64(len(mine.LikedToday)) >= homeCfg.num("day_praise_limit") {
		return "今天的点赞次数已用完", nil
	}
	reward := homeCfg.pairs("normal_praise_reward")
	if friend, _ := st.AreFriends(role.RID, target.RID); friend {
		reward = homeCfg.pairs("friends_praise_reward")
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	splitGains(pairsToGains(reward), 1, assets, items)
	if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
		return "", err
	}
	mine.LikedToday = append(mine.LikedToday, target.RID)
	tst.Likes++
	b, err := h.board(target.SrvID, now)
	if err != nil {
		return "", err
	}
	w := b.Weekly[target.RID]
	w[0]++
	b.Weekly[target.RID] = w
	for _, save := range []func() error{
		func() error { return st.SaveState(role.RID, homeModule, mine) },
		func() error { return st.SaveState(target.RID, homeModule, tst) },
		func() error { return h.saveBoard(target.SrvID, b) },
	} {
		if err := save(); err != nil {
			return "", err
		}
	}
	return "", sendAssets(s, st, role.RID)
}

// 26019: the homes the role liked today.
func (h *homeSvc) worshipped(s *gateway.Session, _ proto.Message) error {
	return h.with(s, false, func(role *store.Role, st *homeState) error {
		list := []proto.Message{}
		for _, rid := range st.LikedToday {
			if r, err := h.st.RoleByID(rid); err == nil {
				list = append(list, proto.Message{"rid": rid, "srv_id": r.SrvID})
			}
		}
		return s.Send(26019, proto.Message{"list": list})
	})
}

// 26020: the popularity rank of the week: likes ×12 and visits ×3.
func (h *homeSvc) rank(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := h.board(me.SrvID, s.Now())
	if err != nil {
		return err
	}
	rids := make([]int64, 0, len(b.Weekly))
	for rid := range b.Weekly {
		if b.score(rid) > 0 {
			rids = append(rids, rid)
		}
	}
	sort.Slice(rids, func(i, j int) bool {
		if a, c := b.score(rids[i]), b.score(rids[j]); a != c {
			return a > c
		}
		return rids[i] < rids[j]
	})
	var mine int64
	list := []proto.Message{}
	for i, rid := range rids {
		if rid == me.RID {
			mine = int64(i + 1)
		}
		if int64(len(list)) >= homeCfg.num("rank_list_limit") {
			continue
		}
		r, err := h.st.RoleByID(rid)
		if err != nil {
			continue
		}
		list = append(list, proto.Message{"rid": rid, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "face": h.faceOf(r), "rank": i + 1, "avatar_bid": 0,
			"score": b.score(rid), "face_update_time": 0, "face_file": ""})
	}
	if mine > homeCfg.num("rank_list_limit") {
		mine = 0
	}
	return s.Send(26020, proto.Message{"my_rank": mine, "my_score": b.score(me.RID), "rank_list": list})
}

// ---- sets of furniture

// collect keeps the pieces of each set the role ever had, owned and placed, which the set rewards count.
func (h *homeSvc) collect(role *store.Role, st *homeState) error {
	ledger, err := h.st.RoleItems(role.RID)
	if err != nil {
		return err
	}
	if st.Collected == nil {
		st.Collected = map[int64][]int64{}
	}
	have := map[int64]bool{}
	for b, n := range ledger {
		if n > 0 {
			have[b] = true
		}
	}
	for _, f := range st.Floors {
		for _, b := range f.bids() {
			have[b] = true
		}
	}
	for b := range have {
		u, ok := homeCfg.unit[b]
		if !ok || b == homeDefaultWall || b == homeDefaultLand {
			continue
		}
		if !contains(st.Collected[u.Set], b) {
			st.Collected[u.Set] = append(st.Collected[u.Set], b)
		}
	}
	return nil
}

// 26013: the sets, what was collected of each and which rewards were taken.
func (h *homeSvc) suits(s *gateway.Session, _ proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		if err := h.collect(role, st); err != nil {
			return err
		}
		list := []proto.Message{}
		for i, awards := range homeCfg.SuitAward {
			set := int64(i + 1)
			got, coll := []proto.Message{}, []proto.Message{}
			for _, a := range awards {
				if contains(st.Claimed, a.ID) {
					got = append(got, proto.Message{"id": a.ID})
				}
			}
			for _, b := range st.Collected[set] {
				coll = append(coll, proto.Message{"bid": b})
			}
			list = append(list, proto.Message{"set_id": set, "reward": got, "collect": coll})
		}
		return s.Send(26013, proto.Message{"list": list})
	})
}

// 26014: take the reward of a set once enough of its pieces were collected.
func (h *homeSvc) claimSuit(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		id := req.Int("id")
		fail := func(msg string) error { return homeReply(s, 26014, false, msg, proto.Message{"id": id}) }
		if err := h.collect(role, st); err != nil {
			return err
		}
		for i, awards := range homeCfg.SuitAward {
			for _, a := range awards {
				if a.ID != id {
					continue
				}
				switch {
				case contains(st.Claimed, id):
					return fail("已经领取过了")
				case int64(len(st.Collected[int64(i+1)])) < a.Num:
					return fail("收集的家具不够")
				}
				assets, items := map[int64]int64{}, map[int64]int64{}
				splitGains(pairsToGains(a.Reward), 1, assets, items)
				if err := h.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
					return err
				}
				st.Claimed = append(st.Claimed, id)
				if err := sendAssets(s, h.st, role.RID); err != nil {
					return err
				}
				return homeReply(s, 26014, true, "", proto.Message{"id": id})
			}
		}
		return fail("奖励不存在")
	})
}

// ---- coin, flags

// 26016: collect the home coin the best floor made while the role was away.
func (h *homeSvc) collectCoin(s *gateway.Session, _ proto.Message) error {
	return h.with(s, true, func(role *store.Role, st *homeState) error {
		if !st.Opened {
			return homeReply(s, 26016, false, "家园尚未开启", nil)
		}
		cur, best := st.refresh()
		acc := st.accumulated(s.Now())
		coin := acc * coinRate(cur[best-1]) / 3600
		if coin <= 0 {
			return homeReply(s, 26016, false, "暂时没有可领取的家园币", nil)
		}
		if err := h.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{homeCoinAsset: coin}, map[int64]int64{}); err != nil {
			return err
		}
		st.HookAt = s.Now().Unix()
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		if err := homeReply(s, 26016, true, "", nil); err != nil {
			return err
		}
		left := max(0, homeCfg.num("day_praise_limit")-int64(len(st.LikedToday)))
		return s.Send(26015, proto.Message{"rest_worship": left, "acc_hook_time": 0, "max_soft": st.MaxSoft})
	})
}

// 26017: whether the home was never opened.
func (h *homeSvc) first(s *gateway.Session, _ proto.Message) error {
	return h.with(s, false, func(_ *store.Role, st *homeState) error {
		first := int64(0)
		if !st.Opened {
			first = 1
		}
		return s.Send(26017, proto.Message{"is_first": first})
	})
}

// 26018: the red dots: a visit nobody looked at (never, we keep no read mark) and coin waiting for three hours.
func (h *homeSvc) red(s *gateway.Session, _ proto.Message) error {
	return h.with(s, false, func(_ *store.Role, st *homeState) error {
		hook := int64(0)
		if st.accumulated(s.Now()) >= homeCfg.num("coin_redpoint_time_condition")*3600 {
			hook = 1
		}
		return s.Send(26018, proto.Message{"code": 0, "hook_code": hook})
	})
}

// 26021: the floor to enter first.
func (h *homeSvc) mainFloor(s *gateway.Session, req proto.Message) error {
	return h.with(s, true, func(_ *store.Role, st *homeState) error {
		floor := req.Int("main_floor")
		if !st.Opened || !st.floorOpen(floor) {
			return homeReply(s, 26021, false, "该楼层尚未解锁", nil)
		}
		st.Main = floor
		_, best := st.refresh()
		return homeReply(s, 26021, true, "", proto.Message{"main_floor": floor, "max_soft_floor": best})
	})
}

// ---- the home bag

// homeBag lists the furniture of the ledger as the home bag (10502).
func homeBag(ledger map[int64]int64) []proto.Message {
	ids := make([]int64, 0)
	for id, n := range ledger {
		if info, ok := itemTable[id]; ok && n > 0 && info.Type == homeFurniture {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]proto.Message, 0, len(ids))
	for i, id := range ids {
		out = append(out, proto.Message{
			"id": id, "base_id": id, "bind": 0, "quantity": ledger[id], "pos": i + 1,
			"expire_type": itemTable[id].ExpireType, "expire_time": 0,
			"main_attr": []proto.Message{}, "enchant": 0, "attr": []proto.Message{}, "extra_attr": []proto.Message{},
			"score": 0, "all_score": 0, "extra": []proto.Message{},
		})
	}
	return out
}

// sendHomeBag sends the home bag whatever is in it.
func sendHomeBag(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	ledger, err := st.RoleItems(role.RID)
	if err != nil {
		return err
	}
	return s.Send(10502, bagReply(s, homeBag(ledger)))
}
