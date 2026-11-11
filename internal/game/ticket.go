package game

import (
	"errors"
	"strings"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	ticketTitleRunes        = 50
	ticketContentRunes      = 500
	ticketMaxOpen           = 5 // tickets not yet answered at one time
	ticketOperationLogTag   = "2dround-h5-ops/v1\n"
	ticketOperationLogBytes = 4096 // phone_info: the H5 operation log, or the original client's device info
	ticketPhoneBytes        = 64
	ticketEmailBytes        = 254
)

// ticketAttachmentError checks what a new ticket carries besides its text; "" when it is fine.
// phone_info starting with ticketOperationLogTag is the H5 client's operation log.
func ticketAttachmentError(phone, email, phoneInfo string) string {
	logged := strings.HasPrefix(phoneInfo, ticketOperationLogTag)
	switch {
	case len(phoneInfo) > ticketOperationLogBytes && logged:
		return "操作日志太长"
	case len(phoneInfo) > ticketOperationLogBytes:
		return "机型信息太长"
	case len(phone) > ticketPhoneBytes:
		return "手机号太长"
	case len(email) > ticketEmailBytes:
		return "邮箱太长"
	case !utf8.ValidString(phoneInfo) && logged:
		return "操作日志格式错误"
	case !utf8.ValidString(phone) || !utf8.ValidString(email) || !utf8.ValidString(phoneInfo):
		return "请输入内容"
	}
	return ""
}

func ticketLine(t store.Ticket) proto.Message {
	return proto.Message{"id": t.ID, "state": t.State, "status2": b2i(t.Read), "end_msg_time": t.AnsweredAt,
		"start_time": t.CreatedAt, "finish_time": t.FinishedAt, "score_time": t.ScoredAt}
}

// RegisterTicket registers the customer-service tickets: 10810 files one (id 0) or asks again on
// one (its id), 10811 closes an answered one (score 0) or rates it (1-5), 10813 lists the role's
// tickets, 10814 shows one with its questions and answers. The answers come from the admin.
func RegisterTicket(r *gateway.Router, st *store.Store) {
	r.Handle(10810, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("id")
		reply := func(code int, msg string) error {
			return s.Send(10810, proto.Message{"code": code, "msg": msg, "id": id})
		}
		title, content := req.Str("title"), req.Str("content")
		switch {
		case utf8.RuneCountInString(content) > ticketContentRunes:
			return reply(0, "内容太长")
		case id == 0 && utf8.RuneCountInString(title) > ticketTitleRunes:
			return reply(0, "标题太长")
		case !utf8.ValidString(title) || !utf8.ValidString(content) || strings.TrimSpace(content) == "" || (id == 0 && strings.TrimSpace(title) == ""):
			return reply(0, "请输入内容")
		}
		now := s.Now().Unix()
		if id != 0 { // a follow-up stores only the question: the contact data and the device info stay as filed
			err := st.AskTicket(role.RID, id, content, now)
			if errors.Is(err, store.ErrNotFound) {
				return reply(0, "工单不存在")
			}
			if err != nil {
				return err
			}
			return reply(1, "提交成功")
		}
		phone, email, phoneInfo := req.Str("phone"), req.Str("email"), req.Str("phone_info")
		if msg := ticketAttachmentError(phone, email, phoneInfo); msg != "" {
			return reply(0, msg)
		}
		mine, err := st.TicketsOf(role.RID)
		if err != nil {
			return err
		}
		open := 0
		for _, t := range mine {
			if t.State == store.TicketFiled {
				open++
			}
		}
		if open >= ticketMaxOpen {
			return reply(0, "还有未回复的问题，请稍后再提交")
		}
		id, err = st.FileTicket(store.Ticket{RID: role.RID, SrvID: role.SrvID, IssueType: int(req.Int("issue_type")), Title: title,
			Phone: phone, Email: email, PhoneInfo: phoneInfo}, content, now)
		if err != nil {
			return err
		}
		return reply(1, "提交成功")
	})
	r.Handle(10811, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		score := req.Int("score")
		if score < 0 || score > 5 {
			return s.Send(10811, proto.Message{"code": 0, "msg": "评分不对"})
		}
		err := st.FinishTicket(role.RID, req.Int("id"), score, s.Now().Unix())
		switch {
		case errors.Is(err, store.ErrNotFound):
			return s.Send(10811, proto.Message{"code": 0, "msg": "工单不存在"})
		case errors.Is(err, store.ErrConflict):
			return s.Send(10811, proto.Message{"code": 0, "msg": "还没有回复"})
		case err != nil:
			return err
		}
		return s.Send(10811, proto.Message{"code": 1, "msg": "感谢您的评价"})
	})
	r.Handle(10813, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		mine, err := st.TicketsOf(role.RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, t := range mine {
			m := ticketLine(t)
			msgs, err := st.TicketMsgs(t.ID)
			if err != nil {
				return err
			}
			m["title"], m["content"] = t.Title, ""
			if len(msgs) > 0 {
				m["content"] = msgs[0].Content
			}
			out = append(out, m)
		}
		return s.Send(10813, proto.Message{"feedback_logs": out})
	})
	r.Handle(10814, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		t, err := st.TicketByID(req.Int("id"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && t.RID != role.RID) {
			return nil
		}
		if err != nil {
			return err
		}
		msgs, err := st.TicketMsgs(t.ID)
		if err != nil {
			return err
		}
		if err := st.ReadTicket(role.RID, t.ID); err != nil {
			return err
		}
		t.Read = true
		out := ticketLine(*t)
		out["title"], out["content"] = t.Title, ""
		qs, as := []proto.Message{}, []proto.Message{}
		for _, m := range msgs {
			if m.Kind == 1 {
				if len(qs) == 0 {
					out["content"] = m.Content
				}
				qs = append(qs, proto.Message{"questions_timer": m.At, "questions_content": m.Content})
			} else {
				as = append(as, proto.Message{"answer_timer": m.At, "answer_content": m.Content})
			}
		}
		out["questions_lists"], out["answer_lists"] = qs, as
		return s.Send(10814, out)
	})
}
