package game

import (
	_ "embed"
	"encoding/json"
	"sort"

	"2droundserver/internal/proto"
)

//go:embed data/combathalo.json
var combatHaloJSON []byte

// combatHalo is a camp halo of a battle: it is on a side when the side has exactly these camps with exactly these head counts
// (client combat_halo_data.lua); `condition` names a weaker halo that this one replaces.
type combatHalo struct {
	ID        int64      `json:"id"`
	Condition int64      `json:"condition"`
	PosInfo   [][2]int64 `json:"pos_info"`
	Attrs     [][]any    `json:"attrs"`
}

var combatHalos = func() []combatHalo {
	var f struct {
		Halos []combatHalo `json:"halos"`
	}
	if err := json.Unmarshal(combatHaloJSON, &f); err != nil {
		panic("combathalo.json: " + err.Error())
	}
	return f.Halos
}()

// campHalos are the halos of a side with these camps, by id (the client's getFormIdListByCamp).
func campHalos(camps []int64) []int64 {
	count := map[int64]int64{}
	for _, c := range camps {
		count[c]++
	}
	have := map[int64]bool{}
	replaced := map[int64]bool{}
	for _, h := range combatHalos {
		ok := true
		for _, p := range h.PosInfo {
			if n, found := count[p[0]]; !found || n != p[1] {
				ok = false
			}
		}
		if ok {
			have[h.ID] = true
			if h.Condition != 0 {
				replaced[h.Condition] = true
			}
		}
	}
	var out []int64
	for id := range have {
		if !replaced[id] {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// haloType is how 20027's halo_list writes the halos of a side: none is 0, one is id*100, two are a*100+b, three are
// a*10000+b*100+c (captures of 2026-10-20, 10-22, 10-27: [7] 2400, [1,1] 1600, [3,3,3,3,4] 822, [1,1,2,3,3,4] 161822).
func haloType(ids []int64) int64 {
	switch len(ids) {
	case 0:
		return 0
	case 1:
		return ids[0] * 100
	case 2:
		return ids[0]*100 + ids[1]
	}
	return ids[0]*10000 + ids[1]*100 + ids[2]
}

// haloListOf is the halo_list of a battle: one entry per side, from the camps of the heroes and monsters in `objects` (object_type 2 and 3).
func haloListOf(objects []proto.Message) []proto.Message {
	camps := map[int64][]int64{}
	for _, o := range objects {
		if t := o.Int("object_type"); t == 2 || t == 3 { // heroes and monsters (a guild boss and campaign monsters have halos too)
			camps[o.Int("group")] = append(camps[o.Int("group")], o.Int("camp_type"))
		}
	}
	out := []proto.Message{}
	for _, g := range []int64{1, 2} {
		out = append(out, proto.Message{"group": g, "type": haloType(campHalos(camps[g]))})
	}
	return out
}

// fillHalo puts the halo_list into a battle intro that has none (the builders of the different fights send an empty one).
func fillHalo(intro proto.Message) {
	if l, ok := intro["halo_list"].([]proto.Message); ok && len(l) > 0 {
		return
	}
	if objs, ok := intro["objects"].([]proto.Message); ok {
		intro["halo_list"] = haloListOf(objs)
	}
}
