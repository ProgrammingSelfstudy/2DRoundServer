package game

import (
	"errors"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func setDaysRankStart(t *testing.T, env *env, start time.Time) time.Time {
	t.Helper()
	server, err := env.st.Server(env.login.SrvID)
	if err != nil {
		t.Fatal(err)
	}
	server.OpenTime = start.Unix()
	if err := env.st.UpsertServer(*server); err != nil {
		t.Fatal(err)
	}
	return time.Unix(drEnd(dayStart(start)), 0)
}

func TestDaysRankIconFollowsWindow(t *testing.T) {
	env := newGameEnv(t, time.Minute)
	env.connect = serveTestPipe(t, env.gw, true)
	start := dayStart(actNow).AddDate(0, 0, -13)
	end := setDaysRankStart(t, env, start)
	before := start.Add(-time.Second)
	env.setNow(before)
	player, _ := env.enter("daysrank-icon", "冲榜图标")
	check := func(open int64) {
		t.Helper()
		for _, cmd := range []uint16{10922, 10924} {
			player.send(cmd, nil)
			matches := 0
			for _, row := range player.waitFor(cmd).Arr("act_list") {
				if row.Int("id") == drIcon {
					matches++
					if row.Int("status") != open {
						t.Fatalf("%d: want icon status %d, got %v", cmd, open, row)
					}
				}
			}
			if matches != 1 {
				t.Fatalf("%d: icon 108 appeared %d times", cmd, matches)
			}
		}
		player.send(10925, proto.Message{"id": drIcon})
		if got := player.recv(10925); got.Int("id") != drIcon || got.Int("status") != open {
			t.Fatalf("10925: want icon status %d, got %v", open, got)
		}
		player.send(22700, proto.Message{"is_cluster": 0})
		if got := player.recv(22700).Arr("rank_list"); len(got) != int(open)*7 || open == 1 && got[0].Int("end_time") != end.Unix() {
			t.Fatalf("22700: want open=%d and end=%s, got %v", open, end, got)
		}
		player.send(22703, nil)
		if got := player.recv(22703).Arr("feat_list"); len(got) != int(open)*14 {
			t.Fatalf("22703: want open=%d, got %v", open, got)
		}
	}
	watcher := newActivityWatcher(env.st, env.gw.Hub, before)
	checkClaimRefused := func(message string) {
		t.Helper()
		player.send(22704, proto.Message{"quest_id": 1001})
		if got := player.recv(22704); got.Int("result") != 0 || got.Str("msg") != message {
			t.Fatalf("22704 outside the window: %v", got)
		}
	}
	check(0)
	checkClaimRefused("活动未开放")
	if next := watcher.tick(before); !next.Equal(start) {
		t.Fatalf("next boundary %s, want %s", next, start)
	}
	env.setNow(start)
	if next := watcher.tick(start); !next.Equal(end) {
		t.Fatalf("next boundary %s, want %s", next, end)
	}
	expectActivityIconPush(t, player, drIcon, 1)
	check(1)
	day9 := start.AddDate(0, 0, 8)
	env.setNow(day9)
	watcher.tick(day9)
	player.expectNothing()
	check(1)
	env.setNow(end.Add(-time.Second))
	check(1)
	env.setNow(end)
	watcher.tick(end)
	expectActivityIconPush(t, player, drIcon, 0)
	check(0)
	checkClaimRefused("活动已结束")
	watcher.tick(end.Add(time.Second))
	player.expectNothing()
}

func TestDaysRankTaskDeltasUsePersistedProgress(t *testing.T) {
	env := newGameEnv(t, time.Minute)
	env.connect = serveTestPipe(t, env.gw, true)
	start := dayStart(actNow)
	end := setDaysRankStart(t, env, start)
	env.setNow(actNow)
	player, role := env.enter("daysrank-tasks", "任务推送")
	other, _ := env.enter("daysrank-other", "其他角色")
	watcher := newActivityWatcher(env.st, env.gw.Hub, actNow)
	watcher.tick(actNow)
	player.expectNothing()

	atDungeon(t, env, role.RID, 10300, false)
	player.send(22704, proto.Message{"quest_id": 1001})
	if got := player.recv(22704); got.Int("result") != 0 {
		t.Fatalf("unlocked but uncleared campaign task was paid: %v", got)
	}
	atDungeon(t, env, role.RID, 10300, true)
	if err := env.st.Exchange(role.RID, nil, nil, map[int64]int64{3: 200}, map[int64]int64{72001: 50}); err != nil {
		t.Fatal(err)
	}
	if err := env.st.Exchange(role.RID, map[int64]int64{3: 120}, map[int64]int64{72001: 35}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := env.st.Exchange(role.RID, map[int64]int64{3: 1}, map[int64]int64{72001: 16}, nil, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("unaffordable debit: %v", err)
	}
	// exploring is revealing rooms: a soul potion fells guard 7 of a floor without empty rooms, which reveals rooms 1, 2
	// and 6; the second potion finds no guard to fell and reveals nothing
	sky := &advState{}
	sky.newRound(actNow)
	fixedFloor(sky, nil)
	sky.Skills = map[int64]advSkillHold{2: {Num: 2}}
	if err := env.st.SaveState(role.RID, advModule, sky); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		player.send(20607, proto.Message{"skill_id": 2, "val": 7})
		if got := player.waitFor(20607); got.Int("code") != int64(1-attempt) {
			t.Fatalf("explore attempt %d: %v", attempt, got)
		}
		player.settle()
	}
	watcher.tick(actNow)
	rows := player.recv(22702).Arr("feat_list")
	values := map[int64]int64{1001: 1, 2001: 30, 2002: 35, 5001: 3, 5002: 3, 7001: 100, 7002: 120}
	if len(rows) != len(values) {
		t.Fatalf("expected just changed tasks: %v", rows)
	}
	for _, row := range rows {
		want, found := values[row.Int("id")]
		progress := row.Arr("progress")
		if !found || row.Int("end_time") != end.Unix() || len(progress) != 1 || progress[0].Int("value") != want {
			t.Fatalf("unexpected task delta: %v", row)
		}
		delete(values, row.Int("id"))
	}
	other.expectNothing()
	watcher.tick(actNow)
	player.expectNothing()

	player.send(22703, nil)
	for _, row := range player.recv(22703).Arr("feat_list") {
		switch row.Int("id") {
		case 4001, 4002, 6001, 6002:
			if row.Int("finish") != questOpen || row.Arr("progress")[0].Int("value") != 0 {
				t.Fatalf("unsupported task should remain at zero: %v", row)
			}
		}
	}
	arena, err := env.st.ArenaPlayerOf(role.RID, 1000, actNow.Unix())
	if err != nil {
		t.Fatal(err)
	}
	arena.Battles = 50
	if err := env.st.SaveArenaPlayer(arena); err != nil {
		t.Fatal(err)
	}
	player.send(22704, proto.Message{"quest_id": 9001})
	if got := player.recv(22704); got.Int("result") != 0 || got.Str("msg") != "任务不存在" {
		t.Fatalf("accepted a hidden arena task: %v", got)
	}
	player.send(22704, proto.Message{"quest_id": 2001})
	if got := player.recv(22704); got.Int("result") != 1 {
		t.Fatalf("claim: %v", got)
	}
	player.settle()
	watcher.tick(actNow)
	if delta := player.recv(22702).Arr("feat_list"); len(delta) != 1 || delta[0].Int("id") != 2001 || delta[0].Int("finish") != questClaimed {
		t.Fatalf("claim delta: %v", delta)
	}
	player.send(22704, proto.Message{"quest_id": 2001})
	if got := player.recv(22704); got.Int("result") != 0 {
		t.Fatalf("repeat claim: %v", got)
	}
	if items, err := env.st.RoleItems(role.RID); err != nil || items[72001] != 20 {
		t.Fatalf("essence ledger: %v (%v)", items, err)
	}
}

func TestDaysRankHeroPoolsUseCurrentStarsAndKnownTemplates(t *testing.T) {
	env := newDungeonEnv(t)
	end := setDaysRankStart(t, env, dayStart(actNow))
	env.setNow(actNow)
	native, nativeRole := env.enter("daysrank-native-hero", "原版英雄")
	hidden, hiddenRole := enterDungeonRole(env, store.PlatformH5, "daysrank-ineligible", "低星英雄")
	eligible, eligibleRole := enterDungeonRole(env, store.PlatformH5, "daysrank-eligible", "五星英雄")
	nativeHeroes := seedH5EligibilityRoster(t, env, nativeRole.RID, store.PartnerSeed{BID: 10401, Star: 4})
	hiddenHeroes := seedH5EligibilityRoster(t, env, hiddenRole.RID, store.PartnerSeed{BID: 10401, Star: 4}, store.PartnerSeed{BID: 99999999, Star: 9})
	seeds := make([]store.PartnerSeed, 6)
	for idx := range seeds {
		seeds[idx] = store.PartnerSeed{BID: 10401, Star: 5}
	}
	eligibleHeroes := seedH5EligibilityRoster(t, env, eligibleRole.RID, seeds...)
	for _, hero := range append(append(nativeHeroes, hiddenHeroes...), eligibleHeroes[0]) {
		if err := env.st.UpgradePartner(hero.RID, hero.PartnerID, 1, 0, 100, 4, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	check := func(client *client, id, rid, self int64) {
		t.Helper()
		client.send(22701, proto.Message{"id": id})
		reply := client.recv(22701)
		rows := reply.Arr("rank_list")
		if len(rows) != 1 || rows[0].Int("rid") != rid || reply.Int("rank") != self {
			t.Fatalf("hero board %d: want winner %d and own rank %d: %v", id, rid, self, reply)
		}
	}
	for _, id := range []int64{4, 7} {
		check(native, id, nativeRole.RID, 1)
		check(hidden, id, eligibleRole.RID, 0)
		check(eligible, id, eligibleRole.RID, 1)
	}
	for idx, player := range []*client{hidden, eligible} {
		player.send(22703, nil)
		for _, task := range player.recv(22703).Arr("feat_list") {
			if task.Int("id") == 3001 && task.Int("finish") != int64(idx) {
				t.Fatalf("H5 breakthrough task: %v", task)
			}
		}
	}
	env.setNow(end)
	for _, id := range []int64{4, 7} {
		check(native, id, nativeRole.RID, 1)
		check(hidden, id, eligibleRole.RID, 0)
		check(eligible, id, eligibleRole.RID, 1)
	}
	var world drWorld
	if _, err := env.st.LoadGlobal(nativeRole.SrvID, drModule, &world); err != nil {
		t.Fatal(err)
	}
	heroes, err := env.st.PartnersOf(eligibleRole.RID)
	if err != nil {
		t.Fatal(err)
	}
	strongest := partnerMessage(heroes[0]).Int("power")
	team := strongest + 4*partnerMessage(heroes[1]).Int("power")
	if world.Frozen[drH5PartnerKey][0].Val != strongest || world.Frozen[drH5PowerKey][0].Val != team {
		t.Fatalf("hero power mismatch: %v vs %d and %d", world.Frozen, strongest, team)
	}
}

func TestDaysRankFreezesWithoutOnlineRolesAndBreaksScoreTiesByRID(t *testing.T) {
	env := newGameEnv(t, time.Minute)
	start := dayStart(actNow)
	end := setDaysRankStart(t, env, start)
	seed := func(name string) *store.Role {
		t.Helper()
		account, _ := env.account(name)
		role, err := env.st.CreateRole(account.ID, env.login.SrvID, name, 1, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		return role
	}
	first, second := seed("先建角色"), seed("先到层数")
	for idx, role := range []*store.Role{second, first} {
		when := start.Add(time.Duration(idx) * time.Hour).Unix()
		if _, err := env.st.ArenaPlayerOf(role.RID, 1200, when); err != nil {
			t.Fatal(err)
		}
		if err := env.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: 30, ClearedAt: when}); err != nil {
			t.Fatal(err)
		}
	}
	watcher := newActivityWatcher(env.st, env.gw.Hub, start)
	watcher.tick(end.Add(-time.Second))
	var world drWorld
	if _, err := env.st.LoadGlobal(first.SrvID, drModule, &world); err != nil || world.Settled {
		t.Fatalf("settled too early: %v (%v)", world, err)
	}
	watcher.tick(end)
	if _, err := env.st.LoadGlobal(first.SrvID, drModule, &world); err != nil || !world.Settled {
		t.Fatalf("the timer did not settle an offline realm: %v (%v)", world, err)
	}
	for _, id := range []string{"2", "5"} {
		if rows := world.Frozen[id]; len(rows) != 2 || rows[0].RID != first.RID || rows[1].RID != second.RID {
			t.Fatalf("board %s should break ties by RID, not timestamp: %v", id, rows)
		}
	}
	if err := env.st.SaveStarTower(&store.StarTower{RID: second.RID, MaxTower: 50}); err != nil {
		t.Fatal(err)
	}
	newActivityWatcher(env.st, env.gw.Hub, end).tick(end.Add(time.Minute))
	for idx, role := range []*store.Role{first, second} {
		mails, err := env.st.MailsForRole(role)
		if err != nil {
			t.Fatal(err)
		}
		for _, subject := range []string{"开服冲榜·爬塔达人奖励", "开服冲榜·武道宗师奖励"} {
			count := 0
			for _, mail := range mails {
				if mail.Subject == subject {
					count++
					if !strings.Contains(mail.Content, "第"+itoa64(int64(idx+1))+"名") || len(mail.Attachments) == 0 {
						t.Fatalf("payout for %s: %v", role.Name, mail)
					}
				}
			}
			if count != 1 {
				t.Fatalf("%s received %d copies of %q", role.Name, count, subject)
			}
		}
	}
}

func TestDaysRankFreezesBeforePostDeadlineGameplay(t *testing.T) {
	env := newGameEnv(t, time.Minute)
	end := setDaysRankStart(t, env, dayStart(actNow))
	env.setNow(actNow)
	env.gw.Router.Handle(1169, func(session *gateway.Session, _ proto.Message) error {
		role := session.Role()
		if err := env.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: 40}); err != nil {
			return err
		}
		return heartbeat(session, nil)
	})
	env.connect = serveTestPipe(t, env.gw, false)
	player, role := env.enter("daysrank-deadline", "截止后爬塔")
	if err := env.st.SaveStarTower(&store.StarTower{RID: role.RID, MaxTower: 15}); err != nil {
		t.Fatal(err)
	}
	env.setNow(end)
	player.send(1169, nil)
	player.recv(1169)
	if progress, err := env.st.StarTowerOf(role.RID); err != nil || progress.MaxTower != 40 {
		t.Fatalf("ordinary gameplay after deadline failed: %v (%v)", progress, err)
	}
	var world drWorld
	if _, err := env.st.LoadGlobal(role.SrvID, drModule, &world); err != nil || !world.Settled || len(world.Frozen["2"]) != 1 || world.Frozen["2"][0].Val != 15 {
		t.Fatalf("settlement incorporated post-deadline progress: %v (%v)", world, err)
	}
	if err := env.st.SetRoleLevel(role.RID, 99); err != nil {
		t.Fatal(err)
	}
	player.send(22701, proto.Message{"id": 2})
	if rows := player.recv(22701).Arr("rank_list"); len(rows) != 1 || rows[0].Int("lev") != int64(role.Lev) {
		t.Fatalf("frozen detail changed after settlement: %v", rows)
	}
}

func TestDaysRankCampaignTieUsesTimeBeforeRID(t *testing.T) {
	firstRole := rankEntry{store.RankRole{RID: 1, DramaAt: 10}, 10300}
	secondRole := rankEntry{store.RankRole{RID: 2, DramaAt: 9}, 10300}
	if daysRankBefore(rankDrama, firstRole, secondRole) || !daysRankBefore(rankDrama, secondRole, firstRole) {
		t.Fatal("the first role to reach the dungeon must be ahead")
	}
	secondRole.role.DramaAt = firstRole.role.DramaAt
	if !daysRankBefore(rankDrama, firstRole, secondRole) || daysRankBefore(rankDrama, secondRole, firstRole) {
		t.Fatal("equal arrival times must sort by RID")
	}
}
