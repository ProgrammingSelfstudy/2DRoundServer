package game

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func expectFestivalIcons(test *testing.T, player *client, open ...int64) {
	test.Helper()
	ids := []int64{13, 15, 505, 508, 801, 806, 908, 909, 1101, 1102, 609, 610, 611, 612, 613, 614, 615, 616}
	for _, cmd := range []uint16{10922, 10924} {
		player.send(cmd, proto.Message{})
		statuses := make(map[int64]int64)
		for _, row := range player.waitFor(cmd).Arr("act_list") {
			id := row.Int("id")
			if _, seen := statuses[id]; seen {
				test.Fatalf("%d: duplicate icon %d", cmd, id)
			}
			statuses[id] = row.Int("status")
		}
		for _, id := range ids {
			if got, ok := statuses[id]; !ok || got != int64(b2i(slices.Contains(open, id))) {
				test.Fatalf("%d: icon %d status %d (present %t), want open icons %v", cmd, id, got, ok, open)
			}
		}
	}
	for _, id := range ids {
		player.send(10925, proto.Message{"id": id})
		if got := player.recv(10925); got.Int("id") != id || got.Int("status") != int64(b2i(slices.Contains(open, id))) {
			test.Fatalf("10925: got %v, want open icons %v", got, open)
		}
	}
}

func expectActivityIconPush(test *testing.T, player *client, id, status int64) {
	test.Helper()
	if got := player.recv(10923); got.Int("id") != id || got.Int("status") != status {
		test.Fatalf("10923: got %v, want icon %d status %d", got, id, status)
	}
}

func festivalSpecJSON(test *testing.T, icon int64) string {
	test.Helper()
	data, err := json.Marshal(ActivitySpec{Title: "登录活动", CliType: icon, PanelType: 16,
		Aims: []ActivityAim{{ID: 1, Str: "登录奖励", Rewards: [][2]int64{{3, 100}}}}})
	if err != nil {
		test.Fatal(err)
	}
	return string(data)
}

func TestScheduledFestivalIconsFollowServerActivities(test *testing.T) {
	env := newGameEnv(test, time.Minute)
	env.connect = serveTestPipe(test, env.gw, true)
	env.setNow(actNow)
	player, role := env.enter("festival-schedule", "节日测试")
	start, end := actNow.Add(time.Minute), actNow.Add(2*time.Minute)
	add := func(srv string, bid, icon int64, from, until time.Time, status int) *store.Activity {
		return env.addActivity(test, store.Activity{SrvID: srv, CampID: bid, BID: bid, Status: status,
			StartAt: from.Unix(), EndAt: until.Unix(), Spec: festivalSpecJSON(test, icon)})
	}
	add(role.SrvID, 90001, 13, start, end, 1)
	add("", 90002, 13, start, end.Add(time.Minute), 1)
	merge := add(role.SrvID, 1011, 15, actNow, end, 1)
	add("", 991011, 0, start, end, 1)
	add(role.SrvID, 90003, 1101, start, end, 1)
	add("symlf_2", 991025, 0, actNow, end, 1)
	add(role.SrvID, 991025, 0, actNow, end, 0)
	expectFestivalIcons(test, player, 508, 612, 15)

	watcher := newActivityWatcher(env.st, env.gw.Hub, actNow)
	watcher.tick(actNow)
	env.setNow(start)
	watcher.tick(start)
	for _, id := range []int64{13, 908, 1101} {
		expectActivityIconPush(test, player, id, 1)
	}
	player.recv(16607)
	expectFestivalIcons(test, player, 508, 612, 13, 15, 908, 1101)
	player.send(16601, proto.Message{})
	if got := player.recv(16601).Arr("holiday_list"); len(got) != 5 {
		test.Fatalf("16601: %v", got)
	}
	player.send(16603, proto.Message{"bid": 991011})
	if got := player.recv(16603); got.Int("remain_sec") != 60 || len(got.Arr("aim_list")) != 1 {
		test.Fatalf("lantern detail: %v", got)
	}
	player.send(16604, proto.Message{"bid": 991011, "aim": 1})
	if got := player.waitFor(16604); got.Int("code") != 1 {
		test.Fatalf("lantern claim: %v", got)
	}
	player.recv(16603)
	player.recv(16606)

	merge.Spec = festivalSpecJSON(test, 0)
	if err := env.st.UpdateActivity(*merge); err != nil {
		test.Fatal(err)
	}
	add(role.SrvID, 90004, 1102, start, end, 1)
	watcher.tick(start)
	expectActivityIconPush(test, player, 15, 0)
	expectActivityIconPush(test, player, 909, 1)
	expectActivityIconPush(test, player, 1102, 1)
	player.recv(16607)
	expectFestivalIcons(test, player, 508, 612, 13, 908, 909, 1101, 1102)

	env.setNow(end)
	watcher.tick(end)
	for _, id := range []int64{908, 909, 1101, 1102} {
		expectActivityIconPush(test, player, id, 0)
	}
	player.recv(16607)
	expectFestivalIcons(test, player, 508, 612, 13)
	player.send(16604, proto.Message{"bid": 991011, "aim": 1})
	if got := player.recv(16604); got.Int("code") != 0 || got.Str("msg") != "活动已经结束" {
		test.Fatalf("closed lantern claim: %v", got)
	}
	env.setNow(end.Add(time.Minute))
	watcher.tick(end.Add(time.Minute))
	expectActivityIconPush(test, player, 13, 0)
	player.recv(16607)
	player.expectNothing()
}

func TestReturnFestivalIconsFollowRoleEligibility(test *testing.T) {
	for _, limit := range []string{"role", "schedule"} {
		test.Run(limit, func(test *testing.T) {
			env := newGameEnv(test, time.Minute)
			env.connect = serveTestPipe(test, env.gw, true)
			start := time.Date(2026, time.August, 5, 0, 0, 0, 0, dayZone)
			scheduleEnd := start.AddDate(0, 0, 20)
			expires := start.AddDate(0, 0, 14)
			if limit == "schedule" {
				scheduleEnd = start.AddDate(0, 0, 3)
				expires = scheduleEnd
			}
			env.setNow(start)
			env.addActivity(test, store.Activity{CampID: retCamp, BID: 101, Status: 1, StartAt: start.Unix(), EndAt: scheduleEnd.Unix(), Spec: festivalSpecJSON(test, 0)})
			player, role := env.enter("return-icons", "归来人")
			other, _ := env.enter("not-returning", "留守人")
			watcher := newActivityWatcher(env.st, env.gw.Hub, start)
			watcher.tick(start)
			expectFestivalIcons(test, player, 508, 610)

			away := *role
			away.Lev, away.LastLogoutAt = 40, start.AddDate(0, 0, -16).Unix()
			ReturnEntered(env.st, &away, start)
			watcher.tick(start)
			expectActivityIconPush(test, player, 801, 1)
			expectActivityIconPush(test, player, 806, 1)
			other.expectNothing()
			expectFestivalIcons(test, player, 508, 610, 801, 806)
			expectFestivalIcons(test, other, 508, 610)
			player.send(27902, proto.Message{})
			if got := player.recv(27902); got.Int("is_open") != 1 || got.Int("period") != 1 || got.Int("open_day") != 1 {
				test.Fatalf("return qualification: %v", got)
			}

			env.setNow(expires.Add(-time.Second))
			expectFestivalIcons(test, player, 508, 610, 801, 806)
			env.setNow(expires)
			watcher.tick(expires)
			expectActivityIconPush(test, player, 801, 0)
			expectActivityIconPush(test, player, 806, 0)
			if limit == "schedule" {
				player.recv(16607)
				other.recv(16607)
			}
			other.expectNothing()
			expectFestivalIcons(test, player, 508, 610)
			player.send(27902, proto.Message{})
			if got := player.recv(27902); got.Int("is_open") != 0 {
				test.Fatalf("return expiration: %v", got)
			}
		})
	}
}

func TestSeasonFestivalIconsFollowWarOrder(test *testing.T) {
	env := newGameEnv(test, time.Minute)
	env.connect = serveTestPipe(test, env.gw, true)
	player, _ := env.enter("season-icons", "季节战令")
	for _, month := range []struct {
		start  time.Time
		period int64
		icon   int64
	}{
		{time.Date(2026, time.June, 1, 0, 0, 0, 0, dayZone), 0, 0},
		{time.Date(2026, time.July, 1, 0, 0, 0, 0, dayZone), 3, 609},
		{time.Date(2026, time.August, 1, 0, 0, 0, 0, dayZone), 4, 610},
		{time.Date(2026, time.September, 1, 0, 0, 0, 0, dayZone), 5, 611},
		{time.Date(2026, time.October, 1, 0, 0, 0, 0, dayZone), 6, 612},
		{time.Date(2026, time.November, 1, 0, 0, 0, 0, dayZone), 7, 613},
		{time.Date(2026, time.December, 1, 0, 0, 0, 0, dayZone), 8, 614},
		{time.Date(2027, time.January, 1, 0, 0, 0, 0, dayZone), 9, 615},
		{time.Date(2027, time.February, 1, 0, 0, 0, 0, dayZone), 10, 616},
		{time.Date(2027, time.March, 1, 0, 0, 0, 0, dayZone), 0, 0},
	} {
		env.setNow(month.start.UTC())
		expectFestivalIcons(test, player, 508, month.icon)
		player.send(25300, proto.Message{})
		if got := player.recv(25300); got.Int("period") != month.period || (len(got.Arr("list")) != 0) != (month.icon != 0) ||
			got.Int("cur_day") != 1 || got.Int("end_time") != month.start.AddDate(0, 1, 0).Unix() {
			test.Fatalf("season starting %s: %v", month.start, got)
		}
	}
	boundary := time.Date(2026, time.August, 1, 0, 0, 0, 0, dayZone)
	watcher := newActivityWatcher(env.st, env.gw.Hub, boundary.Add(-time.Second))
	env.setNow(boundary)
	watcher.tick(boundary)
	expectActivityIconPush(test, player, 617, 1)
	expectActivityIconPush(test, player, 609, 0)
	expectActivityIconPush(test, player, 610, 1)
	player.expectNothing()
}
