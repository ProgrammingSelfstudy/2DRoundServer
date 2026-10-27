package game

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const cycleGiftModule = "cyclegift"

// chargeClock is the time a payment confirmation counts the cycle of a gift by (the tests move it).
var chargeClock = time.Now

type cycleInfo struct {
	Type     int64
	ChargeID int64 `json:"charge_id"`
	Limit    int64
	Sort     int64
}

type cycleReward struct {
	ChargeID int64 `json:"charge_id"`
	Min, Max int64 // the days since the role was created
	RegMin   int64 `json:"reg_min"` // and the time it was created
	RegMax   int64 `json:"reg_max"`
	Sort     int64
	Items    [][2]int64
}

// cycleGifts are the weekly (type 1) and monthly (type 2) gifts of the welfare window (Config.MiscData.data_cycle_gift_*).
var cycleGifts = func() (info []cycleInfo, reward map[int64][]cycleReward) {
	var f struct {
		Cycle struct {
			Info   []cycleInfo   `json:"info"`
			Reward []cycleReward `json:"reward"`
		} `json:"cycle"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.Cycle.Info) != 12 || len(f.Cycle.Reward) == 0 {
		panic("game: bad charge.json cycle")
	}
	reward = map[int64][]cycleReward{}
	for _, r := range f.Cycle.Reward {
		reward[r.ChargeID] = append(reward[r.ChargeID], r)
	}
	for _, rs := range reward {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Sort < rs[j].Sort })
	}
	return f.Cycle.Info, reward
}

var cycleInfos, cycleRewards = cycleGifts()

func cycleLimit(chargeID int64) (cycleInfo, bool) {
	for _, c := range cycleInfos {
		if c.ChargeID == chargeID {
			return c, true
		}
	}
	return cycleInfo{}, false
}

// cycleEnd is when the cycle of a gift type that holds now is over: the next Monday for the weekly gifts, the first of the next
// month for the monthly ones (China time).
func cycleEnd(typ int64, now time.Time) int64 {
	if typ == 1 {
		return leWeekEnd(now)
	}
	n := now.In(dayZone)
	return time.Date(n.Year(), n.Month()+1, 1, 0, 0, 0, 0, dayZone).Unix()
}

type cycleGiftState struct {
	End   [3]int64        // the end of the cycle the counts belong to, by type
	Count map[int64]int64 // gifts bought in the cycle, by charge id
}

// roll empties the counts of the types whose cycle is over.
func (d *cycleGiftState) roll(now time.Time) {
	if d.Count == nil {
		d.Count = map[int64]int64{}
	}
	for typ := int64(1); typ <= 2; typ++ {
		if end := cycleEnd(typ, now); d.End[typ] != end {
			for _, c := range cycleInfos {
				if c.Type == typ {
					delete(d.Count, c.ChargeID)
				}
			}
			d.End[typ] = end
		}
	}
}

// cycleRewardFor is what a gift pays a role of that age: the first row whose days and creation time hold.
func cycleRewardFor(chargeID int64, createdAt, now int64) ([][2]int64, bool) {
	day := (now-createdAt)/86400 + 1
	for _, r := range cycleRewards[chargeID] {
		if r.Min <= day && day <= r.Max && r.RegMin <= createdAt && createdAt <= r.RegMax {
			return r.Items, true
		}
	}
	for _, r := range cycleRewards[chargeID] { // the client picks by the days alone
		if r.Min <= day && day <= r.Max {
			return r.Items, true
		}
	}
	return nil, false
}

// ErrCycleLimit is the answer when a gift was bought as often as a cycle allows; it is a kind of ErrNotPayable.
var ErrCycleLimit = errNotPayable("bought as often as this cycle allows")

func init() {
	for _, c := range cycleInfos {
		c := c
		RegisterChargeGuard(c.ChargeID, func(st *store.Store, role *store.Role) error {
			d := &cycleGiftState{}
			if _, err := st.LoadState(role.RID, cycleGiftModule, d); err != nil {
				return err
			}
			d.roll(chargeClock())
			if d.Count[c.ChargeID] >= c.Limit {
				return ErrCycleLimit
			}
			return nil
		})
		RegisterChargeHook(c.ChargeID, func(st *store.Store, role *store.Role, p ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			d := &cycleGiftState{}
			if _, err := st.LoadState(role.RID, cycleGiftModule, d); err != nil {
				return nil, nil, err
			}
			now := chargeClock()
			d.roll(now)
			if d.Count[c.ChargeID] >= c.Limit {
				return nil, nil, ErrCycleLimit
			}
			items, ok := cycleRewardFor(c.ChargeID, role.CreatedAt, now.Unix())
			if !ok {
				return nil, nil, fmt.Errorf("gift %d pays nothing to a role of this age", c.ChargeID)
			}
			d.Count[c.ChargeID]++
			if err := st.SaveState(role.RID, cycleGiftModule, d); err != nil {
				return nil, nil, err
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			for _, it := range items {
				splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
			}
			return ga, gi, nil
		})
	}
}

// RegisterCycleGifts registers the weekly and monthly gift panels of the welfare window: 21007 lists what each gift of the type
// pays and how many were bought in the cycle. Buying is the top-up (an order, delivered when the operator confirms it).
func RegisterCycleGifts(r *gateway.Router, st *store.Store) {
	r.Handle(21007, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ, now := req.Int("type"), s.Now()
		d := &cycleGiftState{}
		if _, err := st.LoadState(role.RID, cycleGiftModule, d); err != nil {
			return err
		}
		d.roll(now)
		list := []proto.Message{}
		for _, c := range cycleInfos {
			if c.Type == typ {
				list = append(list, proto.Message{"id": c.ChargeID, "count": d.Count[c.ChargeID]})
			}
		}
		return s.Send(21007, proto.Message{"type": typ, "ref_time": cycleEnd(typ, now),
			"reg_day": (now.Unix()-role.CreatedAt)/86400 + 1, "first_gift": list})
	})
}
