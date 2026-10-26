package game

import (
	_ "embed"
	"encoding/json"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// role_exp.json is the level table of the restored client
// (tools/extract_role_exp.py); the server must agree with it because the
// client draws the exp bar from its own copy.
//
//go:embed data/role_exp.json
var roleExpJSON []byte

type expRow struct {
	Lev      int   `json:"lev"`
	ExpTotal int64 `json:"exp_total"` // running sum of exp_max up to and including Lev (the client's own number at the anomalies)
	ExpMax   int64 `json:"exp_max"`
	// Reward is what reaching this level pays: (asset or item id, amount) pairs.
	Reward [][2]int64 `json:"reward"`
}

var expTable = func() []expRow {
	var f struct {
		Levels []expRow `json:"levels"`
		// Anomalies are the levels whose exp_total in the client's table is not the
		// running sum of exp_max. The original server sends the client's own number
		// (capture original-2026-10-22: a level 124 role got exp_total_nextlev
		// 238279300, the running sum would be 204591100), so that number is what the
		// 10301 field shows. Level 24 is the other such level in the table and is
		// treated the same, but was not captured. Levelling itself goes by exp_max alone.
		Anomalies []struct {
			Lev            int   `json:"lev"`
			ClientExpTotal int64 `json:"client_exp_total"`
		} `json:"anomalies_in_client_data"`
	}
	if err := json.Unmarshal(roleExpJSON, &f); err != nil || len(f.Levels) == 0 {
		panic("game: bad role_exp.json")
	}
	for _, a := range f.Anomalies {
		if a.Lev >= 1 && a.Lev <= len(f.Levels) {
			f.Levels[a.Lev-1].ExpTotal = a.ClientExpTotal
		}
	}
	return f.Levels
}()

// expFor returns exp_max (exp needed to leave lev) and the running total at the
// next level; both 0 for a level outside the table.
func expFor(lev int) (expMax, totalNext int64) {
	if lev < 1 || lev > len(expTable) {
		return 0, 0
	}
	r := expTable[lev-1]
	return r.ExpMax, r.ExpTotal
}

// roleBase builds 10301 (role base data) from the stored role. Fields without a
// source (vip, guild, power, buffs, sprites, ...) are the empty state of a new
// player; city_id and the other unknowns are 0 until observed on a device.
func roleBase(r *store.Role) proto.Message {
	expMax, totalNext := expFor(r.Lev)
	return proto.Message{
		"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev,
		"sex": r.Sex, "career": r.Career, "face_id": r.FaceID,
		"exp_max": expMax, "exp_total_nextlev": totalNext,
		"reg_time": r.CreatedAt,
		// 1 while the free first rename (10343) is still available
		"is_first_rename": 1 - r.Renamed,
		"vip_lev":         r.VIPLev, "vip_exp": vipExpFor(r.VIPLev),
		"buffs": []proto.Message{}, "sprites": []proto.Message{},
	}
}

// roleDataRequest answers 10300, which the client sends right after a
// successful 10192/10193. 10301 is what the client's login waits for: on it
// the client creates the role object, closes the login view and starts the
// main scene (role_controller.lua rgp_hander10301); 10304 then creates the city.
func (l *Login) roleDataRequest(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		l.Log.Printf("10300 before any role login: ignored")
		return nil
	}
	fresh, err := l.Store.RoleByID(role.RID)
	if err != nil {
		return err
	}
	if err := sendRoleBase(l.Store, s, fresh); err != nil {
		return err
	}
	ledger, err := l.Store.RoleAssets(fresh.RID)
	if err != nil {
		return err
	}
	if err := s.Send(10302, roleAssets(fresh, ledger)); err != nil {
		return err
	}
	// the login counts for the daily login quest; the client is not told yet, it asks with 10400
	if _, err := questRecord(l.Store, fresh.RID, dayKey(s.Now()), evtLoaded, 1); err != nil {
		return err
	}
	if err := sevenGoalEvent(s, l.Store, fresh, evtLoaded, 1, false); err != nil {
		return err
	}
	if err := warOrderEvent(s, l.Store, fresh, evtLoaded, 1, false); err != nil {
		return err
	}
	if err := newWarOrderEvent(s, l.Store, fresh, evtLoaded, 1, false); err != nil {
		return err
	}
	if err := pushDrama(s, l.Store, fresh.RID); err != nil {
		return err
	}
	// 10304 is a pure server push (the client never requests it): on receipt the
	// client creates the main city scene (main_scene_controller.lua handle10304).
	// play_video=0 skips the intro video.
	if err := s.Send(10304, proto.Message{"play_video": 0}); err != nil {
		return err
	}
	if err := l.askForName(s, fresh); err != nil {
		return err
	}
	if l.Chat != nil {
		return l.Chat.Deliver(s)
	}
	return nil
}

// askForName pushes 10342 to a brand-new role when the flow is switched on: the client
// opens its naming window (role_controller.lua rgp_hander10342), and the first rename
// (10343) grants the starter hero. "Brand new" is: never renamed and no starter hero yet,
// so roles that were created before the switch, and every role that has played, are left
// alone. The original pushes it from its story (capture original-2026-10-22, in the
// middle of the 10010 story); here it comes with the main city.
func (l *Login) askForName(s *gateway.Session, role *store.Role) error {
	if !l.NewRoleFlow || role.Renamed != 0 {
		return nil
	}
	granted, err := l.Store.StarterGranted(role.RID)
	if err != nil || granted {
		return err
	}
	return s.Send(10342, proto.Message{})
}

func init() {
	label, ok := AssetLabelID("exp")
	if !ok {
		panic("game: no exp asset label")
	}
	store.SetLevelRule(&store.LevelRule{
		ExpLabel: label,
		MaxLev:   len(expTable),
		ExpMax:   func(lev int) int64 { m, _ := expFor(lev); return m },
		Reward: func(lev int) (map[int64]int64, map[int64]int64) {
			assets, items := map[int64]int64{}, map[int64]int64{}
			if lev >= 1 && lev <= len(expTable) {
				for _, g := range expTable[lev-1].Reward {
					splitGains([]sellGain{{g[0], g[1]}}, 1, assets, items)
				}
			}
			return assets, items
		},
	})
}

// MaxVIPLevel is the highest VIP level: the client's VIP table
// (Config.VipData.data_get_reward) has 31 rows, levels 0 to 30.
const MaxVIPLevel = 30

// vip.json holds, per VIP level, the VIP experience that level starts at and the
// gift of the VIP page: its price and its content, lists of [id, amount]
// (tools/extract_vip.py).
//
//go:embed data/vip.json
var vipJSON []byte

var vipTable = func() (f struct {
	Exp   []int64      `json:"exp"`
	Price [][][2]int64 `json:"price"`
	Items [][][2]int64 `json:"items"`
}) {
	if err := json.Unmarshal(vipJSON, &f); err != nil || len(f.Exp) != MaxVIPLevel+1 ||
		len(f.Price) != MaxVIPLevel+1 || len(f.Items) != MaxVIPLevel+1 {
		panic("game: bad vip.json")
	}
	return f
}()

var vipExp = vipTable.Exp

// vipExpFor is the VIP experience shown for a VIP level: where that level starts. The
// client draws the bar as vip_exp over the next level's figure; there is no recharge,
// so the experience only follows the level the admin page sets.
func vipExpFor(lev int) int64 {
	if lev < 0 || lev >= len(vipExp) {
		return 0
	}
	return vipExp[lev]
}

// MaxRoleLevel is the highest level of the client's level table.
func MaxRoleLevel() int { return len(expTable) }

// sendRoleBase sends 10301 with the role's stored settings (signature, backdrop, avatar frame, ...).
func sendRoleBase(st *store.Store, s *gateway.Session, r *store.Role) error {
	m, err := withSettings(st, r.RID, roleBase(r))
	if err != nil {
		return err
	}
	return s.Send(10301, m)
}
