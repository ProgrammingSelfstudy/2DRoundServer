package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// horcrux.json: the horcrux (魂器) tables of the client (tools/extract_horcrux.py).
//
//go:embed data/horcrux.json
var horcruxJSON []byte

type horcruxLevel struct {
	Loss  [][]int64          `json:"loss"` // what the step to the next level costs
	Conds [][]any            `json:"conds"`
	Attr  map[string][][]any `json:"attr"` // slot -> [[key, value]]
}

var horcruxTable = func() struct {
	AwakenLoss  [][]int64 `json:"awaken_loss"`
	AwakenLimit [][]any   `json:"awaken_limit"`
	Heroes      map[int64]struct {
		Name  string  `json:"name"`
		Slots []int64 `json:"slots"`
	} `json:"heroes"`
	Levels   map[int64]horcruxLevel `json:"levels"`
	Fallback map[int64]struct {
		Loss   [][]int64 `json:"loss"`
		Reward [][]int64 `json:"reward"`
	} `json:"fallback"`
} {
	var t struct {
		AwakenLoss  [][]int64 `json:"awaken_loss"`
		AwakenLimit [][]any   `json:"awaken_limit"`
		Heroes      map[int64]struct {
			Name  string  `json:"name"`
			Slots []int64 `json:"slots"`
		} `json:"heroes"`
		Levels   map[int64]horcruxLevel `json:"levels"`
		Fallback map[int64]struct {
			Loss   [][]int64 `json:"loss"`
			Reward [][]int64 `json:"reward"`
		} `json:"fallback"`
	}
	if err := json.Unmarshal(horcruxJSON, &t); err != nil || len(t.Heroes) != 97 {
		panic("game: bad horcrux.json: " + fmt.Sprint(err))
	}
	return t
}()

const horcruxTop = 35 // the last level

// horcruxStarOK checks the hero conditions of the client ("partner_star" n: the hero's star; "star_tower" is not checked).
func horcruxStarOK(conds [][]any, star int) bool {
	for _, c := range conds {
		if name, _ := c[0].(string); name == "partner_star" {
			if need, _ := c[1].(float64); float64(star) < need {
				return false
			}
		}
	}
	return true
}

// horcruxBonus adds the horcrux attributes of a hero: the hero's slots of its level's row (the row holds the total of the level).
// Only attack, hp, defense and speed are applied; hit, dodge and tenacity are not.
func horcruxBonus(bid int64, h store.HorcruxState, hp, atk, def, speed int64) (int64, int64, int64, int64) {
	hero, ok := horcruxTable.Heroes[bid]
	if !ok || h.Status < 1 {
		return hp, atk, def, speed
	}
	row := horcruxTable.Levels[h.Lev]
	for _, slot := range hero.Slots {
		for _, a := range row.Attr[fmt.Sprint(slot)] {
			key, _ := a[0].(string)
			v, _ := a[1].(float64)
			switch key {
			case "hp_max":
				hp += int64(v)
			case "atk":
				atk += int64(v)
			case "def":
				def += int64(v)
			case "speed":
				speed += int64(v)
			}
		}
	}
	return hp, atk, def, speed
}

// RegisterHorcrux registers the horcrux: 11404 can it be awakened, 11402 awaken, 11403 strengthen one level, 11405 take one level
// back. 11401 (the soul weapon, which has no table in the client) stays a stub.
func RegisterHorcrux(r *gateway.Router, st *store.Store) {
	reply := func(s *gateway.Session, cmd uint16, field string, ok int, msg string) error {
		return s.Send(cmd, proto.Message{field: ok, "msg": msg})
	}
	// hero loads the hero and its horcrux; msg is set when the request cannot go on
	hero := func(s *gateway.Session, req proto.Message) (p *store.Partner, doc *store.HorcruxDoc, msg string, err error) {
		role := s.Role()
		p, err = st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, "英雄不存在", nil
		} else if err != nil {
			return nil, nil, "", err
		}
		if _, ok := horcruxTable.Heroes[p.BID]; !ok {
			return nil, nil, "该英雄没有魂器", nil
		}
		doc, err = st.HorcruxOf(role.RID)
		return p, doc, "", err
	}
	finish := func(s *gateway.Session, p *store.Partner, doc *store.HorcruxDoc, h store.HorcruxState, spend, gain [][]int64) (string, error) {
		role := s.Role()
		spendA, spendI := splitCost(spend, 1)
		gainA, gainI := splitCost(gain, 1)
		if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return "资产不足", nil
			}
			return "", err
		}
		doc.Heroes[p.PartnerID] = h
		return "", st.SaveHorcrux(role.RID, doc)
	}
	push := func(s *gateway.Session, p *store.Partner) error {
		role := s.Role()
		fresh, err := st.PartnerByID(role.RID, p.PartnerID)
		if err != nil {
			return err
		}
		if err := s.Send(11012, partnerMessage(*fresh)); err != nil {
			return err
		}
		return sendAssets(s, st, role.RID)
	}

	r.Handle(11404, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		p, doc, msg, err := hero(s, req)
		if err != nil {
			return err
		}
		if msg != "" {
			return reply(s, 11404, "result", 0, msg)
		}
		if doc.Heroes[p.PartnerID].Status != 0 {
			return reply(s, 11404, "result", 0, "魂器已觉醒")
		}
		if !horcruxStarOK(horcruxTable.AwakenLimit, p.Star) {
			return reply(s, 11404, "result", 0, "英雄星级不足")
		}
		return reply(s, 11404, "result", 1, "")
	})

	r.Handle(11402, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		p, doc, msg, err := hero(s, req)
		if err != nil {
			return err
		}
		if msg == "" && doc.Heroes[p.PartnerID].Status != 0 {
			msg = "魂器已觉醒"
		}
		if msg == "" && !horcruxStarOK(horcruxTable.AwakenLimit, p.Star) {
			msg = "英雄星级不足"
		}
		if msg == "" {
			msg, err = finish(s, p, doc, store.HorcruxState{Status: 1}, horcruxTable.AwakenLoss, nil)
			if err != nil {
				return err
			}
		}
		if msg != "" {
			return reply(s, 11402, "result", 0, msg)
		}
		if err := reply(s, 11402, "result", 1, "觉醒成功"); err != nil {
			return err
		}
		return push(s, p)
	})

	r.Handle(11403, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		p, doc, msg, err := hero(s, req)
		if err != nil {
			return err
		}
		h := doc.Heroes[req.Int("partner_id")]
		row := horcruxTable.Levels[h.Lev]
		switch {
		case msg != "":
		case h.Status != 1:
			msg = "魂器未觉醒或已满级"
		case len(row.Loss) == 0:
			msg = "魂器已满级"
		case !horcruxStarOK(row.Conds, p.Star):
			msg = "英雄星级不足"
		}
		if msg == "" {
			next := store.HorcruxState{Status: 1, Lev: h.Lev + 1}
			if next.Lev >= horcruxTop {
				next.Status = 2
			}
			msg, err = finish(s, p, doc, next, row.Loss, nil)
			if err != nil {
				return err
			}
		}
		if msg != "" {
			return reply(s, 11403, "result", 0, msg)
		}
		if err := reply(s, 11403, "result", 1, "强化成功"); err != nil {
			return err
		}
		return push(s, p)
	})

	r.Handle(11405, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		p, doc, msg, err := hero(s, req)
		if err != nil {
			return err
		}
		h := doc.Heroes[req.Int("partner_id")]
		if msg == "" && (h.Status < 1 || h.Lev < 1) {
			msg = "魂器没有可回退的等级"
		}
		if msg == "" {
			fb := horcruxTable.Fallback[h.Lev]
			msg, err = finish(s, p, doc, store.HorcruxState{Status: 1, Lev: h.Lev - 1}, fb.Loss, fb.Reward)
			if err != nil {
				return err
			}
		}
		if msg != "" {
			return reply(s, 11405, "code", 0, msg)
		}
		if err := reply(s, 11405, "code", 1, "回退成功"); err != nil {
			return err
		}
		if err := push(s, p); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
