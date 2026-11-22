package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

//go:embed data/backpack_actions.json
var backpackActionsJSON []byte

type shardRecipe struct {
	BID        int64
	Camp, Star int
	Need       int64
}

type backpackActionTable struct {
	Descriptions map[int64]string            `json:"descriptions"`
	HeroCards    map[int64]store.PartnerSeed `json:"hero_cards"`
	HeroShards   map[int64]shardRecipe       `json:"hero_shards"`
}

var backpackActions = func() backpackActionTable {
	var data backpackActionTable
	if err := json.Unmarshal(backpackActionsJSON, &data); err != nil || len(data.HeroShards) == 0 {
		panic("game: bad backpack_actions.json")
	}
	return data
}()

const (
	h5HeroStar = 5
	// Keep even a page of the larger catalog entries inside one protocol frame.
	maxBackpackDetails = 64
)

func isH5Session(s *gateway.Session) bool {
	account, role := s.Account(), s.Role()
	return account != nil && role != nil && account.ID == role.AccountID && account.Platform == store.PlatformH5
}

func heroCardForUse(id int64) (store.PartnerSeed, bool) {
	seed := backpackActions.HeroCards[id]
	_, starKnown := partnerTable[seed.BID].Stars[strconv.Itoa(seed.Star)]
	return seed, seed.Star == h5HeroStar && starKnown
}

// The generic shard table specifies star and camp. Use the eligible heroes
// at their initial star, with legendary heroes reserved for legendary shards.
// Camp 6 denotes the combined Marine and Emperor pool.
var shardPools = func() map[int64][]int64 {
	out := make(map[int64][]int64, len(backpackActions.HeroShards))
	for id, recipe := range backpackActions.HeroShards {
		if recipe.Star != h5HeroStar {
			continue
		}
		if recipe.BID != 0 {
			if _, ok := partnerTable[recipe.BID].Stars[strconv.Itoa(recipe.Star)]; ok {
				out[id] = []int64{recipe.BID}
			}
			continue
		}
		for bid, p := range partnerTable {
			inCamp := p.Camp == recipe.Camp || recipe.Camp == 0 && p.Camp != 7 ||
				recipe.Camp == 6 && (p.Camp == 4 || p.Camp == 5)
			if p.InitStar == recipe.Star && inCamp {
				out[id] = append(out[id], bid)
			}
		}
		slices.Sort(out[id])
	}
	return out
}()

type itemComposition struct {
	Cmd    uint16
	Target int64
	Name   string
	Cost   []sellGain
}

func compositionForMaterial(id int64) itemComposition {
	if pool := shardPools[id]; len(pool) > 0 {
		recipe := backpackActions.HeroShards[id]
		name := fmt.Sprintf("随机%d星英雄", recipe.Star)
		if recipe.BID != 0 {
			name = partnerTable[recipe.BID].Name
		}
		return itemComposition{11008, id, name, []sellGain{{id, recipe.Need}}}
	}
	for target, cost := range recipes {
		for _, item := range cost {
			if item.ID == id {
				return itemComposition{10523, target, itemNames[target], cost}
			}
		}
	}
	return itemComposition{}
}

func bagGains(list []sellGain) []proto.Message {
	out := make([]proto.Message, 0, len(list))
	for _, gain := range list {
		out = append(out, proto.Message{"bid": gain.ID, "num": gain.Amount})
	}
	return out
}

// 10560 is the H5 item detail catalogue. The client asks for base IDs already
// on screen, so it can offer the same operations the game server will accept.
// Quantities and ownership are always rechecked by each mutation.
func backpackDetails(s *gateway.Session, req proto.Message) error {
	if !isH5Session(s) {
		return nil
	}
	ids := req.Arr("items")
	details := []proto.Message{}
	seen := make(map[int64]bool)
	for _, row := range ids[:min(len(ids), maxBackpackDetails)] {
		id := row.Int("bid")
		info, known := itemTable[id]
		if !known || seen[id] {
			continue
		}
		seen[id] = true
		gift := giftTable[id]
		card, useCard := heroCardForUse(id)
		composition := compositionForMaterial(id)
		usable := goesToBackpack(info.Type) && (len(gift.Items) > 0 || useCard || id == retNum("red_item_id"))
		var useName string
		if useCard {
			useName = partnerTable[card.BID].Name
		}
		details = append(details, proto.Message{
			"bid": id, "name": itemNames[id], "description": backpackActions.Descriptions[id], "type": info.Type,
			"sellable": b2i(saleableItem(id, bagBackpack) || saleableItem(id, bagEquipment)),
			"usable":   b2i(usable), "use_name": useName,
			"gift_rand": gift.Rand, "rewards": bagGains(gift.Items),
			"compose_cmd": composition.Cmd, "compose_id": composition.Target, "compose_name": composition.Name,
			"cost": bagGains(composition.Cost), "main_attr": equipmentAttrs(equipmentTable[id]),
		})
	}
	return s.Send(10560, proto.Message{"items": details})
}

func composeHeroShards(st *store.Store) gateway.Handler {
	return func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil || !isH5Session(s) {
			return nil
		}
		reply := func(result int, msg string, heroes []store.Partner) error {
			partners := make([]proto.Message, 0, len(heroes))
			for _, h := range heroes {
				partners = append(partners, proto.Message{"partner_bid": h.BID})
			}
			return s.Send(11008, proto.Message{"result": result, "msg": msg, "partners": partners})
		}
		id, count := req.Int("bid"), req.Int("num")
		pool := shardPools[id]
		if len(pool) == 0 {
			return reply(0, "该碎片不能合成英雄", nil)
		}
		if count < 1 || count > int64(heroSlotCapacity(len(slotBuys))) {
			return reply(0, "合成数量不正确", nil)
		}
		recipe := backpackActions.HeroShards[id]
		seeds := make([]store.PartnerSeed, count)
		for i := range seeds {
			seeds[i] = store.PartnerSeed{BID: pool[rand.Intn(len(pool))], Star: recipe.Star}
		}
		heroes, err := st.ComposePartners(role.RID, id, recipe.Need*count, seeds, heroSlotCapacity)
		if err != nil {
			if msg := bagHeroError(err); msg != "" {
				return reply(0, msg, nil)
			}
			return err
		}
		if err := reply(1, "合成成功", heroes); err != nil {
			return err
		}
		return sendBagHeroes(s, st, role.RID, heroes)
	}
}

func useHeroCard(s *gateway.Session, st *store.Store, rid, id, count int64, card store.PartnerSeed, reply func(int, string) error) error {
	if count < 1 || count > int64(heroSlotCapacity(len(slotBuys))) {
		return reply(0, "使用数量不正确")
	}
	seeds := make([]store.PartnerSeed, count)
	for i := range seeds {
		seeds[i] = card
	}
	heroes, err := st.ComposePartners(rid, id, count, seeds, heroSlotCapacity)
	if err != nil {
		if msg := bagHeroError(err); msg != "" {
			return reply(0, msg)
		}
		return err
	}
	if err := reply(1, "使用成功"); err != nil {
		return err
	}
	return sendBagHeroes(s, st, rid, heroes)
}

func bagHeroError(err error) string {
	switch {
	case errors.Is(err, store.ErrHeroCapacity):
		return "英雄背包已满，无法放入这么多英雄"
	case errors.Is(err, store.ErrConflict):
		return "物品数量不足"
	}
	return ""
}

func sendBagHeroes(s *gateway.Session, st *store.Store, rid int64, heroes []store.Partner) error {
	out := make([]proto.Message, 0, len(heroes))
	for _, h := range heroes {
		out = append(out, partnerMessage(h))
	}
	// A full hero bag can hold more entries than one 11001 frame.
	const maxHeroesPerPush = 200
	for batch := range slices.Chunk(out, maxHeroesPerPush) {
		if err := s.Send(11001, proto.Message{"partners": batch}); err != nil {
			return err
		}
	}
	if err := sendBackpack(s, st); err != nil {
		return err
	}
	return sendRolePower(s, st, rid, 0)
}
