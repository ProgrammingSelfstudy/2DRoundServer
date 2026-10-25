package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// talent.json: the talent slots, learnable skills, level-up chain and forget refunds
// (tools/extract_talent.py).
//
//go:embed data/talent.json
var talentJSON []byte

type talentTables struct {
	Pos   []struct{ Pos, Star int64 }
	Learn map[int64]struct {
		Cost    [][2]int64
		Careers []int
	}
	Level map[int64]struct {
		Next int64
		Cost [][2]int64
	}
	Prev map[int64]int64 // a skill level id -> the level id before it
	Back map[int64]struct {
		Cost  [][2]int64
		Award [][2]int64
	}
}

var talent = func() talentTables {
	var t talentTables
	if err := json.Unmarshal(talentJSON, &t); err != nil || len(t.Pos) == 0 || len(t.Learn) == 0 {
		panic("game: bad talent.json")
	}
	return t
}()

func gains(list [][2]int64) []sellGain {
	out := make([]sellGain, 0, len(list))
	for _, e := range list {
		out = append(out, sellGain{e[0], e[1]})
	}
	return out
}

// talentBase is the first level of a talent skill, which is what the learn table and the
// "one skill of a kind" rule are keyed by.
func talentBase(id int64) int64 {
	for i := 0; i < 16; i++ {
		prev, ok := talent.Prev[id]
		if !ok {
			break
		}
		id = prev
	}
	return id
}

func talentMessages(ts []store.Talent) []proto.Message {
	out := []proto.Message{}
	for _, t := range ts {
		out = append(out, proto.Message{"pos": t.Pos, "skill_id": t.SkillID})
	}
	return out
}

// RegisterTalent registers the talent commands of the hero window's talent tab: 11096 learn a
// skill into a slot, 11097 level it up, 11098 forget it. Slots open at hero stars from the
// client's data_partner_skill_pos; learning and levelling cost what the client's tables say,
// forgetting costs gems and refunds a part of the spent materials (data_partner_skill_back).
// Rules the client does not spell out (our reading): a skill kind can sit in one slot only,
// a skill with a career list is for those careers only. 11099 gives the learned skills of the
// asked heroes. Learned talents are shown, but they do not change a hero's stats or fights yet.
func RegisterTalent(r *gateway.Router, st *store.Store) {
	r.Handle(11099, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ps, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		by := map[int64]store.Partner{}
		for _, p := range ps {
			by[p.PartnerID] = p
		}
		out := []proto.Message{}
		for _, a := range req.Arr("partner_ids") {
			id := a.Int("partner_id")
			out = append(out, proto.Message{"partner_id": id, "dower_skill": talentMessages(by[id].Talents)})
		}
		return s.Send(11099, proto.Message{"partner_ids": out})
	})
	// act runs one talent command: plan checks the hero and slot and gives the change.
	act := func(cmd uint16, plan func(p store.Partner, pos, skill int64) (from, to int64, spend, gain []sellGain, msg string)) {
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			id := req.Int("partner_id")
			reply := func(ok int, msg string, ts []store.Talent) error {
				return s.Send(cmd, proto.Message{"partner_id": id, "result": ok, "msg": msg, "dower_skill": talentMessages(ts)})
			}
			p, err := st.PartnerByID(role.RID, id)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return reply(0, "英雄不存在", nil)
				}
				return err
			}
			pos := req.Int("pos")
			from, to, spend, gain, msg := plan(*p, pos, req.Int("skill_id"))
			if msg != "" {
				return reply(0, msg, p.Talents)
			}
			sa, si, ga, gi := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
			splitGains(spend, 1, sa, si)
			splitGains(gain, 1, ga, gi)
			if err := st.ChangeTalent(role.RID, id, pos, from, to, sa, si, ga, gi); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return reply(0, "材料不足", p.Talents)
				}
				return err
			}
			np, err := st.PartnerByID(role.RID, id)
			if err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, st); err != nil {
				return err
			}
			return reply(1, "", np.Talents)
		})
	}
	held := func(p store.Partner, pos int64) int64 {
		for _, t := range p.Talents {
			if t.Pos == pos {
				return t.SkillID
			}
		}
		return 0
	}
	act(11096, func(p store.Partner, pos, skill int64) (int64, int64, []sellGain, []sellGain, string) {
		var open bool
		for _, sl := range talent.Pos {
			if sl.Pos == pos {
				open = int64(p.Star) >= sl.Star
				if !open {
					return 0, 0, nil, nil, "英雄星级不足，天赋栏未开启（需" + strconv.FormatInt(sl.Star, 10) + "星）"
				}
			}
		}
		if !open {
			return 0, 0, nil, nil, "天赋栏不存在"
		}
		if held(p, pos) != 0 {
			return 0, 0, nil, nil, "该天赋栏已领悟技能"
		}
		l, ok := talent.Learn[skill]
		if !ok {
			return 0, 0, nil, nil, "该技能不可领悟"
		}
		if len(l.Careers) > 0 {
			match := false
			for _, c := range l.Careers {
				match = match || c == partnerTable[p.BID].Type
			}
			if !match {
				return 0, 0, nil, nil, "该英雄职业无法领悟此技能"
			}
		}
		for _, t := range p.Talents {
			if talentBase(t.SkillID) == talentBase(skill) {
				return 0, 0, nil, nil, "已领悟同类技能"
			}
		}
		return 0, skill, gains(l.Cost), nil, ""
	})
	act(11097, func(p store.Partner, pos, _ int64) (int64, int64, []sellGain, []sellGain, string) {
		cur := held(p, pos)
		if cur == 0 {
			return 0, 0, nil, nil, "该天赋栏尚未领悟技能"
		}
		l, ok := talent.Level[cur]
		if !ok || l.Next == 0 {
			return 0, 0, nil, nil, "技能已达到最高等级"
		}
		return cur, l.Next, gains(l.Cost), nil, ""
	})
	act(11098, func(p store.Partner, pos, _ int64) (int64, int64, []sellGain, []sellGain, string) {
		cur := held(p, pos)
		if cur == 0 {
			return 0, 0, nil, nil, "该天赋栏尚未领悟技能"
		}
		b := talent.Back[cur]
		return cur, 0, gains(b.Cost), gains(b.Award), ""
	})
}
