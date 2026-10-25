package game

import (
	_ "embed"
	"encoding/json"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// cities.json: the city ids a role may pick (tools/extract_cities.py).
//
//go:embed data/cities.json
var citiesJSON []byte

var validCity = func() map[int64]bool {
	var f struct {
		Cities []int64 `json:"cities"`
	}
	if err := json.Unmarshal(citiesJSON, &f); err != nil || len(f.Cities) == 0 {
		panic("game: bad cities.json")
	}
	m := map[int64]bool{}
	for _, c := range f.Cities {
		m[c] = true
	}
	return m
}()

// The message board's numbers are the client's own (Config.RoomGrowData.data_const).
const (
	bbsOpenLev   = 35  // bbs_open_limit
	bbsCooldown  = 30  // bbs_time_limit, seconds
	bbsMaxRunes  = 36  // bbs_writing_number
	bbsBoardSize = 300 // bbs_number_limit
	fansListSize = 50
)

func validBBSText(v string) bool {
	if n := utf8.RuneCountInString(v); n == 0 || n > bbsMaxRunes {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// bbsMessage is one line of a board with who wrote it.
func (f *FriendService) bbsMessage(b store.BBS) (proto.Message, bool) {
	a, err := f.Store.RoleByID(b.AuthorRID)
	if err != nil {
		return nil, false
	}
	cfg, _ := f.Store.SettingsOf(a.RID)
	face := int64(a.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	return proto.Message{"bbs_id": b.ID, "bbs_type": b.Type, "reply_name": b.ReplyName, "msg": b.Msg, "time": b.Time, "rid": a.RID, "srv_id": a.SrvID,
		"face_id": face, "avatar_id": cfg.AvatarID, "lev": a.Lev, "sex": a.Sex, "name": a.Name, "face_update_time": 0, "face_file": ""}, true
}

func (f *FriendService) bbsList(owner int64, start, num int) ([]proto.Message, int64, error) {
	rows, total, err := f.Store.BBSList(owner, start, num)
	if err != nil {
		return nil, 0, err
	}
	out := []proto.Message{}
	for _, b := range rows {
		if m, ok := f.bbsMessage(b); ok {
			out = append(out, m)
		}
	}
	return out, total, nil
}

// registerSpace registers the personal space: 25800 home city, 25801 follow / unfollow, 25802 the most
// followed, 25805 / 25806 the honor wall, and the message board 25835-25841. The honor badges and the growth
// way have no source yet, so the wall only shows what the admin page grants and the growth way is empty.
func (f *FriendService) registerSpace(r *gateway.Router) {
	r.Handle(25800, func(s *gateway.Session, req proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		id := req.Int("city_id")
		if !validCity[id] {
			return s.Send(25800, proto.Message{"code": 0, "msg": "城市不存在", "city_id": id})
		}
		if err := f.Store.SetCityID(me.RID, id); err != nil {
			return err
		}
		return s.Send(25800, proto.Message{"code": 1, "msg": "设置成功", "city_id": id})
	})
	r.Handle(25801, func(s *gateway.Session, req proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		t, why := f.target(req.Int("rid"), req.Str("srv_id"))
		switch {
		case t == nil:
			return s.Send(25801, proto.Message{"code": 0, "msg": why, "flag": 0})
		case t.RID == me.RID:
			return s.Send(25801, proto.Message{"code": 0, "msg": "不能关注自己", "flag": 0})
		}
		now, err := f.Store.ToggleFollow(me.RID, t.RID, s.Now().Unix())
		if err != nil {
			return err
		}
		msg := "已取消关注"
		if now {
			msg = "关注成功"
		}
		return s.Send(25801, proto.Message{"code": 1, "msg": msg, "flag": b2i(now)})
	})
	r.Handle(25802, func(s *gateway.Session, _ proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		rank, err := f.Store.FansRank(me.SrvID, me.RID)
		if err != nil {
			return err
		}
		top, err := f.Store.FansTop(me.SrvID, fansListSize)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for i, row := range top {
			t, err := f.Store.RoleByID(row.RID)
			if err != nil {
				continue
			}
			cfg, _ := f.Store.SettingsOf(t.RID)
			face := int64(t.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			list = append(list, proto.Message{"rid": t.RID, "srv_id": t.SrvID, "name": t.Name, "lev": t.Lev, "face": face, "rank": i + 1, "avatar_bid": cfg.AvatarID,
				"fans_num": row.Fans, "look_id": cfg.LookID, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(25802, proto.Message{"rank": rank, "rank_list": list})
	})
	// the honor wall
	wall := func(rid int64) (proto.Message, error) {
		owned, worn, err := f.Store.BadgesOf(rid)
		if err != nil {
			return nil, err
		}
		have, use := []proto.Message{}, []proto.Message{}
		for _, b := range owned {
			have = append(have, proto.Message{"id": b.ID, "time": b.Time})
		}
		for pos := int64(1); pos <= 8; pos++ {
			if id, ok := worn[pos]; ok {
				use = append(use, proto.Message{"pos": pos, "id": id})
			}
		}
		return proto.Message{"point": 0, "honor_badges": have, "use_badges": use}, nil
	}
	r.Handle(25806, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		t, _ := f.target(req.Int("rid"), req.Str("srv_id"))
		if t == nil {
			return nil
		}
		m, err := wall(t.RID)
		if err != nil {
			return err
		}
		return s.Send(25806, m)
	})
	r.Handle(25805, func(s *gateway.Session, req proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		pos, id := req.Int("pos"), req.Int("id")
		if pos < 1 || pos > 8 {
			return s.Send(25805, proto.Message{"code": 0, "msg": "位置不对", "pos": pos, "id": id})
		}
		switch err := f.Store.WearBadge(me.RID, pos, id); {
		case err == store.ErrConflict:
			return s.Send(25805, proto.Message{"code": 0, "msg": "尚未获得该徽章", "pos": pos, "id": id})
		case err != nil:
			return err
		}
		return s.Send(25805, proto.Message{"code": 1, "msg": "", "pos": pos, "id": id})
	})
	// the message board
	r.Handle(25837, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		owner, why := f.target(req.Int("rid"), req.Str("srv_id"))
		if owner == nil {
			return s.Send(25837, proto.Message{"result": 0, "msg": why, "rid": req.Int("rid"), "srv_id": req.Str("srv_id"), "start": 0, "num": 0, "max_num": 0, "room_bbs_info": []proto.Message{}})
		}
		start, num := int(req.Int("start")), int(req.Int("num"))
		if num < 1 || num > 50 {
			num = 10
		}
		list, total, err := f.bbsList(owner.RID, max(start, 0), num)
		if err != nil {
			return err
		}
		return s.Send(25837, proto.Message{"result": 1, "msg": "", "rid": owner.RID, "srv_id": owner.SrvID, "start": start, "num": len(list), "max_num": total, "room_bbs_info": list})
	})
	// write or reply: the common checks and the push to the board's owner
	post := func(s *gateway.Session, cmd uint16, req proto.Message, replyTo int64) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(cmd, proto.Message{"result": 0, "msg": msg}) }
		owner, why := f.target(req.Int("rid"), req.Str("srv_id"))
		if owner == nil {
			return refuse(why)
		}
		fresh, err := f.Store.RoleByID(me.RID)
		if err != nil {
			return err
		}
		text := req.Str("msg")
		switch {
		case fresh.Lev < bbsOpenLev || owner.Lev < bbsOpenLev:
			return refuse("35级开启留言板功能")
		case !validBBSText(text):
			return refuse("留言内容不合法，最多36个字")
		}
		if last, err := f.Store.LastBBSTime(me.RID); err != nil {
			return err
		} else if left := last + bbsCooldown - s.Now().Unix(); left > 0 {
			return refuse("留言太频繁，请稍后再试")
		}
		b := store.BBS{OwnerRID: owner.RID, AuthorRID: me.RID, Type: 1, Msg: text, Time: s.Now().Unix()}
		if replyTo != 0 {
			orig, err := f.Store.BBSByID(replyTo)
			if err != nil || orig.OwnerRID != owner.RID {
				return refuse("留言不存在")
			}
			if me.RID != owner.RID && me.RID != orig.AuthorRID {
				return refuse("只有留言板的主人可以回复")
			}
			who, err := f.Store.RoleByID(orig.AuthorRID)
			if err != nil {
				return refuse("留言不存在")
			}
			b.Type, b.ReplyName = 2, who.Name
		} else if me.RID != owner.RID {
			cfg, err := f.Store.SettingsOf(owner.RID)
			if err != nil {
				return err
			}
			if cfg.BBSSet == 2 {
				if ok, err := f.Store.AreFriends(me.RID, owner.RID); err != nil {
					return err
				} else if !ok {
					return refuse("对方只允许好友留言")
				}
			}
		}
		id, err := f.Store.AddBBS(b, bbsBoardSize)
		if err != nil {
			return err
		}
		b.ID = id
		if err := s.Send(cmd, proto.Message{"result": 1, "msg": ""}); err != nil {
			return err
		}
		if sess := f.Hub.Online(owner.RID); sess != nil && owner.RID != me.RID {
			if m, ok := f.bbsMessage(b); ok {
				_, total, _ := f.Store.BBSList(owner.RID, 0, 1)
				sess.Send(25841, proto.Message{"max_num": total, "rid": owner.RID, "srv_id": owner.SrvID, "room_bbs_info": []proto.Message{m}})
			}
		}
		return nil
	}
	r.Handle(25835, func(s *gateway.Session, req proto.Message) error { return post(s, 25835, req, 0) })
	r.Handle(25836, func(s *gateway.Session, req proto.Message) error {
		if req.Int("bbs_id") == 0 {
			return s.Send(25836, proto.Message{"result": 0, "msg": "留言不存在"})
		}
		return post(s, 25836, req, req.Int("bbs_id"))
	})
	r.Handle(25838, func(s *gateway.Session, req proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		id := req.Int("bbs_id")
		b, err := f.Store.BBSByID(id)
		if err != nil {
			return s.Send(25838, proto.Message{"result": 0, "msg": "留言不存在", "bbs_id": id})
		}
		if me.RID != b.OwnerRID && me.RID != b.AuthorRID {
			return s.Send(25838, proto.Message{"result": 0, "msg": "不能删除别人的留言", "bbs_id": id})
		}
		if err := f.Store.DeleteBBS(id); err != nil {
			return err
		}
		return s.Send(25838, proto.Message{"result": 1, "msg": "", "bbs_id": id})
	})
	r.Handle(25839, func(s *gateway.Session, req proto.Message) error {
		me := s.Role()
		if me == nil {
			return nil
		}
		typ := req.Int("type")
		if typ != 1 && typ != 2 {
			return s.Send(25839, proto.Message{"result": 0, "msg": "参数错误", "type": typ})
		}
		if err := f.Store.SetBBSSet(me.RID, int(typ)); err != nil {
			return err
		}
		return s.Send(25839, proto.Message{"result": 1, "msg": "设置成功", "type": typ})
	})
}
