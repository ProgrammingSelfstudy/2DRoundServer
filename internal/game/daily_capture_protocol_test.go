package game

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
)

// Compare complete payloads with the retained, sanitized original-client
// evidence, rather than deriving the expected replies from the local tables.
func assertDailyCaptureReply(t *testing.T, sourceLine int, got capturedReply) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/captures/original-2026-11-05-daily/daily-evidence.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row struct {
			Direction  string          `json:"dir"`
			Command    uint16          `json:"cmd"`
			SourceLine int             `json:"source_line"`
			Message    json.RawMessage `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row.Direction != "s2c" || row.SourceLine != sourceLine {
			continue
		}
		actual, err := json.Marshal(got.msg)
		if err != nil {
			t.Fatal(err)
		}
		var want, have any
		if err := json.Unmarshal(row.Message, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(actual, &have); err != nil {
			t.Fatal(err)
		}
		if got.cmd != row.Command || !reflect.DeepEqual(have, want) {
			t.Fatalf("daily source line %d: got %d %s, want %d %s", sourceLine, got.cmd, actual, row.Command, row.Message)
		}
		return
	}
	t.Fatalf("missing daily evidence source line %d", sourceLine)
}

func dailyBackpack(c *client) map[int64]proto.Message {
	c.t.Helper()
	for _, frame := range captureReplies(c, 10500, proto.Message{}) {
		if frame.cmd == 10500 {
			out := map[int64]proto.Message{}
			for _, item := range frame.msg.Arr("item_list") {
				out[item.Int("id")] = item
			}
			return out
		}
	}
	c.t.Fatal("missing full backpack")
	return nil
}

func applyDailyBagUpdates(c *client, cached map[int64]proto.Message, frames []capturedReply, base, quantity int64) {
	c.t.Helper()
	var rewardUpdates int
	for _, frame := range frames {
		switch frame.cmd {
		case 10510, 10512:
			for _, item := range frame.msg.Arr("item_list") {
				if item.Int("storage") != 1 {
					c.t.Fatalf("daily reward in wrong storage: %v", item)
				}
				if item.Int("base_id") == base {
					rewardUpdates++
					if item.Int("quantity") != quantity || item.Int("bind") != 1 {
						c.t.Fatalf("daily absolute quantity/binding: %v", item)
					}
					if old, ok := cached[item.Int("id")]; ok && (old.Int("base_id") != base || old.Int("pos") != item.Int("pos")) {
						c.t.Fatalf("existing reward stack identity changed: old=%v new=%v", old, item)
					}
				}
				copy := proto.Message{}
				for key, value := range item {
					if key != "storage" {
						copy[key] = value
					}
				}
				// Delta schemas also contain empty equipment extensions
				// absent from10500; compare the full-backpack projection.
				payload, err := proto.Encode(proto.DefaultOptions(), c.schema.Recv[10500], proto.Message{"item_list": []proto.Message{copy}})
				if err != nil {
					c.t.Fatal(err)
				}
				normalized, err := proto.Decode(proto.DefaultOptions(), c.schema.Recv[10500], payload)
				if err != nil {
					c.t.Fatal(err)
				}
				cached[item.Int("id")] = normalized.Arr("item_list")[0]
			}
		case 10500, 10501:
			c.t.Fatal("daily claim refreshed a full bag")
		}
	}
	if fresh := dailyBackpack(c); rewardUpdates != 1 || !reflect.DeepEqual(cached, fresh) {
		c.t.Fatalf("daily cache differs from full backpack: updates=%d cached=%v fresh=%v", rewardUpdates, cached, fresh)
	}
}

// Daily sources695,699-701,728: status0 -> absolute bag -> reward -> empty
// success/status1, also retained when reopening. An absent stack must be added.
func TestCapturedDailyCheckinNotifications(t *testing.T) {
	for _, initial := range []int64{3, 0} {
		t.Run(strconv.FormatInt(initial, 10), func(t *testing.T) {
			e := newEnv(t, time.Minute)
			e.setNow(time.Date(2026, 11, 5, 7, 46, 44, 0, dayZone))
			c, role := e.enter("daily-checkin", "签到")
			for count := int64(0); count < 3; count++ {
				if err := e.st.DoCheckin(role.RID, 202611, 20261101+count, count, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			e.addItems(t, role, map[int64]int64{99955: initial, 10403: 2})
			cached := dailyBackpack(c)
			beforeAssets, err := e.st.RoleAssets(role.RID)
			if err != nil {
				t.Fatal(err)
			}
			state := captureReplies(c, 14100, proto.Message{})
			if len(state) != 1 {
				t.Fatalf("sign-in state pushes: %v", replyCodes(state))
			}
			assertDailyCaptureReply(t, 695, state[0])
			frames := captureReplies(c, 14101, proto.Message{})
			bagCommand := uint16(10512)
			if initial == 0 {
				bagCommand = 10510
			}
			if !reflect.DeepEqual(replyCodes(frames), []uint16{bagCommand, 12742, 14101}) {
				t.Fatalf("sign-in notifications: %v", replyCodes(frames))
			}
			assertDailyCaptureReply(t, 700, frames[1])
			assertDailyCaptureReply(t, 701, frames[2])
			applyDailyBagUpdates(c, cached, frames, 99955, initial+1)
			reopened := captureReplies(c, 14100, proto.Message{})
			if len(reopened) != 1 {
				t.Fatalf("reopened sign-in state pushes: %v", replyCodes(reopened))
			}
			assertDailyCaptureReply(t, 728, reopened[0])
			// Extra-claim conditions are unknown; retain the existing guard
			// against granting the free sign-in more than once in a day.
			refused := captureReplies(c, 14101, proto.Message{})
			if !reflect.DeepEqual(replyCodes(refused), []uint16{14101}) || refused[0].msg.Int("code") != 0 || refused[0].msg.Int("day") != 4 || refused[0].msg.Int("status") != 1 {
				t.Fatalf("duplicate sign-in pushed rewards: %v", refused)
			}
			items, err := e.st.RoleItems(role.RID)
			if err != nil || !reflect.DeepEqual(items, map[int64]int64{99955: initial + 1, 10403: 2}) {
				t.Fatalf("sign-in ledger: %v %v", items, err)
			}
			afterAssets, err := e.st.RoleAssets(role.RID)
			if err != nil || !reflect.DeepEqual(beforeAssets, afterAssets) {
				t.Fatalf("item-only sign-in changed currencies: %v %v %v", beforeAssets, afterAssets, err)
			}
			stored, err := e.st.Checkin(role.RID)
			if err != nil || stored.Count != 4 || stored.LastDay != 20261105 {
				t.Fatalf("sign-in count advanced more than once: %+v %v", stored, err)
			}
		})
	}
}

// Daily sources437,755-758: request index12, bag balance7, reward5, descending
// threshold list including14400, then empty success/time14400.
func TestCapturedOnlineGiftNotifications(t *testing.T) {
	for _, initial := range []int64{2, 0} {
		t.Run(strconv.FormatInt(initial, 10), func(t *testing.T) {
			e := newEnv(t, time.Minute)
			e.setNow(time.Date(2026, 11, 5, 7, 51, 23, 0, dayZone))
			c, role := e.enter("daily-online", "在线")
			if err := e.st.AddOnlineSeconds(role.RID, 20261105, 71460); err != nil {
				t.Fatal(err)
			}
			for _, threshold := range []int64{60, 150, 210, 530, 840, 1680, 2400, 3600, 5400, 7200, 10800} {
				if err := e.st.ClaimOnlineGift(role.RID, 20261105, threshold, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			e.addItems(t, role, map[int64]int64{10403: initial, 99955: 4})
			cached := dailyBackpack(c)
			beforeAssets, err := e.st.RoleAssets(role.RID)
			if err != nil {
				t.Fatal(err)
			}
			frames := captureReplies(c, 10927, proto.Message{"time": 12})
			want := []uint16{10512, 12742, 10926, 10927}
			if initial == 0 {
				// Inserting the lower base id also moves the unrelated stack.
				want = []uint16{10510, 10512, 12742, 10926, 10927}
			}
			if !reflect.DeepEqual(replyCodes(frames), want) {
				t.Fatalf("online claim notifications: %v", replyCodes(frames))
			}
			end := len(frames)
			assertDailyCaptureReply(t, 756, frames[end-3])
			assertDailyCaptureReply(t, 757, frames[end-2])
			assertDailyCaptureReply(t, 758, frames[end-1])
			applyDailyBagUpdates(c, cached, frames, 10403, initial+5)
			state := captureReplies(c, 10926, proto.Message{})
			if len(state) != 1 {
				t.Fatalf("online progress pushes: %v", replyCodes(state))
			}
			assertDailyCaptureReply(t, 757, state[0])
			refused := captureReplies(c, 10927, proto.Message{"time": 12})
			if !reflect.DeepEqual(replyCodes(refused), []uint16{10927}) || refused[0].msg.Int("code") != 0 || refused[0].msg.Int("time") != 14400 {
				t.Fatalf("duplicate online claim pushed rewards: %v", refused)
			}
			items, err := e.st.RoleItems(role.RID)
			if err != nil || !reflect.DeepEqual(items, map[int64]int64{10403: initial + 5, 99955: 4}) {
				t.Fatalf("online ledger: %v %v", items, err)
			}
			afterAssets, err := e.st.RoleAssets(role.RID)
			if err != nil || !reflect.DeepEqual(beforeAssets, afterAssets) {
				t.Fatalf("item-only online claim changed currencies: %v %v %v", beforeAssets, afterAssets, err)
			}
			claimed, err := e.st.OnlineGiftClaims(role.RID, 20261105)
			if err != nil || len(claimed) != 12 || claimed[11] != 14400 {
				t.Fatalf("claim must persist the threshold, not index12: %v %v", claimed, err)
			}
		})
	}
}

func TestOnlineGiftRejectedClaimsDoNotPushOrCredit(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 11, 5, 7, 46, 0, 0, dayZone))
	c, role := e.enter("daily-online-refused", "拒领")
	for _, index := range []int64{0, 13, 14400, 12} {
		frames := captureReplies(c, 10927, proto.Message{"time": index})
		if !reflect.DeepEqual(replyCodes(frames), []uint16{10927}) || frames[0].msg.Int("code") != 0 {
			t.Fatalf("invalid/early index%d pushed rewards: %v", index, frames)
		}
	}
	items, err := e.st.RoleItems(role.RID)
	if err != nil || len(items) != 0 {
		t.Fatalf("rejected claim credited items: %v %v", items, err)
	}
	claimed, err := e.st.OnlineGiftClaims(role.RID, 20261105)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("rejected claim persisted: %v %v", claimed, err)
	}
}
