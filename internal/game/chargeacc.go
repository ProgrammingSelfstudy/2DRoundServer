package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const chargeAccModule = "chargeacc"

type chargeAccState struct{ Claimed []int64 }

// accSum is the number the accumulated top-up page counts: the VIP experience that came with the payments (the original showed
// 1500 for a role with 1500 VIP experience, capture 2026-10-21b).
func accSum(rc *store.RoleCharge) int64 { return rc.VIPExp }

func chargeAccList(rc *store.RoleCharge, d *chargeAccState) []proto.Message {
	rows := append(chargeAcc[:0:0], chargeAcc...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID }) // the original listed them from the last
	out := []proto.Message{}
	for _, r := range rows {
		status := int64(0)
		switch {
		case has(d.Claimed, r.ID):
			status = 2
		case accSum(rc) >= r.Sum:
			status = 1
		}
		out = append(out, proto.Message{"id": r.ID, "status": status})
	}
	return out
}

// RegisterChargeAcc registers the accumulated top-up rewards: 16712 the page, 16713 take a reward that the paid sum has reached.
func RegisterChargeAcc(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *store.RoleCharge, *chargeAccState, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil, nil
		}
		rc, err := st.RoleCharge(role.RID)
		if err != nil {
			return nil, nil, nil, err
		}
		d := &chargeAccState{}
		if _, err := st.LoadState(role.RID, chargeAccModule, d); err != nil {
			return nil, nil, nil, err
		}
		return role, rc, d, nil
	}
	r.Handle(16712, func(s *gateway.Session, _ proto.Message) error {
		role, rc, d, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(16712, proto.Message{"charge_sum": accSum(rc), "list": chargeAccList(rc, d)})
	})
	r.Handle(16713, func(s *gateway.Session, req proto.Message) error {
		role, rc, d, err := load(s)
		if role == nil {
			return err
		}
		id := req.Int("id")
		refuse := func(msg string) error {
			return s.Send(16713, proto.Message{"code": 0, "msg": msg, "list": chargeAccList(rc, d)})
		}
		idx := -1
		for i, a := range chargeAcc {
			if a.ID == id {
				idx = i
			}
		}
		switch {
		case idx < 0:
			return refuse("奖励不存在")
		case has(d.Claimed, id):
			return refuse("已经领取过了")
		case accSum(rc) < chargeAcc[idx].Sum:
			return refuse("累计充值未达到")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range chargeAcc[idx].Items {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Claimed = append(d.Claimed, id)
		if err := st.SaveState(role.RID, chargeAccModule, d); err != nil {
			return err
		}
		if err := s.Send(16713, proto.Message{"code": 1, "msg": "领取成功", "list": chargeAccList(rc, d)}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
