package game

import (
	_ "embed"
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// compose.json holds the client's compose recipes (tools/extract_compose.py).
//
//go:embed data/compose.json
var composeJSON []byte

// recipes maps the composed item's base id to what one piece costs. The
// client's extra cost ("loss") is empty for every recipe it has, so it is not
// modelled; a recipe that has one is left out and cannot be composed.
var recipes = func() map[int64][]sellGain {
	var f struct {
		Recipes map[int64]struct {
			Need [][2]int64 `json:"need"`
			Loss [][2]int64 `json:"loss"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(composeJSON, &f); err != nil || len(f.Recipes) == 0 {
		panic("game: bad compose.json")
	}
	out := map[int64][]sellGain{}
	for id, r := range f.Recipes {
		if len(r.Loss) > 0 {
			continue
		}
		for _, n := range r.Need {
			out[id] = append(out[id], sellGain{n[0], n[1]})
		}
	}
	return out
}()

// composeTimes is how many pieces to make: num 1 (the compose button) makes
// one, num 0 (fast compose) as many as the ledger allows, any other num that
// many. It is 0 when the role cannot afford even one.
func composeTimes(need []sellGain, ledger map[int64]int64, num int64) int64 {
	max := int64(-1)
	for _, n := range need {
		if t := ledger[n.ID] / n.Amount; max < 0 || t < max {
			max = t
		}
	}
	switch {
	case num == 0:
		return max
	case num <= max:
		return num
	}
	return 0
}

// RegisterCompose registers 10523 (compose an item from others in the backpack).
func RegisterCompose(r *gateway.Router, st *store.Store) {
	r.Handle(10523, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(flag int, msg string) error { return s.Send(10523, proto.Message{"flag": flag, "msg": msg}) }
		id := req.Int("id")
		need, ok := recipes[id]
		if !ok {
			return reply(0, "该物品不能合成")
		}
		ledger, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		times := composeTimes(need, ledger, req.Int("num"))
		if times <= 0 {
			return reply(0, "材料不足")
		}
		sold := make([]store.ItemSale, 0, len(need))
		for _, n := range need {
			sold = append(sold, store.ItemSale{ItemID: n.ID, Num: n.Amount * times})
		}
		if err := st.SellItems(role.RID, sold, nil, map[int64]int64{id: times}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "材料不足")
			}
			return err
		}
		if err := reply(1, "合成成功"); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
