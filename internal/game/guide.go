package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// RegisterGuide registers the small client-state commands that need an answer so the client
// does not wait: 11121 the guide's network check (echoed: the client goes on to the next step
// when it gets its own id and step back), 11122 a guide was finished or skipped, 10956 a client
// flag was set, 20063 which fights the role is in (none: fights are answered at once here).
// The server pushes no guide (11120), so the client plays none of its own accord, and keeps
// none of these.
func RegisterGuide(r *gateway.Router) {
	r.Handle(11121, func(s *gateway.Session, req proto.Message) error {
		return s.Send(11121, proto.Message{"id": req.Int("id"), "n": req.Int("n")})
	})
	r.Handle(11122, func(s *gateway.Session, _ proto.Message) error { return s.Send(11122, proto.Message{}) })
	r.Handle(10956, func(s *gateway.Session, _ proto.Message) error { return s.Send(10956, proto.Message{}) })
	r.Handle(20063, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(20063, proto.Message{"type_list": []proto.Message{}})
	})
}
