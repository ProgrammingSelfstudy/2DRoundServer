package game

import (
	"encoding/json"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const limitGiftModule = "limitgift"

// limitGift is a gift that a role is offered for a few hours when it reaches something (Config.StarGiftData): a hero star, a
// level. The id is its place in the client's table. The gift is bought with the top-up product ChargeID.
type limitGift struct {
	ID       int64
	Name     string
	Kind     int64 // 1 hero star, 2 role level, 3 holy equipment (not offered)
	Cond     []json.RawMessage
	Items    [][2]int64
	ChargeID int64 `json:"charge_id"`
	Secs     int64
	need     int64 // the star or level
}

var limitGifts = func() []*limitGift {
	var f struct {
		LG []*limitGift `json:"limit_gifts"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.LG) != 23 {
		panic("game: bad charge.json limit_gifts")
	}
	for _, g := range f.LG {
		if g.Kind != 3 {
			_ = json.Unmarshal(g.Cond[1], &g.need)
		}
	}
	return f.LG
}()

type limitGiftEntry struct {
	Start  int64 // when it was offered (unix), 0 = not offered
	Bought bool
}

type limitGiftState struct{ Gifts map[int64]*limitGiftEntry }

func (d *limitGiftState) entry(id int64) *limitGiftEntry {
	if d.Gifts == nil {
		d.Gifts = map[int64]*limitGiftEntry{}
	}
	if d.Gifts[id] == nil {
		d.Gifts[id] = &limitGiftEntry{}
	}
	return d.Gifts[id]
}

// open says whether the gift can be bought at that time.
func (d *limitGiftState) open(g *limitGift, now int64) bool {
	e := d.Gifts[g.ID]
	return e != nil && e.Start > 0 && !e.Bought && now < e.Start+g.Secs
}

// trigger offers the gifts whose condition the role has reached: of each kind only the highest one reached and not offered yet
// (the lower ones are marked as passed, so that a role of level 60 is not offered the gifts of 18 and 48 too). It returns the
// ids newly offered.
func (d *limitGiftState) trigger(lev, star int64, now int64) []int64 {
	var out []int64
	for kind, have := range map[int64]int64{1: star, 2: lev} {
		var top *limitGift
		for _, g := range limitGifts {
			if g.Kind == kind && g.need <= have && d.Gifts[g.ID] == nil && (top == nil || g.need > top.need) {
				top = g
			}
		}
		for _, g := range limitGifts {
			if g.Kind == kind && g.need <= have && d.Gifts[g.ID] == nil {
				if g == top {
					d.entry(g.ID).Start = now
					out = append(out, g.ID)
				} else {
					d.entry(g.ID).Start = -1 // passed
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func limitGiftByCharge(chargeID int64) *limitGift {
	for _, g := range limitGifts {
		if g.ChargeID == chargeID && g.Kind != 3 {
			return g
		}
	}
	return nil
}

func init() {
	for _, g := range limitGifts {
		if g.Kind == 3 {
			continue
		}
		g := g
		RegisterChargeGuard(g.ChargeID, func(st *store.Store, role *store.Role) error {
			d := &limitGiftState{}
			if _, err := st.LoadState(role.RID, limitGiftModule, d); err != nil {
				return err
			}
			if !d.open(g, chargeClock().Unix()) {
				return errNotPayable("the gift is not on offer")
			}
			return nil
		})
		RegisterChargeHook(g.ChargeID, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			d := &limitGiftState{}
			if _, err := st.LoadState(role.RID, limitGiftModule, d); err != nil {
				return nil, nil, err
			}
			if !d.open(g, chargeClock().Unix()) {
				return nil, nil, errNotPayable("the gift is not on offer")
			}
			d.entry(g.ID).Bought = true
			if err := st.SaveState(role.RID, limitGiftModule, d); err != nil {
				return nil, nil, err
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			for _, it := range g.Items {
				splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
			}
			return ga, gi, nil
		})
	}
}

// CheckLimitGifts offers the limited gifts that the role has just reached and tells the client (21211, which makes it pop up).
// It is asked at login, when the gifts are listed and after a hero's star went up.
func CheckLimitGifts(s *gateway.Session, st *store.Store, now time.Time) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	fresh, err := st.RoleByID(role.RID)
	if err != nil {
		return err
	}
	ps, err := st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	star := int64(0)
	for _, p := range ps {
		star = max(star, int64(p.Star))
	}
	d := &limitGiftState{}
	if _, err := st.LoadState(role.RID, limitGiftModule, d); err != nil {
		return err
	}
	ids := d.trigger(int64(fresh.Lev), star, now.Unix())
	if len(ids) == 0 {
		return nil
	}
	if err := st.SaveState(role.RID, limitGiftModule, d); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Send(21211, proto.Message{"id": id}); err != nil {
			return err
		}
	}
	return nil
}

// RegisterLimitGift registers 21210, the gifts on offer with the time each one ends.
func RegisterLimitGift(r *gateway.Router, st *store.Store) {
	r.Handle(21210, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		now := s.Now()
		if err := CheckLimitGifts(s, st, now); err != nil {
			return err
		}
		d := &limitGiftState{}
		if _, err := st.LoadState(role.RID, limitGiftModule, d); err != nil {
			return err
		}
		list := []proto.Message{}
		for _, g := range limitGifts {
			if g.Kind != 3 && d.open(g, now.Unix()) {
				list = append(list, proto.Message{"id": g.ID, "num": 1, "end_time": d.Gifts[g.ID].Start + g.Secs})
			}
		}
		return s.Send(21210, proto.Message{"gifts": list})
	})
}
