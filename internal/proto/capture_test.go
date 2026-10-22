package proto

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

const probeDir = "../../testdata/captures/wire-probe-2026-10-18/"

// Known values the probe fed to the native client (probe_main.lua). Decoding the
// bytes the client really put on the wire must give these back.
var probeSend = map[int][]Message{
	1197:  {{"sign": "ABCD", "idx": int64(0x01020304), "code": int64(0x0506), "time": int64(0x0708090A)}},
	1164:  {{"args": []Message{{"key": "k1", "val": "v1"}, {"key": "k2", "val": "v2v"}}}, {"args": []Message{}}},
	10191: {{"sex": int64(1), "name": "", "career": int64(0x0102), "playform": "p"}},
	10192: {{"rid": int64(0x11223344), "srv_id": "a_1"}},
	1169:  {{}},
}

func TestReplayProbeSendFrames(t *testing.T) {
	s, _ := LoadSchema()
	o := DefaultOptions()
	f, err := os.Open(probeDir + "send_frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seen := map[int]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row struct {
			Cmd        int    `json:"cmd"`
			PayloadHex string `json:"payload_hex"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		raw, _ := hex.DecodeString(row.PayloadHex)
		fields := s.Send[uint16(row.Cmd)]
		got, err := Decode(o, fields, raw)
		if err != nil {
			t.Fatalf("cmd %d: %v", row.Cmd, err)
		}
		want := probeSend[row.Cmd][seen[row.Cmd]]
		seen[row.Cmd]++
		if !reflect.DeepEqual(map[string]any(got), map[string]any(want)) {
			t.Fatalf("cmd %d: got %#v want %#v", row.Cmd, got, want)
		}
		again, _ := Encode(o, fields, got)
		if hex.EncodeToString(again) != row.PayloadHex {
			t.Fatalf("cmd %d: re-encode %x != %s", row.Cmd, again, row.PayloadHex)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 distinct commands, saw %v", seen)
	}
}

// Server->client direction: bytes built by the capture server were decoded by
// the real native client into the values below (see probe-log.txt).
func TestReplayProbeRecvFrames(t *testing.T) {
	s, _ := LoadSchema()
	o := DefaultOptions()
	var rows []struct {
		Cmd        int    `json:"cmd"`
		PayloadHex string `json:"payload_hex"`
	}
	b, _ := os.ReadFile(probeDir + "recv_frames.json")
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	want := map[int]Message{
		10192: {"code": 1, "msg": "ok", "timestamp": 0x01020304, "world_lev": 0x0506},
		1164: {"code": 1, "msg": "", "least_career": 9, "roles": []Message{{
			"rid": 0x0A0B0C0D, "srv_id": "a_1", "name": "路飞", "lev": 0x0102, "sex": 1,
			"career": 0x0304, "face_id": 0x05060708, "is_online": 1}}},
	}
	for _, r := range rows {
		enc, err := Encode(o, s.Recv[uint16(r.Cmd)], want[r.Cmd])
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(enc) != r.PayloadHex {
			t.Fatalf("cmd %d: encode %x != captured %s", r.Cmd, enc, r.PayloadHex)
		}
	}
}

// Real client, real login flow (testdata/captures/real-login-2026-10-18).
// The capture server answered the heartbeat with canned 10192/1164 frames; the
// client accepted them and went on to send 10192 (with the fake role) and 10300.
func TestReplayRealLoginFrames(t *testing.T) {
	s, _ := LoadSchema()
	o := DefaultOptions()
	f, err := os.Open("../../testdata/captures/real-login-2026-10-18/capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var sawHandshake bool
	var pending Message // the last 1197, which signs the next frame
	signed, seen := 0, map[int]int{}
	var first1164 Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var row struct {
			Event      string `json:"event"`
			Cmd        int    `json:"cmd"`
			PayloadHex string `json:"payload_hex"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Event == "handshake" {
			sawHandshake = true
		}
		if row.Event != "frame" {
			continue
		}
		raw, _ := hex.DecodeString(row.PayloadHex)
		m, err := Decode(o, s.Send[uint16(row.Cmd)], raw)
		if err != nil {
			t.Fatalf("cmd %d: %v", row.Cmd, err)
		}
		if again, _ := Encode(o, s.Send[uint16(row.Cmd)], m); hex.EncodeToString(again) != row.PayloadHex {
			t.Fatalf("cmd %d: re-encode differs", row.Cmd)
		}
		seen[row.Cmd]++
		if row.Cmd == 1197 {
			pending = m
			continue
		}
		if row.Cmd == 1164 && first1164 == nil {
			first1164 = m
		}
		if pending != nil { // 1197 signs the frame that follows it
			sum := md5.Sum([]byte(fmt.Sprintf("%d%d%d__sszg666__", pending.Int("idx"), pending.Int("time"), row.Cmd)))
			if hex.EncodeToString(sum[:]) != pending.Str("sign") {
				t.Fatalf("sign mismatch for cmd %d: %x vs %s", row.Cmd, sum, pending.Str("sign"))
			}
			if pending.Int("code") != int64(row.Cmd)+(pending.Int("idx")+5)%16 {
				t.Fatalf("code formula for cmd %d: %d", row.Cmd, pending.Int("code"))
			}
			signed++
			pending = nil
		} else if row.Cmd != 10300 { // 10300 is exempt from signing (gamenet.lua SendPrev)
			t.Fatalf("cmd %d was not preceded by 1197", row.Cmd)
		}
	}
	if !sawHandshake || signed < 3 || seen[10192] == 0 || seen[10300] == 0 {
		t.Fatalf("capture incomplete: handshake=%v signed=%d seen=%v", sawHandshake, signed, seen)
	}
	args := map[string]string{}
	for _, kv := range first1164.Arr("args") {
		args[kv.Str("key")] = kv.Str("val")
	}
	if len(args) != 26 || args["platform"] != "symlf" || args["enter_srv_id"] != "symlf_1" || args["account"] == "" {
		t.Fatalf("unexpected 1164 args: %v", args)
	}
}
