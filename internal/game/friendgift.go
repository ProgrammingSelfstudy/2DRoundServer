package game

import (
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Friend gifts (friend points). What the client states itself (friend_award_panel.lua):
// "each person may send 10 a day and draw 30; past the draw limit a draw only gives coin;
// sending also gives the sender coin". The amounts are not in the client, they are ours.
const (
	giftSendsPerDay  = 10   // from the client's text
	giftDrawsPerDay  = 30   // from the client's text
	friendPointsEach = 10   // friend points for one drawn gift: our policy
	coinEach         = 1000 // coin for sending a gift, and for a draw past the limit: our policy
)

func giftAssets(point bool) map[int64]int64 {
	name, n := "coin", int64(coinEach)
	if point {
		name, n = "friend_point", friendPointsEach
	}
	id, _ := AssetLabelID(name)
	return map[int64]int64{id: n}
}

// sendList answers 13300 for a role: its friends with the gift flags and the day's counters.
// present_count is how many gifts it may still send today (the client treats 0 as "limit
// reached"), draw_count how many it drew today, draw_all the most it may draw.
func (f *FriendService) sendList(s *gateway.Session, me *store.Role, friends []store.Role) error {
	g, err := f.Store.GiftState(me.RID, dayKey(s.Now()))
	if err != nil {
		return err
	}
	return s.Send(13300, proto.Message{
		"present_count": max(0, giftSendsPerDay-g.SentCount), "draw_count": g.DrawCount, "draw_all": giftDrawsPerDay,
		"friend_list": roleEntries(friends, func(r *store.Role) proto.Message { return f.friendEntry(r, &g) }),
	})
}

// sendGift sends one gift to a friend: msg says why not when ok is false.
func (f *FriendService) sendGift(s *gateway.Session, me *store.Role, rid int64) (msg string, ok bool, err error) {
	if is, err := f.Store.AreFriends(me.RID, rid); err != nil || !is {
		return "对方不是你的好友", false, err
	}
	switch err := f.Store.SendGift(me.RID, rid, dayKey(s.Now()), giftSendsPerDay, giftAssets(false), nil); {
	case errors.Is(err, store.ErrGiftLimit):
		return "今日赠送友情点已达上限!", false, nil
	case errors.Is(err, store.ErrConflict):
		return "今天已经赠送过了", false, nil
	case err != nil:
		return "", false, err
	}
	// the friend, if online, sees a gift waiting from this role
	if sess := f.Hub.Online(rid); sess != nil {
		if g, err := f.Store.GiftState(rid, dayKey(s.Now())); err == nil {
			if err := sess.Send(13302, f.friendEntry(me, &g)); err != nil {
				f.Log.Printf("friend: gift push to role %d: %v", rid, err)
			}
		}
	}
	return "赠送成功，获得贝里", true, nil
}

// drawGift draws the gift a friend sent today. Past giftDrawsPerDay it pays coin instead.
func (f *FriendService) drawGift(s *gateway.Session, me *store.Role, rid int64) (msg string, ok bool, err error) {
	over := false
	_, err = f.Store.DrawGift(rid, me.RID, dayKey(s.Now()), func(drawnToday int) (map[int64]int64, map[int64]int64) {
		over = drawnToday >= giftDrawsPerDay
		return giftAssets(!over), nil
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "没有可领取的礼物", false, nil
	case err != nil:
		return "", false, err
	case over:
		return "领取次数已用完，获得贝里", true, nil
	}
	return "领取成功，获得友情点", true, nil
}

// flagsOf is the is_present / is_draw of one friend for a role, from a fresh read.
func (f *FriendService) flagsOf(s *gateway.Session, me *store.Role, rid int64) (present, draw int, g store.GiftState, err error) {
	g, err = f.Store.GiftState(me.RID, dayKey(s.Now()))
	return b2i(g.Sent[rid]), b2i(g.Waiting[rid]), g, err
}

// gift answers 13316: code 0 sends a gift to the friend, code 1 draws the one it sent.
func (f *FriendService) gift(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rid, srvID, code := req.Int("rid"), req.Str("srv_id"), req.Int("code")
	var msg string
	var ok bool
	var err error
	switch {
	case srvID != f.SrvID:
		msg = "暂不支持跨服好友"
	case code == 0:
		msg, ok, err = f.sendGift(s, me, rid)
	case code == 1:
		msg, ok, err = f.drawGift(s, me, rid)
	default:
		msg = "操作不合法"
	}
	if err != nil {
		return err
	}
	present, draw, g, err := f.flagsOf(s, me, rid)
	if err != nil {
		return err
	}
	if err := s.Send(13316, proto.Message{
		"rid": rid, "srv_id": srvID, "present_count": max(0, giftSendsPerDay-g.SentCount), "draw_count": g.DrawCount,
		"code": b2i(ok), "msg": msg, "type": 0, "is_present": present, "is_draw": draw,
	}); err != nil {
		return err
	}
	if ok {
		if err := sendAssets(s, f.Store, me.RID); err != nil { // coin / friend points changed
			return err
		}
		if code == 0 {
			if err := featCount(s, f.Store, me.RID, counterPresent, featFriendPresent, 1); err != nil {
				return err
			}
			return questEvent(s, f.Store, me.RID, evtFriendPresent, 1)
		}
		return questEvent(s, f.Store, me.RID, evtGetFriendPt, friendPointsEach)
	}
	return nil
}

// giftBatch answers 13317: the same for a list of friends. It stops sending at the daily
// limit. The client does not read the list in the answer, so the friend list is sent again
// afterwards to bring the flags and the counters up to date.
func (f *FriendService) giftBatch(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	code := req.Int("code")
	done := []proto.Message{}
	msg := "没有可操作的好友"
	for _, e := range req.Arr("list") {
		if e.Str("srv_id") != f.SrvID {
			continue
		}
		var m string
		var ok bool
		var err error
		switch code {
		case 0:
			m, ok, err = f.sendGift(s, me, e.Int("rid"))
		case 1:
			m, ok, err = f.drawGift(s, me, e.Int("rid"))
		}
		if err != nil {
			return err
		}
		if ok {
			present, draw, _, err := f.flagsOf(s, me, e.Int("rid"))
			if err != nil {
				return err
			}
			done = append(done, proto.Message{"rid": e.Int("rid"), "srv_id": f.SrvID, "is_present": present, "is_draw": draw})
		} else if m == "今日赠送友情点已达上限!" {
			msg = m
			break
		}
	}
	if len(done) > 0 {
		msg = "赠送成功，获得贝里"
		if code == 1 {
			msg = "领取成功"
		}
	}
	if err := s.Send(13317, proto.Message{"code": b2i(len(done) > 0), "msg": msg, "type": 0, "list": done}); err != nil {
		return err
	}
	if len(done) == 0 {
		return nil
	}
	if err := sendAssets(s, f.Store, me.RID); err != nil {
		return err
	}
	friends, err := f.Store.Friends(me.RID)
	if err != nil {
		return err
	}
	if err := f.sendList(s, me, friends); err != nil {
		return err
	}
	if code == 0 {
		if err := featCount(s, f.Store, me.RID, counterPresent, featFriendPresent, int64(len(done))); err != nil {
			return err
		}
		return questEvent(s, f.Store, me.RID, evtFriendPresent, int64(len(done)))
	}
	return questEvent(s, f.Store, me.RID, evtGetFriendPt, friendPointsEach*int64(len(done)))
}
