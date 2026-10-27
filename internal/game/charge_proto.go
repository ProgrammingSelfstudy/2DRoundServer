package game

import (
	"errors"
	"fmt"
	"strings"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// ChargeTierList is 16700: the diamond tiers of the top-up page with the extra of the first purchase marked for the ones
// the role never bought.
func ChargeTierList(st *store.Store, rid int64) ([]proto.Message, error) {
	rc, err := st.RoleCharge(rid)
	if err != nil {
		return nil, err
	}
	list := []proto.Message{}
	for _, p := range chargeTiers() {
		first := rc.Bought[fmt.Sprint(p.ID)] == 0
		extra := p.Add
		if first {
			extra = p.FirstAdd
		}
		list = append(list, proto.Message{"id": p.ID, "name": p.Name, "need_rmb": p.Cents(), "get_gold": p.Gold, "add_gold": extra, "is_first": b2i(first), "pic": p.Pic})
	}
	return list, nil
}

// SendChargeState pushes what a confirmed order changed: the diamonds and the backpack, the VIP experience and level (10301) and
// the top-up page (16700).
func SendChargeState(s *gateway.Session, st *store.Store, rid int64) error {
	if err := sendAssets(s, st, rid); err != nil {
		return err
	}
	if err := sendBackpack(s, st); err != nil {
		return err
	}
	role, err := st.RoleByID(rid)
	if err != nil {
		return err
	}
	if err := sendRoleBase(st, s, role); err != nil {
		return err
	}
	list, err := ChargeTierList(st, rid)
	if err != nil {
		return err
	}
	if err := s.Send(16700, proto.Message{"list": list}); err != nil {
		return err
	}
	rc, err := st.RoleCharge(rid)
	if err != nil {
		return err
	}
	d := &chargeAccState{}
	if _, err := st.LoadState(rid, chargeAccModule, d); err != nil {
		return err
	}
	if err := s.Send(16712, proto.Message{"charge_sum": accSum(rc), "list": chargeAccList(rc, d)}); err != nil {
		return err
	}
	// a fund that was just bought
	fd := &monthFundState{}
	if _, err := st.LoadState(rid, monthFundModule, fd); err != nil {
		return err
	}
	for i := range monthFunds {
		if err := s.Send(24701, monthFundMessage(fd, i, dayNo(s.Now()))); err != nil {
			return err
		}
	}
	return nil
}

// RegisterCharge registers the top-up side of the client: 16700 the tiers, 21016 whether a product can be bought and 10947 the
// request to pay, which opens an order and answers the address of its payment page (payBase is where the login service is
// reached from the phone). The order is paid by the QR code the operator put in the admin page and delivered when the operator
// confirms it there.
func RegisterCharge(r *gateway.Router, st *store.Store, payBase string) {
	r.Handle(16700, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list, err := ChargeTierList(st, role.RID)
		if err != nil {
			return err
		}
		return s.Send(16700, proto.Message{"list": list})
	})
	r.Handle(21016, func(s *gateway.Session, req proto.Message) error {
		id := req.Int("charge_id")
		p, ok := ChargeProductByID(id)
		return s.Send(21016, proto.Message{"charge_id": id, "status": b2i(ok && ChargeDeliverable(p))})
	})
	r.Handle(10947, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		o, err := CreateChargeOrder(st, role, req.Int("charge_id"), s.Now())
		switch {
		case errors.Is(err, ErrNoProduct), errors.Is(err, ErrNotPayable), errors.Is(err, ErrTooManyOpen):
			return s.Send(10947, proto.Message{"code": 0, "url": ""})
		case err != nil:
			return err
		}
		return s.Send(10947, proto.Message{"code": 1, "url": strings.TrimRight(payBase, "/") + "/pay/order/" + o.OrderNo})
	})
}
