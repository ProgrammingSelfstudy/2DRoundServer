package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

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
