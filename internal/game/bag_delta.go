package game

import (
	"reflect"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Delta storage codes are not the full-list command suffixes: 10500 is
// storage1 and 10501 is storage3 (original 2026-11-04).
type bagItemKey struct{ storage, id int64 }
type bagSnapshot map[bagItemKey]proto.Message

// snapshotBags uses the same instances and positions as the full bag replies.
// Ordinary equipment is stacked; artifacts and holy equipment remain unique.
func snapshotBags(st *store.Store, rid int64) (bagSnapshot, error) {
	ledger, err := st.RoleItems(rid)
	if err != nil {
		return nil, err
	}
	arts, err := loadArtifacts(st, rid)
	if err != nil {
		return nil, err
	}
	holy, err := loadHoly(st, rid)
	if err != nil {
		return nil, err
	}
	bp, eq := backpackItems(ledger), equipmentBag(ledger)
	bp = append(bp, artifactBag(arts, len(bp)+1)...)
	eq = append(eq, holyBag(holy, len(eq)+1)...)
	out := bagSnapshot{}
	for storage, items := range map[int64][]proto.Message{1: bp, 3: eq} {
		for _, item := range items {
			item["storage"] = storage
			out[bagItemKey{storage, item.Int("id")}] = item
		}
	}
	return out, nil
}

// sendBagDelta sends removals, additions and changed absolute states. Quantity
// is the new balance, never the amount gained. Position changes also need an
// update so the client's cached slots agree with the next full bag reply.
func sendBagDelta(s *gateway.Session, before, after bagSnapshot) error {
	keys := make([]bagItemKey, 0, len(before)+len(after))
	for key := range before {
		keys = append(keys, key)
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].storage != keys[j].storage {
			return keys[i].storage < keys[j].storage
		}
		return keys[i].id < keys[j].id
	})
	var removed, added, updated []proto.Message
	for _, key := range keys {
		old, was := before[key]
		fresh, now := after[key]
		switch {
		case !now:
			removed = append(removed, proto.Message{"storage": key.storage, "id": key.id, "pos": old.Int("pos")})
		case !was:
			added = append(added, fresh)
		case !reflect.DeepEqual(old, fresh):
			updated = append(updated, fresh)
		}
	}
	for _, push := range []struct {
		cmd   uint16
		items []proto.Message
	}{{10511, removed}, {10510, added}, {10512, updated}} {
		if len(push.items) > 0 {
			if err := s.Send(push.cmd, proto.Message{"item_list": push.items}); err != nil {
				return err
			}
		}
	}
	return nil
}
