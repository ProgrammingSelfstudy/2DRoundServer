package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// partners.json is the client's hero basics (tools/extract_partners.py).
//
//go:embed data/partners.json
var partnersJSON []byte

type starInfo struct {
	Skills [][2]int64 `json:"skills"` // [pos, skill_bid]
	Add    [4]int64   `json:"add"`    // hp, atk, def, speed multipliers, 1000 = x1
	LevMax int        `json:"lev_max"`
	// Flat attribute bonuses of the star (name, value).
	Attr map[string]int64 `json:"attr"`
	// What going up to this star takes (tools/extract_partners.py): the hero
	// itself (bid, star, 1), specific heroes (bid, star, n), heroes of a camp
	// (camp, star, n; camp 0 = any) and other costs (id, amount).
	Self       [][3]int64 `json:"self"`
	Fodder     [][3]int64 `json:"fodder"`
	CampFodder [][3]int64 `json:"camp_fodder"`
	Cost       [][2]int64 `json:"cost"`
}

type partnerInfo struct {
	Name     string              `json:"name"`
	InitStar int                 `json:"init_star"`
	Type     int                 `json:"type"`
	BreakID  int                 `json:"break_id"`
	Camp     int                 `json:"camp"`
	Attr     map[string]int64    `json:"attr"`
	Stars    map[string]starInfo `json:"stars"`
}

var partnerTable = func() map[int64]partnerInfo {
	var f struct {
		Partners map[int64]partnerInfo `json:"partners"`
	}
	if err := json.Unmarshal(partnersJSON, &f); err != nil || len(f.Partners) == 0 {
		panic("game: bad partners.json")
	}
	return f.Partners
}()

// The first hero of a new role is the client's own "初始英雄" (data_partner_const):
// 10401 for a male role, 20402 for a female one.
const (
	starterMale   = 10401
	starterFemale = 20402
)

// starterSeeds is the hero list a new role starts with. Which sex value is the
// female one is read from the client's creation window: 2.
func starterSeeds(sex int) []store.PartnerSeed {
	bid := int64(starterMale)
	if sex == 2 {
		bid = starterFemale
	}
	return []store.PartnerSeed{{BID: bid, Star: partnerTable[bid].InitStar}}
}

// partnerStats works out a hero's attributes. The client only holds the level-1
// values and a per-level growth ("add_*", per mille of the level-1 value, which
// fits the numbers: 33 atk and 3314 add_atk is about +3.3 a level); the real
// formula is on the original server. Ours: (base + add * (lev - 1) / 1000),
// times the break step's multiplier and the star's multiplier (1000 = x1),
// plus the star's flat bonuses.
func partnerStats(p store.Partner) (hp, atk, def, speed int64) {
	info := partnerTable[p.BID]
	a := info.Attr
	lev, brk := shownLevBreak(p)
	br := growth.Brach[brachKey(info, brk)]
	st := info.Stars[strconv.Itoa(p.Star)]
	one := func(base, add, brMul, stMul int64, flat string) int64 {
		v := base + add*int64(lev-1)/1000
		if brMul == 0 {
			brMul = 1000
		}
		if stMul == 0 {
			stMul = 1000
		}
		return v*brMul/1000*stMul/1000 + st.Attr[flat]
	}
	hp, atk = one(a["hp_max"], a["add_hp"], br.Add[0], st.Add[0], "hp_max"), one(a["atk"], a["add_atk"], br.Add[1], st.Add[1], "atk")
	def, speed = one(a["def"], a["add_def"], br.Add[2], st.Add[2], "def"), one(a["speed"], a["add_speed"], br.Add[3], st.Add[3], "speed")
	for _, e := range p.Equips { // worn equipment adds its main attributes on top
		eq := equipmentTable[e.BaseID]
		hp, atk, def, speed = hp+eq.Attr["hp_max"], atk+eq.Attr["atk"], def+eq.Attr["def"], speed+eq.Attr["speed"]
	}
	return
}

// heroPower is the power a hero shows. The weights were fitted to the 22 captured heroes up to level 16
// (original-2026-10-21/22, 19 of them exact and the rest off by one): atk 2, hp 0.4, def 2.65, speed 6.4. From
// level 30 on the original adds more (skills, break steps), so its numbers come out higher than ours. Our
// own attributes (partnerStats) are 7-15% under the captured ones at low levels, so our powers are too.
func heroPower(atk, hp, def, speed int64) int64 {
	return int64(math.Round(2*float64(atk) + 0.4*float64(hp) + 2.65*float64(def) + 6.4*float64(speed)))
}

// partnerMessage builds one hero entry for 11000 / 11025 / 11026 / 11007 (the
// encoder sends only the fields a command's table has).
func partnerMessage(p store.Partner) proto.Message {
	info := partnerTable[p.BID]
	a := info.Attr
	hp, atk, def, speed := partnerStats(p)
	shownLev, shownBreak := shownLevBreak(p)
	skills := []proto.Message{}
	for _, sk := range info.Stars[strconv.Itoa(p.Star)].Skills {
		skills = append(skills, proto.Message{"pos": sk[0], "skill_bid": sk[1]})
	}
	return proto.Message{
		"partner_id": p.PartnerID, "bid": p.BID, "lev": shownLev, "star": p.Star, "skills": skills,
		"break_lev": shownBreak, "power": heroPower(atk, hp, def, speed),
		"is_lock": lockList(p), "use_skin": p.UseSkin, "end_time": 0,
		"resonate_lev": b2i(p.ResSlot > 0), "resonate_break_lev": 0, "horcrux_status": 0, "horcrux_lev": 0,
		"atk": atk, "def_p": def, "def_s": def, "hp": hp, "speed": speed,
		"hit_rate": a["hit_rate"], "dodge_rate": a["dodge_rate"], "crit_rate": a["crit_rate"], "crit_ratio": a["crit_ratio"],
		"hit_magic": a["hit_magic"], "dodge_magic": a["dodge_magic"], "def": def,
		"atk2": 0, "hp2": 0, "def2": 0, "speed2": 0,
		"eqms": eqmsOf(p), "artifacts": []proto.Message{}, "dower_skill": talentMessages(p.Talents),
	}
}

// heroListMax is the number of hero slots a role starts with ("初始英雄格子数" in the
// client's data_partner_const); buying slots adds to it (heroSlotCapacity).
const heroListMax = 300

// slotBuys is the client's data_partner_buy: the gems of the n-th slot purchase
// (index n-1); every purchase adds heroSlotsPerBuy slots, and there are ten.
var slotBuys = [...]int64{100, 150, 200, 250, 300, 300, 300, 300, 300, 300}

const heroSlotsPerBuy = 5

// heroSlotCapacity is how many heroes a role may hold after `buys` purchases.
func heroSlotCapacity(buys int) int { return heroListMax + heroSlotsPerBuy*buys }

// RegisterPartner registers the hero list (11000 full, 11025 brief) and the
// details by id (11026). The first time a role asks for the list it receives
// the starter heroes. The encoder sends only the fields a command's table has,
// so one full message serves all three.
func RegisterPartner(r *gateway.Router, st *store.Store) {
	messages := func(rid int64, only map[int64]bool) ([]proto.Message, error) {
		ps, err := st.PartnersOf(rid)
		if err != nil {
			return nil, err
		}
		msgs := make([]proto.Message, 0, len(ps))
		for _, p := range ps {
			if _, ok := partnerTable[p.BID]; ok && (only == nil || only[p.PartnerID]) {
				msgs = append(msgs, partnerMessage(p))
			}
		}
		return msgs, nil
	}
	list := func(cmd uint16) gateway.Handler {
		return func(s *gateway.Session, _ proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			if _, err := st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
				return err
			}
			msgs, err := messages(role.RID, nil)
			if err != nil {
				return err
			}
			buys, err := st.HeroSlotBuys(role.RID)
			if err != nil {
				return err
			}
			return s.Send(cmd, proto.Message{"sort_type": 0, "num": heroSlotCapacity(buys), "buy_num": buys, "partners": msgs})
		}
	}
	r.Handle(11000, list(11000))
	r.Handle(11025, list(11025))
	RegisterPartnerGrowth(r, st)
	r.Handle(11009, func(s *gateway.Session, _ proto.Message) error { return buyHeroSlot(s, st) })
	RegisterPartnerStar(r, st)
	RegisterSummon(r, st)
	RegisterEliteSummon(r, st)
	RegisterHolidayElite(r, st)
	RegisterTimeSummon(r, st)
	RegisterSnatch(r, st)
	RegisterAnimate(r, st)
	RegisterTermBegins(r, st)
	RegisterReturnAction(r, st)
	RegisterYearMonster(r, st)
	RegisterDial(r, st)
	RegisterDramaPass(r, st)
	RegisterPresage(r, st)
	RegisterActivity(r, st)
	RegisterSevenGoal(r, st)
	RegisterWarOrder(r, st)
	RegisterElitePass(r, st)
	RegisterNewWarOrder(r, st)
	RegisterHeroLock(r, st)
	RegisterTalent(r, st)
	RegisterReset(r, st)
	RegisterReturn(r, st)
	RegisterResonance(r, st)
	RegisterResonanceStone(r, st)
	RegisterSkin(r, st)
	RegisterSeerpalace(r, st)
	RegisterVIPGifts(r, st)
	// 11063 the attribute list of one hero (the tips panel behind its numbers): the same numbers the hero entry
	// carries, the rest from the hero's table; what we do not model is 0.
	r.Handle(11063, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		p, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, ok := partnerTable[p.BID]; !ok {
			return nil
		}
		m := partnerMessage(*p)
		a := partnerTable[p.BID].Attr
		out := proto.Message{"partner_id": p.PartnerID, "ext_data": []proto.Message{}}
		for _, k := range []string{"atk", "def_p", "def_s", "hp", "speed", "hit_rate", "dodge_rate", "crit_rate", "crit_ratio", "hit_magic", "dodge_magic", "def"} {
			out[k] = m[k]
		}
		for _, k := range []string{"dam", "res", "cure", "be_cure", "tenacity", "dam_p", "dam_s", "res_p", "res_s"} {
			out[k] = a[k]
		}
		return s.Send(11063, out)
	})
	r.Handle(11026, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		only := map[int64]bool{}
		for _, a := range req.Arr("partner_ids") {
			only[a.Int("partner_id")] = true
		}
		msgs, err := messages(role.RID, only)
		if err != nil {
			return err
		}
		return s.Send(11026, proto.Message{"partners": msgs})
	})
}

// buyHeroSlot answers 11009: buy the next batch of hero slots with gems.
func buyHeroSlot(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	buys, err := st.HeroSlotBuys(role.RID)
	if err != nil {
		return err
	}
	reply := func(ok int, msg string, buys int) error {
		return s.Send(11009, proto.Message{"result": ok, "msg": msg, "num": heroSlotCapacity(buys), "buy_num": buys})
	}
	if buys >= len(slotBuys) {
		return reply(0, "已达到购买上限", buys)
	}
	if err := st.BuyHeroSlot(role.RID, buys, map[int64]int64{3: slotBuys[buys]}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return reply(0, "钻石不足", buys)
		}
		return err
	}
	if err := reply(1, "购买成功", buys+1); err != nil {
		return err
	}
	return sendAssets(s, st, role.RID)
}

// heroLockType is HeroConst.LockType.eHeroLock, the lock the owner sets with 11015.
const heroLockType = 1

// lockList is the is_lock list of a hero entry.
func lockList(p store.Partner) []proto.Message {
	if !p.Locked {
		return []proto.Message{}
	}
	return []proto.Message{{"is_lock": 1, "lock_type": heroLockType}}
}
