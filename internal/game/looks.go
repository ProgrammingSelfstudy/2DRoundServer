package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// looks.json: the avatar frames and titles of the client (tools/extract_looks.py).
//
//go:embed data/looks.json
var looksJSON []byte

type lookInfo struct {
	Name    string
	Minutes int64 // how long it lasts once activated; 0 = for good
	Loss    [][2]int64
}

var looksTable = func() map[string]map[int64]lookInfo {
	var f struct {
		Avatar map[int64]lookInfo
		Title  map[int64]lookInfo
	}
	if err := json.Unmarshal(looksJSON, &f); err != nil || len(f.Avatar) == 0 || len(f.Title) == 0 {
		panic("game: bad looks.json")
	}
	return map[string]map[int64]lookInfo{store.LookAvatar: f.Avatar, store.LookTitle: f.Title}
}()

// LookChoice is a cosmetic as the admin page offers it.
type LookChoice struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// LookChoices lists the avatar frames and titles of the client, by kind and id.
func LookChoices() []LookChoice {
	var out []LookChoice
	for _, k := range []string{store.LookAvatar, store.LookTitle} {
		for id, v := range looksTable[k] {
			out = append(out, LookChoice{Kind: k, ID: id, Name: v.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LookKnown reports whether the admin may grant that cosmetic: avatar frames and titles must be in
// the client's tables, a backdrop must be an item of the client, a face a head of the client
// (heads are the hero and skin ids), a bubble is not checked (the client has no table for it).
func LookKnown(kind string, id int64) bool {
	switch kind {
	case store.LookAvatar, store.LookTitle:
		_, ok := looksTable[kind][id]
		return ok
	case store.LookBackdrop:
		_, ok := itemTable[id]
		return ok
	case store.LookFace, store.LookBubble:
		return id > 0
	}
	return false
}

const (
	signatureMax = 60 // characters (our policy; the client has no limit in its code)
	vipFaceVIP4  = 4  // the client's tips: face 1001 at VIP 4, face 1002 at VIP 7
	vipFaceVIP7  = 7
)

func validSignature(v string) bool {
	if utf8.RuneCountInString(v) > signatureMax {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ownedFaces are the heads a role may use: those of the heroes it has, the VIP ones and the ones
// granted from the admin page (the client's tips say "获得X可解锁").
func ownedFaces(st *store.Store, role *store.Role, now int64) ([]int64, error) {
	set := map[int64]bool{}
	ps, err := st.PartnersOf(role.RID)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		set[p.BID] = true
	}
	if fresh, err := st.RoleByID(role.RID); err == nil {
		if fresh.VIPLev >= vipFaceVIP4 {
			set[1001] = true
		}
		if fresh.VIPLev >= vipFaceVIP7 {
			set[1002] = true
		}
	}
	extra, err := st.LooksOf(role.RID, store.LookFace, now)
	if err != nil {
		return nil, err
	}
	for _, l := range extra {
		set[l.ID] = true
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// withSettings adds the stored settings to a 10301 message.
func withSettings(st *store.Store, rid int64, m proto.Message) (proto.Message, error) {
	cfg, err := st.SettingsOf(rid)
	if err != nil {
		return m, err
	}
	m["signature"], m["is_show_vip"], m["auto_pk"] = cfg.Signature, cfg.ShowVIP, cfg.AutoPK
	m["backdrop_id"], m["avatar_base_id"] = cfg.BackdropID, cfg.AvatarID
	// the same on every level-1 role of the original server (capture original-2026-10-22)
	m["room_bbs_set"], m["is_open_home"] = 1, 0
	if face, _ := m["face_id"].(int); face >= 10000 { // 110401 for head 10401
		m["look_id"] = 100000 + face
	}
	return m, nil
}

// RegisterLooks registers what a role sets about itself and its cosmetics: the signature (10309),
// auto fight (10318), showing the VIP level (10348), the head (10325 what it may use, 10327 use),
// the space backdrop (10328 / 10329), avatar frames (21500 list, 21501 use, 21503 activate), chat
// bubbles (12700 / 12701 / 12703) and titles (23300 list, 23301 use, 23302 list, 23303 activate).
// An avatar frame or title is activated for the `loss` the client's table lists and lasts its
// `expire_time` minutes; the rest are handed out from the admin page. Compositing titles (23304)
// and the custom head upload (10330-10333) are not implemented.
func RegisterLooks(r *gateway.Router, st *store.Store) {
	r.Handle(10309, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		v := strings.TrimSpace(req.Str("signature"))
		if !validSignature(v) {
			return s.Send(10309, proto.Message{"code": 0, "msg": "签名过长或含有非法字符", "signature": ""})
		}
		if err := st.SetSignature(role.RID, v); err != nil {
			return err
		}
		return s.Send(10309, proto.Message{"code": 1, "msg": "", "signature": v})
	})
	flag := func(cmd uint16, field string, set func(rid int64, v int) error) {
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			v := int(req.Int(field))
			if v != 0 && v != 1 {
				return s.Send(cmd, proto.Message{"code": 0, "msg": "参数错误", field: v})
			}
			if err := set(role.RID, v); err != nil {
				return err
			}
			return s.Send(cmd, proto.Message{"code": 1, "msg": "", field: v})
		})
	}
	flag(10318, "auto_pk", st.SetAutoPK)
	flag(10348, "is_show_vip", st.SetShowVIP)

	r.Handle(10325, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ids, err := ownedFaces(st, role, s.Now().Unix())
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"face_id": id})
		}
		return s.Send(10325, proto.Message{"face_list": list})
	})
	r.Handle(10327, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("face_id")
		ids, err := ownedFaces(st, role, s.Now().Unix())
		if err != nil {
			return err
		}
		i := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
		if i == len(ids) || ids[i] != id {
			return s.Send(10327, proto.Message{"code": 0, "msg": "尚未获得该头像", "face_id": id})
		}
		if err := st.SetRoleFace(role.RID, int(id)); err != nil {
			return err
		}
		return s.Send(10327, proto.Message{"code": 1, "msg": "", "face_id": id})
	})

	// backdrops: id 0 is the default, the others are owned ones
	r.Handle(10328, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		owned, err := st.LooksOf(role.RID, store.LookBackdrop, s.Now().Unix())
		if err != nil {
			return err
		}
		list := []proto.Message{{"backdrop_id": 0}}
		for _, l := range owned {
			list = append(list, proto.Message{"backdrop_id": l.ID})
		}
		return s.Send(10328, proto.Message{"backdrop_list": list})
	})
	r.Handle(10329, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("backdrop_id")
		if err := st.UseLook(role.RID, store.LookBackdrop, id, s.Now().Unix()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return s.Send(10329, proto.Message{"code": 0, "msg": "尚未获得该背景", "backdrop_id": id})
			}
			return err
		}
		return s.Send(10329, proto.Message{"code": 1, "msg": "", "backdrop_id": id})
	})

	// frames, bubbles and titles share their shape: a list with the end time of each, use, activate
	type family struct {
		kind           string
		list, use, act uint16
		current        func(store.RoleSettings) int64
		wrap           func(cur int64, list []proto.Message) proto.Message // the list command's answer
		activateAnswer uint16
	}
	entry := func(kind string, l store.Look) proto.Message {
		m := proto.Message{"base_id": l.ID, "expire_time": l.EndTime}
		if kind == store.LookTitle {
			m["lev"] = 0
		}
		return m
	}
	listOf := func(s *gateway.Session, kind string) ([]proto.Message, error) {
		if kind == store.LookAvatar { // everyone has the free "冒险徽章"
			if err := st.EnsureLook(s.Role().RID, kind, store.DefaultAvatarID); err != nil {
				return nil, err
			}
		}
		owned, err := st.LooksOf(s.Role().RID, kind, s.Now().Unix())
		if err != nil {
			return nil, err
		}
		out := []proto.Message{}
		for _, l := range owned {
			out = append(out, entry(kind, l))
		}
		return out, nil
	}
	fams := []family{
		{kind: store.LookAvatar, list: 21500, use: 21501, act: 21503,
			current: func(c store.RoleSettings) int64 { return c.AvatarID },
			wrap:    func(_ int64, l []proto.Message) proto.Message { return proto.Message{"avatar_frame": l} }},
		{kind: store.LookBubble, list: 12700, use: 12701, act: 12703,
			current: func(c store.RoleSettings) int64 { return c.BubbleID },
			wrap: func(cur int64, l []proto.Message) proto.Message {
				return proto.Message{"say_frame_bid": cur, "say_frame": l}
			}},
		{kind: store.LookTitle, list: 23300, use: 23301, act: 23303,
			current: func(c store.RoleSettings) int64 { return c.TitleID },
			wrap:    func(cur int64, l []proto.Message) proto.Message { return proto.Message{"base_id": cur, "honor": l} }},
	}
	for _, f := range fams {
		f := f
		answer := func(s *gateway.Session, cmd uint16, plain bool) error {
			l, err := listOf(s, f.kind)
			if err != nil {
				return err
			}
			cfg, err := st.SettingsOf(s.Role().RID)
			if err != nil {
				return err
			}
			if plain { // the activation / refresh answers carry the list only
				return s.Send(cmd, f.wrap(0, l))
			}
			return s.Send(cmd, f.wrap(f.current(cfg), l))
		}
		r.Handle(f.list, func(s *gateway.Session, _ proto.Message) error {
			if s.Role() == nil {
				return nil
			}
			return answer(s, f.list, false)
		})
		r.Handle(f.use, func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			id := req.Int("base_id")
			if f.kind == store.LookAvatar {
				if err := st.EnsureLook(role.RID, f.kind, store.DefaultAvatarID); err != nil {
					return err
				}
			}
			if err := st.UseLook(role.RID, f.kind, id, s.Now().Unix()); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return nil // not owned: the client sent it for an item it shows as usable, there is no refusal message
				}
				return err
			}
			return s.Send(f.use, proto.Message{"base_id": id})
		})
		if f.kind == store.LookTitle { // 23302: the list again, without the one in use
			r.Handle(23302, func(s *gateway.Session, _ proto.Message) error {
				if s.Role() == nil {
					return nil
				}
				return answer(s, 23302, true)
			})
		}
		if f.kind == store.LookBubble {
			r.Handle(f.act, func(s *gateway.Session, _ proto.Message) error {
				if s.Role() == nil {
					return nil
				}
				return answer(s, f.act, true)
			})
			continue
		}
		r.Handle(f.act, func(s *gateway.Session, req proto.Message) error {
			role := s.Role()
			if role == nil {
				return nil
			}
			info, ok := looksTable[f.kind][req.Int("base_id")]
			if ok {
				now := s.Now().Unix()
				var end int64
				if info.Minutes > 0 {
					end = now + info.Minutes*60
				}
				assets, items := map[int64]int64{}, map[int64]int64{}
				splitGains(gains(info.Loss), 1, assets, items)
				err := st.ActivateLook(role.RID, f.kind, req.Int("base_id"), now, end, assets, items)
				switch {
				case err == nil:
					if err := sendAssets(s, st, role.RID); err != nil {
						return err
					}
					if err := sendBackpack(s, st); err != nil {
						return err
					}
				case !errors.Is(err, store.ErrConflict):
					return err
				}
			}
			return answer(s, f.act, true)
		})
	}
}
