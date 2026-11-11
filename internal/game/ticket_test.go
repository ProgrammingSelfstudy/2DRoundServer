package game

import (
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestTicketOperationLog(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("ops", "甲")
	file := func(phoneInfo string) proto.Message {
		c.send(10810, proto.Message{"issue_type": 1, "title": "操作反馈", "content": "进入战斗后卡住", "phone_info": phoneInfo})
		return c.waitFor(10810)
	}

	attachment := "2dround-h5-ops/v1\n打开英雄\n"
	attachment += strings.Repeat("x", 4096-len(attachment))
	for _, input := range []string{attachment + "x", "2dround-h5-ops/v1\n\xff"} {
		if r := file(input); r.Int("code") != 0 || r.Int("id") != 0 {
			t.Fatalf("accepted invalid attachment: %v", r)
		}
	}
	// the original client's device info has no tag but the same size limit, and invalid text is refused too
	legacyInfo := "原版机型\n"
	legacyInfo += strings.Repeat("x", 4096-len(legacyInfo))
	for _, input := range []string{legacyInfo + "x", "原版机型\xff"} {
		if r := file(input); r.Int("code") != 0 || r.Int("id") != 0 {
			t.Fatalf("accepted invalid device info: %v", r)
		}
	}
	var filed int64
	for _, input := range []string{attachment, legacyInfo} {
		r := file(input)
		if r.Int("code") != 1 || r.Int("id") == 0 {
			t.Fatalf("file: %v", r)
		}
		filed = r.Int("id")
		ticket, err := e.st.TicketByID(filed)
		if err != nil || ticket.PhoneInfo != input {
			t.Fatalf("phone_info did not round-trip: %v", err)
		}
	}

	// phone and email are bounded as well
	for _, extra := range []proto.Message{{"phone": strings.Repeat("1", 65)}, {"email": strings.Repeat("a", 255)}} {
		extra["issue_type"], extra["title"], extra["content"] = 1, "联系方式", "x"
		c.send(10810, extra)
		if r := c.waitFor(10810); r.Int("code") != 0 || r.Int("id") != 0 {
			t.Fatalf("accepted a long contact: %v", r)
		}
	}

	// a follow-up keeps the device info as filed: a too long or broken log there neither blocks
	// the question nor replaces what was stored
	for _, input := range []string{attachment + "x", "2dround-h5-ops/v1\n\xff", "new log"} {
		c.send(10810, proto.Message{"id": filed, "content": "还是不行", "phone_info": input})
		if r := c.waitFor(10810); r.Int("code") != 1 || r.Int("id") != filed {
			t.Fatalf("follow-up with %.20q: %v", input, r)
		}
	}
	if ticket, err := e.st.TicketByID(filed); err != nil || ticket.PhoneInfo != legacyInfo {
		t.Fatalf("follow-up changed phone_info: %v", err)
	}
	if msgs, err := e.st.TicketMsgs(filed); err != nil || len(msgs) != 4 {
		t.Fatalf("expected the question and three follow-ups, got %d: %v", len(msgs), err)
	}

	if tickets, err := e.st.TicketsOf(role.RID); err != nil || len(tickets) != 2 {
		t.Fatalf("expected only two valid tickets, got %d: %v", len(tickets), err)
	}
}

func TestTickets(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, _ := e.enter("a", "甲")
	cb, _ := e.enter("b", "乙")
	file := func(title, content string) proto.Message {
		c.send(10810, proto.Message{"issue_type": 1, "title": title, "content": content, "id": 0})
		return c.waitFor(10810)
	}
	if r := file("", "x"); r.Int("code") != 0 {
		t.Fatalf("no title: %v", r)
	}
	if r := file("卡了", "  "); r.Int("code") != 0 {
		t.Fatalf("blank content: %v", r)
	}
	r := file("卡了", "进不去战斗")
	id := r.Int("id")
	if r.Int("code") != 1 || id == 0 {
		t.Fatalf("file: %v", r)
	}
	c.send(10813, proto.Message{})
	l := c.waitFor(10813).Arr("feedback_logs")
	if len(l) != 1 || l[0].Int("state") != 1 || l[0].Str("title") != "卡了" || l[0].Str("content") != "进不去战斗" {
		t.Fatalf("list: %v", l)
	}
	// others cannot see or touch it
	cb.send(10813, proto.Message{})
	if l := cb.waitFor(10813).Arr("feedback_logs"); len(l) != 0 {
		t.Fatalf("other's list: %v", l)
	}
	cb.send(10811, proto.Message{"id": id, "score": 5})
	if r := cb.waitFor(10811); r.Int("code") != 0 {
		t.Fatalf("other rates: %v", r)
	}
	// nothing to close before an answer
	c.send(10811, proto.Message{"id": id, "score": 0})
	if r := c.waitFor(10811); r.Int("code") != 0 {
		t.Fatalf("close unanswered: %v", r)
	}
	if err := e.st.AnswerTicket(id, "请更新客户端", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	c.send(10813, proto.Message{})
	if l := c.waitFor(10813).Arr("feedback_logs"); l[0].Int("state") != 2 || l[0].Int("status2") != 0 || l[0].Int("end_msg_time") == 0 {
		t.Fatalf("answered: %v", l)
	}
	c.send(10814, proto.Message{"id": id})
	d := c.waitFor(10814)
	if len(d.Arr("questions_lists")) != 1 || len(d.Arr("answer_lists")) != 1 || d.Arr("answer_lists")[0].Str("answer_content") != "请更新客户端" || d.Int("status2") != 1 {
		t.Fatalf("detail: %v", d)
	}
	c.send(10810, proto.Message{"id": id, "title": "卡了", "content": "还是不行"})
	if r := c.waitFor(10810); r.Int("code") != 1 {
		t.Fatalf("ask again: %v", r)
	}
	if err := e.st.AnswerTicket(id, "已处理", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	c.send(10811, proto.Message{"id": id, "score": 0})
	c.waitFor(10811)
	c.send(10811, proto.Message{"id": id, "score": 5})
	if r := c.waitFor(10811); r.Int("code") != 1 {
		t.Fatalf("rate: %v", r)
	}
	c.send(10814, proto.Message{"id": id})
	if d := c.waitFor(10814); d.Int("state") != 4 || d.Int("score_time") == 0 || d.Int("finish_time") == 0 || len(d.Arr("questions_lists")) != 2 {
		t.Fatalf("rated: %v", d)
	}
}
