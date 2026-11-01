package game

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"2droundserver/internal/proto"
)

// Compare every retained 20027, including the newly captured five-camp combinations.
func TestHaloOfCaptures(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/captures/*/*.jsonl")
	checked := 0
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			var r struct {
				Cmd uint16 `json:"cmd"`
				Dir string `json:"dir"`
				Msg struct {
					Halo []struct{ Group, Type int64 } `json:"halo_list"`
					Objs []struct {
						Type, Group, Camp int64
					} `json:"-"`
				} `json:"msg"`
			}
			line := sc.Bytes()
			if err := json.Unmarshal(line, &r); err != nil || r.Cmd != 20027 || r.Dir != "s2c" {
				continue
			}
			var raw struct {
				Msg struct {
					Objects []struct {
						ObjectType int64 `json:"object_type"`
						Group      int64 `json:"group"`
						Camp       int64 `json:"camp_type"`
					} `json:"objects"`
				} `json:"msg"`
			}
			if err := json.Unmarshal(line, &raw); err != nil {
				t.Fatal(err)
			}
			var objs []proto.Message
			for _, o := range raw.Msg.Objects {
				objs = append(objs, proto.Message{"object_type": o.ObjectType, "group": o.Group, "camp_type": o.Camp})
			}
			got := map[int64]int64{}
			for _, h := range haloListOf(objs) {
				got[h.Int("group")] = h.Int("type")
			}
			for _, h := range r.Msg.Halo {
				if got[h.Group] != h.Type {
					t.Errorf("%s: group %d: halo type %d, the original sent %d", f, h.Group, got[h.Group], h.Type)
				}
				checked++
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("read capture %s: %v", f, err)
		}
		fh.Close()
	}
	t.Logf("checked %d original halo entries across %d retained JSONL files", checked, len(files))
	if checked < 7 {
		t.Fatalf("only %d halo entries checked", checked)
	}
}

func TestFillHaloKeepsOwn(t *testing.T) {
	intro := proto.Message{"halo_list": []proto.Message{{"group": 1, "type": 5}}, "objects": []proto.Message{}}
	fillHalo(intro)
	if l := intro["halo_list"].([]proto.Message); len(l) != 1 || l[0].Int("type") != 5 {
		t.Fatalf("overwritten: %v", l)
	}
	intro = proto.Message{"halo_list": []proto.Message{}, "objects": []proto.Message{{"object_type": int64(2), "group": int64(1), "camp_type": int64(7)}}}
	fillHalo(intro)
	if l := intro["halo_list"].([]proto.Message); len(l) != 2 || l[0].Int("type") != 2400 {
		t.Fatalf("not filled: %v", l)
	}
}

// Expectations come from the client table plus the independently captured wire IDs.
// In particular, formation-preview Condition must not remove any of 21,22,23.
func TestBattleHaloCampCountsAndEncoding(t *testing.T) {
	cases := []struct {
		name       string
		camps, ids []int64
		wire       int64
	}{
		{"empty", nil, nil, 0},
		{"no matching row", []int64{1}, nil, 0},
		{"single special camp", []int64{7}, []int64{24}, 2400},
		{"five same camp", []int64{1, 1, 1, 1, 1}, []int64{1}, 100},
		{"six same camp", []int64{1, 1, 1, 1, 1, 1}, []int64{30}, 3000},
		{"four plus light", []int64{3, 3, 3, 3, 4}, []int64{8, 22}, 822},
		{"three simultaneous matches", []int64{1, 1, 2, 3, 3, 4}, []int64{16, 18, 22}, 161822},
		{"five distinct camps", []int64{1, 2, 3, 4, 5}, []int64{21, 22, 23}, 212223},
		{"permuted five camps", []int64{5, 3, 1, 4, 2}, []int64{21, 22, 23}, 212223},
		{"missing a base camp", []int64{1, 2, 4, 5}, []int64{22, 23}, 2223},
		{"duplicate dark camp", []int64{1, 2, 3, 4, 5, 5}, []int64{20, 22}, 2022},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids := campHalos(tc.camps)
			if !slices.Equal(ids, tc.ids) {
				t.Fatalf("camps %v: IDs %v, want %v", tc.camps, ids, tc.ids)
			}
			if got := haloType(ids); got != tc.wire {
				t.Fatalf("IDs %v: wire %d, want %d", ids, got, tc.wire)
			}
		})
	}
}
func TestBattleHaloKeepsSidesAndActorKindsSeparate(t *testing.T) {
	objects := []proto.Message{}
	for _, camp := range []int64{1, 2, 3, 4, 5} {
		objects = append(objects, proto.Message{"object_type": 2, "group": 1, "camp_type": camp})
	}
	objects = append(objects, proto.Message{"object_type": 3, "group": 2, "camp_type": 7}, proto.Message{"object_type": 4, "group": 2, "camp_type": 4})
	got := haloListOf(objects)
	if len(got) != 2 || got[0].Int("group") != 1 || got[0].Int("type") != 212223 || got[1].Int("group") != 2 || got[1].Int("type") != 2400 {
		t.Fatalf("hero/monster sides mixed or non-actor counted: %v", got)
	}
}
