package game

import (
	"encoding/json"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// firstGift is a gift of a first top-up window: two tiers (fid 1 and 2) of three days each (ids 1~3 and 4~6).
type firstGift struct {
	ID, Fid, Day int64
	NeedExp      int64 `json:"need_exp"`
	Items        [][2]int64
}

// firstSet is one of the client's first top-up windows: the commands that show and take, the state it keeps and its gift table.
type firstSet struct {
	key        string // the table in charge.json, and the state module
	show, take uint16
	choose     uint16 // 0, or the command that picks the hero of the window (the new first top-up)
	gifts      []firstGift
}

// firstChooseHeroes are the heroes to pick from in the new first top-up window (its three buttons), given as a 5-star reward.
var firstChooseHeroes = [3]int64{20501, 30506, 10505}

const firstChooseStar = 5

// firstSets: 21000 is the plain window (data_first_charge_data), 21012~21014 the new one that also gives one hero of three,
// 21030/21031 and 21032/21033 two more windows with their own tables (the second one asks for 10000 and 50000 experience).
var firstSets = func() []*firstSet {
	var f map[string]json.RawMessage
	if err := json.Unmarshal(chargeJSON, &f); err != nil {
		panic("game: bad charge.json")
	}
	mk := func(key string, show, take, choose uint16) *firstSet {
		g := []firstGift{}
		if err := json.Unmarshal(f[key], &g); err != nil || len(g) != 6 {
			panic("game: bad charge.json " + key)
		}
		sort.Slice(g, func(i, j int) bool { return g[i].ID < g[j].ID })
		return &firstSet{key: key, show: show, take: take, choose: choose, gifts: g}
	}
	return []*firstSet{mk("first", 21000, 21001, 0), mk("first_new", 21012, 21013, 21014), mk("first_lwt", 21030, 21031, 0), mk("first_3", 21032, 21033, 0)}
}()

// firstChargeState is kept per tier (fid 1 and 2 are the indexes 0 and 1).
type firstChargeState struct {
	Start  [2]int64 // day number of the day the paid sum reached the tier, 0 = not yet
	Taken  [2]int64 // days of the tier taken, in order
	Last   [2]int64 // day number of the last day taken
	Chosen int64    // the hero picked (1~3), 0 = none
}

// status is a gift's state: 0 not reached (or a later day), 1 waits, 2 taken.
func (fs *firstSet) status(d *firstChargeState, id, today int64) int64 {
	g := fs.gifts[id-1]
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
func (fs *firstSet) activate(d *firstChargeState, rc *store.RoleCharge, today int64) bool {
	changed := false
	for _, g := range fs.gifts {
		if i := g.Fid - 1; d.Start[i] == 0 && accSum(rc) >= g.NeedExp {
			d.Start[i], changed = today, true
		}
	}
	return changed
}

func (fs *firstSet) window(d *firstChargeState, today int64) proto.Message {
	list := make([]proto.Message, len(fs.gifts))
	for i, g := range fs.gifts {
		list[i] = proto.Message{"id": g.ID, "status": fs.status(d, g.ID, today)}
	}
	m := proto.Message{"first_gift": list}
	if fs.choose == 0 {
		m["end_time"] = 0
		return m
	}
	// 0 cannot pick yet, 1 may pick, 2 picked
	switch {
	case d.Chosen != 0:
		m["choosen_status"], m["has_choosen_id"] = 2, d.Chosen
	case d.Start[0] != 0:
		m["choosen_status"], m["has_choosen_id"] = 1, 0
	default:
		m["choosen_status"], m["has_choosen_id"] = 0, 0
	}
	return m
}

// RegisterFirstCharge registers the first top-up windows. A tier opens when the paid sum (VIP experience) reaches it, the gifts
// come one a day from that day.
func RegisterFirstCharge(r *gateway.Router, st *store.Store) {
	for _, fs := range firstSets {
		fs := fs
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
			if _, err := st.LoadState(role.RID, "firstcharge_"+fs.key, d); err != nil {
				return nil, nil, 0, err
			}
			today := dayNo(s.Now())
			if fs.activate(d, rc, today) {
				if err := st.SaveState(role.RID, "firstcharge_"+fs.key, d); err != nil {
					return nil, nil, 0, err
				}
			}
			return role, d, today, nil
		}
		r.Handle(fs.show, func(s *gateway.Session, _ proto.Message) error {
			role, d, today, err := load(s)
			if role == nil {
				return err
			}
			return s.Send(fs.show, fs.window(d, today))
		})
		r.Handle(fs.take, func(s *gateway.Session, req proto.Message) error {
			role, d, today, err := load(s)
			if role == nil {
				return err
			}
			refuse := func(msg string) error { return s.Send(fs.take, proto.Message{"code": 0, "msg": msg}) }
			id := req.Int("id")
			if id < 1 || id > int64(len(fs.gifts)) {
				return refuse("奖励不存在")
			}
			switch fs.status(d, id, today) {
			case 0:
				return refuse("还不能领取")
			case 2:
				return refuse("已经领取过了")
			}
			g := fs.gifts[id-1]
			ga, gi := map[int64]int64{}, map[int64]int64{}
			for _, it := range g.Items {
				splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return err
			}
			d.Taken[g.Fid-1], d.Last[g.Fid-1] = g.Day, today
			if err := st.SaveState(role.RID, "firstcharge_"+fs.key, d); err != nil {
				return err
			}
			if err := s.Send(fs.take, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
				return err
			}
			if err := s.Send(fs.show, fs.window(d, today)); err != nil {
				return err
			}
			if err := sendAssets(s, st, role.RID); err != nil {
				return err
			}
			return sendBackpack(s, st)
		})
		if fs.choose == 0 {
			continue
		}
		r.Handle(fs.choose, func(s *gateway.Session, req proto.Message) error {
			role, d, today, err := load(s)
			if role == nil {
				return err
			}
			refuse := func(msg string) error { return s.Send(fs.choose, proto.Message{"code": 0, "msg": msg}) }
			idx := req.Int("id")
			switch {
			case idx < 1 || idx > int64(len(firstChooseHeroes)):
				return refuse("英雄不存在")
			case d.Chosen != 0:
				return refuse("已经选择过英雄了")
			case d.Start[0] == 0:
				return refuse("充值后才能选择")
			}
			heroes, err := st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{{BID: firstChooseHeroes[idx-1], Star: firstChooseStar}}})
			if err != nil {
				return err
			}
			d.Chosen = idx
			if err := st.SaveState(role.RID, "firstcharge_"+fs.key, d); err != nil {
				return err
			}
			if err := s.Send(fs.choose, proto.Message{"code": 1, "msg": "选择成功"}); err != nil {
				return err
			}
			msgs := make([]proto.Message, 0, len(heroes))
			for _, h := range heroes {
				msgs = append(msgs, partnerMessage(h))
			}
			if err := s.Send(11001, proto.Message{"partners": msgs}); err != nil {
				return err
			}
			return s.Send(fs.show, fs.window(d, today))
		})
	}
}
