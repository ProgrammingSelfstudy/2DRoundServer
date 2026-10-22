package game

import (
	"log"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// RegisterEmpty answers every command in emptyReplies that has no real handler
// with an empty message of the same id: zeros, empty strings, empty arrays.
// "Empty" is the no-data state, not invented content. Scalar fields that the
// request and the reply share by name and type are echoed back, because
// replies identify what they answer by repeating the key (id, bid, type, ...).
// A command leaves emptyReplies when it gets a real handler, which always wins.
func RegisterEmpty(r *gateway.Router, lg *log.Logger) {
	schema, err := proto.LoadSchema()
	if err != nil {
		lg.Printf("empty replies: %v", err)
		return
	}
	n := 0
	for _, e := range emptyReplies {
		cmd := e.Cmd
		if r.Has(cmd) {
			continue
		}
		echo := echoFields(schema.Send[cmd], schema.Recv[cmd])
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			reply := proto.Message{}
			for _, name := range echo {
				reply[name] = req[name]
			}
			return s.Send(cmd, reply)
		})
		n++
	}
	lg.Printf("empty replies registered for %d commands", n)
}

// echoFields lists the scalar fields present in both tables with the same type.
func echoFields(send, recv []proto.Field) []string {
	have := map[string]string{}
	for _, f := range send {
		if f.Type != "array" {
			have[f.Name] = f.Type
		}
	}
	var out []string
	for _, f := range recv {
		if t, ok := have[f.Name]; ok && t == f.Type {
			out = append(out, f.Name)
		}
	}
	return out
}
