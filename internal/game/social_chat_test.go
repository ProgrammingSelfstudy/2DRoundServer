package game

import (
	"errors"
	"testing"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestH5SocialBlacklistFiltersPrivateAndChannels(t *testing.T) {
	e := newSocialEnv(t)
	ca, a := e.enter(store.PlatformH5, "a", "甲")
	cb, b := e.enter(store.PlatformH5, "b", "乙")
	cc, _ := e.enter(store.PlatformH5, "c", "丙")
	gid, err := e.st.CreateGuild(store.Guild{SrvID: a.SrvID, Name: "测试公会", LeaderRID: a.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.JoinGuild(gid, b.RID, 2, 15); err != nil {
		t.Fatal(err)
	}
	say := func(channel int64, text string) {
		ca.send(12762, proto.Message{"channel": channel, "len": 0, "msg": text, "sign": chatSign(a.RID, a.SrvID, channel, 0, text)})
		if m := ca.recv(12761); m.Str("msg") != text {
			t.Fatalf("echo: %v", m)
		}
		if r := ca.recv(12762); r.Int("code") != 1 {
			t.Fatalf("send: %v", r)
		}
		if channel == chatWorld {
			cc.recv(12761)
		}
	}
	for _, ch := range []int64{chatWorld, chatGuild} {
		say(ch, "拉黑前的频道消息")
		cb.recv(12761)
	}
	cb.send(13332, rid(a))
	if r := cb.recv(13332); r.Int("code") != 1 {
		t.Fatalf("block: %v", r)
	}
	private := func(from *client, to *store.Role) {
		from.send(12720, proto.Message{"to_rid": to.RID, "to_srv_id": to.SrvID, "len": 0, "msg": "私聊"})
		if r := from.recv(12720); r.Int("code") != 0 || r.Str("msg") != privateChatUnavailable {
			t.Fatalf("blocked chat: %v", r)
		}
	}
	private(ca, b)
	cb.expectNothing()
	private(cb, a)
	ca.expectNothing()
	for _, ch := range []int64{chatWorld, chatGuild} {
		say(ch, "拉黑后的频道消息")
		cb.send(12766, proto.Message{"channel": ch})
		if r := cb.recv(12766); len(r.Arr("msg_list")) != 0 {
			t.Fatalf("blocked speaker in history %d: %v", ch, r)
		}
	}
	cc.expectNothing() // neither guild post should reach an outsider
	cb.send(12762, proto.Message{"channel": chatGuild, "msg": "单向屏蔽", "sign": chatSign(b.RID, b.SrvID, chatGuild, 0, "单向屏蔽")})
	if result := ca.recv(12761); result.Str("msg") != "单向屏蔽" {
		t.Fatalf("the blocked speaker did not receive the blocker's message: %v", result)
	}
	cb.recv(12761)
	cb.recv(12762)
	cb.send(12766, proto.Message{"channel": chatGuild})
	if history := cb.recv(12766).Arr("msg_list"); len(history) != 1 || history[0].Str("msg") != "单向屏蔽" {
		t.Fatalf("filtered more than the blocked speaker: %v", history)
	}

	cb.send(13333, proto.Message{"rid": a.RID, "srv_id": "other_1"})
	if r := cb.recv(13333); r.Int("code") != 0 {
		t.Fatalf("unblock on wrong server: %v", r)
	}
	private(ca, b)
	cb.send(13333, rid(a))
	if r := cb.recv(13333); r.Int("code") != 1 {
		t.Fatalf("unblock: %v", r)
	}
	ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": b.SrvID, "len": 0, "msg": "解除后私聊"})
	if m := cb.recv(12721); m.Str("msg") != "解除后私聊" {
		t.Fatalf("unblocked DM: %v", m)
	}
	ca.recv(12721)
	ca.recv(12720)
}

func TestH5SocialOfflineDeliveryKeepsFailedSnapshotAndNewArrivals(t *testing.T) {
	e := newSocialEnv(t)
	_, a := e.role(store.PlatformH5, "a", "甲")
	_, b := e.role(store.PlatformH5, "b", "乙")
	_, c := e.role(store.PlatformH5, "c", "丙")
	queue := func(from int64, text string) error {
		return e.st.AddOfflineChat(from, b.RID, 0, text, chatOfflineKeep)
	}
	if err := queue(a.RID, "拉黑前排队"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AddOfflineChat(b.RID, a.RID, 0, "反方向排队", chatOfflineKeep); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBlack(a.RID, b.RID, maxBlack); err != nil {
		t.Fatal(err)
	}
	if waiting, err := e.st.TakeOfflineChat(a.RID); err != nil || len(waiting) != 0 {
		t.Fatalf("reverse queue wasn't purged: %v, %v", waiting, err)
	}
	if err := queue(a.RID, "拉黑后排队"); !errors.Is(err, store.ErrSocialBlocked) {
		t.Fatalf("queued a blocked message: %v", err)
	}
	if err := e.st.RequestFriend(b.RID, a.RID); !errors.Is(err, store.ErrSocialBlocked) {
		t.Fatalf("stored a blocked friend request: %v", err)
	}
	if removed, err := e.st.RemoveBlack(a.RID, b.RID); err != nil || !removed {
		t.Fatalf("unblock: %v %v", removed, err)
	}
	if err := queue(c.RID, "离线消息"); err != nil {
		t.Fatal(err)
	}
	disconnected := errors.New("connection closed before the push")
	check := func(waiting []store.OfflineChat, text string) {
		t.Helper()
		if len(waiting) != 1 || waiting[0].Msg != text || waiting[0].FromRID != c.RID {
			t.Fatalf("want %q from %d, got %v", text, c.RID, waiting)
		}
	}
	if err := e.st.DeliverOfflineChat(b.RID, func(waiting []store.OfflineChat) error {
		check(waiting, "离线消息")
		return disconnected
	}); !errors.Is(err, disconnected) {
		t.Fatalf("failed delivery: %v", err)
	}
	if err := e.st.DeliverOfflineChat(b.RID, func(waiting []store.OfflineChat) error {
		check(waiting, "离线消息")
		return queue(c.RID, "发送期间新消息")
	}); err != nil {
		t.Fatal(err)
	}
	waiting, err := e.st.TakeOfflineChat(b.RID)
	if err != nil {
		t.Fatal(err)
	}
	check(waiting, "发送期间新消息")
}

func TestH5SocialOfflineChatOnLogin(t *testing.T) {
	e := newSocialEnv(t)
	sender, a := e.enter(store.PlatformH5, "a", "甲")
	account, b := e.role(store.PlatformH5, "b", "乙")
	other, c := e.enter(store.PlatformH5, "c", "丙")
	queue := func(from *client, text string, flag int) {
		t.Helper()
		from.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": b.SrvID, "len": flag, "msg": text})
		if echo := from.recv(12721); echo.Int("flag") != 1 || echo.Int("rid") != b.RID || echo.Int("len") != int64(flag) || echo.Str("msg") != text {
			t.Fatalf("offline send echo: %v", echo)
		}
		if reply := from.recv(12720); reply.Int("code") != 1 {
			t.Fatalf("offline send: %v", reply)
		}
	}
	queue(sender, "拉黑前排队", 0)
	queue(other, "第一条", 0)
	queue(other, "第二条", 2)
	sender.send(13332, rid(b))
	if block := sender.recv(13332); block.Int("code") != 1 {
		t.Fatalf("block offline role: %v", block)
	}
	sender.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": b.SrvID, "msg": "拉黑后不可发送"})
	if reply := sender.recv(12720); reply.Int("code") != 0 || reply.Str("msg") != privateChatUnavailable {
		t.Fatalf("offline block not enforced: %v", reply)
	}

	receiver := e.connect(account, b)
	receiver.send(10300, proto.Message{})
	intoCity(receiver)
	list := receiver.recv(12722).Arr("offline_list")
	if len(list) != 1 || list[0].Int("rid") != c.RID || list[0].Str("name") != c.Name {
		t.Fatalf("wrong offline senders: %v (blocked sender %d)", list, a.RID)
	}
	if messages := list[0].Arr("msg_list"); len(messages) != 2 || messages[0].Str("msg") != "第一条" || messages[1].Str("msg") != "第二条" ||
		messages[0].Int("tick") == 0 || messages[1].Int("len") != 2 {
		t.Fatalf("offline messages lost ordering or metadata: %v", messages)
	}
	receiver.send(10300, proto.Message{})
	intoCity(receiver)
	receiver.expectNothing()
}

func TestLegacySocialChatUnchanged(t *testing.T) {
	e := newSocialEnv(t)
	sender, a := e.enter("symlf", "a", "甲")
	legacy, b := e.enter("symlf", "b", "乙")
	h5, c := e.enter(store.PlatformH5, "c", "丙")
	if err := e.st.AddOfflineChat(a.RID, b.RID, 0, "旧版离线消息", chatOfflineKeep); err != nil {
		t.Fatal(err)
	}
	for _, listener := range []*client{legacy, h5} {
		listener.send(13332, rid(a))
		if reply := listener.recv(13332); reply.Int("code") != 1 {
			t.Fatalf("block: %v", reply)
		}
	}
	legacy.send(10300, proto.Message{})
	intoCity(legacy)
	list := legacy.recv(12722).Arr("offline_list")
	if len(list) != 1 || list[0].Int("rid") != a.RID {
		t.Fatalf("legacy queue unexpectedly filtered: %v", list)
	}
	if messages := list[0].Arr("msg_list"); len(messages) != 1 || messages[0].Str("msg") != "旧版离线消息" {
		t.Fatalf("legacy queued message changed: %v", messages)
	}
	for _, target := range []struct {
		cli  *client
		role *store.Role
	}{{legacy, b}, {h5, c}} {
		sender.send(12720, proto.Message{"to_rid": target.role.RID, "to_srv_id": target.role.SrvID, "len": 0, "msg": "旧版私聊"})
		if push := target.cli.recv(12721); push.Int("rid") != a.RID || push.Str("msg") != "旧版私聊" {
			t.Fatalf("legacy live message changed: %v", push)
		}
		sender.recv(12721)
		if reply := sender.recv(12720); reply.Int("code") != 1 {
			t.Fatalf("legacy private reply changed: %v", reply)
		}
	}

	sender.send(12762, proto.Message{"channel": chatWorld, "msg": "频道消息", "sign": chatSign(a.RID, a.SrvID, chatWorld, 0, "频道消息")})
	if push := legacy.recv(12761); push.Str("msg") != "频道消息" {
		t.Fatalf("legacy channel push changed: %v", push)
	}
	sender.recv(12761)
	sender.recv(12762)
	legacy.send(12766, proto.Message{"channel": chatWorld})
	if history := legacy.recv(12766).Arr("msg_list"); len(history) != 1 || history[0].Str("msg") != "频道消息" {
		t.Fatalf("legacy channel history changed: %v", history)
	}
	h5.send(12766, proto.Message{"channel": chatWorld})
	if history := h5.recv(12766).Arr("msg_list"); len(history) != 0 {
		t.Fatalf("H5 saw a blocked legacy speaker: %v", history)
	}
	legacy.send(13333, proto.Message{"rid": a.RID, "srv_id": "other_1"})
	if reply := legacy.recv(13333); reply.Int("code") != 1 {
		t.Fatalf("legacy unblock changed: %v", reply)
	}
}
