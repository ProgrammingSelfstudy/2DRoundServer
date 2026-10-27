package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// RegisterWelfare registers the welfare window's paid and account parts as the state of a
// player who bought and bound nothing, since there is no payment, phone or wechat here:
// (16705 / 16706 the month cards are monthcard.go) 21002 the recharge
// count (0), 21007 (cyclegift.go) 21008 the daily award (not open), 24703 the red dots (none), 10987 the
// subscription red dot (none), 16633 / 16634 / 16635 / 16636 the wechat and phone binding
// (told as done, so the window does not nag; a new binding is refused) and 24600-24604 the
// survey (not open). The sign-in (14100, 14101) is checkin.go.
func RegisterWelfare(r *gateway.Router, st *store.Store) {
	r.Handle(21002, func(s *gateway.Session, _ proto.Message) error { return s.Send(21002, proto.Message{"count": 0}) })
	r.Handle(21008, func(s *gateway.Session, _ proto.Message) error { return s.Send(21008, proto.Message{"status": 0}) })
	r.Handle(24703, func(s *gateway.Session, req proto.Message) error {
		return s.Send(24703, proto.Message{"ids": []proto.Message{{"id": req.Int("id"), "status": 0}}})
	})
	r.Handle(10987, func(s *gateway.Session, _ proto.Message) error { return s.Send(10987, proto.Message{"is_point": 0}) })
	r.Handle(10988, func(s *gateway.Session, _ proto.Message) error { return s.Send(10988, proto.Message{}) })
	r.Handle(16633, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(16633, proto.Message{"code": 1, "items": []proto.Message{}})
	})
	r.Handle(16634, func(s *gateway.Session, _ proto.Message) error { return s.Send(16634, proto.Message{}) })
	r.Handle(16635, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(16635, proto.Message{"code": 1, "items": []proto.Message{}})
	})
	r.Handle(16636, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(16636, proto.Message{"code": 0, "msg": "暂不支持绑定手机"})
	})
	r.Handle(24600, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(24600, proto.Message{"status": 0, "start_time": 0, "end_time": 0, "flag": 0})
	})
	r.Handle(24601, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(24601, proto.Message{"id": 0, "title": "", "memo": "", "rewards": []proto.Message{}})
	})
	r.Handle(24602, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(24602, proto.Message{"questionnaire_list": []proto.Message{}})
	})
	for _, cmd := range []uint16{24603, 24604} {
		cmd := cmd
		r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error {
			return s.Send(cmd, proto.Message{"code": 0, "msg": "问卷未开放"})
		})
	}
}
