package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// empower.json: the resonance empowerment (赋能) rules of the client (tools/extract_empower.py).
//
//go:embed data/empower.json
var empowerJSON []byte

var empowerCfg = func() struct {
	Cost map[int64]struct {
		Item [][]int64 `json:"item"`
		Coin [][]int64 `json:"coin"`
	} `json:"cost"`
	DayLimit  int64 `json:"day_limit"`
	Span      int64 `json:"span"` // minutes
	Condition int64 `json:"condition"`
} {
	var t struct {
		Cost map[int64]struct {
			Item [][]int64 `json:"item"`
			Coin [][]int64 `json:"coin"`
		} `json:"cost"`
		DayLimit  int64 `json:"day_limit"`
		Span      int64 `json:"span"`
		Condition int64 `json:"condition"`
	}
	if err := json.Unmarshal(empowerJSON, &t); err != nil || len(t.Cost) != 20 {
		panic("game: bad empower.json: " + fmt.Sprint(err))
	}
	return t
}()

// empowerStar is the star an empowered hero gets: the average star of the offering heroes' best total ("供奉英雄历史最高平均星级").
func empowerStar(maxStars int) int { return maxStars / 5 }

// empowerBreak is the highest break step the hero has in the client's table.
func empowerBreak(info partnerInfo) int {
	n := 0
	for {
		if _, ok := growth.Brach[brachKey(info, n+1)]; !ok {
			return n
		}
		n++
	}
}

// expireEmpowered removes the empowered heroes whose time is up: what they wear goes back to the bag, then the hero is gone.
func expireEmpowered(st *store.Store, rid int64, now time.Time) error {
	d, err := st.EmpowerOf(rid)
	if err != nil || len(d.Heroes) == 0 {
		return err
	}
	var gone []int64
	for id, end := range d.Heroes {
		if end <= now.Unix() {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i] < gone[j] })
	isGone := map[int64]bool{}
	for _, id := range gone {
		isGone[id] = true
	}
	back := map[int64]int64{}
	arts, err := st.ArtifactsOf(rid)
	if err != nil {
		return err
	}
	for i, a := range arts.Pieces {
		if isGone[a.Partner] && a.Pos > 0 {
			arts.Pieces[i].Partner, arts.Pieces[i].Pos = 0, 0
			back[a.Base]++
		}
	}
	holy, err := st.HolyOf(rid)
	if err != nil {
		return err
	}
	for i, x := range holy.Pieces {
		if isGone[x.Partner] && x.Slot > 0 {
			holy.Pieces[i].Partner, holy.Pieces[i].Slot = 0, 0
			back[x.Base]++
		}
	}
	if err := st.Exchange(rid, nil, nil, nil, back); err != nil {
		return err
	}
	if err := st.SaveArtifacts(rid, arts); err != nil {
		return err
	}
	if err := st.SaveHoly(rid, holy); err != nil {
		return err
	}
	hx, err := st.HorcruxOf(rid)
	if err != nil {
		return err
	}
	for _, id := range gone {
		delete(hx.Heroes, id)
		delete(d.Heroes, id)
		if err := st.SetPartnerLocked(rid, id, false); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	if err := st.SaveHorcrux(rid, hx); err != nil {
		return err
	}
	if err := st.ConsumePartners(rid, gone, nil, nil, nil, nil); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return st.SaveEmpower(rid, d)
}

// RegisterEmpower registers the resonance empowerment (26420~26423): a hero of the role is copied into a high-star hero of the same
// kind, for a day. 26420 says the star it would get and how many times are left today, 26421 does it (the talent skills of the
// request go to the new hero), 26422 lists the talent skills the role's heroes know, 26423 shows the power of the new hero.
func RegisterEmpower(r *gateway.Router, st *store.Store) {
	state := func(s *gateway.Session) (*store.EmpowerDoc, int, int, error) {
		role := s.Role()
		if err := expireEmpowered(st, role.RID, s.Now()); err != nil {
			return nil, 0, 0, err
		}
		d, err := st.EmpowerOf(role.RID)
		if err != nil {
			return nil, 0, 0, err
		}
		if day := dayNo(s.Now()); d.Day != day {
			d.Day, d.Used = day, 0
		}
		_, maxStars, err := st.StoneOf(role.RID)
		if err != nil {
			return nil, 0, 0, err
		}
		return d, maxStars, empowerStar(maxStars), nil
	}
	left := func(d *store.EmpowerDoc) int64 { return max(0, empowerCfg.DayLimit-d.Used) }
	r.Handle(26420, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		d, _, star, err := state(s)
		if err != nil {
			return err
		}
		return s.Send(26420, proto.Message{"star": star, "num": left(d)})
	})
	// plan checks a request and builds the new hero (nothing is stored)
	plan := func(role *store.Role, d *store.EmpowerDoc, maxStars, star int, req proto.Message) (src *store.Partner, talents []store.Talent, msg string, err error) {
		if int64(maxStars) < empowerCfg.Condition {
			return nil, nil, "供奉英雄历史最高总星数不足" + strconv.FormatInt(empowerCfg.Condition, 10) + "星", nil
		}
		src, err = st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, "英雄不存在", nil
		} else if err != nil {
			return nil, nil, "", err
		}
		info, ok := partnerTable[src.BID]
		if !ok {
			return nil, nil, "该英雄不能赋能", nil
		}
		if _, empowered := d.Heroes[src.PartnerID]; empowered {
			return nil, nil, "赋能英雄不能作为赋能对象", nil
		}
		if src.Star >= star {
			return nil, nil, "赋能对象的星级需要低于目标星级", nil
		}
		if _, ok := info.Stars[strconv.Itoa(star)]; !ok {
			return nil, nil, "该英雄没有目标星级", nil
		}
		seen := map[int64]bool{}
		for _, sk := range req.Arr("skills") {
			pos, id := sk.Int("pos"), sk.Int("skill_id")
			open := false
			for _, sl := range talent.Pos {
				if sl.Pos == pos && int64(star) >= sl.Star {
					open = true
				}
			}
			l, known := talent.Learn[id]
			switch {
			case !open:
				return nil, nil, "天赋栏未开启", nil
			case !known:
				return nil, nil, "该技能不可选择", nil
			case seen[pos]:
				return nil, nil, "天赋栏重复", nil
			}
			if len(l.Careers) > 0 {
				match := false
				for _, c := range l.Careers {
					match = match || c == info.Type
				}
				if !match {
					return nil, nil, "该英雄职业无法选择此技能", nil
				}
			}
			for _, t := range talents {
				if talentBase(t.SkillID) == talentBase(id) {
					return nil, nil, "已选同类技能", nil
				}
			}
			seen[pos] = true
			talents = append(talents, store.Talent{Pos: pos, SkillID: id})
		}
		return src, talents, "", nil
	}
	newHero := func(src *store.Partner, star int, talents []store.Talent) store.Partner {
		info := partnerTable[src.BID]
		return store.Partner{BID: src.BID, Star: star, Lev: info.Stars[strconv.Itoa(star)].LevMax, BreakLev: empowerBreak(info), Talents: talents}
	}
	r.Handle(26421, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		no := func(msg string) error { return s.Send(26421, proto.Message{"result": 0, "msg": msg}) }
		d, maxStars, star, err := state(s)
		if err != nil {
			return err
		}
		if left(d) == 0 {
			return no("今日赋能次数已用完")
		}
		src, talents, msg, err := plan(role, d, maxStars, star, req)
		if err != nil {
			return err
		}
		if msg != "" {
			return no(msg)
		}
		cost, ok := empowerCfg.Cost[int64(star)]
		if !ok {
			return no("目标星级没有赋能配置")
		}
		spendA, spendI := splitCost(append(append([][]int64(nil), cost.Item...), cost.Coin...), 1)
		h := newHero(src, star, talents)
		np, err := st.GrantEmpowered(role.RID, h.BID, h.Star, h.Lev, h.BreakLev, talents, spendA, spendI)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("材料不足")
			}
			return err
		}
		d.Used++
		d.Heroes[np.PartnerID] = s.Now().Add(time.Duration(empowerCfg.Span) * time.Minute).Unix()
		if err := st.SaveEmpower(role.RID, d); err != nil {
			return err
		}
		fresh, err := st.PartnerByID(role.RID, np.PartnerID)
		if err != nil {
			return err
		}
		if err := s.Send(26421, proto.Message{"result": 1, "msg": "赋能成功"}); err != nil {
			return err
		}
		if err := s.Send(11001, proto.Message{"partners": []proto.Message{partnerMessage(*fresh)}}); err != nil {
			return err
		}
		if err := s.Send(26424, proto.Message{"partner_id": np.PartnerID}); err != nil {
			return err
		}
		if err := s.Send(26420, proto.Message{"star": star, "num": left(d)}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
	r.Handle(26422, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ps, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		seen := map[int64]bool{}
		var ids []int64
		for _, p := range ps {
			for _, t := range p.Talents {
				if !seen[t.SkillID] {
					seen[t.SkillID] = true
					ids = append(ids, t.SkillID)
				}
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"skill_id": id})
		}
		return s.Send(26422, proto.Message{"skills": list})
	})
	r.Handle(26423, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, maxStars, star, err := state(s)
		if err != nil {
			return err
		}
		src, talents, msg, err := plan(role, d, maxStars, star, req)
		if err != nil {
			return err
		}
		power := int64(0)
		if msg == "" {
			h := newHero(src, star, talents)
			hp, atk, def, speed := partnerStats(h)
			power = heroPower(atk, hp, def, speed)
		}
		return s.Send(26423, proto.Message{"partner_id": req.Int("partner_id"), "power": power})
	})
}
