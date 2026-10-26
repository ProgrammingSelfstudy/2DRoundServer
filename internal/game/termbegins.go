package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// termbegins.json: the school-term action (actiontermbegins 26700~26712) of the client (tools/extract_termbegins.py).
//
//go:embed data/termbegins.json
var termBeginsJSON []byte

const (
	tbModule       = "termbegins"
	tbCamp         = 97008 // ActionRankCommonType.term_begins: the camp_id the operator gives the activity in the schedule
	tbFormChapter  = 25    // PartnerConst.Fun_Form.TermBegins
	tbFormBoss     = 26    // PartnerConst.Fun_Form.TermBeginsBoss
	combatTBLevel  = 32    // BattleConst.Fight_Type.TermBegins
	combatTBBoss   = 33    // BattleConst.Fight_Type.TermBeginsBoss
	tbRoundDays    = 7     // "every seven days is a round, the last two days open the world boss"
	tbBossDays     = 2
	tbHPPerPower   = 3          // a level's monster has this many hp per point of its recommended power (as the limit exercise)
	tbBossHP       = 50_000_000 // the world boss's hp of a server and a round: ours, the table has none
	tbMinRanked    = 100        // "fewer than 100 players count as 100" for the rank percent
	tbRankListMax  = 100
	tbAnchorSecond = 1767542400 // 2026-01-05 00:00 +08:00, a Monday: the start of a schedule that has none
)

type tbChapter struct {
	Order, Diff, Power, Unit int64
	Name                     string
	Award                    [][2]int64
}

type tbRoundCfg struct {
	Chapters []tbChapter
	Boss     struct {
		ID, Unit int64
		Name     string
		Award    [][2]int64
	}
	MaxOrder int64 `json:"max_order"`
	MaxDiff  int64 `json:"max_diff"`
}

var tbCfg = func() (c struct {
	Const   map[string]json.RawMessage
	Rounds  []tbRoundCfg
	Units   map[string]monsterDef
	Collect []struct {
		ID, Count int64
		Award     [][2]int64
	}
	Ranks []struct {
		Rank1, Rank2 int64
		Award        [][2]int64
	}
}) {
	if err := json.Unmarshal(termBeginsJSON, &c); err != nil || len(c.Rounds) == 0 || len(c.Collect) == 0 || len(c.Ranks) == 0 {
		panic("game: bad termbegins.json")
	}
	return
}()

func tbNum(name string) int64 {
	var n int64
	_ = json.Unmarshal(tbCfg.Const[name], &n)
	return n
}

func tbPair(name string) [2]int64 {
	var p [2]int64
	_ = json.Unmarshal(tbCfg.Const[name], &p)
	return p
}

func (c *tbRoundCfg) chapter(order int64) (tbChapter, bool) {
	for _, ch := range c.Chapters {
		if ch.Order == order {
			return ch, true
		}
	}
	return tbChapter{}, false
}

// tbRole is what a role has done in the round.
type tbRole struct {
	Start   int64 // the activity's start and the round (counted from it) this state belongs to
	Round   int64
	Day     int64
	Order   int64   // the next level to fight; above the last one when all are cleared
	Left    int64   // hp left of that level's monster; 0 is untouched
	Count   int64   // fights left today
	Buys    int64   // fights bought today
	Paper   int64   // full-mark papers handed in this round
	Claimed []int64 // the paper rewards taken
	Last    int64   // the damage of the last world boss fight
	Total   int64   // all damage on the world boss this round
}

// tbWorld is what a server has done in the round.
type tbWorld struct {
	Start  int64
	Round  int64
	Paper  int64
	Hit    int64           // damage the world boss has taken
	Dmg    map[int64]int64 // per role
	Papers map[int64]int64 // per role
}

// tbWindow is the round the schedule is in: its number from 1, the card of the table, and when it starts, ends and opens the boss.
func tbWindow(start int64, now time.Time) (round int64, cfg *tbRoundCfg, begin, end, bossAt int64) {
	if start <= 0 {
		start = tbAnchorSecond
	}
	from := dayStart(time.Unix(start, 0)).Unix()
	days := (dayStart(now).Unix() - from) / 86400
	if days < 0 {
		days = 0
	}
	idx := days / tbRoundDays
	begin = from + idx*tbRoundDays*86400
	end = begin + tbRoundDays*86400
	return idx + 1, &tbCfg.Rounds[int(idx)%len(tbCfg.Rounds)], begin, end, end - tbBossDays*86400
}

func (w *tbWorld) ranking(m map[int64]int64) []int64 {
	var ids []int64
	for rid, v := range m {
		if v > 0 {
			ids = append(ids, rid)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] || (m[ids[i]] == m[ids[j]] && ids[i] < ids[j]) })
	return ids
}

func rankOf(ids []int64, rid int64) int64 {
	for i, id := range ids {
		if id == rid {
			return int64(i + 1)
		}
	}
	return 0
}

// tbPercent is the place as a share of the ones ranked, at least tbMinRanked of them, rounded up.
func tbPercent(rank, n int64, floor int64) int64 {
	if rank <= 0 {
		return 0
	}
	d := max(n, floor)
	return (rank*100 + d - 1) / d
}

func tbRankAward(percent int64) [][2]int64 {
	for _, r := range tbCfg.Ranks {
		if percent > r.Rank1 && percent <= r.Rank2 {
			return r.Award
		}
	}
	return tbCfg.Ranks[0].Award
}

func (w *tbWorld) buff() int64 {
	pct := min(w.Paper*100/max(tbNum("collection_quantity"), 1), 100)
	return pct * tbNum("damage")
}

func (w *tbWorld) bossLeft() int64 { return max(0, tbBossHP-w.Hit) }

func (r *tbRole) hpOf(cfg *tbRoundCfg) int64 {
	ch, ok := cfg.chapter(r.Order)
	if !ok {
		return 0
	}
	return ch.Power * tbHPPerPower
}

type tbSvc struct {
	st     *store.Store
	fights *FightService
}

// RegisterTermBegins registers the school-term action (26700 the page, 26701 buy a fight, 26702 fight the level, 26704 hand in
// papers, 26705 / 26706 the paper rewards, 26707 fight the world boss, 26709 mop it up, 26710 the damage rank, 26711 the
// summary, 26712 buy an exam ticket). It runs while the operator has the activity with camp_id 97008 open in the schedule; its
// time is cut into rounds of seven days (the table has two, they repeat). In a round the role fights 20 levels in order (four
// difficulties of five), two free fights a day and six bought for 100 diamonds, the monster's hp carried from fight to fight
// until it dies; a cleared level pays papers and exam tickets. Papers are handed in for the server's progress (every 1% of
// 113750 gives everybody +5% damage in the boss fight) and the personal rewards; on the last two days a world boss, one hp
// pool for the server, is fought with exam tickets (or 100 diamonds for one); every fight pays and adds to the damage rank,
// and the rank's rewards by its percentile are mailed when the round is over. Ours, not captured: the boss's hp, the rank rule
// (damage total, fewer than 100 players count as 100), the rounds cut from the schedule's start, the monster's hp from the
// recommended power, the mop-up (26709) repeating the last damage.
func RegisterTermBegins(r *gateway.Router, db *store.Store) {
	t := &tbSvc{st: db, fights: fightServiceOf(db)}
	r.Handle(26700, t.page)
	r.Handle(26701, t.buy)
	r.Handle(26702, t.fightLevel)
	r.Handle(26704, t.submit)
	r.Handle(26705, t.papers)
	r.Handle(26706, t.claim)
	r.Handle(26707, t.fightBoss)
	r.Handle(26709, t.sweep)
	r.Handle(26710, t.rank)
	r.Handle(26711, t.summary)
	r.Handle(26712, t.buyTicket)
}

// settleWorld mails the rank rewards of a round that is over.
func (t *tbSvc) settleWorld(srv string, w *tbWorld) error {
	ids := w.ranking(w.Dmg)
	for i, rid := range ids {
		award := tbRankAward(tbPercent(int64(i+1), int64(len(ids)), tbMinRanked))
		m := store.Mail{SrvID: srv, Subject: "开学季世界Boss排名奖励", Content: "开学季世界Boss挑战已结束，你的伤害排在第" + itoa64(int64(i+1)) + "名，这是排名奖励。",
			FromName: "开学季", CreatedBy: "termbegins", Attachments: attachmentsOf(award)}
		if _, err := t.st.CreateMail(m, []int64{rid}); err != nil {
			return err
		}
	}
	return nil
}

type tbCtx struct {
	role               *store.Role
	r                  *tbRole
	w                  *tbWorld
	cfg                *tbRoundCfg
	round              int64
	start, end, bossAt int64
	activityEnd, now   int64
	open, bossOpen     bool
}

// with loads the state of the role and the server in the round of now, rolls them over to it (mailing the rank of the round that
// went by) and saves them after fn.
func (t *tbSvc) with(s *gateway.Session, fn func(c *tbCtx) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := t.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	now := s.Now()
	start, activityEnd, open := ActivityOpen(t.st, role.SrvID, tbCamp, now)
	c := &tbCtx{role: role, now: now.Unix(), open: open}
	c.r, c.w = &tbRole{}, &tbWorld{}
	if _, err := t.st.LoadState(role.RID, tbModule, c.r); err != nil {
		return err
	}
	if _, err := t.st.LoadGlobal(role.SrvID, tbModule, c.w); err != nil {
		return err
	}
	if !open {
		return fn(c)
	}
	c.round, c.cfg, c.start, c.end, c.bossAt = tbWindow(start, now)
	c.activityEnd = activityEnd
	if activityEnd <= 0 || activityEnd > c.end {
		c.activityEnd = c.end
	}
	c.bossOpen = c.now >= c.bossAt
	if c.w.Start != start || c.w.Round != c.round {
		if err := t.settleWorld(role.SrvID, c.w); err != nil {
			return err
		}
		*c.w = tbWorld{Start: start, Round: c.round}
	}
	if c.w.Dmg == nil {
		c.w.Dmg = map[int64]int64{}
	}
	if c.w.Papers == nil {
		c.w.Papers = map[int64]int64{}
	}
	if c.r.Start != start || c.r.Round != c.round {
		*c.r = tbRole{Start: start, Round: c.round, Order: 1}
	}
	if day := dayKey(now); c.r.Day != day {
		c.r.Day, c.r.Count, c.r.Buys = day, tbNum("free_fight_count"), 0
	}
	if err := fn(c); err != nil {
		return err
	}
	if err := t.st.SaveGlobal(role.SrvID, tbModule, c.w); err != nil {
		return err
	}
	return t.st.SaveState(role.RID, tbModule, c.r)
}

func (c *tbCtx) hpPer() int64 {
	if c.r.Left <= 0 {
		return 1000
	}
	return max(1, c.r.Left*1000/max(c.r.hpOf(c.cfg), 1))
}

func (c *tbCtx) page() proto.Message {
	if !c.open {
		return proto.Message{"round": 1, "end_time": 0, "difficulty": 1, "order": 1, "cha_count": 0, "buy_count": 0, "hp_per": 1000, "boss_flag": 0,
			"paper_sum": 0, "boss_id": 0, "boss_time": 0, "boss_hp_per": 1000, "dps": 0, "buff_per": 0, "boss_rank": 0}
	}
	per := max(c.cfg.MaxOrder/max(c.cfg.MaxDiff, 1), 1)
	diff := min((c.r.Order-1)/per+1, max(c.cfg.MaxDiff, 1))
	flag := int64(0)
	if c.bossOpen {
		flag = 1
	}
	bossHP := int64(0)
	if left := c.w.bossLeft(); left > 0 {
		bossHP = max(1, left*1000/tbBossHP)
	}
	ids := c.w.ranking(c.w.Dmg)
	return proto.Message{"round": (c.round-1)%int64(len(tbCfg.Rounds)) + 1, "end_time": c.activityEnd, "difficulty": diff, "order": c.r.Order,
		"cha_count": c.r.Count, "buy_count": max(0, tbNum("limit_buy_count")-c.r.Buys), "hp_per": c.hpPer(), "boss_flag": flag, "paper_sum": c.w.Paper,
		"boss_id": c.cfg.Boss.ID, "boss_time": c.bossAt, "boss_hp_per": bossHP, "dps": c.r.Last, "buff_per": c.w.buff(),
		"boss_rank": tbPercent(rankOf(ids, c.role.RID), int64(len(ids)), tbMinRanked)}
}

func (t *tbSvc) page26700(s *gateway.Session, c *tbCtx) error { return s.Send(26700, c.page()) }

func (t *tbSvc) page(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error { return t.page26700(s, c) })
}

func (t *tbSvc) gain(s *gateway.Session, role *store.Role, list [][2]int64) error {
	ga, gi := map[int64]int64{}, map[int64]int64{}
	var known []sellGain
	for _, g := range guildGains(list) {
		if ItemKnown(g.ID) {
			known = append(known, g)
		}
	}
	splitGains(known, 1, ga, gi)
	if err := t.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := sendAssets(s, t.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, t.st)
}

// spend takes a price; false when the role cannot afford it.
func (t *tbSvc) spend(s *gateway.Session, role *store.Role, id, n int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	splitGains([]sellGain{{guildLabel(id), n}}, 1, sa, si)
	if err := t.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{}); err != nil {
		if err == store.ErrConflict {
			return false, nil
		}
		return false, err
	}
	return true, sendAssets(s, t.st, role.RID)
}

func (t *tbSvc) buy(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		refuse := func(msg string) error {
			return s.Send(26701, proto.Message{"code": 0, "msg": msg, "count": c.r.Count, "buy_count": max(0, tbNum("limit_buy_count")-c.r.Buys)})
		}
		switch {
		case !c.open:
			return refuse("活动未开启")
		case c.r.Buys >= tbNum("limit_buy_count"):
			return refuse("购买次数已达上限")
		}
		cost := tbPair("action_num_espensive")
		if ok, err := t.spend(s, c.role, cost[0], cost[1]); err != nil {
			return err
		} else if !ok {
			return refuse("钻石不足")
		}
		c.r.Count++
		c.r.Buys++
		return s.Send(26701, proto.Message{"code": 1, "msg": "", "count": c.r.Count, "buy_count": tbNum("limit_buy_count") - c.r.Buys})
	})
}

func tbIntro(role *store.Role, combat, formType int64, objects []proto.Message, obj proto.Message, n int, target string) proto.Message {
	return proto.Message{
		"combat_type": combat, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
		"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": n, "actor_role_name": role.Name, "target_role_name": target,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
}

// tbHurts is the damage statistics of a fight: the damage shared by the heroes put in, and the one of the highest power.
func (t *tbSvc) hurts(role *store.Role, slots []store.FormationSlot, damage int64) ([]proto.Message, int64, error) {
	all, err := t.st.PartnersOf(role.RID)
	if err != nil {
		return nil, 0, err
	}
	byID := map[int64]store.Partner{}
	for _, h := range all {
		byID[h.PartnerID] = h
	}
	var list []proto.Message
	var best, bestPower int64
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		pw := partnerMessage(h).Int("power")
		list = append(list, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": partnerTable[h.BID].Camp,
			"dps": damage / int64(max(len(slots), 1)), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
		if pw > bestPower {
			best, bestPower = h.BID, pw
		}
	}
	return []proto.Message{{"type": 1, "partner_hurts": list}}, best, nil
}

func (t *tbSvc) monster(unit int64) (monsterDef, bool) {
	m, ok := tbCfg.Units[itoa64(unit)]
	return m, ok
}

// fightLevel (26702) plays the level the role is at with the team saved for the action (11212 of kind 25).
func (t *tbSvc) fightLevel(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		refuse := func(msg string) error { return s.Send(26702, proto.Message{"code": 0, "msg": msg}) }
		if !c.open {
			return refuse("活动未开启")
		}
		ch, ok := c.cfg.chapterOf(c)
		switch {
		case !ok:
			return refuse("已通关全部关卡")
		case c.r.Count <= 0:
			return refuse("挑战次数不足")
		}
		def, ok := t.monster(ch.Unit)
		if !ok {
			return refuse("关卡数据缺失")
		}
		form, err := formationOf(t.st, c.role.RID, tbFormChapter)
		if err != nil {
			return err
		}
		formType := max(1, form.FormationType)
		team, objects, err := t.fights.teamFor(c.role, formType, form.Slots)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		maxHP := ch.Power * tbHPPerPower
		hp := maxHP
		if c.r.Left > 0 {
			hp = c.r.Left
		}
		unit, obj := monsterOf(def, 0)
		unit.hp, unit.maxHP = hp, maxHP
		obj["hp"], obj["hp_max"], obj["object_name"] = hp, maxHP, ch.Name
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
		damage := max(0, hp-sim.units[len(team)].hp)
		if err := s.Send(26702, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		c.r.Count--
		round, order := c.round, ch.Order
		return t.fights.begin(s, c.role, combatTBLevel, tbIntro(c.role, combatTBLevel, formType, objects, obj, len(team), ch.Name), rounds, damage >= hp, func(s *gateway.Session, _ bool) error {
			return t.settleLevel(s, round, order, damage, form.Slots)
		})
	})
}

func (c *tbRoundCfg) chapterOf(ctx *tbCtx) (tbChapter, bool) {
	if ctx.r.Order > c.MaxOrder {
		return tbChapter{}, false
	}
	return c.chapter(ctx.r.Order)
}

func (t *tbSvc) settleLevel(s *gateway.Session, round, order, damage int64, slots []store.FormationSlot) error {
	return t.with(s, func(c *tbCtx) error {
		if !c.open || c.round != round || c.r.Order != order { // the round moved on meanwhile
			return nil
		}
		ch, _ := c.cfg.chapter(order)
		hp := ch.Power * tbHPPerPower
		if c.r.Left > 0 {
			hp = c.r.Left
		}
		won := damage >= hp
		var prize [][2]int64
		result := int64(2)
		if won {
			c.r.Order, c.r.Left, result = order+1, 0, 1
			prize = ch.Award
			if err := t.gain(s, c.role, prize); err != nil {
				return err
			}
		} else {
			c.r.Left = hp - damage
		}
		hurts, _, err := t.hurts(c.role, slots, damage)
		if err != nil {
			return err
		}
		if err := s.Send(26703, proto.Message{"bossid": order, "result": result, "award_list": awardList(prize), "target_role_name": ch.Name, "hurt_statistics": hurts}); err != nil {
			return err
		}
		return t.page26700(s, c)
	})
}

func (t *tbSvc) submit(s *gateway.Session, req proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		refuse := func(msg string) error {
			return s.Send(26704, proto.Message{"code": 0, "msg": msg, "paper_sum": c.w.Paper})
		}
		n := req.Int("count")
		if !c.open {
			return refuse("活动未开启")
		}
		if n < 1 {
			return refuse("数量不对")
		}
		if ok, err := t.spend(s, c.role, tbNum("paper_item_id"), n); err != nil {
			return err
		} else if !ok {
			return refuse("满分试卷不足")
		}
		c.r.Paper += n
		c.w.Paper += n
		c.w.Papers[c.role.RID] += n
		if err := sendBackpack(s, t.st); err != nil {
			return err
		}
		return s.Send(26704, proto.Message{"code": 1, "msg": "", "paper_sum": c.w.Paper})
	})
}

func (t *tbSvc) papers(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		list := []proto.Message{}
		for _, r := range tbCfg.Collect {
			status := 0
			switch {
			case has(c.r.Claimed, r.ID):
				status = 2
			case c.r.Paper >= r.Count:
				status = 1
			}
			list = append(list, proto.Message{"id": r.ID, "staus": status})
		}
		return s.Send(26705, proto.Message{"collect_schedule": list, "num": c.r.Paper})
	})
}

func (t *tbSvc) claim(s *gateway.Session, req proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		id := req.Int("id")
		refuse := func(msg string) error { return s.Send(26706, proto.Message{"code": 0, "msg": msg, "id": id}) }
		for _, r := range tbCfg.Collect {
			if r.ID != id {
				continue
			}
			switch {
			case !c.open:
				return refuse("活动未开启")
			case has(c.r.Claimed, id):
				return refuse("奖励已领取")
			case c.r.Paper < r.Count:
				return refuse("提交的满分试卷不足")
			}
			c.r.Claimed = append(c.r.Claimed, id)
			if err := t.gain(s, c.role, r.Award); err != nil {
				return err
			}
			return s.Send(26706, proto.Message{"code": 1, "msg": "", "id": id})
		}
		return refuse("奖励不存在")
	})
}

// bossHit puts a fight's damage on the world boss, pays the hit and tells the client the result (26708).
func (t *tbSvc) bossHit(s *gateway.Session, c *tbCtx, damage int64, slots []store.FormationSlot) error {
	damage = min(damage, c.w.bossLeft())
	c.w.Hit += damage
	c.w.Dmg[c.role.RID] += damage
	c.r.Last = damage
	c.r.Total += damage
	prize := c.cfg.Boss.Award
	if err := t.gain(s, c.role, prize); err != nil {
		return err
	}
	hurts, best, err := t.hurts(c.role, slots, damage)
	if err != nil {
		return err
	}
	if err := s.Send(26708, proto.Message{"bossid": c.cfg.Boss.ID, "result": 1, "all_dps": damage, "best_partner": best, "award_list": awardList(prize),
		"target_role_name": c.cfg.Boss.Name, "hurt_statistics": hurts}); err != nil {
		return err
	}
	return t.page26700(s, c)
}

// bossGate is why the world boss cannot be fought now, or "".
func (c *tbCtx) bossGate() string {
	switch {
	case !c.open:
		return "活动未开启"
	case !c.bossOpen:
		return "世界Boss尚未开放"
	case c.w.bossLeft() <= 0:
		return "世界Boss已被击败"
	}
	return ""
}

// fightBoss (26707) plays the world boss with the team saved for it (kind 26) for an exam ticket.
func (t *tbSvc) fightBoss(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		refuse := func(msg string) error { return s.Send(26707, proto.Message{"code": 0, "msg": msg}) }
		if msg := c.bossGate(); msg != "" {
			return refuse(msg)
		}
		def, ok := t.monster(c.cfg.Boss.Unit)
		if !ok {
			return refuse("Boss数据缺失")
		}
		form, err := formationOf(t.st, c.role.RID, tbFormBoss)
		if err != nil {
			return err
		}
		formType := max(1, form.FormationType)
		team, objects, err := t.fights.teamFor(c.role, formType, form.Slots)
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		cost := tbPair("boss_fight_cost")
		if ok, err := t.spend(s, c.role, cost[0], cost[1]); err != nil {
			return err
		} else if !ok {
			return refuse("准考证不足")
		}
		buff := c.w.buff()
		for i := range team {
			team[i].atk = team[i].atk * (100 + buff) / 100
		}
		hp := c.w.bossLeft()
		unit, obj := monsterOf(def, 0)
		unit.hp, unit.maxHP = hp, hp
		obj["hp"], obj["hp_max"], obj["object_name"] = hp, hp, c.cfg.Boss.Name
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
		damage := max(0, hp-sim.units[len(team)].hp)
		if err := s.Send(26707, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		round := c.round
		return t.fights.begin(s, c.role, combatTBBoss, tbIntro(c.role, combatTBBoss, formType, objects, obj, len(team), c.cfg.Boss.Name), rounds, damage >= hp, func(s *gateway.Session, _ bool) error {
			return t.with(s, func(c *tbCtx) error {
				if !c.open || c.round != round {
					return nil
				}
				return t.bossHit(s, c, damage, form.Slots)
			})
		})
	})
}

// sweep (26709) repeats the damage of the last boss fight for one exam ticket.
func (t *tbSvc) sweep(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		refuse := func(msg string) error { return s.Send(26709, proto.Message{"code": 0, "msg": msg}) }
		if msg := c.bossGate(); msg != "" {
			return refuse(msg)
		}
		if c.r.Last <= 0 {
			return refuse("还没有挑战过世界Boss")
		}
		cost := tbPair("boss_fight_cost")
		if ok, err := t.spend(s, c.role, cost[0], cost[1]); err != nil {
			return err
		} else if !ok {
			return refuse("准考证不足")
		}
		if err := s.Send(26709, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		form, err := formationOf(t.st, c.role.RID, tbFormBoss)
		if err != nil {
			return err
		}
		return t.bossHit(s, c, c.r.Last, form.Slots)
	})
}

// buyTicket (26712) buys an exam ticket with diamonds; flag is 0 to go on to the formation, else the boss to mop up.
func (t *tbSvc) buyTicket(s *gateway.Session, req proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		flag := req.Int("flag")
		refuse := func(msg string) error { return s.Send(26712, proto.Message{"code": 0, "msg": msg, "flag": flag}) }
		if !c.open {
			return refuse("活动未开启")
		}
		cost := tbPair("entrance_exam_cost")
		if ok, err := t.spend(s, c.role, cost[0], cost[1]); err != nil {
			return err
		} else if !ok {
			return refuse("钻石不足")
		}
		if err := t.gain(s, c.role, [][2]int64{{tbNum("ticket_item_id"), 1}}); err != nil {
			return err
		}
		return s.Send(26712, proto.Message{"code": 1, "msg": "", "flag": flag})
	})
}

// rank (26710) lists the roles by their damage on the world boss.
func (t *tbSvc) rank(s *gateway.Session, req proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		ids := c.w.ranking(c.w.Dmg)
		mine := rankOf(ids, c.role.RID)
		from, to := max(req.Int("start_num"), 1), req.Int("end_num")
		if to <= 0 || to > tbRankListMax {
			to = tbRankListMax
		}
		list := []proto.Message{}
		for i := from; i <= min(to, int64(len(ids))); i++ {
			who, err := t.st.RoleByID(ids[i-1])
			if err != nil {
				continue
			}
			face := int64(who.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			family := ""
			if g, _, err := t.st.GuildOfRole(who.RID); err == nil && g != nil {
				family = g.Name
			}
			all, _ := t.st.PartnersOf(who.RID)
			list = append(list, proto.Message{"rank": i, "r_rid": who.RID, "r_srvid": who.SrvID, "name": who.Name, "family_name": family, "all_dps": c.w.Dmg[who.RID],
				"face_id": face, "lev": who.Lev, "power": rolePower(bestHeroes(all, 5)), "avatar_bid": 0})
		}
		// the damage that lifts the role to the next reward band (the first place needs none)
		next := int64(0)
		if mine != 1 {
			pct := tbPercent(mine, int64(len(ids)), tbMinRanked)
			if mine == 0 {
				pct = 100
			}
			for _, r := range tbCfg.Ranks {
				if r.Rank2 < pct {
					continue
				}
				// the place that is the last of the next better band
				place := max(1, r.Rank1*max(int64(len(ids)), tbMinRanked)/100)
				if place < int64(len(ids)) && place >= 1 {
					next = max(0, c.w.Dmg[ids[place-1]]-c.w.Dmg[c.role.RID]+1)
				}
				break
			}
		}
		return s.Send(26710, proto.Message{"rank": mine, "mydps": c.w.Dmg[c.role.RID], "rank_per": tbPercent(mine, int64(len(ids)), tbMinRanked), "next_power": next, "rank_list": list})
	})
}

// summary (26711) is the card on the activity's tab: the round, the role's place by papers handed in (as a percentile), the papers
// and the levels cleared.
func (t *tbSvc) summary(s *gateway.Session, _ proto.Message) error {
	return t.with(s, func(c *tbCtx) error {
		if !c.open {
			return s.Send(26711, proto.Message{"round": 1, "rank": 0, "count": 0, "order": 0})
		}
		ids := c.w.ranking(c.w.Papers)
		return s.Send(26711, proto.Message{"round": (c.round-1)%int64(len(tbCfg.Rounds)) + 1, "rank": tbPercent(rankOf(ids, c.role.RID), int64(len(ids)), 1),
			"count": c.r.Paper, "order": min(c.r.Order-1, c.cfg.MaxOrder)})
	})
}
