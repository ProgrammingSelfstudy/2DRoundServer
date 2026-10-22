package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// RegisterElfin registers the sprite ("elfin") commands with a defined reply.
func RegisterElfin(r *gateway.Router) {
	r.Handle(26555, spriteTeams)
}

// spriteTeams answers 26555. The reply repeats the requested `type` (the client
// asks per team type and matches the answer to it) followed by that type's
// saved sprite teams. Nobody has any yet, so the list is empty.
func spriteTeams(s *gateway.Session, req proto.Message) error {
	return s.Send(26555, proto.Message{"type": req.Int("type"), "team_list": []proto.Message{}})
}
