package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	activityBoxModule = "activitybox"
	activityAsset     = 9 // the activity points the daily quests pay
)

// activityBoxes are the four boxes of the daily quest window (Config.ActivityData.data_get): the activity points that open each
// and what it holds. They are written here from the table (4 rows).
var activityBoxes = []struct {
	Activity int64
	Rewards  [][2]int64
}{
	{25, [][2]int64{{3, 20}, {1, 10000}}},
	{50, [][2]int64{{3, 30}, {10401, 2}}},
	{75, [][2]int64{{3, 50}, {37001, 1}}},
	{100, [][2]int64{{3, 100}, {1, 50000}}},
}

type activityBoxState struct {
	Day     int64
	Claimed []int64 // the boxes taken today, by their points
}

// activityToday brings the box state to today; the activity points of the day before are used up when the day turns, as the quest
// list is (the points count a day).
func activityToday(s *gateway.Session, st *store.Store, rid int64) (*activityBoxState, error) {
	d := &activityBoxState{}
	if _, err := st.LoadState(rid, activityBoxModule, d); err != nil {
		return nil, err
	}
	today := dayKey(s.Now())
	if d.Day == today {
		return d, nil
	}
	if d.Day != 0 { // a first visit has nothing from yesterday to clear
		ledger, err := st.RoleAssets(rid)
		if err != nil {
			return nil, err
		}
		if left := ledger[activityAsset]; left > 0 {
			if err := st.Exchange(rid, map[int64]int64{activityAsset: left}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
				return nil, err
			}
		}
	}
	d.Day, d.Claimed = today, nil
	return d, st.SaveState(rid, activityBoxModule, d)
}

// RegisterActivityBox registers the activity boxes of the daily quest window: 20300 the boxes taken today, 20301 take one whose
// points the role has.
func RegisterActivityBox(r *gateway.Router, st *store.Store) {
	list := func(d *activityBoxState) []proto.Message {
		out := []proto.Message{}
		for _, a := range d.Claimed {
			out = append(out, proto.Message{"activity": a})
		}
		return out
	}
	r.Handle(20300, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := activityToday(s, st, role.RID)
		if err != nil {
			return err
		}
		return s.Send(20300, proto.Message{"activity_box": list(d)})
	})
	r.Handle(20301, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := activityToday(s, st, role.RID)
		if err != nil {
			return err
		}
		pts := req.Int("activity")
		refuse := func(msg string) error { return s.Send(20301, proto.Message{"code": 0, "msg": msg, "activity": pts}) }
		idx := -1
		for i, b := range activityBoxes {
			if b.Activity == pts {
				idx = i
			}
		}
		ledger, err := st.RoleAssets(role.RID)
		if err != nil {
			return err
		}
		switch {
		case idx < 0:
			return refuse("宝箱不存在")
		case has(d.Claimed, pts):
			return refuse("已经领取过了")
		case ledger[activityAsset] < pts:
			return refuse("活跃度不足")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range activityBoxes[idx].Rewards {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Claimed = append(d.Claimed, pts)
		if err := st.SaveState(role.RID, activityBoxModule, d); err != nil {
			return err
		}
		if err := s.Send(20301, proto.Message{"code": 1, "msg": "领取成功", "activity": pts}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
