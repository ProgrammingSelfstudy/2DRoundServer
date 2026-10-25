package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// heaven.json: the heaven dungeons and the holy equipment dial of the client (tools/extract_heaven.py).
//
//go:embed data/heaven.json
var heavenJSON []byte

const (
	heavenModule  = "heaven"
	combatHeaven  = 28 // BattleConst.Fight_Type.HeavenWar
	heavenCustoms = 10
)

type heavenCustom struct {
	Chapter    int64     `json:"chapter"`
	Order      int64     `json:"order"`
	DunID      int64     `json:"dun_id"`
	Boss       int64     `json:"boss"`
	Power      int64     `json:"power"`
	Units      []int64   `json:"units"`
	Cond       [][]int64 `json:"cond"`
	First      [][]int64 `json:"first"`
	FirstDrops []int64   `json:"first_drops"`
	Auto       [][]int64 `json:"auto"`
}

type heavenChapter struct {
	ID       int64     `json:"id"`
	CondInfo [][]int64 `json:"cond_info"`
	LimitID  int64     `json:"limit_id"`
}

type heavenStarAward struct {
	Award     [][]int64 `json:"award"`
	AwardID   int64     `json:"award_id"`
	LimitStar int64     `json:"limit_star"`
}

type heavenStarCond struct {
	Condition []json.RawMessage `json:"condition"`
}

type heavenGroup struct {
	GroupID   int64               `json:"group_id"`
	Award     [][]int64           `json:"award"`
	CanSetNum int64               `json:"can_set_num"`
	CanSetIDs []int64             `json:"can_set_ids"`
	DayLimit  int64               `json:"day_limit"`
	FreeTimes int64               `json:"free_times"`
	GainOnce  [][]int64           `json:"gain_once"`
	GoldOnce  int64               `json:"gold_once"`
	GoldTen   int64               `json:"gold_ten"`
	ItemOnce  [][]int64           `json:"item_once"`
	ItemTen   [][]int64           `json:"item_ten"`
	Odds      [][]json.RawMessage `json:"odds"`
}

type heavenConf struct {
	Const     map[string]json.RawMessage `json:"const"`
	Chapters  []heavenChapter            `json:"chapters"`
	Customs   []heavenCustom             `json:"customs"`
	StarAward [][]heavenStarAward        `json:"star_award"`
	StarCond  map[string]heavenStarCond  `json:"star_cond"`
	CountBuy  []struct {
		Cost     int64 `json:"cost"`
		Count    int64 `json:"count"`
		LimitVIP int64 `json:"limit_vip"`
	} `json:"count_buy"`
	Dial struct {
		Const  map[string]json.RawMessage `json:"const"`
		Groups []heavenGroup              `json:"groups"`
		Awards [][]struct {
			ID     int64     `json:"id"`
			Times  int64     `json:"times"`
			Reward [][]int64 `json:"reward"`
		} `json:"awards"`
	} `json:"dial"`

	custom map[string]heavenCustom
}

func (c *heavenConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

func hkey(chapter, order int64) string { return fmt.Sprintf("%d:%d", chapter, order) }

var heavenCfg = func() *heavenConf {
	c := &heavenConf{}
	if err := json.Unmarshal(heavenJSON, c); err != nil || len(c.Chapters) != 48 || len(c.Customs) != 480 || len(c.StarCond) == 0 {
		panic("game: bad heaven.json")
	}
	c.custom = map[string]heavenCustom{}
	for _, cu := range c.Customs {
		c.custom[hkey(cu.Chapter, cu.Order)] = cu
	}
	return c
}()

type heavenForm struct {
	Order, FormType, Hallows int64
	Pos                      []pfSlot
}

type heavenState struct {
	Day    int64
	Used   int64
	Bought int64
	Stars  map[string]int64 // "chapter:order" -> bit mask of the star goals met
	Awards map[string]bool  // "chapter:award_id"
	Forms  map[string][]heavenForm
	MaxDun int64
	// the dial
	Dial map[string]*heavenDial
	Log  []heavenLog
}

type heavenDial struct {
	FreeDay  int64
	Pulls    int64 // all time, for the progress awards
	Baodi    int64 // pulls since a good one
	DayCount int64
	DayKey   int64
	Awards   map[string]bool
	Lucky    map[string]int64
}

type heavenLog struct {
	Name     string
	Bid, Num int64
	Group    int64
}

type heavenSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterHeaven registers the heaven dungeons (25200-25232): 48 chapters of ten customs, each with three star goals; the
// tenth custom of a chapter has two teams of monsters and wants two teams of heroes. Every fight costs one of the day's
// two challenges (a loss costs nothing), gems buy two more; a custom with all three stars can be swept. The monsters are
// scaled copies of the role's best heroes (the client has their groups but not their stats). The dial gives holy
// equipment by the odds in the group's text. Level 105 and world level 110 (the client's rules).
func RegisterHeaven(r *gateway.Router, db *store.Store) {
	h := &heavenSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(25200, h.chapters)
	r.Handle(25201, h.chapter)
	r.Handle(25205, h.challenge)
	r.Handle(25207, h.buyCount)
	r.Handle(25208, h.sweep)
	r.Handle(25210, h.getForms)
	r.Handle(25211, h.saveForms)
	r.Handle(25215, h.starAward)
	r.Handle(25217, h.pull)
	r.Handle(25218, h.log)
	r.Handle(25219, h.dialInfo)
	r.Handle(25230, h.setLucky)
	r.Handle(25231, h.dialAward)
	r.Handle(25232, func(s *gateway.Session, _ proto.Message) error { return s.Send(25232, proto.Message{"score": 0}) })
}

func (h *heavenSvc) with(s *gateway.Session, fn func(role *store.Role, st *heavenState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := h.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &heavenState{}
	if _, err := h.st.LoadState(role.RID, heavenModule, st); err != nil {
		return err
	}
	if st.Stars == nil {
		st.Stars = map[string]int64{}
	}
	if st.Awards == nil {
		st.Awards = map[string]bool{}
	}
	if st.Forms == nil {
		st.Forms = map[string][]heavenForm{}
	}
	if st.Dial == nil {
		st.Dial = map[string]*heavenDial{}
	}
	if day := dayKey(s.Now()); st.Day != day {
		st.Day, st.Used, st.Bought = day, 0, 0
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return h.st.SaveState(role.RID, heavenModule, st)
}

func (h *heavenSvc) open(s *gateway.Session, cmd uint16, fail proto.Message, fn func(role *store.Role, st *heavenState) error) error {
	return h.with(s, func(role *store.Role, st *heavenState) error {
		if need := heavenCfg.num("open_lev"); int64(role.Lev) < need {
			fail["msg"] = fmt.Sprintf("个人等级到达%d级开启", need)
			return s.Send(cmd, fail)
		}
		return fn(role, st)
	})
}

func (h *heavenSvc) gain(s *gateway.Session, role *store.Role, items [][]int64) error {
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

func (h *heavenSvc) pay(s *gateway.Session, role *store.Role, items [][]int64, times int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	splitGains(guildGains(l), times, sa, si)
	err := h.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, sendAssets(s, h.st, role.RID)
}

func popcount3(m int64) int64 { return (m & 1) + (m >> 1 & 1) + (m >> 2 & 1) }

func (st *heavenState) chapterStars(ch int64) int64 {
	var n int64
	for o := int64(1); o <= heavenCustoms; o++ {
		n += popcount3(st.Stars[hkey(ch, o)])
	}
	return n
}

func (st *heavenState) cleared(ch, order int64) bool { return st.Stars[hkey(ch, order)]&1 != 0 }

// passStatus is 0 (not passed), 1 (all ten customs cleared) or 2 (all thirty stars).
func (st *heavenState) passStatus(ch int64) int64 {
	n := int64(0)
	for o := int64(1); o <= heavenCustoms; o++ {
		if st.cleared(ch, o) {
			n++
		}
	}
	if n < heavenCustoms {
		return 0
	}
	if st.chapterStars(ch) >= heavenCustoms*3 {
		return 2
	}
	return 1
}

// chapterOpen: the previous chapter passed (`limit_id`) and the stars other chapters must have (`cond_info`).
func (st *heavenState) chapterOpen(c heavenChapter) bool {
	if c.LimitID != 0 && st.passStatus(c.LimitID) == 0 {
		return false
	}
	for _, cond := range c.CondInfo {
		if len(cond) == 2 && st.chapterStars(cond[0]) < cond[1] {
			return false
		}
	}
	return true
}

func (h *heavenSvc) left(st *heavenState) int64 {
	return max(0, heavenCfg.num("refresh_number")+st.Bought-st.Used)
}

func (h *heavenSvc) awardInfo(st *heavenState, ch int64) []proto.Message {
	out := []proto.Message{}
	if int(ch) > len(heavenCfg.StarAward) {
		return out
	}
	stars := st.chapterStars(ch)
	for _, a := range heavenCfg.StarAward[ch-1] {
		flag := int64(0)
		switch {
		case st.Awards[hkey(ch, a.AwardID)]:
			flag = 2
		case stars >= a.LimitStar:
			flag = 1
		}
		out = append(out, proto.Message{"id": a.AwardID, "flag": flag})
	}
	return out
}

func (h *heavenSvc) chapterMessage(st *heavenState, c heavenChapter) proto.Message {
	return proto.Message{"id": c.ID, "all_star": st.chapterStars(c.ID), "award_info": h.awardInfo(st, c.ID), "is_finish": st.passStatus(c.ID)}
}

func (h *heavenSvc) chapters(s *gateway.Session, _ proto.Message) error {
	return h.with(s, func(role *store.Role, st *heavenState) error {
		list := []proto.Message{}
		for _, c := range heavenCfg.Chapters {
			if st.chapterOpen(c) {
				list = append(list, h.chapterMessage(st, c))
			}
		}
		return s.Send(25200, proto.Message{"count": h.left(st), "buy_count": st.Bought, "max_dun_id": st.MaxDun, "chapter_info": list})
	})
}

func (h *heavenSvc) customsMessage(st *heavenState, ch int64) []proto.Message {
	out := []proto.Message{}
	for o := int64(1); o <= heavenCustoms; o++ {
		cu := heavenCfg.custom[hkey(ch, o)]
		state := int64(0)
		switch {
		case st.cleared(ch, o):
			state = 2
		case o == 1 || st.cleared(ch, o-1):
			state = 1
		}
		mask := st.Stars[hkey(ch, o)]
		info := []proto.Message{}
		for i, c := range cu.Cond {
			info = append(info, proto.Message{"star_id": c[0], "flag": mask >> uint(i) & 1})
		}
		out = append(out, proto.Message{"id": o, "state": state, "star": popcount3(mask), "star_info": info})
	}
	return out
}

func (h *heavenSvc) chapter(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return h.with(s, func(role *store.Role, st *heavenState) error {
		if id < 1 || int(id) > len(heavenCfg.Chapters) || !st.chapterOpen(heavenCfg.Chapters[id-1]) {
			return nil
		}
		return s.Send(25201, proto.Message{"id": id, "all_star": st.chapterStars(id), "dun_info": h.customsMessage(st, id)})
	})
}

// push sends what a fight changed: the counts and chapters (25203) and the chapter's customs (25204).
func (h *heavenSvc) push(s *gateway.Session, st *heavenState, ch int64) error {
	list := []proto.Message{}
	for _, c := range heavenCfg.Chapters {
		if st.chapterOpen(c) {
			list = append(list, h.chapterMessage(st, c))
		}
	}
	if err := s.Send(25203, proto.Message{"count": h.left(st), "buy_count": st.Bought, "max_dun_id": st.MaxDun, "chapter_info": list}); err != nil {
		return err
	}
	return s.Send(25204, proto.Message{"id": ch, "dun_info": h.customsMessage(st, ch)})
}

func (h *heavenSvc) buyCount(s *gateway.Session, _ proto.Message) error {
	return h.open(s, 25207, proto.Message{"code": 0, "count": 0, "buy_count": 0}, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error {
			return s.Send(25207, proto.Message{"code": 0, "msg": msg, "count": h.left(st), "buy_count": st.Bought})
		}
		if st.Bought >= int64(len(heavenCfg.CountBuy)) {
			return refuse("今日购买次数已达上限")
		}
		price := heavenCfg.CountBuy[st.Bought]
		if int64(role.VIPLev) < price.LimitVIP {
			return refuse("VIP等级不足")
		}
		ok, err := h.pay(s, role, [][]int64{{3, price.Cost}}, 1)
		if err != nil {
			return err
		}
		if !ok {
			return refuse("钻石不足")
		}
		st.Bought++
		return s.Send(25207, proto.Message{"code": 1, "msg": "购买成功", "count": h.left(st), "buy_count": st.Bought})
	})
}

func (h *heavenSvc) starAward(s *gateway.Session, req proto.Message) error {
	ch, aid := req.Int("id"), req.Int("award_id")
	return h.with(s, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error {
			return s.Send(25215, proto.Message{"code": 0, "msg": msg, "id": ch, "award_info": h.awardInfo(st, ch)})
		}
		if ch < 1 || int(ch) > len(heavenCfg.StarAward) {
			return refuse("奖励不存在")
		}
		for _, a := range heavenCfg.StarAward[ch-1] {
			if a.AwardID != aid {
				continue
			}
			switch {
			case st.Awards[hkey(ch, aid)]:
				return refuse("已经领取")
			case st.chapterStars(ch) < a.LimitStar:
				return refuse("星数不足")
			}
			st.Awards[hkey(ch, aid)] = true
			if err := h.gain(s, role, a.Award); err != nil {
				return err
			}
			return s.Send(25215, proto.Message{"code": 1, "msg": "领取成功", "id": ch, "award_info": h.awardInfo(st, ch)})
		}
		return refuse("奖励不存在")
	})
}

// ---- formations

func (h *heavenSvc) getForms(s *gateway.Session, req proto.Message) error {
	t := req.Int("type")
	return h.with(s, func(role *store.Role, st *heavenState) error {
		out := []proto.Message{}
		for _, f := range st.Forms[strconv.FormatInt(t, 10)] {
			pos := []proto.Message{}
			for _, p := range f.Pos {
				pos = append(pos, proto.Message{"pos": p.Pos, "id": p.ID})
			}
			out = append(out, proto.Message{"order": f.Order, "formation_type": f.FormType, "pos_info": pos, "hallows_id": f.Hallows})
		}
		return s.Send(25210, proto.Message{"type": t, "formations": out})
	})
}

// saveForms (25211): the teams for a fight of one team (type 1) or two (type 2).
func (h *heavenSvc) saveForms(s *gateway.Session, req proto.Message) error {
	t := req.Int("type")
	return h.open(s, 25211, proto.Message{"code": 0}, func(role *store.Role, st *heavenState) error {
		refuse := func(msg string) error { return s.Send(25211, proto.Message{"code": 0, "msg": msg}) }
		heroes, err := h.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		own := map[int64]bool{}
		for _, p := range heroes {
			own[p.PartnerID] = true
		}
		used := map[int64]bool{}
		var forms []heavenForm
		for _, f := range req.Arr("formations") {
			form := heavenForm{Order: f.Int("order"), FormType: f.Int("formation_type"), Hallows: f.Int("hallows_id")}
			for _, p := range f.Arr("pos_info") {
				id := p.Int("id")
				if !own[id] {
					return refuse("英雄不存在")
				}
				if used[id] {
					return refuse("同一英雄不能出现在两支队伍中")
				}
				used[id] = true
				form.Pos = append(form.Pos, pfSlot{Pos: p.Int("pos"), ID: id})
			}
			if len(form.Pos) == 0 && t == 1 {
				return refuse("至少需要上阵一个英雄")
			}
			forms = append(forms, form)
		}
		if len(forms) == 0 {
			return refuse("至少需要上阵一个英雄")
		}
		st.Forms[strconv.FormatInt(t, 10)] = forms
		return s.Send(25211, proto.Message{"code": 1, "msg": "保存成功"})
	})
}
