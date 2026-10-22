package game

import (
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func friendList(c *client) proto.Message {
	c.send(13300, proto.Message{})
	return c.recv(13300)
}

func entryOf(list proto.Message, rid int64) proto.Message {
	for _, e := range list.Arr("friend_list") {
		if e.Int("rid") == rid {
			return e
		}
	}
	return nil
}

func giftReq(c *client, code int, f *store.Role) proto.Message {
	c.send(13316, proto.Message{"rid": f.RID, "srv_id": "symlf_1", "code": code})
	return c.recv(13316)
}

func coinOf(e *env, rid int64) int64 {
	got, _ := e.st.RoleAssets(rid)
	id, _ := AssetLabelID("coin")
	return got[id]
}

func pointsOf(e *env, rid int64) int64 {
	got, _ := e.st.RoleAssets(rid)
	id, _ := AssetLabelID("friend_point")
	return got[id]
}

func TestFriendGiftSendAndDraw(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	befriend(t, ca, a, cb, b)

	// at the start: 10 sends left, 0 drawn, 30 allowed, no flags
	l := friendList(ca)
	if l.Int("present_count") != 10 || l.Int("draw_count") != 0 || l.Int("draw_all") != 30 {
		t.Fatalf("counters: %v", l)
	}
	if en := entryOf(l, b.RID); en.Int("is_present") != 0 || en.Int("is_draw") != 0 {
		t.Fatalf("flags: %v", en)
	}

	// a sends b a gift: the answer says so, a is paid coin, b hears of it
	r := giftReq(ca, 0, b)
	if r.Int("code") != 1 || r.Int("is_present") != 1 || r.Int("is_draw") != 0 || r.Int("present_count") != 9 || r.Int("rid") != b.RID || r.Str("msg") == "" {
		t.Fatalf("send: %v", r)
	}
	if as := ca.recv(10302); as.Str("coin") != "1000" {
		t.Fatalf("a is paid coin for sending: %v", as)
	}
	push := cb.recv(13302)
	if push.Int("rid") != a.RID || push.Int("is_draw") != 1 || push.Int("is_present") != 0 {
		t.Fatalf("b is told a gift waits: %v", push)
	}
	if coinOf(e, a.RID) != 1000 || pointsOf(e, b.RID) != 0 {
		t.Fatalf("ledgers: %d %d", coinOf(e, a.RID), pointsOf(e, b.RID))
	}

	// once a day per friend
	if r := giftReq(ca, 0, b); r.Int("code") != 0 || r.Str("msg") != "今天已经赠送过了" || r.Int("is_present") != 1 {
		t.Fatalf("second send: %v", r)
	}
	if coinOf(e, a.RID) != 1000 {
		t.Fatalf("a refused send paid: %d", coinOf(e, a.RID))
	}

	// b sees the gift in its list, draws it and gets friend points
	if en := entryOf(friendList(cb), a.RID); en.Int("is_draw") != 1 {
		t.Fatalf("b's list: %v", en)
	}
	r = giftReq(cb, 1, a)
	if r.Int("code") != 1 || r.Int("is_draw") != 0 || r.Int("draw_count") != 1 {
		t.Fatalf("draw: %v", r)
	}
	if as := cb.recv(10302); as.Int("friend_point") != 10 {
		t.Fatalf("b's resources after the draw: %v", as)
	}
	if pointsOf(e, b.RID) != 10 {
		t.Fatalf("friend points: %d", pointsOf(e, b.RID))
	}
	// no second draw of the same gift
	if r := giftReq(cb, 1, a); r.Int("code") != 0 || r.Str("msg") != "没有可领取的礼物" {
		t.Fatalf("second draw: %v", r)
	}
	if pointsOf(e, b.RID) != 10 {
		t.Fatal("a refused draw paid")
	}

	// the next day everything starts again, and an undrawn gift of yesterday is gone
	giftReq(cb, 0, a)
	cb.recv(10302)
	ca.recv(13302)
	e.advance(24 * time.Hour)
	l = friendList(ca)
	if l.Int("present_count") != 10 || entryOf(l, b.RID).Int("is_present") != 0 || entryOf(l, b.RID).Int("is_draw") != 0 {
		t.Fatalf("a new day: %v", l)
	}
	if r := giftReq(ca, 1, b); r.Int("code") != 0 {
		t.Fatalf("a gift of yesterday must not be drawn today: %v", r)
	}
}

func TestFriendGiftRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	_ = cb
	// not friends: no gift, nothing paid
	if r := giftReq(ca, 0, b); r.Int("code") != 0 || r.Str("msg") != "对方不是你的好友" {
		t.Fatalf("not friends: %v", r)
	}
	if r := giftReq(ca, 0, a); r.Int("code") != 0 { // to oneself
		t.Fatalf("to self: %v", r)
	}
	ca.send(13316, proto.Message{"rid": b.RID, "srv_id": "other_2", "code": 0})
	if r := ca.recv(13316); r.Int("code") != 0 || r.Str("msg") != "暂不支持跨服好友" {
		t.Fatalf("other server: %v", r)
	}
	ca.send(13316, proto.Message{"rid": b.RID, "srv_id": "symlf_1", "code": 7})
	if r := ca.recv(13316); r.Int("code") != 0 || r.Str("msg") != "操作不合法" {
		t.Fatalf("bad code: %v", r)
	}
	if coinOf(e, a.RID) != 0 {
		t.Fatalf("refusals paid: %d", coinOf(e, a.RID))
	}
}

func TestFriendGiftDailySendLimitAndBatch(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	// twelve friends, made directly in the store
	var friends []*store.Role
	for i := 0; i < 12; i++ {
		acc, _ := e.st.FindOrCreateAccount("symlf", string(rune('A'+i))+"-friend")
		r, err := e.st.CreateRole(acc.ID, "symlf_1", "友"+string(rune('a'+i)), 0, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.RequestFriend(r.RID, a.RID); err != nil {
			t.Fatal(err)
		}
		if err := e.st.AcceptFriendRequest(r.RID, a.RID, 50); err != nil {
			t.Fatal(err)
		}
		friends = append(friends, r)
	}
	list := []proto.Message{}
	for _, f := range friends {
		list = append(list, proto.Message{"rid": f.RID, "srv_id": "symlf_1"})
	}
	ca.send(13317, proto.Message{"code": 0, "list": list})
	r := ca.recv(13317)
	if r.Int("code") != 1 || len(r.Arr("list")) != 10 {
		t.Fatalf("a batch stops at the daily limit of 10: %d sent, %v", len(r.Arr("list")), r.Str("msg"))
	}
	ca.recv(10302)
	l := ca.recv(13300) // the list is sent again so the client sees the flags
	if l.Int("present_count") != 0 {
		t.Fatalf("counters after the batch: %v", l)
	}
	sent := 0
	for _, en := range l.Arr("friend_list") {
		sent += int(en.Int("is_present"))
	}
	if sent != 10 || coinOf(e, a.RID) != 10*coinEach {
		t.Fatalf("flags %d, coin %d", sent, coinOf(e, a.RID))
	}
	// the limit also stops a single send, with the client's own message
	if r := giftReq(ca, 0, friends[11]); r.Int("code") != 0 || r.Str("msg") != "今日赠送友情点已达上限!" {
		t.Fatalf("send past the limit: %v", r)
	}
	// and a batch with nothing left to send says so without paying
	ca.send(13317, proto.Message{"code": 0, "list": list})
	if r := ca.recv(13317); r.Int("code") != 0 || len(r.Arr("list")) != 0 {
		t.Fatalf("empty batch: %v", r)
	}
}

func TestFriendGiftDrawLimitPaysCoinInstead(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	cb, b := e.enter("b", "乙")
	day := dayKey(time.Unix(0, e.clk.Load()))
	// 31 friends each sent b a gift today (made in the store: a role may send only 10 a day)
	var from []*store.Role
	for i := 0; i < 31; i++ {
		acc, _ := e.st.FindOrCreateAccount("symlf", "s"+string(rune('A'+i)))
		r, err := e.st.CreateRole(acc.ID, "symlf_1", "送"+string(rune('a'+i)), 0, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.SendGift(r.RID, b.RID, day, 10, nil, nil); err != nil {
			t.Fatal(err)
		}
		from = append(from, r)
	}
	for i, f := range from {
		cb.send(13316, proto.Message{"rid": f.RID, "srv_id": "symlf_1", "code": 1})
		r := cb.recv(13316)
		if r.Int("code") != 1 {
			t.Fatalf("draw %d: %v", i+1, r)
		}
		cb.recv(10302)
		if i < 30 && !strings.Contains(r.Str("msg"), "友情点") || i == 30 && !strings.Contains(r.Str("msg"), "贝里") {
			t.Fatalf("draw %d message: %q", i+1, r.Str("msg"))
		}
	}
	if pointsOf(e, b.RID) != 30*friendPointsEach || coinOf(e, b.RID) != coinEach {
		t.Fatalf("30 draws give friend points, the 31st only coin: points %d, coin %d", pointsOf(e, b.RID), coinOf(e, b.RID))
	}
}

func TestFriendGiftBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(13316, proto.Message{"rid": 1, "srv_id": "symlf_1", "code": 0})
	c.send(13317, proto.Message{"code": 0, "list": []proto.Message{}})
	c.expectNothing()
}
