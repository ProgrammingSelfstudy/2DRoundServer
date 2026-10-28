package game

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"2droundserver/internal/proto"
)

// Every 20027 of the original captures (2026-10-20 ... 2026-10-27) carries a halo_list; the camps of each side must give its type.
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
		fh.Close()
	}
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
