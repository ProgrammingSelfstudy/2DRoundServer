package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// items.json is the client's item table reduced to what the bag needs:
// type, overlap and expire_type per base id (tools/extract_items.py).
//
//go:embed data/items.json
var itemsJSON []byte

// item_names.json holds the client's item names for the admin pages.
//
//go:embed data/item_names.json
var itemNamesJSON []byte

var itemNames = func() map[int64]string {
	var f struct {
		Names map[int64]string `json:"names"`
	}
	if err := json.Unmarshal(itemNamesJSON, &f); err != nil || len(f.Names) == 0 {
		panic("game: bad item_names.json")
	}
	return f.Names
}()

// ItemName is the client's name for an item base id.
func ItemName(id int64) (string, bool) { n, ok := itemNames[id]; return n, ok }

// ItemListedInBackpack reports whether the game's backpack (10500) shows this item.
func ItemListedInBackpack(id int64) bool {
	info, ok := itemTable[id]
	return ok && goesToBackpack(info.Type)
}

// sellGain is one thing a sold item turns into: an asset (currency) or an item.
type sellGain struct{ ID, Amount int64 }

type itemInfo struct {
	Type, Overlap, ExpireType int
	Value                     []sellGain // what one piece sells for; empty = cannot be sold
}

var itemTable = func() map[int64]itemInfo {
	var f struct {
		Items map[int64][]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(itemsJSON, &f); err != nil || len(f.Items) == 0 {
		panic("game: bad items.json")
	}
	out := make(map[int64]itemInfo, len(f.Items))
	for id, v := range f.Items {
		var nums [3]int
		var value [][2]int64
		for i := range nums {
			if err := json.Unmarshal(v[i], &nums[i]); err != nil {
				panic("game: bad items.json")
			}
		}
		if err := json.Unmarshal(v[3], &value); err != nil {
			panic("game: bad items.json")
		}
		info := itemInfo{Type: nums[0], Overlap: nums[1], ExpireType: nums[2]}
		for _, g := range value {
			info.Value = append(info.Value, sellGain{g[0], g[1]})
		}
		out[id] = info
	}
	return out
}()

// Bag codes (backpack_const.lua Bag_Code). Only the normal backpack has content.
const (
	bagBackpack = 1
	// bagVolume is how many slots every bag reports. The original value is
	// unknown, so this is our own policy.
	bagVolume = 100
)

// goesToBackpack reports whether an item of this type lives in the normal
// backpack (10500). Currencies (type 7) are not items, they are in 10302;
// equipment (10501) needs per-piece attributes that the ledger does not hold;
// the home and sprite types (30-38) belong to other bags whose commands are
// not implemented. Those stay in the ledger and are simply not listed.
func goesToBackpack(t int) bool {
	switch t {
	case 7, 1, 2, 3, 4, 23, 24, 25, 26:
		return false
	}
	return t < 30 || t > 38
}

// backpackItems lists the ledger as 10500 item_list entries, ordered by base id.
// One stack per base id: the instance id is the base id, which is what the
// client sends back when it uses or sells the stack. Ids missing from the
// client's item table are skipped (the client could not draw them).
func backpackItems(ledger map[int64]int64) []proto.Message {
	ids := make([]int64, 0, len(ledger))
	for id, n := range ledger {
		if n <= 0 {
			continue
		}
		if info, ok := itemTable[id]; ok && goesToBackpack(info.Type) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]proto.Message, 0, len(ids))
	for i, id := range ids {
		out = append(out, proto.Message{
			"id": id, "base_id": id, "bind": 0, "quantity": ledger[id], "pos": i + 1,
			"expire_type": itemTable[id].ExpireType, "expire_time": 0,
			"main_attr": []proto.Message{}, "enchant": 0, "attr": []proto.Message{}, "extra_attr": []proto.Message{},
			"score": 0, "all_score": 0, "extra": []proto.Message{},
		})
	}
	return out
}

// spriteBag lists the sprites, eggs and sprite items of the ledger (types 36-38) and the food, items and treasures of the
// home pet (types 31-33, homepet.go) as the sprite bag (10503).
func spriteBag(ledger map[int64]int64) []proto.Message {
	ids := make([]int64, 0)
	for id, n := range ledger {
		if info, ok := itemTable[id]; ok && n > 0 && (info.Type >= 36 && info.Type <= 38 || info.Type >= 31 && info.Type <= 33) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]proto.Message, 0, len(ids))
	for i, id := range ids {
		out = append(out, proto.Message{
			"id": id, "base_id": id, "bind": 0, "quantity": ledger[id], "pos": i + 1,
			"expire_type": itemTable[id].ExpireType, "expire_time": 0,
			"main_attr": []proto.Message{}, "enchant": 0, "attr": []proto.Message{}, "extra_attr": []proto.Message{},
			"score": 0, "all_score": 0, "extra": []proto.Message{},
		})
	}
	return out
}

func bagReply(s *gateway.Session, items []proto.Message) proto.Message {
	if items == nil {
		items = []proto.Message{}
	}
	// server_time changes on every send: the client then drops what it cached and
	// takes this list as the whole truth.
	return proto.Message{"volume": bagVolume, "open_times": 0, "server_time": s.Now().UnixNano() / 1e6 % 0xFFFFFFFF, "item_list": items}
}

// RegisterBag registers the bag commands: 10500 (backpack, from the item ledger)
// 10502 (home bag: the furniture, homeworld.go) and 10503 (the sprite bag; the equipment bag 10501 is in equipment.go).
func RegisterBag(r *gateway.Router, st *store.Store) {
	r.Handle(10500, func(s *gateway.Session, _ proto.Message) error { return sendBackpack(s, st) })
	r.Handle(10521, sellPreview(st))
	r.Handle(10522, sell(st))
	r.Handle(10502, func(s *gateway.Session, _ proto.Message) error { return sendHomeBag(s, st) })
	r.Handle(10503, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ledger, err := st.RoleItems(role.RID)
		if err != nil {
			return err
		}
		return s.Send(10503, bagReply(s, spriteBag(ledger)))
	})
}

// sendBackpack answers 10500 for the session's role (also used to refresh the
// client's backpack after mail attachments were claimed). The equipment bag (10501)
// lives in the same item ledger and the client keeps its own copy of it, so it is
// sent with it: a piece that arrives by mail, shop or gift box shows up in the hero's
// equipment window without the client asking (seen on a device: it said
// "背包中无可穿戴装备" after the mail had been claimed).
func sendBackpack(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	ledger, err := st.RoleItems(role.RID)
	if err != nil {
		return err
	}
	if err := s.Send(10500, bagReply(s, backpackItems(ledger))); err != nil {
		return err
	}
	if err := s.Send(10501, bagReply(s, equipmentBag(ledger))); err != nil {
		return err
	}
	// the sprite and home bags follow only when there is something in them; their handlers always send them
	if furniture := homeBag(ledger); len(furniture) > 0 {
		if err := s.Send(10502, bagReply(s, furniture)); err != nil {
			return err
		}
	}
	if sprites := spriteBag(ledger); len(sprites) > 0 {
		return s.Send(10503, bagReply(s, sprites))
	}
	return nil
}

// sendSpriteBag sends the sprite bag (10503) whatever is in it.
func sendSpriteBag(s *gateway.Session, st *store.Store) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	ledger, err := st.RoleItems(role.RID)
	if err != nil {
		return err
	}
	return s.Send(10503, bagReply(s, spriteBag(ledger)))
}

// ItemSummary is what the admin pages show about an item.
type ItemSummary struct {
	BaseID     int64  `json:"base_id"`
	Name       string `json:"name"`
	Type       int    `json:"type"`
	Overlap    int    `json:"overlap"`     // stack limit in the client's table
	Sellable   bool   `json:"sellable"`    // has a sell value
	InBackpack bool   `json:"in_backpack"` // the game's backpack (10500) lists it
	Currency   bool   `json:"currency"`    // a currency (type 7): it belongs in the asset ledger, not in a mail's item list
}

func summary(id int64) ItemSummary {
	info := itemTable[id]
	return ItemSummary{BaseID: id, Name: itemNames[id], Type: info.Type, Overlap: info.Overlap, Sellable: len(info.Value) > 0,
		InBackpack: goesToBackpack(info.Type), Currency: info.Type == 7}
}

// ItemKnown reports whether the client has an item with this base id.
func ItemKnown(id int64) bool { _, ok := itemTable[id]; return ok }

// ItemIsCurrency reports whether the id is a currency (item type 7).
func ItemIsCurrency(id int64) bool { return itemTable[id].Type == 7 }

// SearchItems finds items by base id (the digits are a prefix of the id) or by name
// (contains the text), ordered by id, at most limit of them.
func SearchItems(q string, limit int) []ItemSummary {
	q = strings.TrimSpace(q)
	out := []ItemSummary{}
	if q == "" || limit <= 0 {
		return out
	}
	digits := strings.Trim(q, "0123456789") == ""
	ids := make([]int64, 0, len(itemTable))
	for id := range itemTable {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		hit := strings.Contains(itemNames[id], q)
		if digits {
			hit = hit || strings.HasPrefix(strconv.FormatInt(id, 10), q)
		}
		if hit {
			out = append(out, summary(id))
			if len(out) == limit {
				break
			}
		}
	}
	return out
}
