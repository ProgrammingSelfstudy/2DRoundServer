package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// gifts.json is the client's gift box table (tools/extract_gifts.py): per gift
// id, rand (0 = every entry, n = n random entries) and its [id, amount] entries.
// Gifts the client holds no content for are not in it.
//
//go:embed data/gifts.json
var giftsJSON []byte

type giftInfo struct {
	Rand  int
	Items []sellGain
}

var giftTable = func() map[int64]giftInfo {
	var f struct {
		Gifts map[int64]struct {
			Rand  int        `json:"rand"`
			Items [][2]int64 `json:"items"`
		} `json:"gifts"`
	}
	if err := json.Unmarshal(giftsJSON, &f); err != nil || len(f.Gifts) == 0 {
		panic("game: bad gifts.json")
	}
	out := make(map[int64]giftInfo, len(f.Gifts))
	for id, g := range f.Gifts {
		info := giftInfo{Rand: g.Rand}
		for _, it := range g.Items {
			info.Items = append(info.Items, sellGain{it[0], it[1]})
		}
		out[id] = info
	}
	return out
}()

// maxUseAtOnce caps one 10515 request (our policy).
const maxUseAtOnce = 1000

// openGifts works out what opening n boxes of gift id gives. rand 0 gives every
// entry once per box; rand k gives k entries per box, drawn without repeating
// within a box (our reading of rand_num: the client has no text for it). pick
// returns a random index below its argument.
func openGifts(g giftInfo, n int64, pick func(int) int) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	if g.Rand <= 0 {
		splitGains(g.Items, n, assets, items)
		return
	}
	k := g.Rand
	if k > len(g.Items) {
		k = len(g.Items)
	}
	for ; n > 0; n-- {
		pool := append([]sellGain(nil), g.Items...)
		for i := 0; i < k; i++ {
			j := pick(len(pool))
			splitGains(pool[j:j+1], 1, assets, items)
			pool = append(pool[:j], pool[j+1:]...)
		}
	}
	return
}

// RegisterUse registers 10515 (use an item): gift boxes the client has a
// table for turn into their content. Other kinds of use are refused.
func RegisterUse(r *gateway.Router, st *store.Store) {
	r.Handle(10515, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id, n := req.Int("id"), req.Int("quantity")
		reply := func(flag int, msg string) error {
			return s.Send(10515, proto.Message{"flag": flag, "base_id": id, "msg": msg})
		}
		info, known := itemTable[id]
		gift, isGift := giftTable[id]
		switch {
		case !known || !goesToBackpack(info.Type):
			return reply(0, "物品不存在")
		case id == retNum("red_item_id"):
			return returnRedbagUse(s, st, role, n, reply)
		case !isGift:
			return reply(0, "该物品暂不能使用")
		case n <= 0 || n > maxUseAtOnce:
			return reply(0, "使用数量不正确")
		}
		assets, items := openGifts(gift, n, rand.Intn)
		if err := st.SellItems(role.RID, []store.ItemSale{{ItemID: id, Num: n}}, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "物品数量不足")
			}
			return err
		}
		if err := reply(1, "使用成功"); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
