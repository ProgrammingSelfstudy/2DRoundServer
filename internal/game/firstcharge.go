package game

import (
	"encoding/json"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const firstChargeModule = "firstcharge"

// firstGifts are the first top-up gifts of the client (data_first_charge_data): two tiers (fid 1 from 60 VIP experience, fid 2
// from 1000), each with a gift for three days in a row (ids 1~3 and 4~6).
var firstGifts = func() []struct {
	ID, Fid, Day, NeedExp int64
	Items                 [][2]int64
} {
	var f struct {
		First []struct {
			ID, Fid, Day int64
			NeedExp      int64 `json:"need_exp"`
			Items        [][2]int64
		} `json:"first"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.First) != 6 {
		panic("game: bad charge.json")
	}
	out := make([]struct {
		ID, Fid, Day, NeedExp int64
		Items                 [][2]int64
	}, len(f.First))
	for i, r := range f.First {
		out[i] = struct {
			ID, Fid, Day, NeedExp int64
			Items                 [][2]int64
		}{r.ID, r.Fid, r.Day, r.NeedExp, r.Items}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}()

// firstChargeState is kept per tier (fid 1 and 2 are the indexes 0 and 1).
type firstChargeState struct {
	Start [2]int64 // day number of the day the paid sum reached the tier, 0 = not yet
	Taken [2]int64 // days of the tier taken, in order
	Last  [2]int64 // day number of the last day taken
}

// status is a gift's state: 0 not reached (or a later day), 1 waits, 2 taken.
func (d *firstChargeState) status(id, today int64) int64 {
	g := firstGifts[id-1]
	i := g.Fid - 1
	switch {
	case d.Start[i] == 0:
		return 0
	case g.Day <= d.Taken[i]:
		return 2
	case g.Day == d.Taken[i]+1 && d.Last[i] < today && today >= d.Start[i]+g.Day-1:
		return 1
	}
	return 0
}

// activate starts the tiers that the paid sum reached; it reports whether anything changed.
func (d *firstChargeState) activate(rc *store.RoleCharge, today int64) bool {
	changed := false
	for _, g := range firstGifts {
		if i := g.Fid - 1; d.Start[i] == 0 && accSum(rc) >= g.NeedExp {
			d.Start[i], changed = today, true
		}
	}
	return changed
}

func (d *firstChargeState) list(today int64) []proto.Message {
	out := make([]proto.Message, len(firstGifts))
	for i, g := range firstGifts {
		out[i] = proto.Message{"id": g.ID, "status": d.status(g.ID, today)}
	}
	return out
}

// RegisterFirstCharge registers the first top-up gifts: 21000 the window (what each day's gift is), 21001 take a gift. A tier
// opens when the paid sum (VIP experience) reaches it, the gifts come one a day from that day.
func RegisterFirstCharge(r *gateway.Router, st *store.Store) {
	load := func(s *gateway.Session) (*store.Role, *firstChargeState, int64, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, 0, nil
		}
		rc, err := st.RoleCharge(role.RID)
		if err != nil {
			return nil, nil, 0, err
		}
		d := &firstChargeState{}
		if _, err := st.LoadState(role.RID, firstChargeModule, d); err != nil {
			return nil, nil, 0, err
		}
		today := dayNo(s.Now())
		if d.activate(rc, today) {
			if err := st.SaveState(role.RID, firstChargeModule, d); err != nil {
				return nil, nil, 0, err
			}
		}
		return role, d, today, nil
	}
	window := func(d *firstChargeState, today int64) proto.Message {
		return proto.Message{"end_time": 0, "first_gift": d.list(today)}
	}
	r.Handle(21000, func(s *gateway.Session, _ proto.Message) error {
		role, d, today, err := load(s)
		if role == nil {
			return err
		}
		return s.Send(21000, window(d, today))
	})
	r.Handle(21001, func(s *gateway.Session, req proto.Message) error {
		role, d, today, err := load(s)
		if role == nil {
			return err
		}
		id := req.Int("id")
		if id < 1 || id > int64(len(firstGifts)) {
			return s.Send(21001, proto.Message{"code": 0, "msg": "奖励不存在"})
		}
		switch d.status(id, today) {
		case 0:
			return s.Send(21001, proto.Message{"code": 0, "msg": "还不能领取"})
		case 2:
			return s.Send(21001, proto.Message{"code": 0, "msg": "已经领取过了"})
		}
		g := firstGifts[id-1]
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range g.Items {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Taken[g.Fid-1], d.Last[g.Fid-1] = g.Day, today
		if err := st.SaveState(role.RID, firstChargeModule, d); err != nil {
			return err
		}
		if err := s.Send(21001, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if err := s.Send(21000, window(d, today)); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
