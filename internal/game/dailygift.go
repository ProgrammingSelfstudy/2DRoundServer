package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const dailyGiftModule = "dailygift"

// The 每日礼包 tab of the top-up window (vip/view/daily_gift_panel.lua, Config.ChargeData.data_daily_gift_data and _award): gifts the
// client lists with a price, how often a day they can be bought, the VIP level they need and, by the role's level, what they pay (the
// rows the panel shows itself). 21006 tells the panel how many of each were bought today.
//
// The gift of 1 yuan (id 201) is product 301, which the 一元礼包 (onecentgift.go) already sells once; it is left to that module, so its
// count here stays 0 and a second purchase is refused. Whether the original gave the two one reward or two is not known.
type dailyGift struct {
	ID       int64 `json:"id"`
	ChargeID int64 `json:"charge_id"`
	Name     string
	Val      int64
	Limit    int64
	LimitVIP int64 `json:"limit_vip"`
}

type dailyGiftRow struct {
	Min, Max int64
	Items    [][2]int64
}

var dailyGifts, dailyGiftAward = func() (gifts []dailyGift, award map[int64][]dailyGiftRow) {
	var f struct {
		DailyGift struct {
			Gifts []dailyGift               `json:"gifts"`
			Award map[string][]dailyGiftRow `json:"award"`
		} `json:"daily_gift"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.DailyGift.Gifts) != 4 || len(f.DailyGift.Award) == 0 {
		panic("game: bad charge.json daily_gift")
	}
	award = map[int64][]dailyGiftRow{}
	for k, rows := range f.DailyGift.Award {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			panic("game: bad charge.json daily_gift award")
		}
		award[id] = rows
	}
	return f.DailyGift.Gifts, award
}()

// dailyGiftState counts what was bought on one day (the China-time day number), by gift id.
type dailyGiftState struct {
	Day    int64            `json:"day"`
	Counts map[string]int64 `json:"counts"`
}

func (d *dailyGiftState) roll(day int64) {
	if d.Day != day || d.Counts == nil {
		d.Day, d.Counts = day, map[string]int64{}
	}
}

func (d *dailyGiftState) count(id int64) int64 { return d.Counts[strconv.FormatInt(id, 10)] }

// dailyGiftReward is what a gift pays a role of that level, nil when no row of the table covers it.
func dailyGiftReward(giftID, lev int64) [][2]int64 {
	for _, r := range dailyGiftAward[giftID] {
		if r.Min <= lev && lev <= r.Max {
			return r.Items
		}
	}
	return nil
}

// dailyGiftCheck says whether the role may buy the gift now: its VIP level, a reward for its level, and the daily limit.
func dailyGiftCheck(st *store.Store, role *store.Role, g dailyGift, d *dailyGiftState) error {
	if int64(role.VIPLev) < g.LimitVIP {
		return errNotPayable(fmt.Sprintf("VIP%d可购买", g.LimitVIP))
	}
	if dailyGiftReward(g.ID, int64(role.Lev)) == nil {
		return errNotPayable("no reward for this level")
	}
	if d.count(g.ID) >= g.Limit {
		return errNotPayable("bought up for today")
	}
	return nil
}

func loadDailyGifts(st *store.Store, rid int64, now time.Time) (*dailyGiftState, error) {
	d := &dailyGiftState{}
	if _, err := st.LoadState(rid, dailyGiftModule, d); err != nil {
		return nil, err
	}
	d.roll(dayNo(now))
	return d, nil
}

func init() {
	for _, g := range dailyGifts {
		if g.ChargeID == oneCentCharge { // sold by the 一元礼包
			continue
		}
		g := g
		RegisterChargeGuard(g.ChargeID, func(st *store.Store, role *store.Role) error {
			d, err := loadDailyGifts(st, role.RID, chargeClock())
			if err != nil {
				return err
			}
			return dailyGiftCheck(st, role, g, d)
		})
		RegisterChargeHook(g.ChargeID, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			d, err := loadDailyGifts(st, role.RID, chargeClock())
			if err != nil {
				return nil, nil, err
			}
			if err := dailyGiftCheck(st, role, g, d); err != nil {
				return nil, nil, err
			}
			d.Counts[strconv.FormatInt(g.ID, 10)]++
			if err := st.SaveState(role.RID, dailyGiftModule, d); err != nil {
				return nil, nil, err
			}
			assets, items := map[int64]int64{}, map[int64]int64{}
			for _, it := range dailyGiftReward(g.ID, int64(role.Lev)) {
				splitGains([]sellGain{{it[0], it[1]}}, 1, assets, items)
			}
			return assets, items, nil
		})
	}
	registerAdminModule(AdminModule{Key: dailyGiftModule, Title: "每日礼包购买次数", Window: "21006", Scope: "role", New: adminState[dailyGiftState]})
}

// dailyGiftList is 21006: how many of each gift the role bought today.
func dailyGiftList(st *store.Store, rid int64, now time.Time) ([]proto.Message, error) {
	d, err := loadDailyGifts(st, rid, now)
	if err != nil {
		return nil, err
	}
	list := make([]proto.Message, 0, len(dailyGifts))
	for _, g := range dailyGifts {
		list = append(list, proto.Message{"id": g.ID, "count": d.count(g.ID)})
	}
	return list, nil
}

func registerDailyGift(r *gateway.Router, st *store.Store) {
	r.Handle(21006, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list, err := dailyGiftList(st, role.RID, s.Now())
		if err != nil {
			return err
		}
		return s.Send(21006, proto.Message{"first_gift": list})
	})
}
