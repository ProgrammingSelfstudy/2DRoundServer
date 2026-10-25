package game

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// intoCity reads what follows 10300 up to the main city push, so the next frame
// is whatever comes after it.
func intoCity(c *client) {
	c.recv(10301)
	c.recv(10302)
	c.recv(13000)
	c.recv(10304)
}

func TestPrivateChatBetweenOnlineRoles(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")

	ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": "symlf_1", "len": 0, "msg": "你好"})
	// the recipient sees the sender (flag 2) ...
	got := cb.recv(12721)
	if got.Int("flag") != 2 || got.Int("rid") != a.RID || got.Str("name") != "甲" || got.Str("msg") != "你好" || got.Int("tick") == 0 {
		t.Fatalf("recipient: %v", got)
	}
	// ... the sender gets an echo that shows the recipient (flag 1)
	echo := ca.recv(12721)
	if echo.Int("flag") != 1 || echo.Int("rid") != b.RID || echo.Str("name") != "乙" || echo.Str("msg") != "你好" {
		t.Fatalf("echo: %v", echo)
	}
	if r := ca.recv(12720); r.Int("code") != 1 {
		t.Fatalf("reply: %v", r)
	}
	// the content flag is passed on
	ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": "symlf_1", "len": 2, "msg": "<link>"})
	if got := cb.recv(12721); got.Int("len") != 2 {
		t.Fatalf("content flag: %v", got)
	}
	ca.recv(12721)
	ca.recv(12720)
	// nothing was stored for an online recipient
	if n, _ := e.st.TakeOfflineChat(b.RID); len(n) != 0 {
		t.Fatalf("stored for an online role: %v", n)
	}
}

func TestPrivateChatRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	_, banned := e.enter("c", "丙")
	e.st.SetRoleStatus(banned.RID, 0)
	bad := func(to int64, srv, msg, want string) {
		t.Helper()
		ca.send(12720, proto.Message{"to_rid": to, "to_srv_id": srv, "len": 0, "msg": msg})
		if r := ca.recv(12720); r.Int("code") != 0 || r.Str("msg") != want {
			t.Fatalf("want %q, got %v", want, r)
		}
	}
	bad(a.RID, "symlf_1", "hi", "不能和自己聊天")
	bad(999999, "symlf_1", "hi", "玩家不存在")
	bad(banned.RID, "symlf_1", "hi", "玩家不存在")
	bad(b.RID, "other_2", "hi", "暂不支持跨服聊天")
	bad(b.RID, "symlf_1", "", "消息内容不合法")
	bad(b.RID, "symlf_1", "   ", "消息内容不合法")
	bad(b.RID, "symlf_1", strings.Repeat("字", maxChatRunes+1), "消息内容不合法")
	bad(b.RID, "symlf_1", "a\x00b", "消息内容不合法")
	bad(b.RID, "symlf_1", "a\nb", "消息内容不合法")
	// the longest allowed message goes through
	ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": "symlf_1", "len": 0, "msg": strings.Repeat("字", maxChatRunes)})
	ca.recv(12721)
	if r := ca.recv(12720); r.Int("code") != 1 {
		t.Fatalf("longest message: %v", r)
	}
	if n, _ := e.st.TakeOfflineChat(b.RID); len(n) != 0 {
		t.Fatalf("a refused message was stored: %v", n)
	}
}

func TestPrivateChatToOfflineRoleIsDeliveredOnce(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	cb.c.Close()
	for e.gw.Hub.Online(b.RID) != nil {
		time.Sleep(10 * time.Millisecond)
	}
	for _, text := range []string{"一", "二"} {
		ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": "symlf_1", "len": 0, "msg": text})
		if echo := ca.recv(12721); echo.Int("flag") != 1 {
			t.Fatalf("echo: %v", echo)
		}
		if r := ca.recv(12720); r.Int("code") != 1 {
			t.Fatalf("reply: %v", r)
		}
	}

	// b comes back: the messages arrive after the main city is up
	cb2, _ := e.dialSameRole("b", b)
	cb2.send(10300, proto.Message{})
	intoCity(cb2)
	off := cb2.recv(12722).Arr("offline_list")
	if len(off) != 1 || off[0].Int("rid") != a.RID || off[0].Str("name") != "甲" || len(off[0].Arr("msg_list")) != 2 {
		t.Fatalf("offline list: %v", off)
	}
	if m := off[0].Arr("msg_list"); m[0].Str("msg") != "一" || m[1].Str("msg") != "二" || m[0].Int("tick") == 0 {
		t.Fatalf("messages must be in order: %v", m)
	}
	cb2.send(12723, proto.Message{"rid": a.RID, "srv_id": "symlf_1"}) // read: nothing to answer
	cb2.expectNothing()

	// once: the next login has nothing for b
	cb2.c.Close()
	cb3, _ := e.dialSameRole("b", b)
	cb3.send(10300, proto.Message{})
	intoCity(cb3)
	cb3.expectNothing()
}

func TestOfflineChatKeepsTheNewest(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, a := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	for i := 0; i < 5; i++ {
		if err := e.st.AddOfflineChat(a.RID, b.RID, 0, string(rune('a'+i)), 3); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.st.TakeOfflineChat(b.RID)
	if err != nil || len(got) != 3 || got[0].Msg != "c" || got[2].Msg != "e" {
		t.Fatalf("kept: %v %v", got, err)
	}
	if got, _ := e.st.TakeOfflineChat(b.RID); len(got) != 0 {
		t.Fatalf("taken twice: %v", got)
	}
}

func TestWorldChat(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, _ := e.enter("b", "乙")

	send := func(text string) {
		ca.send(12762, proto.Message{"channel": 1, "len": 0, "msg": text, "sign": chatSign(a.RID, "symlf_1", 1, 0, text)})
	}
	send("大家好")
	for _, c := range []*client{ca, cb} { // everyone online, the sender included
		m := c.recv(12761)
		who := m.Arr("role_list")
		if m.Int("channel") != 1 || m.Str("msg") != "大家好" || m.Int("tick") == 0 || len(who) != 1 || who[0].Int("rid") != a.RID || who[0].Str("name") != "甲" {
			t.Fatalf("push: %v", m)
		}
	}
	if r := ca.recv(12762); r.Int("code") != 1 {
		t.Fatalf("reply: %v", r)
	}
	if strings.Contains(e.logs.String(), "sign mismatch") {
		t.Fatalf("a correct sign was reported:\n%s", e.logs.String())
	}

	// a wrong sign is only logged
	ca.send(12762, proto.Message{"channel": 1, "len": 0, "msg": "再说一句", "sign": "wrong"})
	ca.recv(12761)
	cb.recv(12761)
	if r := ca.recv(12762); r.Int("code") != 1 || !strings.Contains(e.logs.String(), "sign mismatch") {
		t.Fatalf("wrong sign: %v\n%s", r, e.logs.String())
	}

	// history: a role that was not there hears of both
	cc, _ := e.enter("c", "丙")
	cc.send(12766, proto.Message{"channel": 1})
	h := cc.recv(12766)
	list := h.Arr("msg_list")
	if h.Int("channel") != 1 || len(list) != 2 || list[0].Str("msg") != "大家好" || list[1].Str("msg") != "再说一句" || list[0].Arr("role_list")[0].Str("name") != "甲" {
		t.Fatalf("history: %v", h)
	}
	cc.send(12766, proto.Message{"channel": 4})
	if h := cc.recv(12766); len(h.Arr("msg_list")) != 0 {
		t.Fatalf("history of another channel: %v", h)
	}
}

func TestWorldChatRefusals(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	bad := func(channel int, text, want string) {
		t.Helper()
		ca.send(12762, proto.Message{"channel": channel, "len": 0, "msg": text, "sign": ""})
		if r := ca.recv(12762); r.Int("code") != 0 || r.Str("msg") != want {
			t.Fatalf("want %q, got %v", want, r)
		}
	}
	bad(4, "公会", "你还没有加入公会")   // guild, without being in one
	bad(7, "好友", "该频道暂未开放")    // friend channel goes through 12720
	bad(1024, "跨服", "该频道暂未开放") // cross-server
	bad(1, "", "消息内容不合法")
	bad(1, strings.Repeat("a", maxChatRunes+1), "消息内容不合法")
	// nothing was said, so nothing is in the history
	ca.send(12766, proto.Message{"channel": 1})
	if h := ca.recv(12766); len(h.Arr("msg_list")) != 0 {
		t.Fatalf("history: %v", h)
	}
}

func TestChatHistoryIsBounded(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	for i := 0; i < chatHistory+5; i++ {
		ca.send(12762, proto.Message{"channel": 1, "len": 0, "msg": strings.Repeat("x", 1+i%3) + string(rune('A'+i%26)), "sign": ""})
		ca.recv(12761)
		ca.recv(12762)
	}
	ca.send(12766, proto.Message{"channel": 1})
	if h := ca.recv(12766); len(h.Arr("msg_list")) != chatHistory {
		t.Fatalf("history length %d, want %d", len(h.Arr("msg_list")), chatHistory)
	}
}

func TestChatRateLimit(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.chat.MinInterval = 400 * time.Millisecond
	ca, _ := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	say := func() proto.Message {
		ca.send(12762, proto.Message{"channel": 1, "len": 0, "msg": "hi", "sign": ""})
		first := ca.recv(12761)
		_ = first
		return ca.recv(12762)
	}
	if r := say(); r.Int("code") != 1 {
		t.Fatalf("first: %v", r)
	}
	cb.recv(12761)
	// too soon: refused, and it is not broadcast
	ca.send(12762, proto.Message{"channel": 1, "len": 0, "msg": "again", "sign": ""})
	if r := ca.recv(12762); r.Int("code") != 0 || r.Str("msg") != "发言太快了，请稍后再试" {
		t.Fatalf("second: %v", r)
	}
	// private chat shares the limit
	ca.send(12720, proto.Message{"to_rid": b.RID, "to_srv_id": "symlf_1", "len": 0, "msg": "x"})
	if r := ca.recv(12720); r.Int("code") != 0 {
		t.Fatalf("private chat during the pause: %v", r)
	}
	e.advance(450 * time.Millisecond)
	if r := say(); r.Int("code") != 1 {
		t.Fatalf("after the pause: %v", r)
	}
	cb.recv(12761)
	cb.expectNothing() // the refused message never reached b
}

func TestChatBeforeRoleLoginIsIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	c := e.dial(true)
	c.send(12720, proto.Message{"to_rid": 1, "to_srv_id": "symlf_1", "len": 0, "msg": "x"})
	c.send(12762, proto.Message{"channel": 1, "len": 0, "msg": "x", "sign": ""})
	c.send(12766, proto.Message{"channel": 1})
	c.send(12723, proto.Message{"rid": 1, "srv_id": "symlf_1"})
	c.expectNothing()
}

func TestBroadcastReachesEveryoneOnline(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, _ := e.enter("a", "甲")
	cb, _ := e.enter("b", "乙")
	h := httptest.NewServer(e.mail.AdminHandler())
	defer h.Close()
	post := func(msg string) (int, map[string]any) {
		resp, err := http.Post(h.URL+"/push/broadcast?msg="+url.QueryEscape(msg), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	code, out := post("服务器 10 分钟后维护")
	if code != 200 || out["pushed"] != float64(2) {
		t.Fatalf("broadcast: %d %v", code, out)
	}
	for _, c := range []*client{ca, cb} {
		m := c.recv(12761)
		if m.Int("channel") != 32 || m.Str("msg") != "服务器 10 分钟后维护" || len(m.Arr("role_list")) != 0 || m.Int("tick") == 0 {
			t.Fatalf("push: %v", m)
		}
	}
	// it is not chat: not in the world history
	ca.send(12766, proto.Message{"channel": 1})
	if h := ca.recv(12766); len(h.Arr("msg_list")) != 0 {
		t.Fatalf("a broadcast went into the chat history: %v", h)
	}
	// nothing sent for a bad message
	for _, bad := range []string{"", "   ", strings.Repeat("字", 101), "a\nb"} {
		if code, _ := post(bad); code != 400 {
			t.Fatalf("msg %q: %d", bad, code)
		}
	}
	ca.expectNothing()
	// with nobody online it still answers, with 0
	ca.c.Close()
	cb.c.Close()
	for e.gw.Hub.Count() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if code, out := post("x"); code != 200 || out["pushed"] != float64(0) {
		t.Fatalf("nobody online: %d %v", code, out)
	}
}

func TestGuildChat(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	cc, c := e.enter("c", "丙")
	say := func(cl *client, r *store.Role, ch int64, text string) {
		cl.send(12762, proto.Message{"channel": ch, "len": 0, "msg": text, "sign": chatSign(r.RID, "symlf_1", ch, 0, text)})
	}
	say(ca, a, 4, "没有公会")
	if r := ca.recv(12762); r.Int("code") != 0 {
		t.Fatalf("without a guild: %v", r)
	}
	gid, err := e.st.CreateGuild(store.Guild{SrvID: "symlf_1", Name: "海贼联盟", LeaderRID: a.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.JoinGuild(gid, b.RID, 2, 15); err != nil {
		t.Fatal(err)
	}
	say(ca, a, 4, "会内的话")
	for _, cl := range []*client{ca, cb} {
		m := cl.recv(12761)
		who := m.Arr("role_list")
		if m.Int("channel") != 4 || m.Str("msg") != "会内的话" || len(who) != 1 || who[0].Str("g_name") != "海贼联盟" || who[0].Int("gid") != gid {
			t.Fatalf("guild push: %v", m)
		}
	}
	if r := ca.recv(12762); r.Int("code") != 1 {
		t.Fatalf("reply: %v", r)
	}
	cc.expectNothing() // the outsider heard nothing
	say(cc, c, 4, "我不在会里")
	if r := cc.recv(12762); r.Int("code") != 0 {
		t.Fatalf("outsider: %v", r)
	}
	cb.send(12766, proto.Message{"channel": 4})
	if h := cb.recv(12766); len(h.Arr("msg_list")) != 1 {
		t.Fatalf("history: %v", h)
	}
	cc.send(12766, proto.Message{"channel": 4})
	if h := cc.recv(12766); len(h.Arr("msg_list")) != 0 {
		t.Fatalf("history of an outsider: %v", h)
	}
}
