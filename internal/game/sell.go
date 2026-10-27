package game

import (
	"errors"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// quote is the result of pricing a sell request.
type quote struct {
	sold   []store.ItemSale
	assets map[int64]int64 // currency label -> amount
	items  map[int64]int64 // item base id -> count
	msg    string          // why it failed; empty when it can go ahead
}

// priceSale checks a sell request (10521 preview, 10522 confirm) against the
// ledger. The request lists {id, bid, num}; the instance id of a stack is its
// base id, so a request whose two ids differ is refused. Every line must be a
// backpack item the client says can be sold (a flat value list), for no more
// than the role holds; the same item listed twice is added up first.
func priceSale(ledger map[int64]int64, args []proto.Message) quote {
	q := quote{assets: map[int64]int64{}, items: map[int64]int64{}}
	want := map[int64]int64{}
	for _, a := range args {
		id, bid, num := a.Int("id"), a.Int("bid"), a.Int("num")
		if id != bid || num <= 0 {
			q.msg = "物品不存在"
			return q
		}
		want[bid] += num
	}
	if len(want) == 0 {
		q.msg = "请选择要出售的物品"
		return q
	}
	ids := make([]int64, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		info, known := itemTable[id]
		switch {
		case !known || !goesToBackpack(info.Type) || ledger[id] <= 0:
			q.msg = "物品不存在"
			return q
		case len(info.Value) == 0:
			q.msg = "该物品不可出售"
			return q
		case ledger[id] < want[id]:
			q.msg = "物品数量不足"
			return q
		}
		q.sold = append(q.sold, store.ItemSale{ItemID: id, Num: want[id]})
		splitGains(info.Value, want[id], q.assets, q.items)
	}
	return q
}

// sellPreview answers 10521: what the listed items would sell for.
func sellPreview(st *store.Store) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		items := []proto.Message{}
		if req.Int("storage") == bagBackpack {
			ledger, err := st.RoleItems(role.RID)
			if err != nil {
				return err
			}
			if q := priceSale(ledger, req.Arr("args")); q.msg == "" {
				for _, id := range mergedKeys(q.assets, q.items) {
					n := q.assets[id]
					if n == 0 {
						n = q.items[id]
					}
					items = append(items, proto.Message{"bid": id, "num": n})
				}
			}
		}
		return s.Send(10521, proto.Message{"items": items})
	}
}

// sell answers 10522: sells the listed items for good, then re-sends the
// resources (10302) and the backpack (10500).
func sell(st *store.Store) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		fail := func(msg string) error { return s.Send(10522, proto.Message{"flag": 0, "msg": msg}) }
		if req.Int("storage") != bagBackpack {
			return fail("该背包暂不支持出售")
		}
		ledger, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		q := priceSale(ledger, req.Arr("args"))
		if q.msg != "" {
			return fail(q.msg)
		}
		if err := st.SellItems(role.RID, q.sold, q.assets, q.items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return fail("物品数量不足") // sold in another session between the check and the update
			}
			return err
		}
		if err := s.Send(10522, proto.Message{"flag": 1, "msg": "出售成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	}
}

// mergedKeys returns the ids of both maps, ascending; an id is in at most one.
func mergedKeys(a, b map[int64]int64) []int64 {
	out := make([]int64, 0, len(a)+len(b))
	for k := range a {
		out = append(out, k)
	}
	for k := range b {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// sendAssets sends 10302 for a role (the resource bar). When the role has gone up
// a level since the session last knew (experience was credited), it also sends
// 10344, which opens the client's level-up window with the rewards of the levels.
func sendAssets(s *gateway.Session, st *store.Store, rid int64) error {
	ledger, err := st.RoleAssets(rid)
	if err != nil {
		return err
	}
	fresh, err := st.RoleByID(rid)
	if err != nil {
		return err
	}
	if err := s.Send(10302, roleAssets(fresh, ledger)); err != nil {
		return err
	}
	if known := s.Role(); known != nil && known.RID == rid && fresh.Lev > known.Lev {
		old := known.Lev
		copied := *fresh
		s.SetRole(&copied)
		if err := s.Send(10344, proto.Message{"old_lev": old, "lev": fresh.Lev}); err != nil {
			return err
		}
		if err := pushLevelFeats(s, st, rid, fresh.Lev); err != nil {
			return err
		}
		return offerLimitGifts(s, st, rid, int64(fresh.Lev), 0, s.Now()) // a level that opens a limited gift
	}
	return nil
}
