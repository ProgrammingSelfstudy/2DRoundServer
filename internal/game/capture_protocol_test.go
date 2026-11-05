package game

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type capturedReply struct {
	cmd uint16
	msg proto.Message
}

// Read through a queued heartbeat, including pushes after the confirmation.
// This catches full-bag refreshes and duplicate rewards instead of discarding
// unexpected frames while looking for just the operation's reply.
func captureReplies(c *client, cmd uint16, req proto.Message) []capturedReply {
	c.t.Helper()
	c.settle()
	c.send(cmd, req)
	c.send(1169, proto.Message{})
	var out []capturedReply
	for i := 0; i < 100; i++ {
		code, msg := c.recvNext()
		if code == 1169 {
			return out
		}
		if enterPush[code] || code == 10403 || code == 16401 || code == 10344 {
			continue
		}
		out = append(out, capturedReply{code, msg})
	}
	c.t.Fatal("operation did not reach the heartbeat barrier")
	return nil
}

func replyCodes(frames []capturedReply) []uint16 {
	out := make([]uint16, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.cmd)
	}
	return out
}

func rewardTotals(t *testing.T, m proto.Message) map[int64]int64 {
	t.Helper()
	out := map[int64]int64{}
	if m.Int("source") != 0 {
		t.Fatalf("reward source: %v", m)
	}
	for _, item := range m.Arr("asset_list") {
		n, err := strconv.ParseInt(item.Str("num"), 10, 64)
		if err != nil || n <= 0 || item.Int("is_bind") != 1 || item.Int("id") != 0 || item.Int("move_to") != 0 {
			t.Fatalf("invalid reward entry: %v", item)
		}
		out[item.Int("bid")] += n
	}
	return out
}

// round1 sources787-790: deletion -> absolute balance -> reward -> reply.
func TestCapturedFruitDecomposeNotifications(t *testing.T) {
	for _, n := range []int64{1, 3} {
		t.Run(strconv.FormatInt(n, 10), func(t *testing.T) {
			e, c, role := emoEnv(t)
			e.addItems(t, role, map[int64]int64{19103: 3, 19502: 1700})
			frames := captureReplies(c, 31130, proto.Message{"id": 19103, "num": n})
			want := []uint16{10512, 12742, 31130}
			if n == 3 {
				want = []uint16{10511, 10512, 12742, 31130}
				gone := frames[0].msg.Arr("item_list")
				if len(gone) != 1 || gone[0].Int("id") != 19103 || gone[0].Int("storage") != 1 {
					t.Fatalf("consumed stack deletion: %v", gone)
				}
			}
			if !reflect.DeepEqual(replyCodes(frames), want) {
				t.Fatalf("decompose order: %v, want %v", replyCodes(frames), want)
			}
			balances := map[int64]int64{}
			for _, f := range frames {
				if f.cmd == 10512 {
					for _, item := range f.msg.Arr("item_list") {
						if item.Int("storage") != 1 {
							t.Fatalf("wrong storage: %v", item)
						}
						balances[item.Int("base_id")] = item.Int("quantity")
					}
				}
			}
			if balances[19502] != 1700+25*n || n == 1 && balances[19103] != 2 {
				t.Fatalf("absolute quantities: %v", balances)
			}
			gain := rewardTotals(t, frames[len(frames)-2].msg)
			if !reflect.DeepEqual(gain, map[int64]int64{19502: 25 * n}) {
				t.Fatalf("reward: %v", gain)
			}
			if frames[len(frames)-1].msg.Str("msg") != "分解成功" {
				t.Fatal("wrong confirmation text")
			}
			items, _ := e.st.RoleItems(role.RID)
			if items[19502] != 1700+25*n || items[19103] != 3-n {
				t.Fatalf("notice must not credit twice: %v", items)
			}
			failed := captureReplies(c, 31130, proto.Message{"id": 19103, "num": 3})
			if !reflect.DeepEqual(replyCodes(failed), []uint16{31130}) || failed[0].msg.Int("result") != 0 {
				t.Fatalf("failed decomposition pushed changes or rewards: %v", failed)
			}
		})
	}
}

// round1 sources767-772: assets -> bag increments -> success -> per-pull
// rewards -> used diamond count. Compare all credited values with the notices.
func TestCapturedFruitDrawNotifications(t *testing.T) {
	e, c, role := emoEnv(t)
	e.grantAssets(c, role, map[int64]int64{3: 93703, emoSeed: 110})
	e.addItems(t, role, map[int64]int64{19502: 1000})
	beforeA, _ := e.st.RoleAssets(role.RID)
	beforeI, _ := e.st.RoleItems(role.RID)
	frames := captureReplies(c, 31115, proto.Message{"id": 10001, "type": 3, "up_id": 100011})
	if len(frames) < 5 || frames[0].cmd != 10302 {
		t.Fatalf("draw start: %v", replyCodes(frames))
	}
	end := len(frames)
	if !reflect.DeepEqual(replyCodes(frames[end-3:]), []uint16{31115, 12742, 31116}) || frames[end-3].msg.Str("msg") != "召唤成功" || frames[end-1].msg.Int("times") != 10 {
		t.Fatalf("draw completion: %v", frames)
	}
	updates := map[int64]int64{}
	for _, f := range frames[1 : end-3] {
		if f.cmd != 10510 && f.cmd != 10512 {
			t.Fatalf("unexpected draw push %d", f.cmd)
		}
		for _, item := range f.msg.Arr("item_list") {
			if item.Int("storage") != 1 {
				t.Fatalf("fruit item in wrong bag: %v", item)
			}
			updates[item.Int("base_id")] = item.Int("quantity")
		}
	}
	if len(frames[end-2].msg.Arr("asset_list")) != 11 {
		t.Fatal("ten pull results and the extra seed reward must retain separate entries")
	}
	gain := rewardTotals(t, frames[end-2].msg)
	afterA, _ := e.st.RoleAssets(role.RID)
	afterI, _ := e.st.RoleItems(role.RID)
	if afterA[3] != beforeA[3]-3000 || gain[emoSeed] != 100 || afterA[emoSeed] != beforeA[emoSeed]+100 {
		t.Fatalf("draw cost/seed reward mismatch: %v %v %v", beforeA, afterA, gain)
	}
	for id, amount := range gain {
		if itemTable[id].Type == 7 {
			continue
		}
		if afterI[id]-beforeI[id] != amount || updates[id] != afterI[id] {
			t.Fatalf("reward and absolute inventory diverged for %d: notice=%d before=%d after=%d push=%d", id, amount, beforeI[id], afterI[id], updates[id])
		}
	}
	failed := captureReplies(c, 31115, proto.Message{"id": 10001, "type": 1})
	if !reflect.DeepEqual(replyCodes(failed), []uint16{31115}) || failed[0].msg.Int("result") != 0 {
		t.Fatalf("over-limit pull pushed changes or rewards: %v", failed)
	}
}

func TestCapturedForgeNotifications(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("forge-wire", "锻造")
	grantItems(t, e, c, role, map[int64]int64{1: 100000}, map[int64]int64{40101: 6, 40102: 12})
	frames := captureReplies(c, 11080, proto.Message{"base_id": 40102, "num": 1})
	if !reflect.DeepEqual(replyCodes(frames), []uint16{10302, 10512, 12742, 11080}) {
		t.Fatalf("forge order: %v", replyCodes(frames))
	}
	for _, item := range frames[1].msg.Arr("item_list") {
		want := map[int64]int64{40101: 3, 40102: 13}
		if item.Int("storage") != 3 || item.Int("quantity") != want[item.Int("base_id")] {
			t.Fatalf("forge absolute quantity/storage: %v", item)
		}
	}
	if len(frames[1].msg.Arr("item_list")) != 2 || !reflect.DeepEqual(rewardTotals(t, frames[2].msg), map[int64]int64{40102: 1}) || frames[3].msg.Int("result") != 1 || frames[3].msg.Str("msg") != "" {
		t.Fatalf("forge rewards/confirmation: %v", frames)
	}
	items, _ := e.st.RoleItems(role.RID)
	assets, _ := e.st.RoleAssets(role.RID)
	if items[40101] != 3 || items[40102] != 13 || assets[1] != 98944 {
		t.Fatalf("forge credited more than once: %v %v", items, assets)
	}
	failed := captureReplies(c, 11080, proto.Message{"base_id": 40102, "num": 2})
	if !reflect.DeepEqual(replyCodes(failed), []uint16{11080}) || failed[0].msg.Int("result") != 0 {
		t.Fatalf("failed forge pushed changes: %v", failed)
	}
}

// Creating a new stack while exhausting another must leave a cached client
// bag identical to a fresh10501, including an unrelated equipment stack.
func TestForgeDeltaReconstructsEquipmentBag(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("forge-cache", "背包")
	grantItems(t, e, c, role, map[int64]int64{1: 1056}, map[int64]int64{40101: 3, 40201: 2})
	c.send(10501, proto.Message{})
	cached := map[int64]proto.Message{}
	for _, item := range c.waitFor(10501).Arr("item_list") {
		cached[item.Int("id")] = item
	}
	frames := captureReplies(c, 11080, proto.Message{"base_id": 40102, "num": 1})
	var additions, deletions, rewards int
	for _, f := range frames {
		switch f.cmd {
		case 10511:
			for _, item := range f.msg.Arr("item_list") {
				delete(cached, item.Int("id"))
				deletions++
			}
		case 10510, 10512:
			for _, item := range f.msg.Arr("item_list") {
				copy := proto.Message{}
				for key, value := range item {
					if key != "storage" {
						copy[key] = value
					}
				}
				cached[item.Int("id")] = copy
				if f.cmd == 10510 {
					additions++
				}
			}
		case 12742:
			rewards++
		case 10500, 10501:
			t.Fatal("forge refreshed a full bag")
		}
	}
	c.send(10501, proto.Message{})
	fresh := map[int64]proto.Message{}
	for _, item := range c.waitFor(10501).Arr("item_list") {
		fresh[item.Int("id")] = item
	}
	if additions != 1 || deletions != 1 || rewards != 1 || !reflect.DeepEqual(cached, fresh) {
		t.Fatalf("incremental cache differs from full bag: added=%d removed=%d rewards=%d cache=%v full=%v", additions, deletions, rewards, cached, fresh)
	}
}

func TestCapturedHolyWearNotifications(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("holy-wire", "神装")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 6}}); err != nil {
		t.Fatal(err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	hero := heroes[0].PartnerID
	e.give(c, role, map[int64]int64{1030101: 1})
	bag := holyBagOf(t, c)
	item := bag[0].Int("id")
	worn := captureReplies(c, 11093, proto.Message{"partner_id": hero, "holy_eqm_id": item, "type": 1})
	if !reflect.DeepEqual(replyCodes(worn), []uint16{10511, 11091, 11093, 10306}) || worn[2].msg.Str("msg") != "" {
		t.Fatalf("wear order/text: %v", worn)
	}
	gone := worn[0].msg.Arr("item_list")
	if len(gone) != 1 || gone[0].Int("id") != item || gone[0].Int("storage") != 3 || len(worn[1].msg.Arr("holy_eqm")) != 1 {
		t.Fatalf("wear inventory/hero: %v", worn)
	}
	peak := worn[3].msg.Int("max_power")
	unworn := captureReplies(c, 11093, proto.Message{"partner_id": hero, "holy_eqm_id": item, "type": 0})
	if !reflect.DeepEqual(replyCodes(unworn), []uint16{10510, 11091, 11093, 10306}) || unworn[2].msg.Str("msg") != "" {
		t.Fatalf("unwear order/text: %v", unworn)
	}
	back := unworn[0].msg.Arr("item_list")
	if len(back) != 1 || back[0].Int("id") != item || back[0].Int("storage") != 3 || len(unworn[1].msg.Arr("holy_eqm")) != 0 || unworn[3].msg.Int("power") >= worn[3].msg.Int("power") || unworn[3].msg.Int("max_power") != peak {
		t.Fatalf("unwear identity/power high-water mark: %v", unworn)
	}
	state := rolePowerState{}
	if _, err := e.st.LoadState(role.RID, "role_power", &state); err != nil || state.Max != peak {
		t.Fatalf("power maximum not persisted: %+v %v", state, err)
	}
}

func TestCapturedFailureNotices(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("failure-wire", "提示")
	if _, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10502, Star: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RaiseMaxStars(role.RID, 30); err != nil {
		t.Fatal(err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	frames := captureReplies(c, 26421, proto.Message{"partner_id": heroes[0].PartnerID, "skills": []proto.Message{}})
	if !reflect.DeepEqual(replyCodes(frames), []uint16{26421, 12745}) || frames[0].msg.Int("result") != 0 || frames[0].msg.Str("msg") != "" || frames[1].msg.Int("bid") != 10096 || frames[1].msg.Int("need_num") != 1 || frames[1].msg.Int("num") != 0 {
		t.Fatalf("empower missing material: %v", frames)
	}
	if err := e.st.SetRoleLevel(role.RID, 20); err != nil {
		t.Fatal(err)
	}
	frames = captureReplies(c, 20207, proto.Message{"num": 1})
	if !reflect.DeepEqual(replyCodes(frames), []uint16{20207, 12741}) || frames[0].msg.Int("code") != 0 || frames[0].msg.Str("msg") != "" || frames[1].msg.Str("msg") != "钻石不足" {
		t.Fatalf("arena missing diamonds: %v", frames)
	}
	items, _ := e.st.RoleItems(role.RID)
	after, _ := e.st.PartnersOf(role.RID)
	if items[arenaTicket] != 0 || len(after) != len(heroes) {
		t.Fatal("failed requests changed inventory or heroes")
	}
}

// round2 sources801-821: combat37, four rounds, then1037/result1 even though
// the attacker's recorded result is a loss. No rewards or battle settlement.
func TestCapturedReplayPlayback(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("replay-wire", "录像")
	beforeA, _ := e.st.RoleAssets(role.RID)
	beforeI, _ := e.st.RoleItems(role.RID)
	intro := proto.Message{"combat_type": 37, "is_auto": 1, "play_speed": 2}
	rounds := make([]fightRound, 4)
	for i := range rounds {
		rounds[i] = fightRound{alive: []int64{1, 6}, plays: []proto.Message{}}
	}
	id := recordReplay(e.st, 37, replaySide{RID: role.RID, SrvID: role.SrvID}, replaySide{}, intro, rounds, false, e.gw.Now().Unix())
	if id == 0 {
		t.Fatal("could not create the local replay fixture")
	}
	bad := captureReplies(c, 20036, proto.Message{"replay_id": id, "replay_srv_id": "other-server"})
	if !reflect.DeepEqual(replyCodes(bad), []uint16{20036}) || bad[0].msg.Int("code") != 0 {
		t.Fatalf("replay namespace ignored: %v", bad)
	}
	start := captureReplies(c, 20036, proto.Message{"replay_id": id, "replay_srv_id": role.SrvID})
	if !reflect.DeepEqual(replyCodes(start), []uint16{20000, 20036, 20027}) || start[0].msg.Int("combat_map") != 0 || start[0].msg.Int("combat_type") != 37 || start[1].msg.Int("code") != 1 || start[1].msg.Str("msg") != "" {
		t.Fatalf("replay startup: %v", start)
	}
	ready := captureReplies(c, 20001, proto.Message{})
	if !reflect.DeepEqual(replyCodes(ready), []uint16{20001, 20002}) || ready[1].msg.Int("action_count") != 1 {
		t.Fatalf("replay ready: %v", ready)
	}
	for round := 1; round <= 4; round++ {
		plays := captureReplies(c, 20019, proto.Message{})
		if !reflect.DeepEqual(replyCodes(plays), []uint16{20004}) || plays[0].msg.Int("action_count") != int64(round) {
			t.Fatalf("replay round%d: %v", round, plays)
		}
		next := captureReplies(c, 20005, proto.Message{})
		if round < 4 {
			if !reflect.DeepEqual(replyCodes(next), []uint16{20002}) || next[0].msg.Int("action_count") != int64(round+1) {
				t.Fatalf("next replay round: %v", next)
			}
		} else if !reflect.DeepEqual(replyCodes(next), []uint16{20006}) || next[0].msg.Int("combat_type") != 1037 || next[0].msg.Int("result") != 1 || len(next[0].msg.Arr("item_rewards")) != 0 || len(next[0].msg.Arr("hurt_statistics")) != 0 {
			t.Fatalf("replay completion: %v", next)
		}
	}
	if extra := captureReplies(c, 20005, proto.Message{}); len(extra) != 0 {
		t.Fatalf("stale acknowledgement settled twice: %v", extra)
	}
	afterA, _ := e.st.RoleAssets(role.RID)
	afterI, _ := e.st.RoleItems(role.RID)
	if !reflect.DeepEqual(beforeA, afterA) || !reflect.DeepEqual(beforeI, afterI) {
		t.Fatal("replay playback changed assets or items")
	}
	replay, _ := e.st.ReplayByID(id, role.RID)
	if replay.Plays != 1 {
		t.Fatalf("rejected server request counted as playback: %+v", replay)
	}
}

// original-2026-11-03 starup-extra sources 419-430: a star-up that uses up a levelled hero
// pays its levels back (10302), puts its worn equipment into the bag, lists all of it in one
// 12742, then updates the hero (11002) before 11006 and the answer with an empty msg.
func TestCapturedStarUpRefund(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	seeds := []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10501, Star: 5}, {BID: 10405, Star: 5},
		{BID: 10401, Star: 5}, {BID: 10402, Star: 5}, {BID: 10403, Star: 5}}
	if ok, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil || !ok {
		t.Fatal(ok, err)
	}
	hs, _ := e.st.PartnersOf(role.RID)
	if err := e.st.UpgradePartner(role.RID, hs[1].PartnerID, 1, 0, 50, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SellItems(role.RID, nil, nil, map[int64]int64{40101: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.EquipPiece(role.RID, hs[1].PartnerID, 1, 40101); err != nil {
		t.Fatal(err)
	}
	levelled, err := e.st.PartnerByID(role.RID, hs[1].PartnerID)
	if err != nil {
		t.Fatal(err)
	}
	wantAssets, wantItems := refund(*levelled)
	if len(wantAssets) == 0 || wantItems[40101] != 1 {
		t.Fatalf("test needs a refund with assets and the sword: %v %v", wantAssets, wantItems)
	}
	beforeA, _ := e.st.RoleAssets(role.RID)
	var expend1, expend2 []proto.Message
	for _, h := range hs[1:3] {
		expend1 = append(expend1, proto.Message{"partner_id": h.PartnerID})
	}
	for _, h := range hs[3:6] {
		expend2 = append(expend2, proto.Message{"partner_id": h.PartnerID})
	}
	frames := captureReplies(c, 11005, proto.Message{"partner_id": hs[0].PartnerID, "expend1": expend1, "expend2": expend2, "item_expend": []proto.Message{}})
	codes := replyCodes(frames)
	// the new star opens a limited gift (21211) after the answer
	want := []uint16{10302, 10510, 12742, 11002, 11006, 11005}
	if len(codes) < len(want) || !reflect.DeepEqual(codes[:len(want)], want) {
		t.Fatalf("order: %v, want %v first", codes, want)
	}
	if frames[5].msg.Int("result") != 1 || frames[5].msg.Str("msg") != "" {
		t.Fatalf("answer: %v", frames[5].msg)
	}
	shown := rewardTotals(t, frames[2].msg)
	for k, v := range wantAssets {
		if shown[k] != v {
			t.Fatalf("12742 asset %d: %d, want %d (%v)", k, shown[k], v, shown)
		}
	}
	if shown[40101] != 1 {
		t.Fatalf("the sword must be listed: %v", shown)
	}
	afterA, _ := e.st.RoleAssets(role.RID)
	for k, v := range wantAssets {
		if afterA[k]-beforeA[k] != v {
			t.Fatalf("asset %d credited %d, want %d", k, afterA[k]-beforeA[k], v)
		}
	}
	items, _ := e.st.RoleItems(role.RID)
	if items[40101] != 1 {
		t.Fatalf("the sword is credited once: %v", items)
	}
}

// original-2026-11-05-materials sources 597-603: a sweep sends 10302, the changed bag entries,
// one 12742 of the gain, the daily quest and feat (10409, 16401), the state (13030) and then
// the answer with an empty msg.
func TestCapturedStoneSweepOrder(t *testing.T) {
	e, c, role := stoneEnv(t)
	if _, err := e.st.MarkStoneCleared(role.RID, 101); err != nil {
		t.Fatal(err)
	}
	frames := captureReplies(c, 13032, proto.Message{"id": 101})
	codes := replyCodes(frames)
	n := len(codes)
	if n < 4 || codes[0] != 10302 || codes[n-2] != 13030 || codes[n-1] != 13032 {
		t.Fatalf("sweep order: %v", codes)
	}
	notice := -1
	for i, code := range codes {
		if code == 12742 {
			notice = i
		}
		if code == 10500 {
			t.Fatalf("a sweep must not send the full bag: %v", codes)
		}
	}
	if notice < 1 || notice > n-3 {
		t.Fatalf("12742 must follow the credit and precede the state: %v", codes)
	}
	if r := frames[n-1].msg; r.Int("code") != 1 || r.Str("msg") != "" || r.Int("id") != 101 {
		t.Fatalf("answer: %v", r)
	}
	if got := rewardTotals(t, frames[notice].msg); got[1] != 100000 {
		t.Fatalf("notice: %v", got)
	}
}
