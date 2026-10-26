package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// homepet.json: the home pet of the client (tools/extract_homepet.py).
//
//go:embed data/homepet.json
var homepetJSON []byte

const (
	petModule     = "homepet"
	petHome       = 1 // state: at home
	petOnWay      = 2 // state: travelling
	petNameMax    = 6
	petFood       = 32 // item types: food and travelling items in the sprite bag (10503), treasures, photos and letters
	petTool       = 31
	petTreasure   = 33
	petPhotoType  = 34
	petLetterType = 35
	petEgg        = 37
	petEggTool    = 38
	petGoCost     = 10  // vigor a trip costs (not in the client, ours)
	petStep       = 600 // seconds between two events of a trip (ours)
	petFirstCity  = 100
	petCities     = 9
	petCatchUp    = 300 // judgements played at most when the role comes back after a long time
	petEvGo       = 1   // evt_sid of the departure and of the return
	petEvBack     = 2
	petEvPhoto    = 6 // type of the events that give a photo or a letter, claimed one by one
	petEvLetter   = 7
	petSlotFoodA  = 1 // slots of the travelling bag: 1 and 3 hold food, 2 and 4 hold items
	petSlotFoodB  = 3
	petSlotToolA  = 2
	petSlotToolB  = 4
	petBagTypeAll = 4
)

type petEventConf struct {
	SID, ID, Type, Notice int64
	Award                 [][2]int64
	Drop                  []int64
}

type petTalkConf struct {
	ID, Type, Pro, Vigor int64
	Expend, Award2       [][2]int64
}

type petConf struct {
	Const  map[string]json.RawMessage `json:"const"`
	Events []petEventConf             `json:"events"`
	Talks  []petTalkConf              `json:"talks"`
	event  map[int64]petEventConf
	byCity map[int64][]int64 // city index -> sids that can happen on the way there
}

var petCfg = func() *petConf {
	c := &petConf{}
	if err := json.Unmarshal(homepetJSON, c); err != nil || len(c.Events) != 206 || len(c.Talks) != 40 {
		panic("game: bad homepet.json")
	}
	c.event = map[int64]petEventConf{}
	c.byCity = map[int64][]int64{}
	for _, e := range c.Events {
		c.event[e.SID] = e
		if e.SID > petEvBack {
			city := e.ID / 10 % 10
			c.byCity[city] = append(c.byCity[city], e.SID)
		}
	}
	return c
}()

func (c *petConf) num(key string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[key], &n)
	return n
}

// petEvent is one event of a trip: it happens at Time and, when it is announced to the player (the departure, the return, a
// photo or a letter), waits for the player to take it (26106).
type petEvent struct {
	ID, Time, SID int64
	Award         [][2]int64
	Taken         bool
}

type petTrip struct {
	Start, End, City int64
	Events           []petEvent
	Sent             int // events already pushed to the client with 26108
}

type petCard struct{ ID, Base int64 }

// petState is what a role keeps of its pet.
type petState struct {
	Name     string
	Renames  int64
	Vigor    int64
	VigorAt  int64 // when the vigor was last brought up to date
	Judge    int64 // the last time the pet judged whether to go
	Set      map[int64]int64
	Trip     *petTrip
	Trips    int64 // trips begun, the first one has fixed rewards
	NextEvt  int64
	Day      int64
	DayTalk  int64
	DayFeed  int64
	Feed     int64 // the pocket money the pet asked for and the player did not answer yet
	Photos   []petCard
	Letters  []petCard
	NextItem int64
}

type petSvc struct {
	st *store.Store
}

// RegisterHomepet registers the home pet (26100-26113): a dog that regains vigor at home, is talked to and given pocket money,
// goes on a trip by itself when it has food in its travelling bag, tells what it saw on the way and brings back rewards,
// photos and letters. The rules (vigor cost of a trip, time of the events, what the drops give) are ours, not captured.
func RegisterHomepet(r *gateway.Router, db *store.Store) {
	h := &petSvc{st: db}
	r.Handle(26100, h.info)
	r.Handle(26101, h.vigor)
	r.Handle(26102, h.rename)
	r.Handle(26103, h.talk)
	r.Handle(26104, h.pocketMoney)
	r.Handle(26105, h.events)
	r.Handle(26106, h.take)
	r.Handle(26107, h.setBag)
	r.Handle(26108, h.news)
	r.Handle(26109, h.tripEvents)
	r.Handle(26110, h.treasures)
	r.Handle(26111, h.album)
	r.Handle(26112, h.albums)
	r.Handle(26113, h.drop)
}

// with runs fn on the pet of the session's role, brought up to date, and saves it.
func (h *petSvc) with(s *gateway.Session, fn func(role *store.Role, p *petState, now int64) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	homeLock.Lock()
	defer homeLock.Unlock()
	p := &petState{}
	if _, err := h.st.LoadState(role.RID, petModule, p); err != nil {
		return err
	}
	now := s.Now().Unix()
	if p.Set == nil {
		p.Set = map[int64]int64{}
	}
	if p.VigorAt == 0 { // a pet is born with the first look
		p.Vigor, p.VigorAt, p.Judge = petCfg.num("power_initial_value"), now, now
	}
	if day := dayKey(s.Now()); p.Day != day {
		p.Day, p.DayTalk, p.DayFeed = day, 0, 0
	}
	if err := h.advance(role.RID, p, now); err != nil {
		return err
	}
	if err := fn(role, p, now); err != nil {
		return err
	}
	return h.st.SaveState(role.RID, petModule, p)
}

func (p *petState) regen(until int64) {
	max, every := petCfg.num("power_upper_limit"), petCfg.num("power_recovery_speed")
	if p.Vigor >= max {
		p.Vigor, p.VigorAt = max, until
		return
	}
	if until <= p.VigorAt || every <= 0 {
		return
	}
	n := (until - p.VigorAt) / every
	if p.Vigor+n >= max {
		p.Vigor, p.VigorAt = max, until
		return
	}
	p.Vigor += n
	p.VigorAt += n * every
}

// vigorTime is when the next point of vigor comes, 0 when it is full.
func (p *petState) vigorTime() int64 {
	if p.Vigor >= petCfg.num("power_upper_limit") {
		return 0
	}
	return p.VigorAt + petCfg.num("power_recovery_speed")
}

// food is the slot with food the pet takes along, 0 when it has none.
func (p *petState) food(ledger map[int64]int64) int64 {
	for _, k := range []int64{petSlotFoodA, petSlotFoodB} {
		if id := p.Set[k]; id != 0 && ledger[id] > 0 && itemTable[id].Type == petFood {
			return k
		}
	}
	return 0
}

// advance plays what happened since the pet was looked at: it regains vigor at home, judges every
// travel_judgement_interval seconds whether to go (the more vigor, the likelier, and only with food in the bag), comes back
// when the trip is over.
func (h *petSvc) advance(rid int64, p *petState, now int64) error {
	every := petCfg.num("travel_judgement_interval")
	if every <= 0 {
		every = 900
	}
	for i := 0; i < petCatchUp; i++ {
		if p.Trip != nil && p.Trip.End > p.Judge { // away: nothing to judge until it is back
			if p.Trip.End > now {
				return nil
			}
			p.Judge, p.VigorAt = p.Trip.End, p.Trip.End
		}
		next := p.Judge + every
		if next > now {
			p.regen(now)
			return nil
		}
		p.regen(next)
		p.Judge = next
		ledger, err := h.st.RoleItems(rid)
		if err != nil {
			return err
		}
		k := p.food(ledger)
		if k == 0 || p.Vigor < petGoCost {
			continue
		}
		if p.Trips > 0 && rand.Int63n(petCfg.num("power_upper_limit")) >= p.Vigor {
			continue
		}
		food := p.Set[k]
		if err := h.st.Exchange(rid, map[int64]int64{}, map[int64]int64{food: 1}, map[int64]int64{}, map[int64]int64{}); err != nil {
			continue
		}
		p.Vigor -= petGoCost
		p.VigorAt = next
		p.startTrip(next)
	}
	p.Judge = now
	p.regen(now)
	return nil
}

func (p *petState) event(t, sid int64, award [][2]int64) petEvent {
	p.NextEvt++
	return petEvent{ID: p.NextEvt, Time: t, SID: sid, Award: award}
}

// startTrip makes the whole trip at once: the departure, the events on the way, the return with what the way gave.
func (p *petState) startTrip(start int64) {
	trip := &petTrip{Start: start, City: petFirstCity}
	trip.Events = append(trip.Events, p.event(start, petEvGo, nil))
	if p.Trips == 0 { // the first trip is the same for everyone
		var steps [][3]json.RawMessage
		_ = json.Unmarshal(petCfg.Const["initial_sid"], &steps)
		for i, st := range steps {
			var sid int64
			var award [][2]int64
			_ = json.Unmarshal(st[1], &sid)
			_ = json.Unmarshal(st[2], &award)
			t := start + int64(i+1)*petStep
			if sid == petEvBack {
				trip.End = t
			}
			trip.Events = append(trip.Events, p.event(t, sid, award))
		}
	} else {
		city := rand.Int63n(petCities)
		trip.City = petFirstCity + city
		pool := append([]int64(nil), petCfg.byCity[city]...)
		sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
		rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		n := 3 + rand.Intn(3)
		if n > len(pool) {
			n = len(pool)
		}
		var back [][2]int64
		sum := map[int64]int64{}
		for i, sid := range pool[:n] {
			e := petCfg.event[sid]
			award := e.Award
			if len(award) == 0 && len(e.Drop) > 0 {
				award = petDrop(e.Type)
			}
			trip.Events = append(trip.Events, p.event(start+int64(i+1)*petStep, sid, award))
			if e.Notice == 0 { // what the way gives without asking comes with the return
				for _, a := range award {
					sum[a[0]] += a[1]
				}
			}
		}
		ids := make([]int64, 0, len(sum))
		for id := range sum {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			back = append(back, [2]int64{id, sum[id]})
		}
		trip.End = start + int64(n+1)*petStep
		trip.Events = append(trip.Events, p.event(trip.End, petEvBack, back))
	}
	p.Trips++
	p.Trip = trip
}

// petDrop is what a drop of the client's event table gives; the real drop tables are on the original server.
func petDrop(typ int64) [][2]int64 {
	var pool []int64
	switch typ {
	case 4:
		for _, u := range homeCfg.Units {
			if u.Type != homeUnitFloor && u.Type != homeUnitWall {
				pool = append(pool, u.Bid)
			}
		}
	case 5:
		pool = itemsOfType(petTreasure)
	case 11:
		pool = itemsOfType(petEgg)
	case 12:
		pool = itemsOfType(petEggTool)
	}
	if len(pool) == 0 {
		return nil
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i] < pool[j] })
	return [][2]int64{{pool[rand.Intn(len(pool))], 1}}
}

func itemsOfType(t int) []int64 {
	var out []int64
	for id, info := range itemTable {
		if info.Type == t {
			out = append(out, id)
		}
	}
	return out
}

func (p *petState) state() int64 {
	if p.Trip != nil && p.Trip.End > p.Judge {
		return petOnWay
	}
	return petHome
}

func (p *petState) setItems() []proto.Message {
	keys := make([]int64, 0, len(p.Set))
	for k, id := range p.Set {
		if id != 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]proto.Message, 0, len(keys))
	for _, k := range keys {
		out = append(out, proto.Message{"key": k, "id": p.Set[k]})
	}
	return out
}

func petAwardMessages(l [][2]int64) []proto.Message {
	out := make([]proto.Message, 0, len(l))
	for _, a := range l {
		out = append(out, proto.Message{"id": a[0], "num": a[1]})
	}
	return out
}

func (p *petState) eventMessages(l []petEvent) []proto.Message {
	out := make([]proto.Message, 0, len(l))
	for _, e := range l {
		out = append(out, proto.Message{"evt_id": e.ID, "time": e.Time, "evt_sid": e.SID, "award": petAwardMessages(e.Award)})
	}
	return out
}

// arrived lists the events that happened and that the player has to take, the ones the client announces.
func (p *petState) arrived(now int64) []petEvent {
	var out []petEvent
	if p.Trip == nil {
		return out
	}
	for _, e := range p.Trip.Events {
		if e.Time <= now && !e.Taken && petCfg.event[e.SID].Notice == 1 {
			out = append(out, e)
		}
	}
	return out
}

// 26100
func (h *petSvc) info(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		return s.Send(26100, proto.Message{
			"pet_id": 1, "name": p.Name, "state": p.state(), "vigor": p.Vigor, "vigor_time": p.vigorTime(),
			"set_item": p.setItems(), "rename_count": p.Renames, "day_talk": p.DayTalk, "day_feed": p.DayFeed,
		})
	})
}

// 26101
func (h *petSvc) vigor(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		return s.Send(26101, proto.Message{"vigor": p.Vigor, "vigor_time": p.vigorTime()})
	})
}

// 26102: a name of six letters at most, a hundred diamonds each time.
func (h *petSvc) rename(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(role *store.Role, p *petState, _ int64) error {
		name := req.Str("name")
		reply := func(ok int64, msg string) error {
			return s.Send(26102, proto.Message{"code": ok, "msg": msg, "name": p.Name, "rename_count": p.Renames})
		}
		if n := utf8.RuneCountInString(name); n == 0 || n > petNameMax {
			return reply(0, "名字长度不对")
		}
		cost := map[int64]int64{homeDiamond: petCfg.num("rename_cost")}
		if err := h.st.Exchange(role.RID, cost, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			return reply(0, "钻石不足")
		}
		p.Name, p.Renames = name, p.Renames+1
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		return reply(1, "改名成功")
	})
}

// talkPick picks a line of the type by weight.
func talkPick(typ int64) petTalkConf {
	var total int64
	for _, t := range petCfg.Talks {
		if t.Type == typ {
			total += t.Pro
		}
	}
	n := rand.Int63n(total)
	for _, t := range petCfg.Talks {
		if t.Type != typ {
			continue
		}
		if n < t.Pro {
			return t
		}
		n -= t.Pro
	}
	return petCfg.Talks[0]
}

// 26103: type 1 is a word of the pet (flag 0), type 2 is the pet asking for pocket money (flag 1), which 26104 gives.
func (h *petSvc) talk(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		typ := req.Int("type")
		reply := func(code int64, msg string, id, flag int64) error {
			return s.Send(26103, proto.Message{"code": code, "msg": msg, "id": id, "flag": flag,
				"day_talk": p.DayTalk, "day_feed": p.DayFeed, "vigor": p.Vigor})
		}
		const rest = "萌宠也需要休息滴,明天再试吧~"
		switch typ {
		case 1:
			if p.DayTalk >= petCfg.num("conversation_upper_limit") {
				return reply(0, rest, 0, 0)
			}
			p.DayTalk++
			return reply(1, "", talkPick(1).ID, 0)
		case 2:
			if p.DayFeed >= petCfg.num("pocket_money_upper_limit") {
				return reply(0, rest, 0, 0)
			}
			t := talkPick(2)
			p.Feed = t.ID
			return reply(1, "", t.ID, 1)
		}
		return reply(0, "参数错误", 0, 0)
	})
}

// 26104: the pocket money the pet asked for is paid, the pet gets some vigor and may give something back.
func (h *petSvc) pocketMoney(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, p *petState, _ int64) error {
		reply := func(code int64, msg string) error {
			return s.Send(26104, proto.Message{"code": code, "msg": msg, "vigor": p.Vigor, "day_talk": p.DayTalk, "day_feed": p.DayFeed})
		}
		if p.Feed == 0 || p.DayFeed >= petCfg.num("pocket_money_upper_limit") {
			p.Feed = 0
			return reply(0, "萌宠也需要休息滴,明天再试吧~")
		}
		var t petTalkConf
		for _, c := range petCfg.Talks {
			if c.ID == p.Feed {
				t = c
			}
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		splitGains(pairsToGains(t.Expend), 1, spendA, spendI)
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		splitGains(pairsToGains(t.Award2), 1, gainA, gainI)
		if err := h.st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			return reply(0, "零花钱不足")
		}
		p.Feed = 0
		p.DayFeed++
		if max := petCfg.num("power_upper_limit"); p.Vigor+t.Vigor > max {
			p.Vigor = max
		} else {
			p.Vigor += t.Vigor
		}
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		if err := sendPetBag(s, h.st); err != nil {
			return err
		}
		if err := reply(1, ""); err != nil {
			return err
		}
		return s.Send(26101, proto.Message{"vigor": p.Vigor, "vigor_time": p.vigorTime()})
	})
}

// 26105: the events waiting for the player.
func (h *petSvc) events(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, now int64) error {
		l := p.arrived(now)
		if p.Trip != nil {
			p.Trip.Sent = len(p.Trip.Events)
		}
		return s.Send(26105, proto.Message{"evt_list": p.eventMessages(l)})
	})
}

// 26108: the events that happened since the client last heard of them.
func (h *petSvc) news(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, now int64) error {
		var l []petEvent
		if p.Trip != nil {
			for i, e := range p.Trip.Events {
				if i >= p.Trip.Sent && e.Time <= now && petCfg.event[e.SID].Notice == 1 && !e.Taken {
					l = append(l, e)
				}
			}
			p.Trip.Sent = len(p.Trip.Events)
		}
		return s.Send(26108, proto.Message{"evt_list": p.eventMessages(l)})
	})
}

// 26106: the player takes an event: the return pays what the way gave, a photo or a letter goes into the album.
func (h *petSvc) take(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(role *store.Role, p *petState, now int64) error {
		id := req.Int("evt_id")
		fail := func(msg string) error {
			return s.Send(26106, proto.Message{"code": 0, "msg": msg, "flag": 0, "award": []proto.Message{}, "evt_id": id})
		}
		if p.Trip == nil {
			return fail("事件不存在")
		}
		at := -1
		for i, e := range p.Trip.Events {
			if e.ID == id {
				at = i
			}
		}
		if at < 0 || p.Trip.Events[at].Taken || p.Trip.Events[at].Time > now {
			return fail("事件不存在")
		}
		e := &p.Trip.Events[at]
		conf := petCfg.event[e.SID]
		var album []petCard
		var pay [][2]int64
		switch {
		case e.SID == petEvBack:
			pay = e.Award
		case conf.Type == petEvPhoto || conf.Type == petEvLetter:
			list := &p.Photos
			if conf.Type == petEvLetter {
				list = &p.Letters
			}
			for _, a := range e.Award {
				if !petOwns(*list, a[0]) {
					p.NextItem++
					*list = append(*list, petCard{ID: p.NextItem, Base: a[0]})
					album = append(album, (*list)[len(*list)-1])
				}
			}
		}
		if len(pay) > 0 {
			gainA, gainI := map[int64]int64{}, map[int64]int64{}
			splitGains(pairsToGains(pay), 1, gainA, gainI)
			if err := h.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, gainA, gainI); err != nil {
				return err
			}
		}
		e.Taken = true
		if e.SID == petEvBack { // the trip is over, nothing of it stays
			p.Trip.Events = nil
			p.Trip.Sent = 0
		}
		if err := sendAssets(s, h.st, role.RID); err != nil {
			return err
		}
		if err := sendPetBag(s, h.st); err != nil {
			return err
		}
		if len(album) > 0 {
			typ := int64(petPhotoType)
			if conf.Type == petEvLetter {
				typ = petLetterType
			}
			if err := s.Send(26112, albumMessage(typ, album)); err != nil {
				return err
			}
		}
		return s.Send(26106, proto.Message{"code": 1, "msg": "", "flag": 1, "award": petAwardMessages(append(append([][2]int64(nil), pay...), albumAward(album)...)), "evt_id": id})
	})
}

func albumAward(l []petCard) [][2]int64 {
	out := make([][2]int64, 0, len(l))
	for _, a := range l {
		out = append(out, [2]int64{a.Base, 1})
	}
	return out
}

func petOwns(l []petCard, base int64) bool {
	for _, a := range l {
		if a.Base == base {
			return true
		}
	}
	return false
}

func albumMessage(typ int64, l []petCard) proto.Message {
	list := make([]proto.Message, 0, len(l))
	for _, a := range l {
		list = append(list, proto.Message{"id": a.ID, "base_id": a.Base})
	}
	return proto.Message{"type": typ, "list": list}
}

// 26107: the travelling bag, two foods and two items. The food and the items must be in the bag; slots 1 and 2 cannot be
// changed while the pet is away.
func (h *petSvc) setBag(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(role *store.Role, p *petState, _ int64) error {
		fail := func(msg string) error {
			return s.Send(26107, proto.Message{"code": 0, "msg": msg, "set_item": p.setItems()})
		}
		ledger, err := h.st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		next := map[int64]int64{}
		for _, m := range req.Arr("set_item") {
			key, id := m.Int("key"), m.Int("id")
			if key < 1 || key > petBagTypeAll {
				return fail("参数错误")
			}
			if id == 0 {
				continue
			}
			want := petFood
			if key == petSlotToolA || key == petSlotToolB {
				want = petTool
			}
			if itemTable[id].Type != want || ledger[id] <= 0 {
				return fail("物品不存在")
			}
			next[key] = id
		}
		if p.state() != petHome {
			for _, k := range []int64{petSlotFoodA, petSlotToolA} {
				if next[k] != p.Set[k] {
					return fail("萌宠已出行")
				}
			}
		}
		p.Set = next
		return s.Send(26107, proto.Message{"code": 1, "msg": "", "set_item": p.setItems()})
	})
}

// 26109: all events of the trip in course, also the ones that did not happen yet, which the client shows as time passes.
func (h *petSvc) tripEvents(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		if p.Trip == nil || len(p.Trip.Events) == 0 {
			return s.Send(26109, proto.Message{"start_time": 0, "evt_list": []proto.Message{}, "city_id": 0})
		}
		return s.Send(26109, proto.Message{"start_time": p.Trip.Start, "evt_list": p.eventMessages(p.Trip.Events), "city_id": p.Trip.City})
	})
}

// 26110: the treasures the role ever had in its bag.
func (h *petSvc) treasures(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, _ *petState, _ int64) error {
		ledger, err := h.st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		ids := []int64{}
		for id, n := range ledger {
			if n > 0 && itemTable[id].Type == petTreasure {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := make([]proto.Message, 0, len(ids))
		for _, id := range ids {
			list = append(list, proto.Message{"id": id})
		}
		return s.Send(26110, proto.Message{"list": list})
	})
}

// 26111: the photos (type 34) or the letters (type 35) of the album.
func (h *petSvc) album(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		typ := req.Int("type")
		if typ == petLetterType {
			return s.Send(26111, albumMessage(typ, p.Letters))
		}
		return s.Send(26111, albumMessage(petPhotoType, p.Photos))
	})
}

// 26112: both albums at once.
func (h *petSvc) albums(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		if err := s.Send(26112, albumMessage(petPhotoType, p.Photos)); err != nil {
			return err
		}
		return s.Send(26112, albumMessage(petLetterType, p.Letters))
	})
}

// 26113: throw away a photo or a letter.
func (h *petSvc) drop(s *gateway.Session, req proto.Message) error {
	return h.with(s, func(_ *store.Role, p *petState, _ int64) error {
		typ, id := req.Int("type"), req.Int("id")
		list := &p.Photos
		if typ == petLetterType {
			list = &p.Letters
		}
		for i, a := range *list {
			if a.ID == id {
				*list = append((*list)[:i:i], (*list)[i+1:]...)
				return s.Send(26113, proto.Message{"code": 1, "msg": "", "type": typ, "id": id})
			}
		}
		return s.Send(26113, proto.Message{"code": 0, "msg": "物品不存在", "type": typ, "id": id})
	})
}

// sendPetBag sends the sprite bag (10503) whatever is in it: the pet's food, items and treasures live there with the sprites.
func sendPetBag(s *gateway.Session, st *store.Store) error { return sendSpriteBag(s, st) }
