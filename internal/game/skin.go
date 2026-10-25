package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// skins.json: the client's hero skins (tools/extract_skins.py).
//
//go:embed data/skins.json
var skinsJSON []byte

// SkinInfo is one skin of the client's table.
type SkinInfo struct {
	BID   int64      `json:"bid"` // the hero it belongs to
	Name  string     `json:"name"`
	Level int        `json:"level"`
	Next  int64      `json:"next"` // the skin it upgrades to (0 = none)
	Loss  [][2]int64 `json:"loss"` // what the upgrade costs
}

var skinTable = func() map[int64]SkinInfo {
	var f struct {
		Skins map[int64]SkinInfo `json:"skins"`
	}
	if err := json.Unmarshal(skinsJSON, &f); err != nil || len(f.Skins) == 0 {
		panic("game: bad skins.json")
	}
	return f.Skins
}()

// SkinKnown reports whether the client has a skin with that id.
func SkinKnown(id int64) bool { _, ok := skinTable[id]; return ok }

// SkinChoice is a skin as the admin page offers it.
type SkinChoice struct {
	ID    int64  `json:"id"`
	BID   int64  `json:"bid"`
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// SkinChoices lists every skin, by id.
func SkinChoices() []SkinChoice {
	out := make([]SkinChoice, 0, len(skinTable))
	for id, k := range skinTable {
		out = append(out, SkinChoice{ID: id, BID: k.BID, Name: k.Name, Level: k.Level})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sendSkins sends 11020, the skins the role owns. The client opens a "new skin" window
// for a skin it did not know before, so this is also how a granted skin is announced.
func sendSkins(s *gateway.Session, st *store.Store, rid int64) error {
	owned, err := st.SkinsOf(rid, s.Now().Unix())
	if err != nil {
		return err
	}
	list := []proto.Message{}
	for _, k := range owned {
		list = append(list, proto.Message{"id": k.ID, "end_time": k.EndTime})
	}
	return s.Send(11020, proto.Message{"partner_skins": list})
}

// SendSkins is sendSkins for the admin push.
func SendSkins(s *gateway.Session, st *store.Store, rid int64) error { return sendSkins(s, st, rid) }

// RegisterSkin registers the hero skins: 11020 what the role owns, 11019 wear one on a hero (0
// takes it off; the skin must be the hero's own and not run out), 11021 upgrade an owned skin to
// the next level for the client's `upgrade_loss`. Skins are handed out from the admin page
// (`POST /api/roles/{id}/skins`); the skin shop and chips are not implemented. A worn skin is
// shown but its attribute bonus is not added to the hero's stats yet.
func RegisterSkin(r *gateway.Router, st *store.Store) {
	r.Handle(11020, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		return sendSkins(s, st, role.RID)
	})
	r.Handle(11019, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, skin := req.Int("partner_id"), req.Int("skin_id")
		reply := func(ok int, msg string) error {
			return s.Send(11019, proto.Message{"result": ok, "msg": msg, "partner_id": id})
		}
		p, err := st.PartnerByID(role.RID, id)
		if errors.Is(err, store.ErrNotFound) {
			return reply(0, "英雄不存在")
		}
		if err != nil {
			return err
		}
		if skin != 0 && skinTable[skin].BID != p.BID {
			return reply(0, "该皮肤不属于此英雄")
		}
		if err := st.WearSkin(role.RID, id, skin, s.Now().Unix()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "尚未获得该皮肤")
			}
			return err
		}
		np, err := st.PartnerByID(role.RID, id)
		if err != nil {
			return err
		}
		if err := s.Send(11002, partnerMessage(*np)); err != nil {
			return err
		}
		return reply(1, "")
	})
	r.Handle(11021, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, skin := req.Int("partner_id"), req.Int("skin_id")
		reply := func(ok int, msg string) error {
			return s.Send(11021, proto.Message{"result": ok, "msg": msg, "partner_id": id})
		}
		info, ok := skinTable[skin]
		if !ok || info.Next == 0 || !SkinKnown(info.Next) {
			return reply(0, "该皮肤无法升级")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		splitGains(gains(info.Loss), 1, assets, items)
		if err := st.UpgradeSkin(role.RID, skin, info.Next, s.Now().Unix(), assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "材料不足或尚未获得该皮肤")
			}
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := sendSkins(s, st, role.RID); err != nil {
			return err
		}
		if p, err := st.PartnerByID(role.RID, id); err == nil {
			if err := s.Send(11002, partnerMessage(*p)); err != nil {
				return err
			}
		}
		return reply(1, "")
	})
}
