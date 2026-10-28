package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// maxBlack is how many roles one role may put on its black list. The original limit is unknown, so this is
// our policy.
const maxBlack = 50

// registerBlack registers the black list: 13330 the list, 13332 add (which ends a friendship with the role),
// 13333 take off, 13315 whether a role has a team (always no: there is no friend team). The roles on the list
// cannot send a friend request to the one who listed them. The chat is not filtered. Our rules, not captured.
func (f *FriendService) registerBlack(r *gateway.Router) {
	r.Handle(13330, f.blackList)
	r.OnEnter(f.blackList)
	r.Handle(13332, f.blackAdd)
	r.Handle(13333, f.blackRemove)
	r.Handle(13315, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(13315, proto.Message{"code": 0})
	})
}

// blackEntry is the record of 13330 and 13331; the two name the last-seen time differently.
func (f *FriendService) blackEntry(r *store.Role, seenField string) proto.Message {
	return proto.Message{
		"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "sex": r.Sex, "career": r.Career, "face_id": r.FaceID,
		seenField: r.LastLogoutAt, "is_online": b2i(f.Hub.Online(r.RID) != nil),
	}
}

func (f *FriendService) blackList(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rs, err := f.Store.Blacklist(me.RID)
	if err != nil {
		return err
	}
	return s.Send(13330, proto.Message{"black_list": roleEntries(rs, func(r *store.Role) proto.Message { return f.blackEntry(r, "login_out_time") })})
}

func (f *FriendService) blackAdd(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rid, srvID := req.Int("rid"), req.Str("srv_id")
	reply := func(ok bool, msg string) error {
		return s.Send(13332, proto.Message{"code": b2i(ok), "msg": msg, "rid": rid, "srv_id": srvID})
	}
	t, why := f.target(rid, srvID)
	switch {
	case t == nil:
		return reply(false, why)
	case t.RID == me.RID:
		return reply(false, "不能把自己加入黑名单")
	}
	wasFriend, err := f.Store.AddBlack(me.RID, t.RID, maxBlack)
	switch {
	case errors.Is(err, store.ErrConflict):
		return reply(false, "对方已在黑名单中")
	case errors.Is(err, store.ErrFriendsFull):
		return reply(false, "黑名单已满")
	case err != nil:
		return err
	}
	if wasFriend {
		f.push(t.RID, 13308, proto.Message{"rid": me.RID, "srv_id": f.SrvID})
	}
	return reply(true, "已加入黑名单")
}

func (f *FriendService) blackRemove(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rid, srvID := req.Int("rid"), req.Str("srv_id")
	gone, err := f.Store.RemoveBlack(me.RID, rid)
	if err != nil {
		return err
	}
	msg := "已移出黑名单"
	if !gone {
		msg = "对方不在黑名单中"
	}
	return s.Send(13333, proto.Message{"code": b2i(gone), "msg": msg, "rid": rid, "srv_id": srvID})
}
