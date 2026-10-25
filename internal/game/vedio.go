package game

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The replays (vedio, 19901-19908; 20034 shares one from a result window, 20036 plays one). A fight that is worth a
// replay is kept when it starts: the fight script is the same messages the client was sent (20027, then the 20002
// and 20004 of every round), so a replay is played by the fight service like the fight itself. The lists show a
// replay's two sides, kept next to the script. Our rules, not captured: only the arena keeps replays, a replay is
// public (in the lists everybody sees) once somebody shared it, every role may like 10 replays a day.

const (
	vedioModule   = "vedio"
	vedioMine     = 1  // VedioConst.MyVedio_Type.Myself
	vedioCollect  = 99 // VedioConst.MyVedio_Type.Collect
	vedioTabHot   = 98 // VedioConst.Tab_Index.Hot: the most liked
	vedioTabNew   = 97 // VedioConst.Tab_Index.Newhero: the newest
	vedioLikesDay = 10 // likes a role may give a day
	vedioPage     = 30 // VedioConst.ReqVedioDataNum
)

// vedioTabCombat is the fight type each of the client's tabs (VedioConst.Tab_Index) lists; the tabs of the
// fights this server has no replays of list nothing.
var vedioTabCombat = map[int64]int64{1: combatArena}

// replaySide is one side of a kept fight as the lists show it.
type replaySide struct {
	RID    int64        `json:"rid"`
	SrvID  string       `json:"srv"`
	Name   string       `json:"name"`
	Lev    int64        `json:"lev"`
	Face   int64        `json:"face"`
	Power  int64        `json:"power"`
	Form   int64        `json:"form"`
	Camp   int64        `json:"camp"`
	Heroes []replayHero `json:"heroes"`
	Rank   int64        `json:"rank"`
}

type replayHero struct {
	ID, Pos, BID, Lev, Star, HP, HPMax int64
}

// replayMeta is what the lists need of a replay: A attacked B, and Won says A won.
type replayMeta struct {
	Won bool       `json:"won"`
	A   replaySide `json:"a"`
	B   replaySide `json:"b"`
}

// replayData is the fight script: 20027 and per round the 20002 and 20004 messages, encoded for the wire.
type replayData struct {
	Combat int64         `json:"combat"`
	Won    bool          `json:"won"`
	Intro  string        `json:"intro"`
	Rounds []replayRound `json:"rounds"`
}

type replayRound struct {
	Start string `json:"s"`
	Plays string `json:"p"`
}

func encodeWire(cmd uint16, msg proto.Message) (string, error) {
	sc, err := proto.LoadSchema()
	if err != nil {
		return "", err
	}
	b, err := proto.Encode(proto.DefaultOptions(), sc.Recv[cmd], msg)
	return base64.StdEncoding.EncodeToString(b), err
}

func decodeWire(cmd uint16, s string) (proto.Message, error) {
	sc, err := proto.LoadSchema()
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return proto.Decode(proto.DefaultOptions(), sc.Recv[cmd], b)
}

// recordReplay keeps a fight (a attacked b) and returns the replay id; 0 when it could not be kept (the fight
// itself goes on).
func recordReplay(st *store.Store, combat int64, a, b replaySide, intro proto.Message, rounds []fightRound, won bool, now int64) int64 {
	d := replayData{Combat: combat, Won: won}
	var err error
	if d.Intro, err = encodeWire(20027, intro); err != nil {
		return 0
	}
	for i, r := range rounds {
		var rr replayRound
		if rr.Start, err = encodeWire(20002, roundStart(r, i, combat)); err != nil {
			return 0
		}
		if rr.Plays, err = encodeWire(20004, roundPlays(r, i, combat)); err != nil {
			return 0
		}
		d.Rounds = append(d.Rounds, rr)
	}
	for _, o := range intro.Arr("objects") {
		h := replayHero{ID: o.Int("object_id"), Pos: o.Int("pos"), BID: o.Int("object_bid"), Lev: o.Int("lev"), Star: o.Int("star"), HP: o.Int("hp"), HPMax: o.Int("hp_max")}
		if o.Int("group") == 1 {
			a.Heroes = append(a.Heroes, h)
			if a.Camp == 0 {
				a.Camp = o.Int("camp_type")
			}
		} else {
			b.Heroes = append(b.Heroes, h)
			if b.Camp == 0 {
				b.Camp = o.Int("camp_type")
			}
		}
	}
	meta, _ := json.Marshal(replayMeta{Won: won, A: a, B: b})
	data, _ := json.Marshal(d)
	id, err := st.AddReplay(store.Replay{SrvID: a.SrvID, CombatType: combat, ARID: a.RID, BRID: b.RID, Time: now, Meta: string(meta), Data: string(data)})
	if err != nil {
		return 0
	}
	return id
}

// fightOfReplay turns a kept script back into what the fight service plays.
func fightOfReplay(raw string) (intro proto.Message, rounds []fightRound, d replayData, err error) {
	if err = json.Unmarshal([]byte(raw), &d); err != nil {
		return
	}
	if intro, err = decodeWire(20027, d.Intro); err != nil {
		return
	}
	for _, rr := range d.Rounds {
		var r fightRound
		start, e := decodeWire(20002, rr.Start)
		if e != nil {
			return nil, nil, d, e
		}
		plays, e := decodeWire(20004, rr.Plays)
		if e != nil {
			return nil, nil, d, e
		}
		for _, p := range start.Arr("all_alive") {
			r.alive = append(r.alive, p.Int("pos"))
		}
		r.start, r.plays = start.Arr("skill_plays"), plays.Arr("skill_plays")
		rounds = append(rounds, r)
	}
	if len(rounds) == 0 {
		err = errors.New("replay has no rounds")
	}
	return
}

// vedioState is what a role keeps of the likes it gave on a day.
type vedioState struct {
	Day   int64   `json:"day"`
	Liked []int64 `json:"liked"`
}

type vedioSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterVedio registers the replays (19901-19908), sharing one from a result window (20034) and playing one
// (20036).
func RegisterVedio(r *gateway.Router, db *store.Store) {
	v := &vedioSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(19901, v.mine)
	r.Handle(19902, v.public)
	r.Handle(19903, v.like)
	r.Handle(19904, v.collect)
	r.Handle(19905, v.share)
	r.Handle(19906, v.todayLikes)
	r.Handle(19907, v.hero)
	r.Handle(19908, v.look)
	r.Handle(20034, v.shareResult)
	r.Handle(20036, v.play)
}

func (v *vedioSvc) state(s *gateway.Session, rid int64) (*vedioState, error) {
	vs := &vedioState{}
	if _, err := v.st.LoadState(rid, vedioModule, vs); err != nil {
		return nil, err
	}
	if day := dayKey(s.Now()); vs.Day != day {
		vs.Day, vs.Liked = day, nil
	}
	return vs, nil
}

func (sd replaySide) listed(prefix string, m proto.Message) {
	plist := []proto.Message{}
	for _, h := range sd.Heroes {
		plist = append(plist, proto.Message{"id": h.ID, "pos": h.Pos, "bid": h.BID, "lev": h.Lev, "star": h.Star, "break_lev": 0, "power": 0,
			"hp": h.HP, "hp_max": h.HPMax, "dps": 0, "be_hurt": 0, "cure": 0, "ext": []proto.Message{}})
	}
	m[prefix+"rid"], m[prefix+"srv_id"], m[prefix+"name"], m[prefix+"lev"] = sd.RID, sd.SrvID, sd.Name, sd.Lev
	m[prefix+"face"], m[prefix+"power"], m[prefix+"rank"] = sd.Face, sd.Power, sd.Rank
	m[prefix+"formation_type"], m[prefix+"camp_type"] = sd.Form, sd.Camp
	m[prefix+"face_update_time"], m[prefix+"face_file"] = 0, ""
	m[prefix+"plist"] = plist
}

// vedioEntry is one replay of a list; liked says whether the viewer liked it today.
func vedioEntry(r store.Replay, liked bool) proto.Message {
	var meta replayMeta
	_ = json.Unmarshal([]byte(r.Meta), &meta)
	ret := int64(2)
	if meta.Won {
		ret = 1
	}
	m := proto.Message{"name": "", "flag": b2i(liked), "is_collect": b2i(r.IsCollected), "id": r.ID, "combat_type": r.CombatType, "round": 0, "sec_type": 0,
		"ret": ret, "like": r.Likes, "share": r.Shares, "play": r.Plays, "time": r.Time, "ext": []proto.Message{}}
	meta.A.listed("a_", m)
	meta.B.listed("b_", m)
	return m
}

func (v *vedioSvc) entries(list []store.Replay, vs *vedioState) []proto.Message {
	liked := map[int64]bool{}
	for _, id := range vs.Liked {
		liked[id] = true
	}
	out := make([]proto.Message, 0, len(list))
	for _, r := range list {
		out = append(out, vedioEntry(r, liked[r.ID]))
	}
	return out
}

// mine answers 19901: the replays of the role's own fights, or the ones it collected.
func (v *vedioSvc) mine(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	vs, err := v.state(s, role.RID)
	if err != nil {
		return err
	}
	var list []store.Replay
	switch typ := req.Int("type"); typ {
	case vedioMine:
		list, err = v.st.ReplaysOf(role.RID, vedioPage)
	case vedioCollect:
		list, err = v.st.CollectedReplays(role.RID)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return s.Send(19901, proto.Message{"type": req.Int("type"), "replay_list": v.entries(list, vs)})
}

// public answers 19902: a page of the replays everybody sees, for a tab of the main window. cond_type is the
// client's filter (its meaning is not known to us, it is echoed).
func (v *vedioSvc) public(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	vs, err := v.state(s, role.RID)
	if err != nil {
		return err
	}
	typ := req.Int("type")
	start, num := int(req.Int("start"))-1, int(req.Int("num")) // start counts from 1
	if start < 0 {
		start = 0
	}
	if num <= 0 || num > vedioPage {
		num = vedioPage
	}
	var list []store.Replay
	total := 0
	switch typ {
	case vedioTabHot, vedioTabNew:
		list, total, err = v.st.PublicReplays(role.RID, 0, typ == vedioTabHot, start, num)
	default:
		if combat, ok := vedioTabCombat[typ]; ok {
			list, total, err = v.st.PublicReplays(role.RID, combat, false, start, num)
		}
	}
	if err != nil {
		return err
	}
	return s.Send(19902, proto.Message{"type": typ, "cond_type": req.Int("cond_type"), "start": req.Int("start"), "num": num, "len": total,
		"replay_list": v.entries(list, vs)})
}

// like answers 19903: one like per replay and day, vedioLikesDay a day.
func (v *vedioSvc) like(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	id := req.Int("id")
	reply := func(code int64, msg string) error {
		return s.Send(19903, proto.Message{"code": code, "msg": msg, "id": id})
	}
	vs, err := v.state(s, role.RID)
	if err != nil {
		return err
	}
	r, err := v.st.ReplayByID(id, role.RID)
	if err != nil {
		return err
	}
	switch {
	case r == nil:
		return reply(0, "录像不存在")
	case int64(len(vs.Liked)) >= vedioLikesDay:
		return reply(0, "今日点赞次数已用完")
	}
	for _, l := range vs.Liked {
		if l == id {
			return reply(0, "今天已经赞过了")
		}
	}
	vs.Liked = append(vs.Liked, id)
	if err := v.st.SaveState(role.RID, vedioModule, vs); err != nil {
		return err
	}
	if err := v.st.BumpReplay(id, 1, 0, 0); err != nil {
		return err
	}
	return reply(1, "点赞成功")
}

// collect answers 19904: type 1 collects a replay, 0 lets it go.
func (v *vedioSvc) collect(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	id, typ := req.Int("id"), req.Int("type")
	ok, err := v.st.SetReplayCollect(role.RID, id, typ == 1)
	if err != nil {
		return err
	}
	if !ok {
		return s.Send(19904, proto.Message{"code": 0, "msg": "录像不存在", "id": id, "type": typ})
	}
	msg := "收藏成功"
	if typ != 1 {
		msg = "已取消收藏"
	}
	return s.Send(19904, proto.Message{"code": 1, "msg": msg, "id": id, "type": typ})
}

// share answers 19905: the replay becomes public. The chat channels get no announcement (our simplification).
func (v *vedioSvc) share(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	r, err := v.st.ReplayByID(req.Int("id"), role.RID)
	if err != nil {
		return err
	}
	if r == nil {
		return s.Send(19905, proto.Message{"code": 0, "msg": "录像不存在"})
	}
	if err := v.st.BumpReplay(r.ID, 0, 1, 0); err != nil {
		return err
	}
	return s.Send(19905, proto.Message{"code": 1, "msg": "分享成功"})
}

// shareResult answers 20034, the share buttons of a result window.
func (v *vedioSvc) shareResult(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	r, err := v.st.ReplayByID(req.Int("replay_id"), role.RID)
	if err != nil {
		return err
	}
	if r == nil {
		return s.Send(20034, proto.Message{"result": 0, "msg": "录像不存在"})
	}
	if err := v.st.BumpReplay(r.ID, 0, 1, 0); err != nil {
		return err
	}
	return s.Send(20034, proto.Message{"result": 1, "msg": "分享成功"})
}

// todayLikes answers 19906: how many likes the role gave today.
func (v *vedioSvc) todayLikes(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	vs, err := v.state(s, role.RID)
	if err != nil {
		return err
	}
	return s.Send(19906, proto.Message{"like": len(vs.Liked)})
}

// hero answers 19907: the card of a hero in a replay (what the list kept of it, no skills or gear).
func (v *vedioSvc) hero(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	r, err := v.st.ReplayByID(req.Int("replay_id"), role.RID)
	if err != nil || r == nil {
		return err
	}
	var meta replayMeta
	if err := json.Unmarshal([]byte(r.Meta), &meta); err != nil {
		return err
	}
	for _, h := range append(append([]replayHero(nil), meta.A.Heroes...), meta.B.Heroes...) {
		if h.ID != req.Int("partner_id") {
			continue
		}
		return s.Send(19907, proto.Message{"replay_id": r.ID, "partner_id": h.ID, "type": req.Int("type"), "pos": h.Pos, "bid": h.BID, "lev": h.Lev,
			"star": h.Star, "break_lev": 0, "power": 0, "now_hp": h.HP, "hp": h.HPMax, "atk": 0, "def": 0, "speed": 0, "crit_rate": 0, "crit_ratio": 0,
			"hit_magic": 0, "dodge_magic": 0, "dps": 0, "behurt": 0, "cure": 0, "skills": []proto.Message{}, "eqms": []proto.Message{},
			"artifacts": []proto.Message{}, "ext": []proto.Message{}})
	}
	return nil
}

// look answers 19908: one replay as a card (the one a chat message links to).
func (v *vedioSvc) look(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	r, err := v.st.ReplayByID(req.Int("replay_id"), role.RID)
	if err != nil || r == nil {
		return err
	}
	vs, err := v.state(s, role.RID)
	if err != nil {
		return err
	}
	m := v.entries([]store.Replay{*r}, vs)[0]
	m["type"], m["channel"] = req.Int("type"), req.Int("channel")
	return s.Send(19908, m)
}

// play answers 20036: the replay is played as a fight that settles nothing.
func (v *vedioSvc) play(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	refuse := func(msg string) error { return s.Send(20036, proto.Message{"code": 0, "msg": msg}) }
	r, err := v.st.ReplayByID(req.Int("replay_id"), role.RID)
	if err != nil {
		return err
	}
	if r == nil {
		return refuse("录像不存在")
	}
	intro, rounds, d, err := fightOfReplay(r.Data)
	if err != nil {
		return refuse("录像已损坏")
	}
	if err := s.Send(20036, proto.Message{"code": 1, "msg": ""}); err != nil {
		return err
	}
	_ = v.st.BumpReplay(r.ID, 0, 0, 1)
	return v.fights.begin(s, role, d.Combat, intro, rounds, d.Won, func(*gateway.Session, bool) error { return nil })
}
