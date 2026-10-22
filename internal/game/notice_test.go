package game

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func (e *env) notice(n store.Notice) *store.Notice {
	e.t.Helper()
	if n.Content == "" {
		n.Content = "内容"
	}
	out, err := e.st.CreateNotice(n)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func TestNoticeListShowsOnlyWhatTheRoleMaySee(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	now := time.Now().Unix()
	long := strings.Repeat("长", 50)
	shown := e.notice(store.Notice{Title: "维护公告", Content: "第一行\n第二行不该出现在摘要里", Status: 1, StartAt: now - 60})
	long1 := e.notice(store.Notice{Title: "长内容", Content: long, Status: 1, SrvID: "symlf_1"})
	e.notice(store.Notice{Title: "别的区", Status: 1, SrvID: "symlf_2"})
	e.notice(store.Notice{Title: "已隐藏", Status: 0})
	e.notice(store.Notice{Title: "未开始", Status: 1, StartAt: now + 3600})
	e.notice(store.Notice{Title: "已结束", Status: 1, StartAt: 1, EndAt: now - 5})

	c.send(10950, proto.Message{})
	r := c.recv(10950)
	list := r.Arr("board_list")
	if r.Int("type") != 1 || len(list) != 2 {
		t.Fatalf("only the two visible notices: %v", r)
	}
	byID := map[int64]proto.Message{}
	for _, b := range list {
		byID[b.Int("id")] = b
	}
	b := byID[shown.ID]
	if b.Str("title") != "维护公告" || b.Str("summary") != "第一行" || b.Str("content") != "第一行\n第二行不该出现在摘要里" ||
		b.Int("flag") != 0 || b.Int("type") != 1 || b.Int("start_time") != now-60 || b.Int("end_time") != 0 {
		t.Fatalf("entry: %v", b)
	}
	if got := byID[long1.ID].Str("summary"); got != strings.Repeat("长", summaryRunes)+"…" || byID[long1.ID].Str("content") != long {
		t.Fatalf("a long first line is cut for the summary only: %q", got)
	}
}

func TestOpeningANoticeMarksItReadForGood(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	n := e.notice(store.Notice{Title: "t", Status: 1})
	other := e.notice(store.Notice{Title: "别区", Status: 1, SrvID: "symlf_2"})
	hidden := e.notice(store.Notice{Title: "隐藏", Status: 0})

	c.send(10952, proto.Message{"id": n.ID})
	if r := c.recv(10952); r.Int("code") != 1 || r.Int("id") != n.ID || r.Str("msg") != "" {
		t.Fatalf("open: %v", r)
	}
	// right after the reply the entry is sent again, read, so the open list greys it and the badge drops
	upd := c.recv(10950).Arr("board_list")
	if len(upd) != 1 || upd[0].Int("id") != n.ID || upd[0].Int("flag") != 1 {
		t.Fatalf("read update: %v", upd)
	}
	for _, id := range []int64{other.ID, hidden.ID, 99999} {
		c.send(10952, proto.Message{"id": id})
		if r := c.recv(10952); r.Int("code") != 0 || r.Str("msg") == "" || r.Int("id") != id {
			t.Fatalf("notice %d must not open: %v", id, r)
		}
	}
	c.expectNothing() // refusals are not followed by a list update
	c.send(10950, proto.Message{})
	if l := c.recv(10950).Arr("board_list"); len(l) != 1 || l[0].Int("flag") != 1 {
		t.Fatalf("read flag after opening: %v", l)
	}
	// a fresh login (another connection) still sees it as read; another role does not
	c2, _ := e.dialSameRole("u1", role)
	c2.send(10950, proto.Message{})
	if l := c2.recv(10950).Arr("board_list"); len(l) != 1 || l[0].Int("flag") != 1 {
		t.Fatalf("the read state is lost on relogin: %v", l)
	}
	c3, _ := e.enter("u2", "乙")
	c3.send(10950, proto.Message{})
	if l := c3.recv(10950).Arr("board_list"); len(l) != 1 || l[0].Int("flag") != 0 {
		t.Fatalf("another role must see it unread: %v", l)
	}
}

func TestNoticeRequestsBeforeRoleLoginAreIgnored(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, tok := e.account("u1")
	c := e.dial(true)
	c.send(1164, loginArgs("u1", tok))
	c.recv(1164)
	c.send(10950, proto.Message{})
	c.expectNothing()
	c.send(10952, proto.Message{"id": 1})
	c.expectNothing()
}

func TestSummarize(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "  \n  ": "", "短": "短", "a\nb": "a", "\n\n先空行\n后": "先空行", "  首尾空格  \n": "首尾空格",
		strings.Repeat("字", summaryRunes):   strings.Repeat("字", summaryRunes),
		strings.Repeat("字", summaryRunes+1): strings.Repeat("字", summaryRunes) + "…",
		strings.Repeat("😀", summaryRunes+2): strings.Repeat("😀", summaryRunes) + "…", // cut on characters, never inside one
	} {
		if got := summarize(in); got != want {
			t.Errorf("summarize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPushNoticeShowsOrDropsForOnlineRoles(t *testing.T) {
	e := newEnv(t, time.Minute)
	c1, _ := e.enter("u1", "甲")
	c2, _ := e.enter("u2", "乙")
	n := e.notice(store.Notice{Title: "新公告", Status: 1})

	shown, dropped, err := e.notes.PushNotice(n.ID)
	if err != nil || shown != 2 || dropped != 0 {
		t.Fatalf("push: %d %d %v", shown, dropped, err)
	}
	for _, c := range []*client{c1, c2} {
		p := c.recv(10950)
		if l := p.Arr("board_list"); len(l) != 1 || l[0].Int("id") != n.ID || l[0].Str("title") != "新公告" {
			t.Fatalf("pushed list: %v", p)
		}
	}
	// the admin hides it: the same push now tells everyone to drop it
	n.Status = 0
	if err := e.st.UpdateNotice(*n); err != nil {
		t.Fatal(err)
	}
	shown, dropped, _ = e.notes.PushNotice(n.ID)
	if shown != 0 || dropped != 2 {
		t.Fatalf("after hiding: %d %d", shown, dropped)
	}
	for _, c := range []*client{c1, c2} {
		if d := c.recv(10951); d.Int("id") != n.ID {
			t.Fatalf("drop: %v", d)
		}
	}
	// a deleted notice has no row; DropNotice still reaches everyone
	e.st.DeleteNotice(n.ID)
	if shown, dropped, _ = e.notes.PushNotice(n.ID); shown != 0 || dropped != 2 {
		t.Fatalf("push of a deleted notice: %d %d", shown, dropped)
	}
	c1.recv(10951)
	c2.recv(10951)
	if d, err := e.notes.DropNotice(n.ID); err != nil || d != 2 {
		t.Fatalf("DropNotice: %d %v", d, err)
	}
	if c1.recv(10951).Int("id") != n.ID {
		t.Fatal("drop id")
	}
	c2.recv(10951)
	// an account that has not entered a role is not pushed to
	_, tok := e.account("u3")
	c3 := e.dial(true)
	c3.send(1164, loginArgs("u3", tok))
	c3.recv(1164)
	n2 := e.notice(store.Notice{Title: "再来一条", Status: 1})
	if shown, _, _ := e.notes.PushNotice(n2.ID); shown != 2 {
		t.Fatalf("only roles in the game are pushed to: %d", shown)
	}
	c1.recv(10950)
	c2.recv(10950)
	c3.expectNothing()
}

func TestNoticeControlEndpoints(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("u1", "甲")
	e.mail.Notices = e.notes
	srv := httptest.NewServer(e.mail.AdminHandler())
	defer srv.Close()
	n := e.notice(store.Notice{Title: "via http", Status: 1})
	call := func(path string) (int, map[string]int) {
		resp, err := http.Post(srv.URL+path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]int
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := call(fmt.Sprintf("/push/notice?id=%d", n.ID)); code != 200 || out["pushed"] != 1 || out["dropped"] != 0 {
		t.Fatalf("push: %d %v", code, out)
	}
	if l := c.recv(10950).Arr("board_list"); len(l) != 1 || l[0].Str("title") != "via http" {
		t.Fatalf("pushed: %v", l)
	}
	if code, out := call(fmt.Sprintf("/push/notice/remove?id=%d", n.ID)); code != 200 || out["dropped"] != 1 {
		t.Fatalf("remove: %d %v", code, out)
	}
	if c.recv(10951).Int("id") != n.ID {
		t.Fatal("drop")
	}
	for _, p := range []string{"/push/notice", "/push/notice?id=abc", "/push/notice?id=0", "/push/notice/remove", "/push/notice/remove?id=-1"} {
		if code, _ := call(p); code != 400 {
			t.Errorf("%s -> %d, want 400", p, code)
		}
	}
}
