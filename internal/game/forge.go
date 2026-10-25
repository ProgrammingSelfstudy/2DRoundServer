package game

import (
	_ "embed"
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// equip_compose.json: the forge house's compose steps (tools/extract_equip_compose.py).
//
//go:embed data/equip_compose.json
var equipComposeJSON []byte

type forgeStep struct{ ID, Need, N, Coin int64 }

var forgeChains = func() [][]forgeStep {
	var f struct {
		Chains []struct{ Steps []forgeStep } `json:"chains"`
	}
	if err := json.Unmarshal(equipComposeJSON, &f); err != nil || len(f.Chains) == 0 {
		panic("game: bad equip_compose.json")
	}
	out := make([][]forgeStep, len(f.Chains))
	for i, c := range f.Chains {
		out[i] = c.Steps
	}
	return out
}()

// forgeStepOf finds the step that makes an item and the steps before it in its chain.
func forgeStepOf(id int64) (step forgeStep, before []forgeStep, ok bool) {
	for _, chain := range forgeChains {
		for i, s := range chain {
			if s.ID == id {
				return s, chain[:i], true
			}
		}
	}
	return forgeStep{}, nil, false
}

type forgeMade struct{ ID, Num int64 }

// forgeAll plans composing as much as possible up to the step that makes `top`: from the lowest step up, each step
// makes as many as the pieces on hand and the coin left allow (the pieces made by a step feed the next one).
func forgeAll(top forgeStep, before []forgeStep, items map[int64]int64, coin int64) (made []forgeMade, spent int64, used map[int64]int64) {
	have := map[int64]int64{}
	for id, n := range items {
		have[id] = n
	}
	used = map[int64]int64{}
	for _, s := range append(append([]forgeStep{}, before...), top) {
		n := have[s.Need] / s.N
		if s.Coin > 0 {
			n = min(n, (coin-spent)/s.Coin)
		}
		if n <= 0 {
			continue
		}
		have[s.Need] -= n * s.N
		used[s.Need] += n * s.N
		have[s.ID] += n
		spent += n * s.Coin
		made = append(made, forgeMade{s.ID, n})
	}
	return
}

// RegisterForge registers the forge house, where equipment is composed from three pieces of the tier below and some
// coin: 11080 makes a number of one item, 11079 shows what composing everything up to an item would make, 11081
// does it, 11082 lists the records. Our rules, not captured.
func RegisterForge(r *gateway.Router, st *store.Store) {
	coinOf := func(rid int64) (int64, error) {
		a, err := st.RoleAssets(rid)
		return a[1], err
	}
	r.Handle(11079, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		top, before, ok := forgeStepOf(req.Int("base_id"))
		list := []proto.Message{}
		var spent int64
		if ok {
			items, err := st.RoleItems(role.RID)
			if err != nil {
				return err
			}
			coin, err := coinOf(role.RID)
			if err != nil {
				return err
			}
			var made []forgeMade
			made, spent, _ = forgeAll(top, before, items, coin)
			for _, m := range made {
				list = append(list, proto.Message{"bid": m.ID, "num": m.Num})
			}
		}
		return s.Send(11079, proto.Message{"type": 0, "coin": spent, "list": list})
	})
	// pay and deliver a plan, and write the record
	run := func(s *gateway.Session, role *store.Role, made []forgeMade, spent int64, used map[int64]int64) error {
		gain := map[int64]int64{}
		for _, m := range made {
			gain[m.ID] += m.Num
		}
		// what a step made and the next one used up is not given out
		net := map[int64]int64{}
		for id, n := range gain {
			net[id] = n
		}
		spendI := map[int64]int64{}
		for id, n := range used {
			if net[id] >= n {
				net[id] -= n
			} else {
				spendI[id] = n - net[id]
				net[id] = 0
			}
		}
		for id, n := range net {
			if n == 0 {
				delete(net, id)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{1: spent}, spendI, map[int64]int64{}, net); err != nil {
			return err
		}
		raw, _ := json.Marshal(made)
		if err := st.AddForgeLog(role.RID, store.ForgeLog{Time: s.Now().Unix(), Coin: spent, Items: string(raw)}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}
	r.Handle(11080, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(11080, proto.Message{"result": 0, "msg": msg}) }
		step, _, ok := forgeStepOf(req.Int("base_id"))
		num := req.Int("num")
		if !ok || num <= 0 {
			return refuse("该装备不能合成")
		}
		made := []forgeMade{{step.ID, num}}
		used := map[int64]int64{step.Need: step.N * num}
		if err := run(s, role, made, step.Coin*num, used); errors.Is(err, store.ErrConflict) {
			return refuse("合成材料不足或贝里不足")
		} else if err != nil {
			return err
		}
		return s.Send(11080, proto.Message{"result": 1, "msg": "合成成功"})
	})
	r.Handle(11081, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(11081, proto.Message{"result": 0, "msg": msg}) }
		top, before, ok := forgeStepOf(req.Int("base_id"))
		if !ok {
			return refuse("该装备不能合成")
		}
		items, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		coin, err := coinOf(role.RID)
		if err != nil {
			return err
		}
		made, spent, used := forgeAll(top, before, items, coin)
		if len(made) == 0 {
			return refuse("暂时无法合成任何装备或贝里不足")
		}
		if err := run(s, role, made, spent, used); errors.Is(err, store.ErrConflict) {
			return refuse("合成材料不足或贝里不足")
		} else if err != nil {
			return err
		}
		return s.Send(11081, proto.Message{"result": 1, "msg": "合成成功"})
	})
	r.Handle(11082, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		logs, err := st.ForgeLogs(role.RID)
		if err != nil {
			return err
		}
		out := []proto.Message{}
		for _, l := range logs {
			var made []forgeMade
			if json.Unmarshal([]byte(l.Items), &made) != nil {
				continue
			}
			items := []proto.Message{}
			for _, m := range made {
				items = append(items, proto.Message{"bid": m.ID, "num": m.Num})
			}
			out = append(out, proto.Message{"items": items, "time": l.Time, "coin": l.Coin})
		}
		return s.Send(11082, proto.Message{"logs": out})
	})
}
