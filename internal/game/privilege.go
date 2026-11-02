package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// privilege.json: the privileges of the top-up window (tools/extract_privilege.py).
//
//go:embed data/privilege.json
var privilegeJSON []byte

const privilegeModule = "privilege"

// A privilege is bought with money (a product of the charge table) or with diamonds (24501), pays the reward row of the role's level
// at once and then shows as owned: for limit_day days (the client's text says weekly / monthly) or for good (limit_type 0). 24502 is
// the list the privilege page reads (by the client's id 101~106); 24500 is the list the rest of the client reads at login by the effect
// type 1~5 (the five entries the original sends, capture original-2026-10-27), which is the id minus 101 for 102~106: 1 the quick
// battle, 2 and 3 the voyage, 4 the territory challenge, 5 the ghost training.
//
// What a privilege does: the quick battle one (102) changes the daily quick battles (drama_extras.go), the two of the voyage (103, 104)
// the refresh of the orders (voyage.go) and the territory challenge one (105) the challenges and purchases of the element temple
// (element.go). Not done: the ghost training one (106, no system of that name on the server); owning it pays only the reward.
type privilegeDef struct {
	ID        int64 `json:"id"`
	Name      string
	ChargeID  int64 `json:"charge_id"`
	PayType   int64 `json:"pay_type"` // 1 money, 2 diamonds
	Loss      int64
	LimitType int64 `json:"limit_type"` // 0 for good, 1 for limit_day days
	LimitDay  int64 `json:"limit_day"`
	LimitLev  int64 `json:"limit_lev"`
}

type privilegeRow struct {
	Min, Max int64
	Items    [][2]int64
}

var privileges, privilegeAward = func() (defs []privilegeDef, award map[int64][]privilegeRow) {
	var f struct {
		Privileges []privilegeDef            `json:"privileges"`
		Award      map[string][]privilegeRow `json:"award"`
	}
	if err := json.Unmarshal(privilegeJSON, &f); err != nil || len(f.Privileges) != 6 || len(f.Award) == 0 {
		panic("game: bad privilege.json")
	}
	award = map[int64][]privilegeRow{}
	for k, rows := range f.Award {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			panic("game: bad privilege.json award")
		}
		award[id] = rows
	}
	return f.Privileges, award
}()

// privilegeForever marks a privilege that never ends in the state.
const privilegeForever = -1

// privilegeState holds, by privilege id, the time each one ends (privilegeForever, or 0 for one never bought).
type privilegeState struct {
	Until map[string]int64 `json:"until"`
}

func (p *privilegeState) until(id int64) int64 { return p.Until[strconv.FormatInt(id, 10)] }
func (p *privilegeState) set(id, t int64) {
	if p.Until == nil {
		p.Until = map[string]int64{}
	}
	p.Until[strconv.FormatInt(id, 10)] = t
}

// active says whether the privilege is owned at that time.
func (p *privilegeState) active(id int64, now time.Time) bool {
	u := p.until(id)
	return u == privilegeForever || u > now.Unix()
}

// expire is the end time as the client reads it: 0 for a privilege that is not owned or never ends.
func (p *privilegeState) expire(id int64, now time.Time) int64 {
	if u := p.until(id); u > now.Unix() {
		return u
	}
	return 0
}

func privilegeOf(id int64) (privilegeDef, bool) {
	for _, d := range privileges {
		if d.ID == id {
			return d, true
		}
	}
	return privilegeDef{}, false
}

// privilegeReward is what a privilege pays a role of that level.
func privilegeReward(id, lev int64) [][2]int64 {
	for _, r := range privilegeAward[id] {
		if r.Min <= lev && lev <= r.Max {
			return r.Items
		}
	}
	return nil
}

func privilegeGain(id, lev int64) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	for _, it := range privilegeReward(id, lev) {
		splitGains([]sellGain{{it[0], it[1]}}, 1, assets, items)
	}
	return
}

func loadPrivileges(st *store.Store, rid int64) (*privilegeState, error) {
	p := &privilegeState{}
	if _, err := st.LoadState(rid, privilegeModule, p); err != nil {
		return nil, err
	}
	return p, nil
}

// privilegeCheck says whether the role may buy the privilege now: its level, and not while it is owned.
func privilegeCheck(role *store.Role, d privilegeDef, p *privilegeState, now time.Time) error {
	if int64(role.Lev) < d.LimitLev {
		return errNotPayable(fmt.Sprintf("needs level %d", d.LimitLev))
	}
	if p.active(d.ID, now) {
		return errNotPayable("still owned")
	}
	return nil
}

// privilegeEnds is when a privilege bought now ends.
func privilegeEnds(d privilegeDef, now time.Time) int64 {
	if d.LimitType == 0 {
		return privilegeForever
	}
	return now.Unix() + d.LimitDay*86400
}

// privilegeList24502 is the list of the privilege page: every privilege by the client's id.
func privilegeList24502(p *privilegeState, now time.Time) []proto.Message {
	list := make([]proto.Message, 0, len(privileges))
	for _, d := range privileges {
		list = append(list, proto.Message{"id": d.ID, "status": b2i(p.active(d.ID, now)), "expire_time": p.expire(d.ID, now)})
	}
	return list
}

// privilegeList24500 is the list the rest of the client reads at login: effect types 1~5, the ids 102~106 minus 101.
func privilegeList24500(p *privilegeState, now time.Time) []proto.Message {
	list := make([]proto.Message, 0, 5)
	for id := int64(1); id <= 5; id++ {
		list = append(list, proto.Message{"id": id, "status": b2i(p.active(id+101, now)), "expire_time": p.expire(id+101, now)})
	}
	return list
}

// sendPrivileges pushes both lists.
func sendPrivileges(s *gateway.Session, st *store.Store, rid int64) error {
	p, err := loadPrivileges(st, rid)
	if err != nil {
		return err
	}
	if err := s.Send(24502, proto.Message{"list": privilegeList24502(p, s.Now())}); err != nil {
		return err
	}
	return s.Send(24500, proto.Message{"list": privilegeList24500(p, s.Now())})
}

func init() {
	for _, d := range privileges {
		if d.PayType != 1 || d.ChargeID == 0 {
			continue
		}
		d := d
		RegisterChargeGuard(d.ChargeID, func(st *store.Store, role *store.Role) error {
			p, err := loadPrivileges(st, role.RID)
			if err != nil {
				return err
			}
			return privilegeCheck(role, d, p, chargeClock())
		})
		RegisterChargeHook(d.ChargeID, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			p, err := loadPrivileges(st, role.RID)
			if err != nil {
				return nil, nil, err
			}
			now := chargeClock()
			if err := privilegeCheck(role, d, p, now); err != nil {
				return nil, nil, err
			}
			p.set(d.ID, privilegeEnds(d, now))
			if err := st.SaveState(role.RID, privilegeModule, p); err != nil {
				return nil, nil, err
			}
			assets, items := privilegeGain(d.ID, int64(role.Lev))
			return assets, items, nil
		})
	}
	registerAdminModule(AdminModule{Key: privilegeModule, Title: "特权", Window: "24500/24501/24502", Scope: "role", New: adminState[privilegeState]})
}

// RegisterPrivilege registers the privilege page: 24502 the list, 24501 buy one with diamonds (the two privileges of the voyage).
func RegisterPrivilege(r *gateway.Router, st *store.Store) {
	r.Handle(24502, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		p, err := loadPrivileges(st, role.RID)
		if err != nil {
			return err
		}
		return s.Send(24502, proto.Message{"list": privilegeList24502(p, s.Now())})
	})
	r.Handle(24501, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(24501, proto.Message{"code": 0, "msg": msg}) }
		d, ok := privilegeOf(req.Int("id"))
		if !ok || d.PayType != 2 {
			return refuse("该特权不能用钻石购买")
		}
		unlock := st.LockRoleCharge(role.RID)
		defer unlock()
		fresh, err := st.RoleByID(role.RID)
		if err != nil {
			return err
		}
		p, err := loadPrivileges(st, role.RID)
		if err != nil {
			return err
		}
		if err := privilegeCheck(fresh, d, p, s.Now()); err != nil {
			if fresh.Lev < int(d.LimitLev) {
				return refuse(fmt.Sprintf("%d级开放", d.LimitLev))
			}
			return refuse("已拥有该特权")
		}
		assets, items := privilegeGain(d.ID, int64(fresh.Lev))
		if err := st.Exchange(role.RID, map[int64]int64{3: d.Loss}, map[int64]int64{}, assets, items); err != nil {
			return refuse("钻石不足")
		}
		p.set(d.ID, privilegeEnds(d, s.Now()))
		if err := st.SaveState(role.RID, privilegeModule, p); err != nil {
			return err
		}
		if err := s.Send(24501, proto.Message{"code": 1, "msg": "购买成功"}); err != nil {
			return err
		}
		if err := sendPrivileges(s, st, role.RID); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
