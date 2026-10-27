package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// registerStubs registers refusal stubs: cmd -> reply, well formed and saying the feature is not open,
// so the client does not hang. They are NOT implementations; the coverage document counts them apart
// (every command registered through here is a stub). A nil reply means no reply at all.
func registerStubs(r *gateway.Router, replies map[uint16]func(req proto.Message) proto.Message) {
	for cmd, mk := range replies {
		cmd, mk := cmd, mk
		r.Handle(cmd, func(s *gateway.Session, req proto.Message) error {
			if s.Role() == nil || mk == nil {
				return nil
			}
			return s.Send(cmd, mk(req))
		})
	}
}

const notOpen = "该功能暂未开放"

// refuse is the stub reply of an action: result/code 0 and a message.
func refuse(field string, extra proto.Message) func(proto.Message) proto.Message {
	return func(proto.Message) proto.Message {
		m := proto.Message{field: 0, "msg": notOpen}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
}

func fixed(m proto.Message) func(proto.Message) proto.Message {
	return func(proto.Message) proto.Message { return m }
}

// registerHeroStubs covers what the hero module still lacks: artifacts (11030-11040, 11048, 11060),
// holy equipment (11086-11094, 25220-25224), soul weapons (11401-11405), resonance empowerment
// (26401, 26420-26432), the fuse red point (11055 / 11056) and 11018.
func registerHeroStubs(r *gateway.Router) {
	registerStubs(r, map[uint16]func(proto.Message) proto.Message{
		11018: fixed(proto.Message{}),
		11055: fixed(proto.Message{}),
		11056: fixed(proto.Message{"is_point": 0}),
		11037: fixed(proto.Message{"lucky": 0}),
		11040: fixed(proto.Message{"partners": []proto.Message{}, "all_star": 0, "lev": 0, "decompose_partners": []proto.Message{}}),
		11048: fixed(proto.Message{"artifact_ref_count": []proto.Message{}}),
		11030: func(req proto.Message) proto.Message {
			return proto.Message{"result": 0, "msg": notOpen, "partner_id": req.Int("partner_id")}
		},
		11032: refuse("result", nil), 11033: refuse("result", proto.Message{"luck_item": 0}), 11034: refuse("result", nil),
		11035: refuse("result", nil), 11036: refuse("result", proto.Message{"flag": 0}), 11038: refuse("result", nil),
		11039: refuse("result", proto.Message{"flag": 0}), 11060: refuse("result", nil),
		11086: func(req proto.Message) proto.Message { return proto.Message{"partner_id": req.Int("partner_id")} },
		11087: refuse("code", proto.Message{"list": []proto.Message{}}), 11088: refuse("code", proto.Message{"list": []proto.Message{}}),
		11089: refuse("code", nil), 11090: refuse("result", nil), 11093: refuse("result", nil), 11094: refuse("result", nil),
		25220: fixed(proto.Message{"num": 0, "holy_eqm_set_cell": []proto.Message{}}),
		25221: refuse("code", nil), 25223: refuse("code", proto.Message{"num": 0}), 25224: refuse("code", nil),
		11401: fixed(proto.Message{}),
		11402: refuse("result", nil), 11403: refuse("result", nil), 11404: refuse("result", nil), 11405: refuse("code", nil),
		26401: refuse("result", nil),
		26420: fixed(proto.Message{"star": 0, "num": 0}),
		26421: refuse("result", nil),
		26422: fixed(proto.Message{"skills": []proto.Message{}}),
		26423: func(req proto.Message) proto.Message {
			return proto.Message{"partner_id": req.Int("partner_id"), "power": 0}
		},
		26430: refuse("result", nil), 26431: refuse("result", proto.Message{"power": 0}), 26432: refuse("result", nil),
	})
}

// registerNoPaymentStubs answers the recharge side of vip and mall. There is no payment here (ROADMAP),
// so the lists are empty, the cards are not owned and every claim is refused. This is the intended end
// state, not a missing feature.
func registerNoPaymentStubs(r *gateway.Router) {
	registerStubs(r, map[uint16]func(proto.Message) proto.Message{
		21005: fixed(proto.Message{"count": 0, "gold": 0, "next_gold": 0}),
		21006: fixed(proto.Message{"first_gift": []proto.Message{}}),
		21035: fixed(proto.Message{}),
		24501: refuse("code", nil),
		24502: fixed(proto.Message{"list": []proto.Message{}}),
		13419: refuse("code", nil),
		16660: fixed(proto.Message{"holiday_exchanges": []proto.Message{}}),
		16661: func(req proto.Message) proto.Message {
			return proto.Message{"code": 0, "msg": notOpen, "bid": req.Int("bid"), "aim": req.Int("aim"), "buy_count": 0}
		},
		21022: fixed(proto.Message{"open_id": []proto.Message{}}),
		21023: refuse("code", nil),
		27800: fixed(proto.Message{"list": []proto.Message{}}),
		27801: refuse("code", nil),
	})
}
