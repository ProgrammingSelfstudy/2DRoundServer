package game

import (
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func stoneRow(lev int) (stoneLevel, bool) {
	if lev < 1 || lev > len(resonate.Levels) {
		return stoneLevel{}, false
	}
	return resonate.Levels[lev-1], true
}

// coreStars is the total stars of the five offering heroes now.
func coreStars(core []store.Partner) int {
	n := 0
	for _, p := range core {
		n += p.Star
	}
	return n
}

// stoneState reads the stone tablet and refreshes the historical highest total of offering stars.
func stoneState(st *store.Store, rid int64) (lev, maxStars int, core []store.Partner, err error) {
	ps, err := st.PartnersOf(rid)
	if err != nil {
		return 0, 0, nil, err
	}
	_, core = crystalOf(ps)
	if stars := coreStars(core); stars > 0 {
		if err := st.RaiseMaxStars(rid, stars); err != nil {
			return 0, 0, nil, err
		}
	}
	lev, maxStars, err = st.StoneOf(rid)
	return
}

// RegisterResonanceStone registers the stone tablet and the alchemy of the resonance window: 26400
// the tablet (its level, the five offering heroes, the attributes of its level times 1000 as the
// original sends them, and the highest total of stars the offering heroes ever had), 26402 raise its
// level (needs 50 total stars, the client's `amp_all_start_limit`, and the table's cost), and
// the alchemy that turns hero exp into the material the tablet needs: 26410 state, 26411 put exp in
// (30000 exp a unit, up to the level's `limit_num` units, `need_time` seconds each), 26412 take back
// the exp of the unfinished units, 26413 collect the finished ones, 26414 (the red dot, no answer). The
// numbers are the client's; the tablet's attributes are shown but not added to the heroes' stats, and
// 26401 (choosing offering heroes by hand; the client's rule text says the five highest-level heroes
// take the places by themselves) is not implemented.
func RegisterResonanceStone(r *gateway.Router, st *store.Store) {
	infoMessage := func(rid int64) (proto.Message, error) {
		lev, maxStars, core, err := stoneState(st, rid)
		if err != nil {
			return nil, err
		}
		list := []proto.Message{}
		for i, p := range core {
			list = append(list, proto.Message{"id": p.PartnerID, "pos": i + 1})
		}
		attr := []proto.Message{}
		if row, ok := stoneRow(lev); ok {
			for _, a := range row.Attr {
				name, _ := a[0].(string)
				val, _ := a[1].(float64)
				attr = append(attr, proto.Message{"attr_id": attrIDs[name], "attr_val": int64(val) * 1000})
			}
		}
		return proto.Message{"lev": lev, "list": list, "attr": attr, "max_partner_lev": maxStars}, nil
	}
	sendInfo := func(s *gateway.Session) error {
		m, err := infoMessage(s.Role().RID)
		if err != nil {
			return err
		}
		return s.Send(26400, m)
	}
	r.Handle(26400, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return sendInfo(s)
	})
	result := func(s *gateway.Session, cmd uint16, ok int, msg string) error {
		return s.Send(cmd, proto.Message{"result": ok, "msg": msg})
	}
	r.Handle(26402, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		lev, maxStars, _, err := stoneState(st, role.RID)
		if err != nil {
			return err
		}
		row, ok := stoneRow(lev)
		if _, next := stoneRow(lev + 1); !ok || !next {
			return result(s, 26402, 0, "已经达到满级")
		}
		if maxStars < resonate.AmpStars {
			return result(s, 26402, 0, "供奉英雄历史最高总星数不足"+strconv.Itoa(resonate.AmpStars)+"星")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(gains(row.Expend), 1, assets, items)
		if err := st.LevelUpStone(role.RID, lev, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return result(s, 26402, 0, "材料不足")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := sendInfo(s); err != nil {
			return err
		}
		return result(s, 26402, 1, "")
	})

	// alchemy: done units grow with time; ready = done - collected
	progress := func(e store.Extract, now int64) (done int64) {
		if e.PerSec <= 0 {
			return e.Total
		}
		done = (now - e.Start) / e.PerSec
		if done > e.Total {
			done = e.Total
		}
		if done < 0 {
			done = 0
		}
		return
	}
	stateMessage := func(rid, now int64) (proto.Message, error) {
		e, ok, err := st.ExtractOf(rid)
		if err != nil {
			return nil, err
		}
		if !ok {
			return proto.Message{"all_num": 0, "do_num": 0, "get_num": 0, "do_end_time": 0, "is_point": 1}, nil
		}
		done := progress(e, now)
		var end int64
		if done < e.Total {
			end = e.Start + (done+1)*e.PerSec
		}
		point := 0
		if done > e.Collected {
			point = 1
		}
		return proto.Message{"all_num": e.Total, "do_num": done - e.Collected, "get_num": e.Collected, "do_end_time": end, "is_point": point}, nil
	}
	sendState := func(s *gateway.Session) error {
		m, err := stateMessage(s.Role().RID, s.Now().Unix())
		if err != nil {
			return err
		}
		return s.Send(26410, m)
	}
	r.Handle(26410, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return sendState(s)
	})
	r.Handle(26414, func(*gateway.Session, proto.Message) error { return nil })
	unit := resonate.Refine[0]
	r.Handle(26411, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		n := req.Int("all_num")
		lev, maxStars, _, err := stoneState(st, role.RID)
		if err != nil {
			return err
		}
		row, ok := stoneRow(lev)
		switch {
		case !ok:
			return result(s, 26411, 0, "提炼不可用")
		case maxStars < resonate.AmpStars:
			return result(s, 26411, 0, "供奉英雄历史最高总星数不足"+strconv.Itoa(resonate.AmpStars)+"星")
		case n < 1 || n > row.Limit:
			return result(s, 26411, 0, "放入的数量不正确")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{unit[0], unit[1] * n}}, 1, assets, items)
		if err := st.StartExtract(role.RID, n, s.Now().Unix(), row.Seconds, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return result(s, 26411, 0, "正在提炼中或经验不足")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendState(s); err != nil {
			return err
		}
		return result(s, 26411, 1, "")
	})
	r.Handle(26412, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		e, ok, err := st.ExtractOf(role.RID)
		if err != nil {
			return err
		}
		if !ok {
			return result(s, 26412, 0, "没有正在提炼的经验")
		}
		done := progress(e, s.Now().Unix())
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{unit[0], unit[1] * (e.Total - done)}}, 1, assets, items)
		if err := st.RetrieveExtract(role.RID, e.Total, done, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return result(s, 26412, 0, "取回失败，请重试")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendState(s); err != nil {
			return err
		}
		return result(s, 26412, 1, "")
	})
	r.Handle(26413, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		e, ok, err := st.ExtractOf(role.RID)
		if err != nil {
			return err
		}
		ready := int64(0)
		if ok {
			ready = progress(e, s.Now().Unix()) - e.Collected
		}
		if !ok || ready <= 0 {
			return result(s, 26413, 0, "没有可收获的材料")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{resonate.Alchemy, ready}}, 1, assets, items)
		if err := st.CollectExtract(role.RID, e.Collected, ready, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return result(s, 26413, 0, "收获失败，请重试")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := sendState(s); err != nil {
			return err
		}
		return result(s, 26413, 1, "")
	})
}
