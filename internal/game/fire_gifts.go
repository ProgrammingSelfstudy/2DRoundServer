package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

//go:embed data/fire_gifts.json
var fireGiftsJSON []byte

const fireGiftModule = "firegifts"

type fireGift struct {
	ID          int64
	ChargeID    int64 `json:"charge_id"`
	Level       int
	Tower       int64
	GuildCareer int   `json:"guild_career"`
	MinPaid     int64 `json:"min_paid"`
	MaxPaid     int64 `json:"max_paid"`
	Seconds     int64
	Items       [][2]int64
}

var fireGifts, fireGiftAwards, fireGiftProducts = func() ([]fireGift, map[int64]fireGift, map[int64]bool) {
	var data struct{ Offers []fireGift }
	if err := json.Unmarshal(fireGiftsJSON, &data); err != nil || len(data.Offers) == 0 {
		panic("game: bad fire_gifts.json")
	}
	awards := make(map[int64]fireGift, len(data.Offers))
	products := make(map[int64]bool)
	for _, offer := range data.Offers {
		awards[offer.ID] = offer
		products[offer.ChargeID] = true
	}
	return data.Offers, awards, products
}()

func (g fireGift) trigger() string {
	switch {
	case g.Level != 0:
		return "level:" + strconv.Itoa(g.Level)
	case g.GuildCareer != 0:
		return "guild:" + strconv.Itoa(g.GuildCareer)
	default:
		return "tower:" + strconv.FormatInt(g.Tower, 10)
	}
}

func (g fireGift) reached(role *store.Role, tower *store.StarTower, guild *store.GuildSkills) bool {
	switch {
	case g.Level != 0:
		return g.Level == role.Lev
	case g.GuildCareer != 0:
		_, unlocked := guild.PvpAttrs[g.GuildCareer]
		return unlocked
	default:
		return g.Tower != 0 && g.Tower == tower.MaxTower
	}
}

type fireGiftWindow struct{ AwardID, EndAt int64 }

type fireGiftState map[string]fireGiftWindow

func loadFireGifts(st *store.Store, rid int64) (fireGiftState, error) {
	var state fireGiftState
	_, err := st.LoadState(rid, fireGiftModule, &state)
	if state == nil {
		state = make(fireGiftState)
	}
	return state, err
}

func discoverFireGifts(st *store.Store, role *store.Role, now time.Time) error {
	state, err := loadFireGifts(st, role.RID)
	if err != nil {
		return err
	}
	tower, err := st.StarTowerOf(role.RID)
	if err != nil {
		return err
	}
	guild, err := st.GuildSkillsOf(role.RID)
	if err != nil {
		return err
	}
	charge, err := st.RoleCharge(role.RID)
	if err != nil {
		return err
	}
	paid := charge.TotalCents / 100
	changed := false
	for _, offer := range fireGifts {
		if !offer.reached(role, tower, guild) {
			continue
		}
		key := offer.trigger()
		if _, seen := state[key]; seen || paid < offer.MinPaid || paid > offer.MaxPaid {
			continue
		}
		state[key] = fireGiftWindow{offer.ID, now.Unix() + offer.Seconds}
		changed = true
	}
	if changed {
		return st.SaveState(role.RID, fireGiftModule, state)
	}
	return nil
}

func init() {
	registerAdminModule(AdminModule{Key: fireGiftModule, Title: "触发限时钜惠", Window: "28000", Scope: "role", New: adminState[fireGiftState]})
}

func activeFireGifts(st *store.Store, rid int64, now time.Time) ([]fireGiftWindow, error) {
	state, err := loadFireGifts(st, rid)
	if err != nil {
		return nil, err
	}
	var active []fireGiftWindow
	seen := make(map[int64]bool)
	for _, offer := range fireGifts {
		window := state[offer.trigger()]
		if window.AwardID != offer.ID || window.EndAt <= now.Unix() || seen[offer.ChargeID] {
			continue
		}
		status, err := st.PayOfferStatus(rid, offer.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if err == nil && status == store.PayDone {
			continue
		}
		seen[offer.ChargeID] = true
		active = append(active, window)
	}
	return active, nil
}

func registerFireGifts(r *gateway.Router, st *store.Store) {
	r.Handle(28000, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		role, err := st.RoleByID(role.RID)
		if err != nil {
			return err
		}
		now := s.Now()
		if err := discoverFireGifts(st, role, now); err != nil {
			return err
		}
		active, err := activeFireGifts(st, role.RID, now)
		if err != nil {
			return err
		}
		gifts := make([]proto.Message, 0, len(active))
		for _, window := range active {
			gifts = append(gifts, proto.Message{"award_id": window.AwardID, "end_time": window.EndAt, "count": 0})
		}
		return s.Send(28000, proto.Message{"gifts": gifts})
	})
}
