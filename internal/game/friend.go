package game

import (
	"errors"
	"log"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// maxFriends is how many friends one role may have. The original limit is
// unknown, so this is our policy.
const maxFriends = 50

// FriendService implements the friend list: requests, accepting and refusing,
// deleting, searching and recommending, plus the online / offline notices.
// Friends live on one server; the client's (srv_id, rid) pairs must name this
// gateway's server. Gifts, intimacy and the other per-friend numbers are not
// implemented and are sent as 0.
type FriendService struct {
	Store *store.Store
	SrvID string
	Hub   *gateway.Hub
	Log   *log.Logger
}

func (f *FriendService) Register(r *gateway.Router) {
	r.Handle(13300, f.list)
	r.Handle(13311, f.requests)
	r.Handle(13303, f.add)
	r.Handle(13306, f.batchAdd)
	r.Handle(13305, f.accept)
	r.Handle(13334, f.acceptAll)
	r.Handle(13312, f.refuseAll)
	r.Handle(13307, f.remove)
	r.Handle(13314, f.search)
	r.Handle(13320, f.recommend)
	r.Handle(13316, f.gift)
	r.Handle(13317, f.giftBatch)
	f.registerBlack(r)
	f.registerProfile(r)
	f.registerSpace(r)
}

// ---- field builders ----

// friendEntry is the per-friend record of 13300 / 13302 / 13310. What has no
// source yet (power, intimacy, guild, partner, gift flags, avatar) is 0 or "".
//
// g is the gift state of the role that gets the entry (nil = no gifts today): is_present says
// that role already sent r a gift today, is_draw that r's gift to it waits to be drawn.
func (f *FriendService) friendEntry(r *store.Role, g *store.GiftState) proto.Message {
	online := f.Hub.Online(r.RID) != nil
	m := proto.Message{
		"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "sex": r.Sex, "career": r.Career, "face_id": r.FaceID,
		"login_time": r.LastLoginAt, "login_out_time": r.LastLogoutAt, "is_online": b2i(online),
	}
	if g != nil {
		m["is_present"], m["is_draw"] = b2i(g.Sent[r.RID]), b2i(g.Waiting[r.RID])
	}
	return m
}

// requestEntry is the record of 13311 (a pending request) and the 13304 push.
func requestEntry(r *store.Role) proto.Message {
	return proto.Message{"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "sex": r.Sex, "career": r.Career, "lev": r.Lev, "face_id": r.FaceID}
}

func roleEntries(rs []store.Role, build func(*store.Role) proto.Message) []proto.Message {
	out := make([]proto.Message, 0, len(rs))
	for i := range rs {
		out = append(out, build(&rs[i]))
	}
	return out
}

// push sends to a role if it is online; a failed push is only logged.
func (f *FriendService) push(rid int64, cmd uint16, msg proto.Message) {
	if sess := f.Hub.Online(rid); sess != nil {
		if err := sess.Send(cmd, msg); err != nil {
			f.Log.Printf("friend: push %d to role %d: %v", cmd, rid, err)
		}
	}
}

// target loads the role a request names, which must be on this server and usable.
func (f *FriendService) target(rid int64, srvID string) (*store.Role, string) {
	if srvID != f.SrvID {
		return nil, "暂不支持跨服好友"
	}
	r, err := f.Store.RoleByID(rid)
	if err != nil || r.SrvID != f.SrvID || r.Status != 1 {
		return nil, "玩家不存在"
	}
	return r, ""
}

// ---- queries ----

func (f *FriendService) list(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	friends, err := f.Store.Friends(me.RID)
	if err != nil {
		return err
	}
	return f.sendList(s, me, friends)
}

func (f *FriendService) requests(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rs, err := f.Store.FriendRequestsTo(me.RID)
	if err != nil {
		return err
	}
	return s.Send(13311, proto.Message{"friend_req_list": roleEntries(rs, requestEntry)})
}

func (f *FriendService) search(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	found := []store.Role{}
	if name := req.Str("name"); name != "" {
		var err error
		if found, err = f.Store.SearchRolesByName(f.SrvID, name, 26); err != nil {
			return err
		}
	}
	return s.Send(13314, proto.Message{"role_list": roleEntries(found, requestEntry)})
}

func (f *FriendService) recommend(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rs, err := f.Store.RecommendFriends(f.SrvID, me.RID, 10)
	if err != nil {
		return err
	}
	return s.Send(13320, proto.Message{"recommend_list": roleEntries(rs, requestEntry)})
}

// ---- requests ----

// ask sends a friend request from me to the role rid; msg says why not when ok is false.
func (f *FriendService) ask(me *store.Role, rid int64, srvID string) (msg string, ok bool, err error) {
	t, why := f.target(rid, srvID)
	switch {
	case t == nil:
		return why, false, nil
	case t.RID == me.RID:
		return "不能添加自己为好友", false, nil
	}
	if is, err := f.Store.AreFriends(me.RID, t.RID); err != nil || is {
		return "对方已经是你的好友", false, err
	}
	if blocked, err := f.Store.IsBlacklisted(me.RID, t.RID); err != nil || blocked {
		return "对方在你的黑名单中", false, err
	}
	if blocked, err := f.Store.IsBlacklisted(t.RID, me.RID); err != nil || blocked {
		return "对方拒绝了你的好友申请", false, err
	}
	for _, c := range []struct {
		rid int64
		msg string
	}{{me.RID, "你的好友数量已达上限"}, {t.RID, "对方的好友数量已达上限"}} {
		n, err := f.Store.CountFriends(c.rid)
		if err != nil {
			return "", false, err
		}
		if n >= maxFriends {
			return c.msg, false, nil
		}
	}
	if back, err := f.Store.HasFriendRequest(t.RID, me.RID); err != nil || back {
		return "对方已向你发出申请，请在申请列表中同意", false, err
	}
	switch err := f.Store.RequestFriend(me.RID, t.RID); {
	case errors.Is(err, store.ErrConflict):
		return "已发送过申请，请等待对方处理", false, nil
	case err != nil:
		return "", false, err
	}
	f.push(t.RID, 13304, requestEntry(me))
	return "好友申请已发送", true, nil
}

func (f *FriendService) add(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	msg, ok, err := f.ask(me, req.Int("rid"), req.Str("srv_id"))
	if err != nil {
		return err
	}
	return s.Send(13303, proto.Message{"code": b2i(ok), "msg": msg})
}

func (f *FriendService) batchAdd(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	sent := 0
	for _, e := range req.Arr("role_ids") {
		_, ok, err := f.ask(me, e.Int("rid"), e.Str("srv_id"))
		if err != nil {
			return err
		}
		if ok {
			sent++
		}
	}
	msg := "没有可发送的好友申请"
	if sent > 0 {
		msg = "好友申请已发送"
	}
	return s.Send(13306, proto.Message{"code": b2i(sent > 0), "msg": msg})
}

// befriend accepts the request from rid to me and tells both sides (13310).
func (f *FriendService) befriend(me *store.Role, rid int64) (string, bool, error) {
	switch err := f.Store.AcceptFriendRequest(rid, me.RID, maxFriends); {
	case errors.Is(err, store.ErrNotFound):
		return "该申请已失效", false, nil
	case errors.Is(err, store.ErrFriendsFull):
		return "好友数量已达上限", false, nil
	case err != nil:
		return "", false, err
	}
	other, err := f.Store.RoleByID(rid)
	if err != nil {
		return "", false, err
	}
	f.push(me.RID, 13310, f.friendEntry(other, nil))
	f.push(other.RID, 13310, f.friendEntry(me, nil))
	return "已添加好友", true, nil
}

func (f *FriendService) accept(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rid, srvID := req.Int("rid"), req.Str("srv_id")
	reply := func(ok bool, msg string) error {
		return s.Send(13305, proto.Message{"rid": rid, "srv_id": srvID, "code": b2i(ok), "msg": msg})
	}
	if srvID != f.SrvID {
		return reply(false, "暂不支持跨服好友")
	}
	if req.Int("agreed") != 1 {
		if _, err := f.Store.DeclineFriendRequest(rid, me.RID); err != nil {
			return err
		}
		return reply(false, "已拒绝申请") // code 0 keeps the client from opening a chat with them
	}
	msg, ok, err := f.befriend(me, rid)
	if err != nil {
		return err
	}
	return reply(ok, msg)
}

func (f *FriendService) acceptAll(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	done := []proto.Message{}
	for _, e := range req.Arr("role_ids") {
		if e.Str("srv_id") != f.SrvID {
			continue
		}
		if _, ok, err := f.befriend(me, e.Int("rid")); err != nil {
			return err
		} else if ok {
			done = append(done, proto.Message{"rid": e.Int("rid"), "srv_id": f.SrvID})
		}
	}
	return s.Send(13334, proto.Message{"role_ids": done})
}

func (f *FriendService) refuseAll(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	if _, err := f.Store.DeclineAllFriendRequests(me.RID); err != nil {
		return err
	}
	return s.Send(13312, proto.Message{"code": 1, "msg": "已全部拒绝"})
}

// ---- deleting ----

func (f *FriendService) remove(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rid, srvID := req.Int("rid"), req.Str("srv_id")
	reply := func(ok bool, msg string) error {
		return s.Send(13307, proto.Message{"code": b2i(ok), "msg": msg, "rid": rid, "srv_id": srvID})
	}
	if srvID != f.SrvID {
		return reply(false, "对方不是你的好友")
	}
	gone, err := f.Store.DeleteFriend(me.RID, rid)
	if err != nil {
		return err
	}
	if !gone {
		return reply(false, "对方不是你的好友")
	}
	f.push(rid, 13308, proto.Message{"rid": me.RID, "srv_id": f.SrvID})
	return reply(true, "删除成功")
}

// ---- online state ----

// announce tells the online friends of a role that it came online or went
// offline (13301).
func (f *FriendService) announce(r *store.Role, online bool) {
	friends, err := f.Store.Friends(r.RID)
	if err != nil {
		f.Log.Printf("friend: announce for role %d: %v", r.RID, err)
		return
	}
	msg := proto.Message{"rid": r.RID, "srv_id": r.SrvID, "login_time": r.LastLoginAt, "is_online": b2i(online), "login_out_time": r.LastLogoutAt}
	for _, fr := range friends {
		f.push(fr.RID, 13301, msg)
	}
}

// Entered is called when a role has logged in (10192 / 10193).
func (f *FriendService) Entered(r *store.Role) {
	if fresh, err := f.Store.RoleByID(r.RID); err == nil {
		f.announce(fresh, true)
	}
}

// Disconnected is the gateway's OnDisconnect hook. A role replaced by a newer
// login of itself is still online, so nothing is announced for it.
func (f *FriendService) Disconnected(s *gateway.Session) {
	r := s.Role()
	if r == nil || f.Hub.Online(r.RID) != nil {
		return
	}
	if err := f.Store.TouchRoleLogout(r.RID); err != nil {
		f.Log.Printf("friend: logout time of role %d: %v", r.RID, err)
		return
	}
	if fresh, err := f.Store.RoleByID(r.RID); err == nil {
		f.announce(fresh, false)
	}
}
