package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// registerRoleExtra answers the role commands that need a system we do not have, with the "nothing
// there" answer: 10907 no system ban, 10330 / 10332 / 10333 custom face (it lives in Tencent COS, which
// we do not run: no keys, refused), 25813 / 25814 room feat tips (nothing to show), 25830 / 25832 the
// growth way (growway.go), the honor / task / level / growth way shares 25815,
// 25817, 25819, 25831 (shared: no chat card is sent) and their lookups 25816, 25818, 25820 and the
// board report 25840 (no reply in the protocol). Our rules, not captured.
func registerRoleExtra(r *gateway.Router, st *store.Store) {
	registerGrowWay(r, st)
	reply := func(cmd uint16, msg func(req proto.Message) proto.Message) {
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			if s.Role() == nil {
				return nil
			}
			return s.Send(cmd, msg(req))
		})
	}
	silent := func(cmd uint16) {
		r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error { return nil })
	}
	reply(10907, func(proto.Message) proto.Message { return proto.Message{"sys_ban": 0} })
	reply(10330, func(proto.Message) proto.Message {
		return proto.Message{"secret_id": "", "secret_key": "", "face_file": ""}
	})
	reply(10332, func(proto.Message) proto.Message {
		return proto.Message{"code": 0, "msg": "暂不支持自定义头像", "face_file": "", "face_update_time": 0}
	})
	reply(10333, func(proto.Message) proto.Message {
		return proto.Message{"code": 1, "msg": "", "face_file": "", "face_update_time": 0}
	})
	reply(25814, func(proto.Message) proto.Message { return proto.Message{"result": 0} })
	for _, cmd := range []uint16{25815, 25817, 25819, 25831} {
		reply(cmd, func(proto.Message) proto.Message { return proto.Message{"result": 1, "msg": ""} })
	}
	// no share is ever stored, so there is nothing to look up (the client opens its tip only for a share it sent)
	for _, cmd := range []uint16{25813, 25816, 25818, 25820, 25840} {
		silent(cmd)
	}
}
